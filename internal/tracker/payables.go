package tracker

import (
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
	t.mu.Lock()
	defer t.mu.Unlock()

	var change Change
	err := t.db.Transaction(func(tx *gorm.DB) error {
		before, snap, err := readPayable(tx, payableID)
		if err != nil {
			return err
		}
		// Reopening a settled month belongs with Undo; until then a closed month's figures
		// stay as they were settled.
		if snap.Cycle.Closed() {
			return fmt.Errorf("%w: %s", ErrCycleClosed, snap.Cycle.Month.Title())
		}

		transferSent := false
		if before.Channel.IsSheena() {
			transfer, err := storage.FindTransfer(tx, before.CycleID, string(before.Channel))
			if err != nil {
				return err
			}
			transferSent = transfer != nil
		}

		after, err := domain.SetAmount(before, cents, time.Now().UTC(), transferSent)
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

		if err := appendEvent(tx, actor, "payable.set_amount", event{
			before:    stateOf(before),
			after:     stateOf(after),
			cycleID:   &after.CycleID,
			billID:    &after.BillID,
			payableID: &after.ID,
		}); err != nil {
			return err
		}

		updated, err := storage.FindCycle(tx, before.CycleID)
		if err != nil {
			return err
		}
		snap, err = readSnapshot(tx, *updated)
		if err != nil {
			return err
		}
		change = Change{Before: before, After: after, Snap: snap}
		return nil
	})
	return change, err
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
