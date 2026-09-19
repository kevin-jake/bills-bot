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
	// ErrTransferUnknown means a button named a Transfer that has since been undone.
	ErrTransferUnknown = errors.New("that transfer is no longer recorded")
	// ErrNotSheenaChannel means a Transfer was aimed at a channel Kevin does not fund.
	ErrNotSheenaChannel = errors.New("only Sheena's accounts take transfers")
	// ErrChannelUnused means nothing in the Cycle is paid through the channel, so a Transfer
	// into it would fund nothing and appear nowhere on the Board.
	ErrChannelUnused = errors.New("nothing this month is paid through that account")
)

// TransferChange is what a Transfer use case did: the Transfer before and after (nil when
// there was or is none), the Payables whose status it moved, and the Cycle as it now stands.
type TransferChange struct {
	Channel domain.Channel
	Before  *domain.Transfer
	After   *domain.Transfer
	// Moved are the Payables funded by recording, or put back to due by undoing, as they
	// now are.
	Moved []domain.Payable
	Snap  domain.Snapshot
}

// RecordTransfer records what Kevin sent into one of Sheena's accounts for a Cycle, and
// funds every Payable on that channel that is still due. Recording again replaces the
// amount and funds anything that has become due since, such as a bill added mid-month.
// Paid Payables are never touched.
func (t *Tracker) RecordTransfer(actor Actor, cycleID int64, channel domain.Channel, cents int64) (TransferChange, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if !channel.IsSheena() {
		return TransferChange{}, fmt.Errorf("%w, not %s", ErrNotSheenaChannel, channel.Label())
	}
	if err := domain.ValidateTransfer(cents); err != nil {
		return TransferChange{}, err
	}

	change := TransferChange{Channel: channel}
	err := t.db.Transaction(func(tx *gorm.DB) error {
		snap, err := readOpenCycle(tx, cycleID)
		if err != nil {
			return err
		}
		if !usesChannel(snap, channel) {
			return fmt.Errorf("%w: %s", ErrChannelUnused, channel.Label())
		}

		row, err := storage.FindTransfer(tx, cycleID, string(channel))
		if err != nil {
			return err
		}
		if row == nil {
			row = &storage.Transfer{CycleID: cycleID, Channel: string(channel)}
		} else {
			before := toDomainTransfer(*row)
			change.Before = &before
		}
		row.SentCents, row.SentAt, row.SentBy = cents, time.Now().UTC(), actor.TelegramID
		if err := storage.SaveTransfer(tx, row); err != nil {
			return err
		}
		after := toDomainTransfer(*row)
		change.After = &after

		if err := appendEvent(tx, actor, "transfer.record", event{
			before:     change.Before,
			after:      change.After,
			cycleID:    &cycleID,
			transferID: &row.ID,
			note:       string(channel),
		}); err != nil {
			return err
		}

		change.Moved, err = moveStatus(tx, actor, snap, channel, row.ID,
			domain.StatusDue, domain.StatusFunded, "transfer.fund")
		if err != nil {
			return err
		}
		change.Snap, err = readSnapshotByID(tx, cycleID)
		return err
	})
	return change, err
}

// UndoTransfer removes a Transfer recorded by mistake, and puts the Payables it funded
// back to due. Anything Sheena has already paid stays paid.
func (t *Tracker) UndoTransfer(actor Actor, transferID int64) (TransferChange, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	var change TransferChange
	err := t.db.Transaction(func(tx *gorm.DB) error {
		row, err := storage.FindTransferByID(tx, transferID)
		if err != nil {
			return err
		}
		if row == nil {
			return ErrTransferUnknown
		}
		snap, err := readOpenCycle(tx, row.CycleID)
		if err != nil {
			return err
		}

		before := toDomainTransfer(*row)
		change = TransferChange{Channel: before.Channel, Before: &before}
		if err := storage.DeleteTransfer(tx, row.ID); err != nil {
			return err
		}
		if err := appendEvent(tx, actor, "transfer.undo", event{
			before:     change.Before,
			cycleID:    &row.CycleID,
			transferID: &row.ID,
			note:       row.Channel,
		}); err != nil {
			return err
		}

		change.Moved, err = moveStatus(tx, actor, snap, before.Channel, row.ID,
			domain.StatusFunded, domain.StatusDue, "transfer.unfund")
		if err != nil {
			return err
		}
		change.Snap, err = readSnapshotByID(tx, row.CycleID)
		return err
	})
	return change, err
}

// moveStatus moves every Payable on channel from one status to another, leaving an event
// for each. The events are not payable.* events, so Undo on a bill passes over them: a
// bill's Undo takes back what someone did to that bill, not what a Transfer did to it.
func moveStatus(tx *gorm.DB, actor Actor, snap domain.Snapshot, channel domain.Channel,
	transferID int64, from, to domain.Status, action string) ([]domain.Payable, error) {
	var moved []domain.Payable
	for _, p := range snap.Payables {
		if p.Channel != channel || p.Status != from {
			continue
		}
		after := p
		after.Status = to
		if err := storage.UpdatePayableState(tx, &storage.Payable{
			ID:          after.ID,
			AmountCents: after.AmountCents,
			Status:      string(after.Status),
			PaidAt:      after.PaidAt,
			PaidBy:      after.PaidBy,
		}); err != nil {
			return nil, err
		}
		if err := appendEvent(tx, actor, action, event{
			before:     stateOf(p),
			after:      stateOf(after),
			cycleID:    &after.CycleID,
			billID:     &after.BillID,
			payableID:  &after.ID,
			transferID: &transferID,
		}); err != nil {
			return nil, err
		}
		moved = append(moved, after)
	}
	return moved, nil
}

// readOpenCycle reads a Cycle that is about to be changed, refusing one that is missing or
// already closed.
func readOpenCycle(tx *gorm.DB, cycleID int64) (domain.Snapshot, error) {
	snap, err := readSnapshotByID(tx, cycleID)
	if err != nil {
		return snap, err
	}
	if snap.Cycle.Closed() {
		return snap, fmt.Errorf("%w: %s", ErrCycleClosed, snap.Cycle.Month.Title())
	}
	return snap, nil
}

func readSnapshotByID(tx *gorm.DB, cycleID int64) (domain.Snapshot, error) {
	cycle, err := storage.FindCycle(tx, cycleID)
	if err != nil {
		return domain.Snapshot{}, err
	}
	if cycle == nil {
		return domain.Snapshot{}, ErrCycleUnknown
	}
	return readSnapshot(tx, *cycle)
}

func usesChannel(snap domain.Snapshot, channel domain.Channel) bool {
	for _, p := range snap.Payables {
		if p.Channel == channel {
			return true
		}
	}
	return false
}

func toDomainTransfer(row storage.Transfer) domain.Transfer {
	return domain.Transfer{
		ID: row.ID, CycleID: row.CycleID, Channel: domain.Channel(row.Channel),
		SentCents: row.SentCents, SentAt: row.SentAt, SentBy: row.SentBy,
	}
}
