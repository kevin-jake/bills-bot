package tracker

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/kevin-jake/bills-bot/internal/domain"
	"github.com/kevin-jake/bills-bot/internal/storage"
	"gorm.io/gorm"
)

var (
	// ErrBillArchived means a change was asked for on a Bill that has left the standing
	// list. Archived Bills are kept for their history, not edited.
	ErrBillArchived = errors.New("that bill has been archived")
	// ErrBillActive means a restore was asked for on a Bill that never left the list.
	ErrBillActive = errors.New("that bill is already on the list")
)

// BillChange is what a change to the standing list did: the Bill before and after, and
// every open Cycle as it now stands, so that the Boards can be brought up to date.
type BillChange struct {
	Before domain.Bill
	After  domain.Bill
	// Section is the heading the Bill is under now, named for the sentence that confirms
	// the change.
	Section string
	// Snaps are the open Cycles after the change, oldest month first.
	Snaps []domain.Snapshot
	// Closed are the Cycles this change closed, which withdrawing the last unpaid line
	// can do, and Reopened the ones it put back to owing, which restoring can. Both are
	// also in Snaps; these name them so the group can be told.
	Closed   []domain.Snapshot
	Reopened []domain.Snapshot
	// Lines is how many Board lines the change put back, or withdrew as a negative number.
	Lines int
	// Charges is how many Bills are charged to this card and were repointed by a rename.
	Charges int
}

// RenameBill gives a Bill a new name. The name is read live wherever a Payable is shown,
// so every month renames with it — including the months already settled, which is what
// the household wants: the thing being paid did not change, only what it is called.
func (t *Tracker) RenameBill(actor Actor, billID int64, name string) (BillChange, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return BillChange{}, domain.ErrNameRequired
	}

	return t.changeBill(actor, billID, "bill.rename",
		func(tx *gorm.DB, row *storage.Bill, change *BillChange) error {
			taken, err := storage.FindActiveBillByName(tx, name)
			if err != nil {
				return err
			}
			if taken != nil && taken.ID != row.ID {
				return fmt.Errorf("%w: %s", ErrBillExists, taken.Name)
			}

			was := row.Name
			row.Name = name
			if strings.EqualFold(was, name) {
				return nil
			}
			// A Bill charged to a card names that card by name, in the standing list and
			// in every Payable snapshot, so a rename has to reach both or the charge
			// points at a card that is gone.
			change.Charges, err = storage.RenameCardName(tx, was, name)
			return err
		})
}

// SetBillAliases records the other names a Bill answers to, which is what lets a typed
// shortcut find it by whatever it is actually called in the house. An empty list clears
// them.
func (t *Tracker) SetBillAliases(actor Actor, billID int64, aliases []string) (BillChange, error) {
	return t.changeBill(actor, billID, "bill.alias",
		func(_ *gorm.DB, row *storage.Bill, _ *BillChange) error {
			row.Aliases = domain.JoinAliases(aliases)
			return nil
		})
}

// MoveBill lists a Bill under a different Section, at the end of it. The Section is read
// live like the name, so the Board regroups the Bill in every month at once.
func (t *Tracker) MoveBill(actor Actor, billID int64, sectionName string) (BillChange, error) {
	return t.changeBill(actor, billID, "bill.move_section",
		func(tx *gorm.DB, row *storage.Bill, _ *BillChange) error {
			section, err := storage.FindSectionByName(tx, sectionName)
			if err != nil {
				return err
			}
			if section == nil {
				return fmt.Errorf("%w: %s", ErrSectionUnknown, strings.TrimSpace(sectionName))
			}
			if section.ID == row.SectionID {
				return nil
			}
			order, err := storage.NextBillOrder(tx, section.ID)
			if err != nil {
				return err
			}
			row.SectionID, row.DisplayOrder = section.ID, order
			return nil
		})
}

// SetBillChannel changes who pays a Bill and out of which account. Unlike the name, the
// channel is a snapshot on each Payable, because which account settled a bill last March
// is a fact about March; so only the open months follow the change, and each of their
// Payables is refitted to the Transfers of the account it has just moved to. Anything
// already paid keeps its channel and stays paid.
func (t *Tracker) SetBillChannel(actor Actor, billID int64, channel domain.Channel, cardName string) (BillChange, error) {
	cardName = strings.TrimSpace(cardName)

	return t.changeBill(actor, billID, "bill.change_channel",
		func(tx *gorm.DB, row *storage.Bill, _ *BillChange) error {
			if err := domain.ValidateBill(row.Name, channel, cardName); err != nil {
				return err
			}
			row.Channel = string(channel)
			row.CardName = nil
			if cardName != "" {
				row.CardName = &cardName
			}
			return repointPayables(tx, actor, row)
		})
}

