package tracker

import (
	"errors"
	"fmt"

	"github.com/kevin-jake/bills-bot/internal/domain"
	"github.com/kevin-jake/bills-bot/internal/storage"
	"gorm.io/gorm"
)

// ErrBillUnknown means a report named a Bill that is not on the list, archived ones and all.
var ErrBillUnknown = errors.New("there is no bill with that name")

// Reports read; they never change anything and so leave no event behind. They still take
// the mutex and a transaction, because a report read while a Cycle is being closed would
// otherwise show half of the change.

// AllBills returns every Bill, archived ones included, so that a report can be asked for a
// Bill that has since left the standing list and still answer with its months.
func (t *Tracker) AllBills() ([]domain.Bill, error) {
	rows, err := storage.ListBills(t.db)
	if err != nil {
		return nil, err
	}
	bills := make([]domain.Bill, 0, len(rows))
	for _, row := range rows {
		bills = append(bills, toDomainBill(row))
	}
	return bills, nil
}

// History reads what a Bill has come to over its last months, newest first.
func (t *Tracker) History(billID int64, months int) (domain.History, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	var history domain.History
	err := t.db.Transaction(func(tx *gorm.DB) error {
		bill, err := storage.FindBill(tx, billID)
		if err != nil {
			return err
		}
		if bill == nil {
			return fmt.Errorf("%w: %d", ErrBillUnknown, billID)
		}
		history.Bill = toDomainBill(*bill).DisplayName()

		names, err := storage.ActorNames(tx)
		if err != nil {
			return err
		}
		lines, err := storage.ListBillHistory(tx, billID, months)
		if err != nil {
			return err
		}
		for _, line := range lines {
			month, err := domain.ParseMonth(line.Month)
			if err != nil {
				return fmt.Errorf("payable %d is in month %q: %w", line.ID, line.Month, err)
			}
			payable := toDomainPayable(storage.PayableLine{
				Payable:   line.Payable,
				BillName:  line.BillName,
				CardLast4: line.CardLast4,
				DueDay:    line.DueDay,
			})
			history.Entries = append(history.Entries, domain.HistoryEntry{
				Month:   month,
				Payable: payable,
				PaidBy:  domain.Names(names).Of(payable.PaidBy),
			})
		}
		return nil
	})
	if err != nil {
		return domain.History{}, err
	}
	return history, nil
}

// AllSnapshots reads every Cycle there has ever been, oldest month first. The exports are
// the one thing that wants the lot; everything else works a month at a time.
func (t *Tracker) AllSnapshots() ([]domain.Snapshot, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	var snaps []domain.Snapshot
	err := t.db.Transaction(func(tx *gorm.DB) error {
		cycles, err := storage.ListCycles(tx)
		if err != nil {
			return err
		}
		for _, cycle := range cycles {
			snap, err := readSnapshot(tx, cycle)
			if err != nil {
				return err
			}
			snaps = append(snaps, snap)
		}
		return nil
	})
	return snaps, err
}

// ActorNames returns the name each person acts under, for a report that says who did
// something.
func (t *Tracker) ActorNames() (domain.Names, error) {
	names, err := storage.ActorNames(t.db)
	if err != nil {
		return nil, err
	}
	return domain.Names(names), nil
}
