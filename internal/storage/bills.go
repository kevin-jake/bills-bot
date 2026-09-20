package storage

import (
	"fmt"
	"time"

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

// UpdateBill rewrites everything about a Bill that the standing list can change. A map
// rather than the struct, so that a cleared card name, card digits or due day is written
// as NULL instead of being skipped as a zero value.
func UpdateBill(db *gorm.DB, b *Bill) error {
	b.UpdatedAt = time.Now().UTC()
	err := db.Model(&Bill{}).Where("id = ?", b.ID).Updates(map[string]any{
		"name":          b.Name,
		"aliases":       b.Aliases,
		"section_id":    b.SectionID,
		"channel":       b.Channel,
		"card_name":     b.CardName,
		"card_last4":    b.CardLast4,
		"due_day":       b.DueDay,
		"display_order": b.DisplayOrder,
		"archived_at":   b.ArchivedAt,
		"updated_at":    b.UpdatedAt,
	}).Error
	if err != nil {
		return fmt.Errorf("update bill %d: %w", b.ID, err)
	}
	return nil
}

// RenameCardName repoints everything that names a card by name at its new name, and
// returns how many Bills were repointed. A Bill charged to a card carries that card's name
// in the standing list and in every Payable snapshot of it, so a rename that reached only
// the card itself would leave the charges pointing at a card that no longer exists.
func RenameCardName(db *gorm.DB, from, to string) (int, error) {
	bills := db.Model(&Bill{}).Where("card_name = ?", from).Update("card_name", to)
	if bills.Error != nil {
		return 0, fmt.Errorf("repoint bills charged to %q: %w", from, bills.Error)
	}
	err := db.Model(&Payable{}).Where("card_name = ?", from).Update("card_name", to).Error
	if err != nil {
		return 0, fmt.Errorf("repoint payables charged to %q: %w", from, err)
	}
	return int(bills.RowsAffected), nil
}

// ListSectionBills returns the active Bills under one Section in display order, which is
// the list a reorder shuffles.
func ListSectionBills(db *gorm.DB, sectionID int64) ([]Bill, error) {
	var bills []Bill
	err := db.Where("section_id = ? AND archived_at IS NULL", sectionID).
		Order("display_order, id").Find(&bills).Error
	if err != nil {
		return nil, fmt.Errorf("list bills in section %d: %w", sectionID, err)
	}
	return bills, nil
}

// FindSection returns the Section with id, or nil when there is none.
func FindSection(db *gorm.DB, id int64) (*Section, error) {
	var found []Section
	if err := db.Where("id = ?", id).Limit(1).Find(&found).Error; err != nil {
		return nil, fmt.Errorf("find section %d: %w", id, err)
	}
	if len(found) == 0 {
		return nil, nil
	}
	return &found[0], nil
}

// SetSectionOrder moves a Section in the Board's running order.
func SetSectionOrder(db *gorm.DB, id int64, order int) error {
	err := db.Model(&Section{}).Where("id = ?", id).Update("display_order", order).Error
	if err != nil {
		return fmt.Errorf("set display order of section %d: %w", id, err)
	}
	return nil
}

// SetBillOrder moves a Bill within its Section.
func SetBillOrder(db *gorm.DB, id int64, order int) error {
	err := db.Model(&Bill{}).Where("id = ?", id).Update("display_order", order).Error
	if err != nil {
		return fmt.Errorf("set display order of bill %d: %w", id, err)
	}
	return nil
}

// OpenPayablesForBill returns a Bill's Payables in Cycles that are still open, oldest
// month first. They are the ones a change to the standing list reaches: a closed month
// records what was actually paid and is left exactly as it was.
func OpenPayablesForBill(db *gorm.DB, billID int64) ([]Payable, error) {
	var payables []Payable
	err := db.Raw(`
		SELECT payables.*
		FROM payables
		JOIN cycles ON cycles.id = payables.cycle_id
		WHERE payables.bill_id = ? AND cycles.closed_at IS NULL
		ORDER BY cycles.month`, billID).Scan(&payables).Error
	if err != nil {
		return nil, fmt.Errorf("list open payables for bill %d: %w", billID, err)
	}
	return payables, nil
}

// FindPayableForBill returns a Bill's Payable in one Cycle, or nil when it has none there.
func FindPayableForBill(db *gorm.DB, cycleID, billID int64) (*Payable, error) {
	var found []Payable
	err := db.Where("cycle_id = ? AND bill_id = ?", cycleID, billID).Limit(1).Find(&found).Error
	if err != nil {
		return nil, fmt.Errorf("find payable for bill %d in cycle %d: %w", billID, cycleID, err)
	}
	if len(found) == 0 {
		return nil, nil
	}
	return &found[0], nil
}

// SetPayableChannel rewrites which account a Payable is paid through, and where that
// leaves it. The amount and who paid it are never touched here.
func SetPayableChannel(db *gorm.DB, id int64, channel string, cardName *string, status string) error {
	err := db.Model(&Payable{}).Where("id = ?", id).Updates(map[string]any{
		"channel":    channel,
		"card_name":  cardName,
		"status":     status,
		"updated_at": time.Now().UTC(),
	}).Error
	if err != nil {
		return fmt.Errorf("set channel of payable %d: %w", id, err)
	}
	return nil
}

// CyclesWithBill returns the id of every Cycle a Bill has a Payable in, oldest month
// first. Restoring an archived Bill has to look at all of them, since a month that closed
// only because the Bill's line was withdrawn has something owing in it again.
func CyclesWithBill(db *gorm.DB, billID int64) ([]int64, error) {
	var ids []int64
	err := db.Raw(`
		SELECT cycles.id
		FROM payables
		JOIN cycles ON cycles.id = payables.cycle_id
		WHERE payables.bill_id = ?
		ORDER BY cycles.month`, billID).Scan(&ids).Error
	if err != nil {
		return nil, fmt.Errorf("list cycles holding bill %d: %w", billID, err)
	}
	return ids, nil
}
