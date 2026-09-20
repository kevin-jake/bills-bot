package tracker_test

import (
	"testing"

	"github.com/kevin-jake/bills-bot/internal/domain"
	"github.com/kevin-jake/bills-bot/internal/storage"
	"github.com/kevin-jake/bills-bot/internal/storage/storagetest"
	"github.com/kevin-jake/bills-bot/internal/tracker"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// billNamed finds a seeded Bill by the name the sticky note gives it.
func billNamed(t *testing.T, tr *tracker.Tracker, name string) domain.Bill {
	t.Helper()

	bills, err := tr.AllBills()
	require.NoError(t, err)
	for _, bill := range bills {
		if bill.Name == name {
			return bill
		}
	}
	t.Fatalf("no bill called %q", name)
	return domain.Bill{}
}

// lineOf finds a Bill's line in a Snapshot.
func lineOf(t *testing.T, snap domain.Snapshot, billID int64) domain.Payable {
	t.Helper()

	for _, p := range snap.Payables {
		if p.BillID == billID {
			return p
		}
	}
	t.Fatalf("bill %d has no line in %s", billID, snap.Cycle.Month)
	return domain.Payable{}
}

// openSeptember opens the seeded month, which is what every propagation test changes.
func openSeptember(t *testing.T, tr *tracker.Tracker) domain.Snapshot {
	t.Helper()

	snap, opened, err := tr.OpenCycle(kevin, domain.Month{Year: 2026, Month: 9})
	require.NoError(t, err)
	require.True(t, opened)
	return snap
}

func TestRenameBillRenamesItInEveryMonth(t *testing.T) {
	tr, _ := newTracker(t)
	openSeptember(t, tr)
	pldt := billNamed(t, tr, "Internet PLDT")

	change, err := tr.RenameBill(kevin, pldt.ID, "  PLDT Fibre  ")

	require.NoError(t, err)
	assert.Equal(t, "Internet PLDT", change.Before.Name)
	assert.Equal(t, "PLDT Fibre", change.After.Name, "the name is trimmed")
	require.Len(t, change.Snaps, 1)
	assert.Equal(t, "PLDT Fibre", lineOf(t, change.Snaps[0], pldt.ID).BillName,
		"the name is read live, so the open month renames with it")
}

func TestRenameACardRepointsEverythingChargedToIt(t *testing.T) {
	tr, db := newTracker(t)
	openSeptember(t, tr)
	airmiles := billNamed(t, tr, "RCBC Visa Airmiles")

	change, err := tr.RenameBill(kevin, airmiles.ID, "RCBC Airmiles Visa")

	require.NoError(t, err)
	assert.Equal(t, 1, change.Charges, "Internet PLDT is the one bill charged to it")
	assert.Equal(t, "→ RCBC Airmiles Visa", billNamed(t, tr, "Internet PLDT").ChannelLabel(),
		"the standing list follows the rename")

	pldt := billNamed(t, tr, "Internet PLDT")
	assert.Equal(t, "→ RCBC Airmiles Visa", lineOf(t, change.Snaps[0], pldt.ID).ChannelLabel(),
		"so does the snapshot on the open month's payable")

	var stale int64
	require.NoError(t, db.Model(&storage.Payable{}).
		Where("card_name = ?", "RCBC Visa Airmiles").Count(&stale).Error)
	assert.Zero(t, stale, "no charge is left pointing at a card that is gone")
}

func TestRenameRefusesANameAlreadyOnTheList(t *testing.T) {
	tr, _ := newTracker(t)
	water := billNamed(t, tr, "Water")

	_, err := tr.RenameBill(kevin, water.ID, "batelec")

	assert.ErrorIs(t, err, tracker.ErrBillExists, "the match is case-insensitive, as the index is")
	assert.Equal(t, "Water", billNamed(t, tr, "Water").Name, "nothing was changed")
}

func TestRenameToItsOwnNameIsAllowed(t *testing.T) {
	tr, _ := newTracker(t)
	water := billNamed(t, tr, "Water")

	change, err := tr.RenameBill(kevin, water.ID, "WATER")

	require.NoError(t, err)
	assert.Equal(t, "WATER", change.After.Name, "a bill may be recapitalised")
	assert.Zero(t, change.Charges, "and nothing is repointed, since it is the same card")
}

func TestAliasesAreWrittenAndCleared(t *testing.T) {
	tr, _ := newTracker(t)
	pldt := billNamed(t, tr, "Internet PLDT")

	change, err := tr.SetBillAliases(kevin, pldt.ID, domain.ParseAliases("fibre, internet"))
	require.NoError(t, err)
	assert.Equal(t, []string{"fibre", "internet"}, change.After.Aliases)
	assert.Equal(t, []string{"fibre", "internet"}, billNamed(t, tr, "Internet PLDT").Aliases)

	change, err = tr.SetBillAliases(kevin, pldt.ID, nil)
	require.NoError(t, err)
	assert.Empty(t, change.After.Aliases, "an empty list takes the aliases back")
}

func TestMoveBillListsItUnderAnotherSectionLast(t *testing.T) {
	tr, _ := newTracker(t)
	openSeptember(t, tr)
	gcash := billNamed(t, tr, "GCash funds")

	change, err := tr.MoveBill(kevin, gcash.ID, "investment")

	require.NoError(t, err)
	assert.Equal(t, "Investment", change.Section, "the section is matched case-insensitively")

	list, err := tr.BillList()
	require.NoError(t, err)
	for _, section := range list {
		if section.Section.Name != "Investment" {
			continue
		}
		assert.Equal(t, "GCash funds", section.Bills[len(section.Bills)-1].Name,
			"it joins the end of its new section")
	}
	assert.Equal(t, change.After.SectionID, lineOf(t, change.Snaps[0], gcash.ID).SectionID,
		"the open month regroups it too")
}

func TestMoveBillRefusesASectionThatIsNotThere(t *testing.T) {
	tr, _ := newTracker(t)
	water := billNamed(t, tr, "Water")

	_, err := tr.MoveBill(kevin, water.ID, "Sundries")

	assert.ErrorIs(t, err, tracker.ErrSectionUnknown)
}

func TestChangingAChannelRefitsTheOpenMonthToItsTransfers(t *testing.T) {
	tr, _ := newTracker(t)
	snap := openSeptember(t, tr)
	bpiCC := billNamed(t, tr, "BPI Visa CC")
	water := billNamed(t, tr, "Water")

	// Money into BPI funds everything on that channel; Kevin's own bills are untouched.
	_, err := tr.RecordTransfer(kevin, snap.Cycle.ID, domain.SheenaBPI, 500_00)
	require.NoError(t, err)

	moved, err := tr.SetBillChannel(kevin, bpiCC.ID, domain.SheenaPSBank, "")
	require.NoError(t, err)
	assert.Equal(t, domain.StatusDue, lineOf(t, moved.Snaps[0], bpiCC.ID).Status,
		"moved to an account with no transfer, it goes back to due")
	assert.Equal(t, domain.SheenaPSBank, lineOf(t, moved.Snaps[0], bpiCC.ID).Channel)

	joined, err := tr.SetBillChannel(kevin, water.ID, domain.SheenaBPI, "")
	require.NoError(t, err)
	assert.Equal(t, domain.StatusFunded, lineOf(t, joined.Snaps[0], water.ID).Status,
		"moved onto an account already funded, it is funded")
}

func TestChangingAChannelLeavesWhatIsPaidAloneAndClosedMonthsUntouched(t *testing.T) {
	tr, db := newTracker(t)
	snap := openSeptember(t, tr)
	water := billNamed(t, tr, "Water")

	_, err := tr.SetAmount(kevin, lineOf(t, snap, water.ID).ID, 1_234_00)
	require.NoError(t, err)
	_, err = tr.MarkPaid(kevin, lineOf(t, snap, water.ID).ID)
	require.NoError(t, err)

	change, err := tr.SetBillChannel(kevin, water.ID, domain.SheenaBDO, "")
	require.NoError(t, err)

	after := lineOf(t, change.Snaps[0], water.ID)
	assert.Equal(t, domain.StatusPaid, after.Status, "a paid line stays paid")
	assert.Equal(t, domain.SheenaBDO, after.Channel,
		"but it does follow the bill, since the month is still open and being worked on")

	// A closed month records what actually paid it, so it keeps the old channel.
	require.NoError(t, db.Exec(`UPDATE cycles SET closed_at = opened_at WHERE id = ?`, snap.Cycle.ID).Error)
	_, err = tr.SetBillChannel(kevin, water.ID, domain.KevinDirect, "")
	require.NoError(t, err)

	closed, err := tr.Snapshot(snap.Cycle.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.SheenaBDO, lineOf(t, closed, water.ID).Channel,
		"a settled month is left exactly as it was")
}

func TestChangingAChannelToACardNeedsTheCardsName(t *testing.T) {
	tr, _ := newTracker(t)
	water := billNamed(t, tr, "Water")

	_, err := tr.SetBillChannel(kevin, water.ID, domain.ChargedToCard, "")
	assert.ErrorIs(t, err, domain.ErrCardNameRequired)

	_, err = tr.SetBillChannel(kevin, water.ID, domain.KevinDirect, "BPI Visa CC")
	assert.ErrorIs(t, err, domain.ErrCardNameUnwanted)

	change, err := tr.SetBillChannel(kevin, water.ID, domain.ChargedToCard, "BPI Visa CC")
	require.NoError(t, err)
	assert.Equal(t, "→ BPI Visa CC", change.After.ChannelLabel())
}

func TestCardDigitsAndDueDaysAreWrittenAndCleared(t *testing.T) {
	tr, _ := newTracker(t)
	bpiCC := billNamed(t, tr, "BPI Visa CC")

	change, err := tr.SetCardLast4(kevin, bpiCC.ID, "4321")
	require.NoError(t, err)
	assert.Equal(t, "BPI Visa CC ••4321", change.After.DisplayName())

	change, err = tr.SetCardLast4(kevin, bpiCC.ID, "")
	require.NoError(t, err)
	assert.Equal(t, "BPI Visa CC", change.After.DisplayName())

	_, err = tr.SetCardLast4(kevin, bpiCC.ID, "12")
	assert.ErrorIs(t, err, domain.ErrLast4Invalid)

	change, err = tr.SetDueDay(kevin, bpiCC.ID, 14)
	require.NoError(t, err)
	assert.Equal(t, "due 14", change.After.DueLabel())

	change, err = tr.SetDueDay(kevin, bpiCC.ID, 0)
	require.NoError(t, err)
	assert.Empty(t, change.After.DueLabel(), "0 means no fixed day")

	_, err = tr.SetDueDay(kevin, bpiCC.ID, 32)
	assert.ErrorIs(t, err, domain.ErrDueDayInvalid)
}

func TestReorderMovesABillWithinItsSectionAndRenumbersIt(t *testing.T) {
	tr, _ := newTracker(t)
	openSeptember(t, tr)
	homeLoan := billNamed(t, tr, "BDO Home Loan")

	change, err := tr.ReorderBill(kevin, homeLoan.ID, domain.Placement{Position: 1})

	require.NoError(t, err)
	assert.Equal(t, 1, change.After.DisplayOrder)
	assert.Equal(t, []string{"BDO Home Loan", "BDO JCB CC", "BDO Unionpay CC"},
		sectionNames(t, tr, "BDO"), "the rest slide down")

	_, err = tr.ReorderBill(kevin, homeLoan.ID, domain.Placement{Step: 1})
	require.NoError(t, err)
	assert.Equal(t, []string{"BDO JCB CC", "BDO Home Loan", "BDO Unionpay CC"},
		sectionNames(t, tr, "BDO"), "one step down swaps it with the next")

	_, err = tr.ReorderBill(kevin, homeLoan.ID, domain.Placement{Position: 99})
	require.NoError(t, err)
	assert.Equal(t, []string{"BDO JCB CC", "BDO Unionpay CC", "BDO Home Loan"},
		sectionNames(t, tr, "BDO"), "a position past the end is the end")
}

func TestReorderShowsOnTheBoardInEveryOpenMonth(t *testing.T) {
	tr, _ := newTracker(t)
	openSeptember(t, tr)
	homeLoan := billNamed(t, tr, "BDO Home Loan")

	change, err := tr.ReorderBill(kevin, homeLoan.ID, domain.Placement{Position: 1})

	require.NoError(t, err)
	require.Len(t, change.Snaps, 1)
	var bdo []string
	for _, group := range change.Snaps[0].SectionGroups() {
		if group.Section.Name != "BDO" {
			continue
		}
		for _, p := range group.Payables {
			bdo = append(bdo, p.BillName)
		}
	}
	assert.Equal(t, []string{"BDO Home Loan", "BDO JCB CC", "BDO Unionpay CC"}, bdo)
}

func TestReorderSectionMovesAHeadingForEveryMonth(t *testing.T) {
	tr, _ := newTracker(t)
	openSeptember(t, tr)

	change, err := tr.ReorderSection(kevin, "utilities", domain.Placement{Position: 1})

	require.NoError(t, err)
	assert.Equal(t, "Utilities", change.Section.Name)
	assert.Equal(t, 1, change.Section.DisplayOrder)
	require.Len(t, change.Sections, 9)
	assert.Equal(t, "Utilities", change.Sections[0].Name)
	assert.Equal(t, "UnionBank", change.Sections[1].Name, "the rest slide down")

	require.Len(t, change.Snaps, 1)
	assert.Equal(t, "Utilities", change.Snaps[0].SectionGroups()[0].Section.Name,
		"the open month's board regroups at once")
}

func TestReorderSectionRefusesAHeadingThatIsNotThere(t *testing.T) {
	tr, _ := newTracker(t)

	_, err := tr.ReorderSection(kevin, "Sundries", domain.Placement{Step: -1})

	assert.ErrorIs(t, err, tracker.ErrSectionUnknown)
}

func TestArchiveTakesTheUnpaidLinesOffTheOpenBoardAndKeepsThePaidOnes(t *testing.T) {
	tr, _ := newTracker(t)
	snap := openSeptember(t, tr)
	water := billNamed(t, tr, "Water")
	batelec := billNamed(t, tr, "Batelec")

	_, err := tr.SetAmount(kevin, lineOf(t, snap, water.ID).ID, 1_234_00)
	require.NoError(t, err)
	_, err = tr.MarkPaid(kevin, lineOf(t, snap, water.ID).ID)
	require.NoError(t, err)

	paid, err := tr.ArchiveBill(kevin, water.ID)
	require.NoError(t, err)
	assert.Zero(t, paid.Lines, "a paid line is a record of the month and stays")
	assert.Equal(t, domain.StatusPaid, lineOf(t, paid.Snaps[0], water.ID).Status)

	unpaid, err := tr.ArchiveBill(kevin, batelec.ID)
	require.NoError(t, err)
	assert.Equal(t, -1, unpaid.Lines, "an unpaid line leaves the board")
	for _, p := range unpaid.Snaps[0].Payables {
		assert.NotEqual(t, batelec.ID, p.BillID, "Batelec is off the september board")
	}
	assert.True(t, billNamed(t, tr, "Batelec").Archived)

	list, err := tr.BillList()
	require.NoError(t, err)
	for _, section := range list {
		for _, bill := range section.Bills {
			assert.NotEqual(t, "Batelec", bill.Name, "an archived bill is off the standing list")
		}
	}
}

func TestArchiveKeepsTheHistoryOfTheMonthsItWasPaidIn(t *testing.T) {
	tr, _ := newTracker(t)
	snap := openSeptember(t, tr)
	water := billNamed(t, tr, "Water")

	_, err := tr.SetAmount(kevin, lineOf(t, snap, water.ID).ID, 1_234_00)
	require.NoError(t, err)
	_, err = tr.MarkPaid(kevin, lineOf(t, snap, water.ID).ID)
	require.NoError(t, err)
	_, err = tr.ArchiveBill(kevin, water.ID)
	require.NoError(t, err)

	history, err := tr.History(water.ID, 12)

	require.NoError(t, err)
	require.Len(t, history.Entries, 1)
	assert.Equal(t, domain.Month{Year: 2026, Month: 9}, history.Entries[0].Month)
	assert.Equal(t, "Kevin", history.Entries[0].PaidBy)
}

func TestArchivingTheLastUnpaidBillClosesTheMonth(t *testing.T) {
	tr := tracker.New(storagetest.OpenEmpty(t))
	_, err := tr.AddSection(kevin, "Utilities")
	require.NoError(t, err)
	settled, err := tr.AddBill(kevin, tracker.BillSpec{
		Name: "Water", Section: "Utilities", Channel: domain.KevinDirect})
	require.NoError(t, err)
	going, err := tr.AddBill(kevin, tracker.BillSpec{
		Name: "Batelec", Section: "Utilities", Channel: domain.KevinDirect})
	require.NoError(t, err)

	snap := openSeptember(t, tr)
	_, err = tr.SetAmount(kevin, lineOf(t, snap, settled.ID).ID, 0)
	require.NoError(t, err)

	change, err := tr.ArchiveBill(kevin, going.ID)

	require.NoError(t, err)
	require.Len(t, change.Closed, 1, "nothing is left owing, so the month is settled")
	assert.Equal(t, domain.Month{Year: 2026, Month: 9}, change.Closed[0].Cycle.Month)
	assert.True(t, change.Snaps[0].Cycle.Closed())
}

func TestAnArchivedBillIsKeptRatherThanEdited(t *testing.T) {
	tr, _ := newTracker(t)
	water := billNamed(t, tr, "Water")
	_, err := tr.ArchiveBill(kevin, water.ID)
	require.NoError(t, err)

	_, err = tr.RenameBill(kevin, water.ID, "Maynilad")
	assert.ErrorIs(t, err, tracker.ErrBillArchived)

	_, err = tr.SetBillChannel(kevin, water.ID, domain.SheenaBDO, "")
	assert.ErrorIs(t, err, tracker.ErrBillArchived)

	_, err = tr.ArchiveBill(kevin, water.ID)
	assert.ErrorIs(t, err, tracker.ErrBillArchived, "archiving twice is not a change")
}

func TestRestorePutsABillBackWithABlankLineInEveryOpenMonth(t *testing.T) {
	tr, _ := newTracker(t)
	snap := openSeptember(t, tr)
	batelec := billNamed(t, tr, "Batelec")

	_, err := tr.SetAmount(kevin, lineOf(t, snap, batelec.ID).ID, 999_00)
	require.NoError(t, err)
	_, err = tr.ArchiveBill(kevin, batelec.ID)
	require.NoError(t, err)

	change, err := tr.RestoreBill(kevin, batelec.ID)

	require.NoError(t, err)
	assert.Equal(t, 1, change.Lines)
	assert.False(t, change.After.Archived)
	assert.Equal(t, "Utilities", change.Section)

	back := lineOf(t, change.Snaps[0], batelec.ID)
	assert.Equal(t, domain.StatusDue, back.Status)
	require.True(t, back.AmountKnown(), "the line was withdrawn, not deleted")
	assert.EqualValues(t, 999_00, *back.AmountCents, "so its figure comes back with it")
}

func TestArchivingHidesTheLineFromTheHistoryUntilItIsRestored(t *testing.T) {
	tr, _ := newTracker(t)
	snap := openSeptember(t, tr)
	batelec := billNamed(t, tr, "Batelec")

	_, err := tr.SetAmount(kevin, lineOf(t, snap, batelec.ID).ID, 999_00)
	require.NoError(t, err)
	_, err = tr.ArchiveBill(kevin, batelec.ID)
	require.NoError(t, err)

	history, err := tr.History(batelec.ID, 12)
	require.NoError(t, err)
	assert.Empty(t, history.Entries, "a month it was withdrawn from owes nothing to show")

	_, err = tr.RestoreBill(kevin, batelec.ID)
	require.NoError(t, err)

	history, err = tr.History(batelec.ID, 12)
	require.NoError(t, err)
	assert.Len(t, history.Entries, 1, "and has it again once the bill is back")
}

func TestRestoringWhatClosedAMonthOpensItAgain(t *testing.T) {
	tr := tracker.New(storagetest.OpenEmpty(t))
	_, err := tr.AddSection(kevin, "Utilities")
	require.NoError(t, err)
	settled, err := tr.AddBill(kevin, tracker.BillSpec{
		Name: "Water", Section: "Utilities", Channel: domain.KevinDirect})
	require.NoError(t, err)
	going, err := tr.AddBill(kevin, tracker.BillSpec{
		Name: "Batelec", Section: "Utilities", Channel: domain.KevinDirect})
	require.NoError(t, err)

	snap := openSeptember(t, tr)
	_, err = tr.SetAmount(kevin, lineOf(t, snap, settled.ID).ID, 0)
	require.NoError(t, err)
	closed, err := tr.ArchiveBill(kevin, going.ID)
	require.NoError(t, err)
	require.Len(t, closed.Closed, 1)

	change, err := tr.RestoreBill(kevin, going.ID)

	require.NoError(t, err)
	require.Len(t, change.Snaps, 1, "and its board is redrawn")
	require.Len(t, change.Reopened, 1, "September owes something again")
	assert.Equal(t, domain.Month{Year: 2026, Month: 9}, change.Reopened[0].Cycle.Month)
	assert.False(t, change.Reopened[0].Cycle.Closed())
}

func TestRestoreRefusesWhenTheNameHasBeenTakenBack(t *testing.T) {
	tr, _ := newTracker(t)
	water := billNamed(t, tr, "Water")
	_, err := tr.ArchiveBill(kevin, water.ID)
	require.NoError(t, err)
	_, err = tr.AddBill(kevin, tracker.BillSpec{
		Name: "water", Section: "Utilities", Channel: domain.KevinDirect})
	require.NoError(t, err)

	_, err = tr.RestoreBill(kevin, water.ID)

	assert.ErrorIs(t, err, tracker.ErrBillExists)
}

func TestRestoreRefusesABillThatNeverLeft(t *testing.T) {
	tr, _ := newTracker(t)

	_, err := tr.RestoreBill(kevin, billNamed(t, tr, "Water").ID)

	assert.ErrorIs(t, err, tracker.ErrBillActive)
}

func TestAChangeLeavesASettledMonthsBoardAlone(t *testing.T) {
	tr, db := newTracker(t)
	snap := openSeptember(t, tr)
	require.NoError(t, db.Exec(`UPDATE cycles SET closed_at = opened_at WHERE id = ?`, snap.Cycle.ID).Error)

	change, err := tr.RenameBill(kevin, billNamed(t, tr, "Water").ID, "Maynilad")

	require.NoError(t, err)
	assert.Empty(t, change.Snaps,
		"a settled month's board is a record; Telegram will not let a bot edit one that old anyway")
}

func TestAChangeToTheListIsRefusedForABillThatIsNotThere(t *testing.T) {
	tr, _ := newTracker(t)

	_, err := tr.RenameBill(kevin, 9999, "Anything")

	assert.ErrorIs(t, err, tracker.ErrBillUnknown)
}

func TestEveryChangeToTheListLeavesItsOwnEvent(t *testing.T) {
	tr, db := newTracker(t)
	openSeptember(t, tr)
	water := billNamed(t, tr, "Water")

	_, err := tr.RenameBill(kevin, water.ID, "Maynilad")
	require.NoError(t, err)
	_, err = tr.MoveBill(kevin, water.ID, "Bahay")
	require.NoError(t, err)
	_, err = tr.SetBillChannel(kevin, water.ID, domain.SheenaBDO, "")
	require.NoError(t, err)
	_, err = tr.ArchiveBill(kevin, water.ID)
	require.NoError(t, err)

	assert.Equal(t, []string{
		"bill.rename", "bill.move_section", "bill.repoint", "bill.change_channel",
		"bill.unlist", "bill.archive",
	}, actionsFor(t, db, water.ID))

	_, err = tr.RestoreBill(kevin, water.ID)
	require.NoError(t, err)
	assert.Equal(t, []string{"bill.relist", "bill.restore"},
		actionsFor(t, db, water.ID)[6:], "and putting it back leaves its own")
}

// actionsFor lists the audit trail left about one Bill, oldest first.
func actionsFor(t *testing.T, db *gorm.DB, billID int64) []string {
	t.Helper()

	var events []storage.Event
	require.NoError(t, db.Where("bill_id = ? AND action != 'cycle.open'", billID).
		Order("id").Find(&events).Error)

	actions := make([]string, 0, len(events))
	for _, e := range events {
		actions = append(actions, e.Action)
	}
	return actions
}

// sectionNames lists the active Bills under one heading, in display order.
func sectionNames(t *testing.T, tr *tracker.Tracker, section string) []string {
	t.Helper()

	list, err := tr.BillList()
	require.NoError(t, err)
	for _, group := range list {
		if group.Section.Name != section {
			continue
		}
		names := make([]string, 0, len(group.Bills))
		for _, bill := range group.Bills {
			names = append(names, bill.Name)
		}
		return names
	}
	t.Fatalf("no section called %q", section)
	return nil
}