// repointPayables moves a Bill's unsettled Payables onto the Bill's new channel. The
// events it leaves are not payable.* events, so a bill's Undo passes over them: Undo takes
// back what someone did to that bill, not where the household decided to pay it from.
func repointPayables(tx *gorm.DB, actor Actor, row *storage.Bill) error {
	payables, err := storage.OpenPayablesForBill(tx, row.ID)
	if err != nil {
		return err
	}
	channel := domain.Channel(row.Channel)

	for _, p := range payables {
		before := p
		if domain.Status(p.Status) != domain.StatusPaid {
			transfer, err := storage.FindTransfer(tx, p.CycleID, row.Channel)
			if err != nil {
				return err
			}
			p.Status = string(domain.UnpaidStatus(channel, transfer != nil))
		}
		p.Channel, p.CardName = row.Channel, row.CardName
		if err := storage.SetPayableChannel(tx, p.ID, p.Channel, p.CardName, p.Status); err != nil {
			return err
		}
		if err := appendEvent(tx, actor, "bill.repoint", event{
			before:    channelState(before),
			after:     channelState(p),
			cycleID:   &p.CycleID,
			billID:    &p.BillID,
			payableID: &p.ID,
		}); err != nil {
			return err
		}
	}
	return nil
}

// channelState is what a bill.repoint event records: where a Payable is paid from and
// where that leaves it.
func channelState(p storage.Payable) map[string]any {
	return map[string]any{"channel": p.Channel, "card_name": p.CardName, "status": p.Status}
}

// SetCardLast4 records the four digits printed on a card, or clears them with "". They are
// read live wherever the Bill is shown, so a card reissued with new digits shows them on
// past months too.
func (t *Tracker) SetCardLast4(actor Actor, billID int64, last4 string) (BillChange, error) {
	last4 = strings.TrimSpace(last4)
	if err := domain.ValidateCardLast4(last4); err != nil {
		return BillChange{}, err
	}

	return t.changeBill(actor, billID, "bill.card_last4",
		func(_ *gorm.DB, row *storage.Bill, _ *BillChange) error {
			row.CardLast4 = nil
			if last4 != "" {
				row.CardLast4 = &last4
			}
			return nil
		})
}

// SetDueDay records the day of the month a Bill falls due, or clears it with 0.
func (t *Tracker) SetDueDay(actor Actor, billID int64, day int) (BillChange, error) {
	if err := domain.ValidateDueDay(day); err != nil {
		return BillChange{}, err
	}

	return t.changeBill(actor, billID, "bill.due_day",
		func(_ *gorm.DB, row *storage.Bill, _ *BillChange) error {
			row.DueDay = nil
			if day != 0 {
				row.DueDay = &day
			}
			return nil
		})
}

// ReorderBill moves a Bill up or down within its Section, renumbering the Section so that
// the order has no gaps left to trip over. The Board reads the order live, so every month
// shifts at once.
func (t *Tracker) ReorderBill(actor Actor, billID int64, to domain.Placement) (BillChange, error) {
	return t.changeBill(actor, billID, "bill.reorder",
		func(tx *gorm.DB, row *storage.Bill, _ *BillChange) error {
			listed, err := storage.ListSectionBills(tx, row.SectionID)
			if err != nil {
				return err
			}
			at := indexOfBill(listed, row.ID)
			if at < 0 {
				return nil
			}

			for i, bill := range domain.Reorder(listed, at, to.Index(at, len(listed))) {
				order := i + 1
				if bill.ID == row.ID {
					row.DisplayOrder = order
					continue
				}
				if bill.DisplayOrder == order {
					continue
				}
				if err := storage.SetBillOrder(tx, bill.ID, order); err != nil {
					return err
				}
			}
			return nil
		})
}

func indexOfBill(bills []storage.Bill, id int64) int {
	for i, bill := range bills {
		if bill.ID == id {
			return i
		}
	}
	return -1
}

