package board_test

import (
	"strings"
	"testing"
	"time"

	"github.com/kevin-jake/bills-bot/internal/board"
	"github.com/kevin-jake/bills-bot/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// september is a Cycle with one paid Bill, three unpaid ones with different due days, and
// an account that has had no Transfer.
func september() domain.Snapshot {
	return domain.Snapshot{
		Cycle:    domain.Cycle{ID: 1, Month: domain.Month{Year: 2026, Month: time.September}},
		Sections: []domain.Section{{ID: 1, Name: "BDO", DisplayOrder: 1}, {ID: 2, Name: "HSBC", DisplayOrder: 2}},
		Payables: []domain.Payable{
			{ID: 1, SectionID: 1, BillName: "BDO JCB CC", CardLast4: "5994", DueDay: 5,
				Channel: domain.SheenaBDO, AmountCents: cents(293083), Status: domain.StatusDue},
			{ID: 2, SectionID: 1, BillName: "BDO Home Loan", DueDay: 25,
				Channel: domain.SheenaBDO, AmountCents: cents(1663931), Status: domain.StatusPaid},
			{ID: 3, SectionID: 2, BillName: "HSBC Mastercard CC", CardLast4: "9361", DueDay: 24,
				Channel: domain.SheenaBPI, Status: domain.StatusDue},
			{ID: 4, SectionID: 2, BillName: "GCash funds",
				Channel: domain.KevinDirect, AmountCents: cents(100000), Status: domain.StatusDue},
		},
	}
}

func TestReminderListsWhatIsStillUnpaidSoonestFirst(t *testing.T) {
	text := board.Reminder([]domain.Snapshot{september()}, time.Date(2026, 9, 15, 8, 0, 0, 0, domain.Manila))

	lines := strings.Split(text, "\n")
	require.Len(t, lines, 7)
	assert.Equal(t, "⏰ <b>Bills still unpaid</b>", lines[0])
	assert.Equal(t, "", lines[1])
	assert.Equal(t, "<b>September 2026</b>", lines[2])
	assert.Equal(t, "☐ BDO JCB CC ••5994 · ₱2,930.83 · Sheena BDO · due 5 ⚠️ overdue", lines[3])
	assert.Equal(t, "? HSBC Mastercard CC ••9361 · — · Sheena BPI · due 24", lines[4],
		"a bill still owing an amount is on the list too")
	assert.Equal(t, "☐ GCash funds · ₱1,000.00 · Kevin", lines[5],
		"a bill with no fixed day comes last")
	assert.Equal(t, "<i>Transfers not sent: Sheena BDO, Sheena BPI</i>", lines[6])
	assert.NotContains(t, text, "BDO Home Loan", "what is paid is not nagged about")
}

func TestReminderMarksOnlyTheDaysAlreadyGoneBy(t *testing.T) {
	// On the 24th the HSBC card is due today, which is not late.
	text := board.Reminder([]domain.Snapshot{september()}, time.Date(2026, 9, 24, 8, 0, 0, 0, domain.Manila))

	assert.Contains(t, text, "· due 24\n", "due today is not overdue")
	assert.Contains(t, text, "· due 5 ⚠️ overdue")
}

func TestReminderNamesTheAccountsWithNoTransfer(t *testing.T) {
	snap := september()
	snap.Transfers = []domain.Transfer{{ID: 1, Channel: domain.SheenaBDO, SentCents: 300000}}

	text := board.Reminder([]domain.Snapshot{snap}, time.Date(2026, 9, 15, 8, 0, 0, 0, domain.Manila))

	assert.Contains(t, text, "<i>Transfers not sent: Sheena BPI</i>")
	assert.NotContains(t, text, "Sheena BDO</i>", "that one was sent")
}

func TestReminderCoversEveryOpenMonthOldestFirst(t *testing.T) {
	august := september()
	august.Cycle = domain.Cycle{ID: 2, Month: domain.Month{Year: 2026, Month: time.August}}

	text := board.Reminder([]domain.Snapshot{august, september()},
		time.Date(2026, 9, 15, 8, 0, 0, 0, domain.Manila))

	assert.Less(t, strings.Index(text, "<b>August 2026</b>"), strings.Index(text, "<b>September 2026</b>"),
		"the month people have stopped looking at comes first")
}

func TestReminderSaysNothingWhenThereIsNothingToSay(t *testing.T) {
	snap := september()
	for i := range snap.Payables {
		snap.Payables[i].Status = domain.StatusPaid
		snap.Payables[i].AmountCents = cents(1)
	}
	snap.Transfers = []domain.Transfer{
		{ID: 1, Channel: domain.SheenaBDO, SentCents: 1}, {ID: 2, Channel: domain.SheenaBPI, SentCents: 1}}

	assert.Empty(t, board.Reminder([]domain.Snapshot{snap}, time.Date(2026, 9, 15, 8, 0, 0, 0, domain.Manila)))
	assert.Empty(t, board.Reminder(nil, time.Date(2026, 9, 15, 8, 0, 0, 0, domain.Manila)))
}
