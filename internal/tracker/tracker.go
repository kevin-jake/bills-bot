// Package tracker is the application service: every use case the bot offers runs through
// here, inside one transaction, and leaves an audit event behind. It is the only package
// that both knows the household's rules and touches the database, which is what keeps the
// rules out of internal/telegram and the machinery out of internal/domain.
package tracker

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/kevin-jake/bills-bot/internal/domain"
	"github.com/kevin-jake/bills-bot/internal/storage"
	"gorm.io/gorm"
)

// Actor is whoever asked for a change. Every mutation records one, because the household
// wants to know who set an amount or marked something paid.
type Actor struct {
	TelegramID int64
	Name       string
}

// System is the actor the scheduler acts as when it opens a Cycle or posts a reminder.
var System = Actor{TelegramID: 0, Name: "system"}

// Tracker serialises the use cases and owns the database handle.
type Tracker struct {
	db *gorm.DB
	// mu serialises use cases. SQLite takes one writer at a time, and each use case reads
	// the state back straight after writing it in order to re-render the Board, so two
	// overlapping updates would otherwise render from each other's half-finished work.
	mu sync.Mutex
}

// New returns a Tracker over an already migrated database.
func New(db *gorm.DB) *Tracker {
	return &Tracker{db: db}
}

// Errors a person can provoke. They are worded to be shown straight back to whoever typed
// the command, and wrapped with the offending name.
var (
	ErrSectionUnknown = errors.New("there is no section with that name")
	ErrSectionExists  = errors.New("there is already a section with that name")
	ErrBillExists     = errors.New("there is already a bill with that name")
)

// SectionBills is one Section and the active Bills listed under it, in display order.
type SectionBills struct {
	Section domain.Section
	Bills   []domain.Bill
}

// BillList returns the standing list grouped by Section in display order. Sections with no
// active Bills are left out, as they are on the Board: an empty heading is noise.
func (t *Tracker) BillList() ([]SectionBills, error) {
	sections, err := storage.ListSections(t.db)
	if err != nil {
		return nil, err
	}
	bills, err := storage.ListActiveBills(t.db)
	if err != nil {
		return nil, err
	}

	bySection := make(map[int64][]domain.Bill, len(sections))
	for _, row := range bills {
		bySection[row.SectionID] = append(bySection[row.SectionID], toDomainBill(row))
	}

	grouped := make([]SectionBills, 0, len(sections))
	for _, section := range sections {
		listed := bySection[section.ID]
		if len(listed) == 0 {
			continue
		}
		grouped = append(grouped, SectionBills{Section: toDomainSection(section), Bills: listed})
	}
	return grouped, nil
}

// AddSection appends a Section to the end of the display order.
func (t *Tracker) AddSection(actor Actor, name string) (domain.Section, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	name = strings.TrimSpace(name)
	if err := domain.ValidateSection(name); err != nil {
		return domain.Section{}, err
	}

	var added domain.Section
	err := t.db.Transaction(func(tx *gorm.DB) error {
		existing, err := storage.FindSectionByName(tx, name)
		if err != nil {
			return err
		}
		if existing != nil {
			return fmt.Errorf("%w: %s", ErrSectionExists, existing.Name)
		}

		order, err := storage.NextSectionOrder(tx)
		if err != nil {
			return err
		}
		row := storage.Section{Name: name, DisplayOrder: order}
		if err := storage.CreateSection(tx, &row); err != nil {
			return err
		}

		added = toDomainSection(row)
		return appendEvent(tx, actor, "section.add", event{after: added, note: name})
	})
	if err != nil {
		return domain.Section{}, err
	}
	return added, nil
}

// BillSpec is a proposed Bill, named the way a person typed it.
type BillSpec struct {
	Name     string
	Section  string
	Channel  domain.Channel
	CardName string
}

