package storage

import (
	"fmt"

	"gorm.io/gorm"
)

// Every function here takes the *gorm.DB to run on rather than holding one, so the same
// call works inside a transaction and out of it. The tracker passes its transaction in.

// ListSections returns every Section in display order.
func ListSections(db *gorm.DB) ([]Section, error) {
	var sections []Section
	if err := db.Order("display_order, id").Find(&sections).Error; err != nil {
		return nil, fmt.Errorf("list sections: %w", err)
	}
	return sections, nil
}

// FindSectionByName looks a Section up case-insensitively, matching the COLLATE NOCASE
// the schema declares. It returns nil when there is no such Section, which is an ordinary
// answer rather than an error.
func FindSectionByName(db *gorm.DB, name string) (*Section, error) {
	var found []Section
	err := db.Where("name = ? COLLATE NOCASE", name).Limit(1).Find(&found).Error
	if err != nil {
		return nil, fmt.Errorf("find section %q: %w", name, err)
	}
	if len(found) == 0 {
		return nil, nil
	}
	return &found[0], nil
}

// NextSectionOrder returns the display order a new Section should take: the end of the list.
func NextSectionOrder(db *gorm.DB) (int, error) {
	return nextOrder(db.Model(&Section{}), "sections")
}

// CreateSection inserts s and fills in its id.
func CreateSection(db *gorm.DB, s *Section) error {
	if err := db.Create(s).Error; err != nil {
		return fmt.Errorf("create section %q: %w", s.Name, err)
	}
	return nil
}

// ListActiveBills returns every Bill still on the standing list, ordered within a Section.
// Callers that need them grouped read the Sections separately, which keeps this a plain
// query rather than a join over two tables that both have a name and a display order.
func ListActiveBills(db *gorm.DB) ([]Bill, error) {
	var bills []Bill
	err := db.Where("archived_at IS NULL").Order("display_order, id").Find(&bills).Error
	if err != nil {
		return nil, fmt.Errorf("list active bills: %w", err)
	}
	return bills, nil
}

// FindActiveBillByName looks a Bill up case-insensitively among those not archived, which
// is the same set the schema's partial unique index protects. Archived names are free to
// reuse, so an archived match is deliberately not returned.
func FindActiveBillByName(db *gorm.DB, name string) (*Bill, error) {
	var found []Bill
	err := db.Where("name = ? COLLATE NOCASE AND archived_at IS NULL", name).Limit(1).Find(&found).Error
	if err != nil {
		return nil, fmt.Errorf("find bill %q: %w", name, err)
	}
	if len(found) == 0 {
		return nil, nil
	}
	return &found[0], nil
}

// NextBillOrder returns the display order a new Bill takes within its Section. Archived
// Bills still count, so that reviving a Section's history cannot collide with a live row.
func NextBillOrder(db *gorm.DB, sectionID int64) (int, error) {
	return nextOrder(db.Model(&Bill{}).Where("section_id = ?", sectionID), "bills")
}

// CreateBill inserts b and fills in its id.
func CreateBill(db *gorm.DB, b *Bill) error {
	if err := db.Create(b).Error; err != nil {
		return fmt.Errorf("create bill %q: %w", b.Name, err)
	}
	return nil
}

// nextOrder reads the highest display_order in scope and returns the one after it. The
// pointer matters: MAX over no rows is NULL, which is the first entry rather than zero.
func nextOrder(scope *gorm.DB, what string) (int, error) {
	var highest *int
	if err := scope.Select("MAX(display_order)").Scan(&highest).Error; err != nil {
		return 0, fmt.Errorf("next %s display order: %w", what, err)
	}
	if highest == nil {
		return 1, nil
	}
	return *highest + 1, nil
}

// AppendEvent writes one row to the audit trail.
func AppendEvent(db *gorm.DB, e *Event) error {
	if err := db.Create(e).Error; err != nil {
		return fmt.Errorf("append %s event: %w", e.Action, err)
	}
	return nil
}

// LatestPayableEvent returns the newest payable.* event for a Payable, or nil when nothing
// has been done to it yet. Events of other kinds that name the Payable, such as a Transfer
// funding it, are passed over: Undo reverses what a person did to the bill itself.
func LatestPayableEvent(db *gorm.DB, payableID int64) (*Event, error) {
	var found []Event
	err := db.Where("payable_id = ? AND action LIKE 'payable.%'", payableID).
		Order("id DESC").Limit(1).Find(&found).Error
	if err != nil {
		return nil, fmt.Errorf("find latest event for payable %d: %w", payableID, err)
	}
	if len(found) == 0 {
		return nil, nil
	}
	return &found[0], nil
}

// ListBills returns every Bill, archived ones included, ordered within a Section. Reports
// read this rather than ListActiveBills: a Bill that has left the standing list still has
// the months it was on it.
func ListBills(db *gorm.DB) ([]Bill, error) {
	var bills []Bill
	if err := db.Order("display_order, id").Find(&bills).Error; err != nil {
		return nil, fmt.Errorf("list bills: %w", err)
	}
	return bills, nil
}

// FindBill returns the Bill with id, archived or not, or nil when there is none.
func FindBill(db *gorm.DB, id int64) (*Bill, error) {
	var found []Bill
	if err := db.Where("id = ?", id).Limit(1).Find(&found).Error; err != nil {
		return nil, fmt.Errorf("find bill %d: %w", id, err)
	}
	if len(found) == 0 {
		return nil, nil
	}
	return &found[0], nil
}
