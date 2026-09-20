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
	// CardLast4 and DueDay come from the Bill too, so a card reissued with new digits or a
	// due day the bank moved shows its current form wherever the Payable is read.
	CardLast4 *string
	DueDay    *int
}

// ListPayableLines returns a Cycle's Payables in Board order: Section display order, then
// Bill display order.
func ListPayableLines(db *gorm.DB, cycleID int64) ([]PayableLine, error) {
	var lines []PayableLine
	err := db.Raw(`
		SELECT payables.*, bills.name AS bill_name, bills.section_id AS section_id,
		       bills.card_last4 AS card_last4, bills.due_day AS due_day
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

// FindPayableLine returns the Payable with id together with its Bill's name and Section, or
// nil when there is none.
func FindPayableLine(db *gorm.DB, id int64) (*PayableLine, error) {
	var found []PayableLine
	err := db.Raw(`
		SELECT payables.*, bills.name AS bill_name, bills.section_id AS section_id,
		       bills.card_last4 AS card_last4, bills.due_day AS due_day
		FROM payables
		JOIN bills ON bills.id = payables.bill_id
		WHERE payables.id = ?`, id).Scan(&found).Error
	if err != nil {
		return nil, fmt.Errorf("find payable %d: %w", id, err)
	}
	if len(found) == 0 {
		return nil, nil
	}
	return &found[0], nil
}

// UpdatePayableState writes a Payable's amount and where it stands. The Bill, Cycle and
// channel snapshot are never rewritten here.
func UpdatePayableState(db *gorm.DB, p *Payable) error {
	p.UpdatedAt = time.Now().UTC()
	// A map rather than the struct, so that nil pointers are written as NULL instead of
	// being skipped as zero values.
	err := db.Model(&Payable{}).Where("id = ?", p.ID).Updates(map[string]any{
		"amount_cents": p.AmountCents,
		"status":       p.Status,
		"paid_at":      p.PaidAt,
		"paid_by":      p.PaidBy,
		"updated_at":   p.UpdatedAt,
	}).Error
	if err != nil {
		return fmt.Errorf("update payable %d: %w", p.ID, err)
	}
	return nil
}

// FindTransfer returns the Transfer into channel for a Cycle, or nil when none was sent.
func FindTransfer(db *gorm.DB, cycleID int64, channel string) (*Transfer, error) {
	var found []Transfer
	err := db.Where("cycle_id = ? AND channel = ?", cycleID, channel).Limit(1).Find(&found).Error
	if err != nil {
		return nil, fmt.Errorf("find %s transfer for cycle %d: %w", channel, cycleID, err)
	}
	if len(found) == 0 {
		return nil, nil
	}
	return &found[0], nil
}

// SetCycleClosed records when a Cycle was closed, or with nil reopens it.
func SetCycleClosed(db *gorm.DB, cycleID int64, closedAt *time.Time) error {
	err := db.Model(&Cycle{}).Where("id = ?", cycleID).
		Updates(map[string]any{"closed_at": closedAt}).Error
	if err != nil {
		return fmt.Errorf("set closed_at of cycle %d: %w", cycleID, err)
	}
	return nil
}

// FindTransferByID returns the Transfer with id, or nil when there is none.
func FindTransferByID(db *gorm.DB, id int64) (*Transfer, error) {
	var found []Transfer
	if err := db.Where("id = ?", id).Limit(1).Find(&found).Error; err != nil {
		return nil, fmt.Errorf("find transfer %d: %w", id, err)
	}
	if len(found) == 0 {
		return nil, nil
	}
	return &found[0], nil
}

// SaveTransfer inserts t when it has no id yet, and otherwise rewrites what was sent, when
// and by whom. Its Cycle and channel are never rewritten.
func SaveTransfer(db *gorm.DB, t *Transfer) error {
	if t.ID == 0 {
		if err := db.Create(t).Error; err != nil {
			return fmt.Errorf("create %s transfer for cycle %d: %w", t.Channel, t.CycleID, err)
		}
		return nil
	}
	err := db.Model(&Transfer{}).Where("id = ?", t.ID).Updates(map[string]any{
		"sent_cents": t.SentCents,
		"sent_at":    t.SentAt,
		"sent_by":    t.SentBy,
	}).Error
	if err != nil {
		return fmt.Errorf("update transfer %d: %w", t.ID, err)
	}
	return nil
}

// DeleteTransfer removes a Transfer that was recorded by mistake.
func DeleteTransfer(db *gorm.DB, id int64) error {
	if err := db.Delete(&Transfer{}, id).Error; err != nil {
		return fmt.Errorf("delete transfer %d: %w", id, err)
	}
	return nil
}

// ListCycles returns every Cycle, oldest month first.
func ListCycles(db *gorm.DB) ([]Cycle, error) {
	var cycles []Cycle
	if err := db.Order("month").Find(&cycles).Error; err != nil {
		return nil, fmt.Errorf("list cycles: %w", err)
	}
	return cycles, nil
}
