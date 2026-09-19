package tracker

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/kevin-jake/bills-bot/internal/domain"
	"github.com/kevin-jake/bills-bot/internal/storage"
	"gorm.io/gorm"
)

var (
	// ErrPayableUnknown means a button or reply named a Payable that no longer exists.
	ErrPayableUnknown = errors.New("that bill is no longer on the board")
	// ErrCycleClosed means the Payable's month has been settled and put away.
	ErrCycleClosed = errors.New("that month is closed")
	// ErrNothingToUndo means nobody has done anything to the Payable yet.
	ErrNothingToUndo = errors.New("there is nothing to undo")
)

// PayableState is the part of a Payable a use case changes. It is what every payable.*
// event stores before and after, so that Undo can put a Payable back exactly as it was.
type PayableState struct {
	AmountCents *int64     `json:"amount_cents"`
	Status      string     `json:"status"`
	PaidBy      *int64     `json:"paid_by"`
	PaidAt      *time.Time `json:"paid_at"`
}

func stateOf(p domain.Payable) PayableState {
	return PayableState{AmountCents: p.AmountCents, Status: string(p.Status), PaidBy: p.PaidBy, PaidAt: p.PaidAt}
}

// Change is what a Payable use case did: the Payable before and after, and its Cycle as it
// now stands, ready for the Board to be re-rendered.
type Change struct {
	Before domain.Payable
	After  domain.Payable
	Snap   domain.Snapshot
	// Closed reports that the change paid the last thing in the Cycle, which closed it.
	// Reopened reports that it left something unpaid in a Cycle that had been closed.
	Closed   bool
	Reopened bool
}

// Payable reads one Payable and the Cycle it belongs to, for a button that is about to act
// on it.
func (t *Tracker) Payable(payableID int64) (domain.Payable, domain.Snapshot, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	var p domain.Payable
	var snap domain.Snapshot
	err := t.db.Transaction(func(tx *gorm.DB) error {
		var err error
		p, snap, err = readPayable(tx, payableID)
		return err
	})
	return p, snap, err
}

// SetAmount records what a Payable comes to this month. Zero means nothing is due, and
// pays the Payable at once; see domain.SetAmount for the rest.
func (t *Tracker) SetAmount(actor Actor, payableID, cents int64) (Change, error) {
	return t.changePayable(actor, payableID, "payable.set_amount", false,
		func(tx *gorm.DB, before domain.Payable) (domain.Payable, error) {
			sent, err := transferSent(tx, before)
			if err != nil {
				return domain.Payable{}, err
			}
			return domain.SetAmount(before, cents, time.Now().UTC(), sent)
		})
}

// MarkPaid records that the actor settled a Payable.
func (t *Tracker) MarkPaid(actor Actor, payableID int64) (Change, error) {
	return t.changePayable(actor, payableID, "payable.mark_paid", false,
		func(_ *gorm.DB, before domain.Payable) (domain.Payable, error) {
			return domain.MarkPaid(before, actor.TelegramID, time.Now().UTC())
		})
}

// Undo puts a Payable back as it was before the last thing done to it. An Undo is itself
// something done to the Payable, so a second Undo reverses the first. The Payable's status
// is then fitted to the Transfers as they stand now, because Undo never touches a Transfer.
//
// Undo is the one change a closed Cycle accepts, since taking back the payment that closed
// a month is exactly how a month is reopened.
func (t *Tracker) Undo(actor Actor, payableID int64) (Change, error) {
	return t.changePayable(actor, payableID, "payable.undo", true,
		func(tx *gorm.DB, before domain.Payable) (domain.Payable, error) {
			last, err := storage.LatestPayableEvent(tx, payableID)
			if err != nil {
				return domain.Payable{}, err
			}
			if last == nil || last.BeforeJSON == nil {
				return domain.Payable{}, fmt.Errorf("%w: %s", ErrNothingToUndo, before.BillName)
			}
			var earlier PayableState
			if err := json.Unmarshal([]byte(*last.BeforeJSON), &earlier); err != nil {
				return domain.Payable{}, fmt.Errorf("read event %d to undo it: %w", last.ID, err)
			}

			restored := before
			restored.AmountCents = earlier.AmountCents
			restored.Status = domain.Status(earlier.Status)
			restored.PaidBy = earlier.PaidBy
			restored.PaidAt = earlier.PaidAt
			sent, err := transferSent(tx, before)
			if err != nil {
				return domain.Payable{}, err
			}
			return domain.FitToTransfers(restored, sent), nil
		})
}