// AddBill adds a Bill to the end of its Section on the standing list, and gives it a due
// Payable in every open Cycle: a bill that starts this month is owed this month. Closed
// Cycles are left alone, because they record a month that has already been settled.
func (t *Tracker) AddBill(actor Actor, spec BillSpec) (domain.Bill, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	name := strings.TrimSpace(spec.Name)
	cardName := strings.TrimSpace(spec.CardName)
	if err := domain.ValidateBill(name, spec.Channel, cardName); err != nil {
		return domain.Bill{}, err
	}

	var added domain.Bill
	err := t.db.Transaction(func(tx *gorm.DB) error {
		section, err := storage.FindSectionByName(tx, spec.Section)
		if err != nil {
			return err
		}
		if section == nil {
			return fmt.Errorf("%w: %s", ErrSectionUnknown, strings.TrimSpace(spec.Section))
		}

		// Asking first rather than letting the partial unique index fire keeps the refusal
		// a sentence about a duplicate name instead of a constraint error.
		existing, err := storage.FindActiveBillByName(tx, name)
		if err != nil {
			return err
		}
		if existing != nil {
			return fmt.Errorf("%w: %s", ErrBillExists, existing.Name)
		}

		order, err := storage.NextBillOrder(tx, section.ID)
		if err != nil {
			return err
		}
		row := storage.Bill{
			Name:         name,
			SectionID:    section.ID,
			Channel:      string(spec.Channel),
			DisplayOrder: order,
		}
		if cardName != "" {
			row.CardName = &cardName
		}
		if err := storage.CreateBill(tx, &row); err != nil {
			return err
		}

		openCycles, err := storage.ListOpenCycles(tx)
		if err != nil {
			return err
		}
		for _, cycle := range openCycles {
			if err := createDuePayable(tx, cycle.ID, row); err != nil {
				return err
			}
		}

		added = toDomainBill(row)
		return appendEvent(tx, actor, "bill.add", event{after: added, billID: &row.ID})
	})
	if err != nil {
		return domain.Bill{}, err
	}
	return added, nil
}

// event is the part of an audit row a use case has to decide; the rest is filled in below.
type event struct {
	before     any
	after      any
	cycleID    *int64
	billID     *int64
	payableID  *int64
	transferID *int64
	note       string
}

// appendEvent writes the audit row for a mutation. before and after are stored as JSON so
// that Undo can restore a row without the audit trail needing a column per field.
func appendEvent(tx *gorm.DB, actor Actor, action string, e event) error {
	row := storage.Event{
		OccurredAt:      time.Now().UTC(),
		ActorTelegramID: actor.TelegramID,
		ActorName:       actor.Name,
		Action:          action,
		CycleID:         e.cycleID,
		BillID:          e.billID,
		PayableID:       e.payableID,
		TransferID:      e.transferID,
		BeforeJSON:      encodeJSON(e.before),
		AfterJSON:       encodeJSON(e.after),
	}
	if e.note != "" {
		row.Note = &e.note
	}
	return storage.AppendEvent(tx, &row)
}

// encodeJSON returns nil for a nil value, so an absent before-state stays NULL rather than
// becoming the string "null".
func encodeJSON(value any) *string {
	if value == nil {
		return nil
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	text := string(encoded)
	return &text
}

func toDomainSection(row storage.Section) domain.Section {
	return domain.Section{ID: row.ID, Name: row.Name, DisplayOrder: row.DisplayOrder}
}

func toDomainBill(row storage.Bill) domain.Bill {
	bill := domain.Bill{
		ID:           row.ID,
		Name:         row.Name,
		SectionID:    row.SectionID,
		Channel:      domain.Channel(row.Channel),
		DisplayOrder: row.DisplayOrder,
		Archived:     row.Archived(),
	}
	if row.CardName != nil {
		bill.CardName = *row.CardName
	}
	if row.CardLast4 != nil {
		bill.CardLast4 = *row.CardLast4
	}
	if row.DueDay != nil {
		bill.DueDay = *row.DueDay
	}
	if row.Aliases != "" {
		bill.Aliases = strings.Split(row.Aliases, ",")
	}
	return bill
}
