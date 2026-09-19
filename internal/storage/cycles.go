package storage

import (
	"fmt"
	"time"

	"gorm.io/gorm"
)

// FindCycleByMonth returns the Cycle for "YYYY-MM", or nil when that month was never opened.
func FindCycleByMonth(db *gorm.DB, month string) (*Cycle, error) {
	var found []Cycle
	if err := db.Where("month = ?", month).Limit(1).Find(&found).Error; err != nil {
		return nil, fmt.Errorf("find cycle %s: %w", month, err)
	}
	if len(found) == 0 {
		return nil, nil
	}
	return &found[0], nil
}

// FindCycle returns the Cycle with id, or nil when there is none.
func FindCycle(db *gorm.DB, id int64) (*Cycle, error) {
	var found []Cycle
	if err := db.Where("id = ?", id).Limit(1).Find(&found).Error; err != nil {
		return nil, fmt.Errorf("find cycle %d: %w", id, err)
	}
	if len(found) == 0 {
		return nil, nil
	}
	return &found[0], nil
}

// LatestCycle returns the most recent open Cycle, or the most recent closed one when none is
// open, or nil when no Cycle exists. An open month is the one people are working on, even
// when a later one has already been opened and closed.
func LatestCycle(db *gorm.DB) (*Cycle, error) {
	var found []Cycle
	err := db.Order("closed_at IS NOT NULL, month DESC").Limit(1).Find(&found).Error
	if err != nil {
		return nil, fmt.Errorf("find latest cycle: %w", err)
	}
	if len(found) == 0 {
		return nil, nil
	}
	return &found[0], nil
}

// ListOpenCycles returns every Cycle not yet closed, oldest month first.
func ListOpenCycles(db *gorm.DB) ([]Cycle, error) {
	var cycles []Cycle
	if err := db.Where("closed_at IS NULL").Order("month").Find(&cycles).Error; err != nil {
		return nil, fmt.Errorf("list open cycles: %w", err)
	}
	return cycles, nil
}

// CreateCycle inserts c and fills in its id.
func CreateCycle(db *gorm.DB, c *Cycle) error {
	if err := db.Create(c).Error; err != nil {
		return fmt.Errorf("create cycle %s: %w", c.Month, err)
	}
	return nil
}

// SetBoardMessage records where a Cycle's Board was posted.
func SetBoardMessage(db *gorm.DB, cycleID, chatID int64, messageID int) error {
	err := db.Model(&Cycle{}).Where("id = ?", cycleID).Updates(map[string]any{
		"board_chat_id":    chatID,
		"board_message_id": messageID,
	}).Error
	if err != nil {
		return fmt.Errorf("record board message for cycle %d: %w", cycleID, err)
	}
	return nil
}

// CreatePayable inserts p and fills in its id.
func CreatePayable(db *gorm.DB, p *Payable) error {
	if p.UpdatedAt.IsZero() {
		p.UpdatedAt = time.Now().UTC()
	}
	if err := db.Create(p).Error; err != nil {
		return fmt.Errorf("create payable for bill %d in cycle %d: %w", p.BillID, p.CycleID, err)
	}
	return nil
}

// PayableLine is a Payable together with the parts of its Bill the Board shows. The name
// and Section are read live from the Bill, so a rename shows on past months too; the
// channel is the Payable's own snapshot, so a change of channel does not.
type PayableLine struct {
	Payable
	BillName  string
	SectionID int64
}

// ListPayableLines returns a Cycle's Payables in Board order: Section display order, then
// Bill display order.
func ListPayableLines(db *gorm.DB, cycleID int64) ([]PayableLine, error) {
	var lines []PayableLine
	err := db.Raw(`
		SELECT payables.*, bills.name AS bill_name, bills.section_id AS section_id
		FROM payables
		JOIN bills    ON bills.id = payables.bill_id
		JOIN sections ON sections.id = bills.section_id
		WHERE payables.cycle_id = ?
		ORDER BY sections.display_order, sections.id, bills.display_order, bills.id`,
		cycleID).Scan(&lines).Error
	if err != nil {
		return nil, fmt.Errorf("list payables for cycle %d: %w", cycleID, err)
	}
	return lines, nil
}

// ListTransfers returns a Cycle's Transfers.
func ListTransfers(db *gorm.DB, cycleID int64) ([]Transfer, error) {
	var transfers []Transfer
	if err := db.Where("cycle_id = ?", cycleID).Order("channel").Find(&transfers).Error; err != nil {
		return nil, fmt.Errorf("list transfers for cycle %d: %w", cycleID, err)
	}
	return transfers, nil
}