// ArchiveBill takes a Bill off the standing list. Its unpaid lines are withdrawn from the
// open months, because a bill nobody owes should not sit on a Board waiting for a figure;
// the months it was paid in keep it exactly as they recorded it, and so does its history.
// Withdrawing the last unpaid line settles that month, which is why the Cycles are
// re-settled afterwards.
//
// A withdrawn line is hidden rather than deleted. The row is what the audit trail hangs
// off, and keeping it is what lets a restore hand back the figure that was already
// entered rather than ask for it again.
func (t *Tracker) ArchiveBill(actor Actor, billID int64) (BillChange, error) {
	return t.changeBill(actor, billID, "bill.archive",
		func(tx *gorm.DB, row *storage.Bill, change *BillChange) error {
			withdrawn, err := unpaidOpenPayables(tx, row.ID)
			if err != nil {
				return err
			}
			for _, p := range withdrawn {
				if err := appendEvent(tx, actor, "bill.unlist", event{
					before:    stateOf(toDomainPayable(storage.PayableLine{Payable: p, BillName: row.Name})),
					cycleID:   &p.CycleID,
					billID:    &p.BillID,
					payableID: &p.ID,
					note:      row.Name,
				}); err != nil {
					return err
				}
				change.Lines--
			}

			archivedAt := time.Now().UTC()
			row.ArchivedAt = &archivedAt
			return nil
		})
}

// RestoreBill puts an archived Bill back on the standing list. The lines archiving
// withdrew come back as they were, figures and all, and any open month that has none —
// one opened while the Bill was away — gets a blank one, exactly as adding it afresh
// would. A month that closed only because its line was withdrawn owes something again,
// so it is reopened.
func (t *Tracker) RestoreBill(actor Actor, billID int64) (BillChange, error) {
	return t.changeBill(actor, billID, "bill.restore",
		func(tx *gorm.DB, row *storage.Bill, change *BillChange) error {
			taken, err := storage.FindActiveBillByName(tx, row.Name)
			if err != nil {
				return err
			}
			if taken != nil {
				return fmt.Errorf("%w: %s", ErrBillExists, taken.Name)
			}
			row.ArchivedAt = nil

			back, err := unpaidOpenPayables(tx, row.ID)
			if err != nil {
				return err
			}
			for _, p := range back {
				if err := appendEvent(tx, actor, "bill.relist", event{
					after:     stateOf(toDomainPayable(storage.PayableLine{Payable: p, BillName: row.Name})),
					cycleID:   &p.CycleID,
					billID:    &p.BillID,
					payableID: &p.ID,
					note:      row.Name,
				}); err != nil {
					return err
				}
				change.Lines++
			}

			cycles, err := storage.ListOpenCycles(tx)
			if err != nil {
				return err
			}
			for _, cycle := range cycles {
				existing, err := storage.FindPayableForBill(tx, cycle.ID, row.ID)
				if err != nil {
					return err
				}
				if existing != nil {
					continue
				}
				if err := createDuePayable(tx, cycle.ID, *row); err != nil {
					return err
				}
				change.Lines++
			}
			return nil
		})
}

// unpaidOpenPayables are a Bill's lines in months still open that nobody has settled:
// the ones archiving withdraws and restoring hands back.
func unpaidOpenPayables(tx *gorm.DB, billID int64) ([]storage.Payable, error) {
	payables, err := storage.OpenPayablesForBill(tx, billID)
	if err != nil {
		return nil, err
	}
	unpaid := make([]storage.Payable, 0, len(payables))
	for _, p := range payables {
		if domain.Status(p.Status) != domain.StatusPaid {
			unpaid = append(unpaid, p)
		}
	}
	return unpaid, nil
}

// changeBill is the shape every change to the standing list shares: read the Bill, apply
// the change and whatever it reaches into, write it, leave an event, then settle the open
// Cycles, since a change to the list can be the thing that finishes a month.
//
// An archived Bill is refused, apart from the restore that brings it back: it is kept for
// what it says about the months it was paid in, and editing it would rewrite those.
func (t *Tracker) changeBill(actor Actor, billID int64, action string,
	apply func(tx *gorm.DB, row *storage.Bill, change *BillChange) error) (BillChange, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	var change BillChange
	err := t.db.Transaction(func(tx *gorm.DB) error {
		row, err := storage.FindBill(tx, billID)
		if err != nil {
			return err
		}
		if row == nil {
			return fmt.Errorf("%w: %d", ErrBillUnknown, billID)
		}
		switch {
		case row.Archived() && action != "bill.restore":
			return fmt.Errorf("%w: %s", ErrBillArchived, row.Name)
		case !row.Archived() && action == "bill.restore":
			return fmt.Errorf("%w: %s", ErrBillActive, row.Name)
		}
		change.Before = toDomainBill(*row)

		if err := apply(tx, row, &change); err != nil {
			return err
		}
		if err := storage.UpdateBill(tx, row); err != nil {
			return err
		}
		change.After = toDomainBill(*row)

		section, err := storage.FindSection(tx, row.SectionID)
		if err != nil {
			return err
		}
		if section != nil {
			change.Section = section.Name
		}

		if err := appendEvent(tx, actor, action, event{
			before: change.Before, after: change.After, billID: &row.ID,
		}); err != nil {
			return err
		}
		// Only a restore can change what an already closed month owes, by handing back a
		// line that was withdrawn from it. Every other change reaches the open months
		// alone, so there is nothing to re-settle beyond them.
		var held []int64
		if action == "bill.restore" {
			if held, err = storage.CyclesWithBill(tx, row.ID); err != nil {
				return err
			}
		}
		return settleCycles(tx, actor, &change, held)
	})
	if err != nil {
		return BillChange{}, err
	}
	return change, nil
}

