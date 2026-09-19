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
	// ErrNoCycle means no month has been opened at all yet.
	ErrNoCycle = errors.New("no month has been opened yet")
	// ErrCycleUnknown means the month asked for has not been opened.
	ErrCycleUnknown = errors.New("that month has not been opened")
)

// OpenCycle opens month with a due Payable of unknown amount for every Bill on the standing
// list. It is idempotent: opening a month that already exists changes nothing and returns
// that month with opened false, so the scheduler and /newmonth can both call it freely.
func (t *Tracker) OpenCycle(actor Actor, month domain.Month) (snap domain.Snapshot, opened bool, err error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	err = t.db.Transaction(func(tx *gorm.DB) error {
		existing, err := storage.FindCycleByMonth(tx, month.String())
		if err != nil {
			return err
		}
		if existing != nil {
			snap, err = readSnapshot(tx, *existing)
			return err
		}

		cycle := storage.Cycle{Month: month.String(), OpenedAt: time.Now().UTC()}
		if err := storage.CreateCycle(tx, &cycle); err != nil {
			return err
		}

		bills, err := storage.ListActiveBills(tx)
		if err != nil {
			return err
		}
		for _, bill := range bills {
			if err := createDuePayable(tx, cycle.ID, bill); err != nil {
				return err
			}
		}

		opened = true
		if err := appendEvent(tx, actor, "cycle.open", event{
			cycleID: &cycle.ID,
			after:   map[string]any{"month": cycle.Month, "payables": len(bills)},
		}); err != nil {
			return err
		}
		snap, err = readSnapshot(tx, cycle)
		return err
	})
	if err != nil {
		return domain.Snapshot{}, false, err
	}
	return snap, opened, nil
}

// Snapshot reads one Cycle as it stands now.
func (t *Tracker) Snapshot(cycleID int64) (domain.Snapshot, error) {
	return t.readCycle(func(tx *gorm.DB) (*storage.Cycle, error) {
		return storage.FindCycle(tx, cycleID)
	}, ErrCycleUnknown)
}

// MonthSnapshot reads the Cycle for month.
func (t *Tracker) MonthSnapshot(month domain.Month) (domain.Snapshot, error) {
	return t.readCycle(func(tx *gorm.DB) (*storage.Cycle, error) {
		return storage.FindCycleByMonth(tx, month.String())
	}, fmt.Errorf("%w: %s", ErrCycleUnknown, month.Title()))
}

// CurrentSnapshot reads the Cycle people are most likely working on: the newest open one,
// or the newest of all when every Cycle is closed.
func (t *Tracker) CurrentSnapshot() (domain.Snapshot, error) {
	return t.readCycle(storage.LatestCycle, ErrNoCycle)
}

// OpenSnapshots reads every open Cycle, oldest month first.
func (t *Tracker) OpenSnapshots() ([]domain.Snapshot, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	var snaps []domain.Snapshot
	err := t.db.Transaction(func(tx *gorm.DB) error {
		cycles, err := storage.ListOpenCycles(tx)
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

// RecordBoard remembers where a Cycle's Board was posted. It is bookkeeping about a
// Telegram message rather than a change to the household's bills, so it leaves no event.
func (t *Tracker) RecordBoard(cycleID, chatID int64, messageID int) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	return storage.SetBoardMessage(t.db, cycleID, chatID, messageID)
}

// readCycle finds a Cycle and reads it, all in one transaction so the Board never mixes
// two moments. missing is returned when find comes back empty.
func (t *Tracker) readCycle(find func(*gorm.DB) (*storage.Cycle, error), missing error) (domain.Snapshot, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	var snap domain.Snapshot
	err := t.db.Transaction(func(tx *gorm.DB) error {
		cycle, err := find(tx)
		if err != nil {
			return err
		}
		if cycle == nil {
			return missing
		}
		snap, err = readSnapshot(tx, *cycle)
		return err
	})
	return snap, err
}

func readSnapshot(tx *gorm.DB, cycle storage.Cycle) (domain.Snapshot, error) {
	month, err := domain.ParseMonth(cycle.Month)
	if err != nil {
		return domain.Snapshot{}, fmt.Errorf("cycle %d has month %q: %w", cycle.ID, cycle.Month, err)
	}
	snap := domain.Snapshot{Cycle: domain.Cycle{
		ID:       cycle.ID,
		Month:    month,
		OpenedAt: cycle.OpenedAt,
		ClosedAt: cycle.ClosedAt,
	}}
	if cycle.BoardChatID != nil {
		snap.Cycle.BoardChatID = *cycle.BoardChatID
	}
	if cycle.BoardMessageID != nil {
		snap.Cycle.BoardMessageID = *cycle.BoardMessageID
	}

	sections, err := storage.ListSections(tx)
	if err != nil {
		return domain.Snapshot{}, err
	}
	for _, s := range sections {
		snap.Sections = append(snap.Sections, toDomainSection(s))
	}

	lines, err := storage.ListPayableLines(tx, cycle.ID)
	if err != nil {
		return domain.Snapshot{}, err
	}
	for _, line := range lines {
		snap.Payables = append(snap.Payables, toDomainPayable(line))
	}

	transfers, err := storage.ListTransfers(tx, cycle.ID)
	if err != nil {
		return domain.Snapshot{}, err
	}
	for _, tr := range transfers {
		snap.Transfers = append(snap.Transfers, domain.Transfer{
			ID: tr.ID, CycleID: tr.CycleID, Channel: domain.Channel(tr.Channel),
			SentCents: tr.SentCents, SentAt: tr.SentAt, SentBy: tr.SentBy,
		})
	}
	return snap, nil
}

// createDuePayable gives bill a Payable in a Cycle: due, amount unknown, and paid through
// whatever channel the Bill has right now.
func createDuePayable(tx *gorm.DB, cycleID int64, bill storage.Bill) error {
	return storage.CreatePayable(tx, &storage.Payable{
		CycleID:  cycleID,
		BillID:   bill.ID,
		Status:   string(domain.StatusDue),
		Channel:  bill.Channel,
		CardName: bill.CardName,
	})
}

func toDomainPayable(line storage.PayableLine) domain.Payable {
	p := domain.Payable{
		ID:          line.ID,
		CycleID:     line.CycleID,
		BillID:      line.BillID,
		BillName:    line.BillName,
		SectionID:   line.SectionID,
		Channel:     domain.Channel(line.Channel),
		AmountCents: line.AmountCents,
		Status:      domain.Status(line.Status),
		PaidAt:      line.PaidAt,
		PaidBy:      line.PaidBy,
	}
	if line.CardName != nil {
		p.CardName = *line.CardName
	}
	return p
}