// changePayable is the shape every Payable use case shares: read the Payable, apply a
// rule, write the result and its event, then close or reopen the Cycle to match. A closed
// Cycle is refused unless allowClosed.
func (t *Tracker) changePayable(actor Actor, payableID int64, action string, allowClosed bool,
	apply func(tx *gorm.DB, before domain.Payable) (domain.Payable, error)) (Change, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	var change Change
	err := t.db.Transaction(func(tx *gorm.DB) error {
		before, snap, err := readPayable(tx, payableID)
		if err != nil {
			return err
		}
		if snap.Cycle.Closed() && !allowClosed {
			return fmt.Errorf("%w: %s", ErrCycleClosed, snap.Cycle.Month.Title())
		}

		after, err := apply(tx, before)
		if err != nil {
			return err
		}
		if err := storage.UpdatePayableState(tx, &storage.Payable{
			ID:          after.ID,
			AmountCents: after.AmountCents,
			Status:      string(after.Status),
			PaidAt:      after.PaidAt,
			PaidBy:      after.PaidBy,
		}); err != nil {
			return err
		}
		if err := appendEvent(tx, actor, action, event{
			before:    stateOf(before),
			after:     stateOf(after),
			cycleID:   &after.CycleID,
			billID:    &after.BillID,
			payableID: &after.ID,
		}); err != nil {
			return err
		}

		change = Change{Before: before, After: after}
		change.Snap, change.Closed, change.Reopened, err = settleCycle(tx, actor, after.CycleID)
		return err
	})
	return change, err
}

// settleCycle closes a Cycle once everything in it is paid, and reopens a closed one that
// has something unpaid again. It returns the Cycle as it then stands.
func settleCycle(tx *gorm.DB, actor Actor, cycleID int64) (snap domain.Snapshot, closed, reopened bool, err error) {
	snap, err = readSnapshotByID(tx, cycleID)
	if err != nil {
		return snap, false, false, err
	}
	wasClosedAt := snap.Cycle.ClosedAt

	var closedAt *time.Time
	switch allPaid := snap.AllPaid(); {
	case allPaid && !snap.Cycle.Closed():
		now := time.Now().UTC()
		closedAt, closed = &now, true
	case !allPaid && snap.Cycle.Closed():
		reopened = true
	default:
		return snap, false, false, nil
	}

	if err := storage.SetCycleClosed(tx, cycleID, closedAt); err != nil {
		return snap, false, false, err
	}
	action := "cycle.close"
	if reopened {
		action = "cycle.reopen"
	}
	if err := appendEvent(tx, actor, action, event{
		cycleID: &cycleID,
		before:  map[string]any{"month": snap.Cycle.Month.String(), "closed_at": wasClosedAt},
		after:   map[string]any{"month": snap.Cycle.Month.String(), "closed_at": closedAt},
	}); err != nil {
		return snap, false, false, err
	}
	snap.Cycle.ClosedAt = closedAt
	return snap, closed, reopened, nil
}

// transferSent reports whether a Transfer has gone into p's channel in p's Cycle.
func transferSent(tx *gorm.DB, p domain.Payable) (bool, error) {
	if !p.Channel.IsSheena() {
		return false, nil
	}
	transfer, err := storage.FindTransfer(tx, p.CycleID, string(p.Channel))
	if err != nil {
		return false, err
	}
	return transfer != nil, nil
}

// readPayable finds a Payable and reads its whole Cycle alongside it.
func readPayable(tx *gorm.DB, payableID int64) (domain.Payable, domain.Snapshot, error) {
	line, err := storage.FindPayableLine(tx, payableID)
	if err != nil {
		return domain.Payable{}, domain.Snapshot{}, err
	}
	if line == nil {
		return domain.Payable{}, domain.Snapshot{}, ErrPayableUnknown
	}
	cycle, err := storage.FindCycle(tx, line.CycleID)
	if err != nil {
		return domain.Payable{}, domain.Snapshot{}, err
	}
	if cycle == nil {
		return domain.Payable{}, domain.Snapshot{}, ErrPayableUnknown
	}
	snap, err := readSnapshot(tx, *cycle)
	if err != nil {
		return domain.Payable{}, domain.Snapshot{}, err
	}
	return toDomainPayable(*line), snap, nil
}