// settleCycles reads back every Cycle a change to the standing list could have finished
// or unfinished — the open ones, and any closed month the Bill still has a line in — and
// closes or reopens each to match what is now owed in it.
func settleCycles(tx *gorm.DB, actor Actor, change *BillChange, also []int64) error {
	open, err := storage.ListOpenCycles(tx)
	if err != nil {
		return err
	}

	ids := make([]int64, 0, len(open)+len(also))
	seen := make(map[int64]bool, len(open)+len(also))
	for _, cycle := range append(open, cyclesByID(also)...) {
		if !seen[cycle.ID] {
			seen[cycle.ID] = true
			ids = append(ids, cycle.ID)
		}
	}

	for _, id := range ids {
		snap, closed, reopened, err := settleCycle(tx, actor, id)
		if err != nil {
			return err
		}
		switch {
		case closed:
			change.Closed = append(change.Closed, snap)
		case reopened:
			change.Reopened = append(change.Reopened, snap)
		}
		// A month that was already closed and stayed closed is left out: its Board is a
		// record of a settled month, and Telegram will not let a bot edit a message that
		// old anyway. One that has just closed is in, because its Board has to say so.
		if !snap.Cycle.Closed() || closed {
			change.Snaps = append(change.Snaps, snap)
		}
	}
	return nil
}

// cyclesByID is the plumbing that lets a list of ids be appended to a list of Cycles.
func cyclesByID(ids []int64) []storage.Cycle {
	cycles := make([]storage.Cycle, 0, len(ids))
	for _, id := range ids {
		cycles = append(cycles, storage.Cycle{ID: id})
	}
	return cycles
}

// SectionChange is what a reorder of the headings did: the Sections in their new running
// order, and the open Cycles, whose Boards now group in that order.
type SectionChange struct {
	Section  domain.Section
	Sections []domain.Section
	Snaps    []domain.Snapshot
}

// ReorderSection moves a heading in the Board's running order, renumbering the lot so the
// order has no gaps. The Board reads the order live, so every month regroups at once.
func (t *Tracker) ReorderSection(actor Actor, name string, to domain.Placement) (SectionChange, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	var change SectionChange
	err := t.db.Transaction(func(tx *gorm.DB) error {
		section, err := storage.FindSectionByName(tx, name)
		if err != nil {
			return err
		}
		if section == nil {
			return fmt.Errorf("%w: %s", ErrSectionUnknown, strings.TrimSpace(name))
		}
		listed, err := storage.ListSections(tx)
		if err != nil {
			return err
		}
		at := indexOfSection(listed, section.ID)
		if at < 0 {
			return nil
		}

		before := toDomainSection(*section)
		for i, row := range domain.Reorder(listed, at, to.Index(at, len(listed))) {
			order := i + 1
			if row.DisplayOrder != order {
				if err := storage.SetSectionOrder(tx, row.ID, order); err != nil {
					return err
				}
			}
			row.DisplayOrder = order
			change.Sections = append(change.Sections, toDomainSection(row))
			if row.ID == section.ID {
				change.Section = toDomainSection(row)
			}
		}

		if err := appendEvent(tx, actor, "section.reorder", event{
			before: before, after: change.Section, note: section.Name,
		}); err != nil {
			return err
		}

		cycles, err := storage.ListOpenCycles(tx)
		if err != nil {
			return err
		}
		for _, cycle := range cycles {
			snap, err := readSnapshot(tx, cycle)
			if err != nil {
				return err
			}
			change.Snaps = append(change.Snaps, snap)
		}
		return nil
	})
	if err != nil {
		return SectionChange{}, err
	}
	return change, nil
}

func indexOfSection(sections []storage.Section, id int64) int {
	for i, section := range sections {
		if section.ID == id {
			return i
		}
	}
	return -1
}
