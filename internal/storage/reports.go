package storage

import (
	"fmt"

	"gorm.io/gorm"
)

// The queries here read across Cycles rather than within one, which is what the history
// and the exports need and what every other query in this package deliberately avoids.

// HistoryLine is one month of a Bill: its Payable together with the month it falls in and
// the parts of the Bill that are read live, exactly as PayableLine does within a Cycle.
type HistoryLine struct {
	Payable
	Month     string
	BillName  string
	CardLast4 *string
	DueDay    *int
}

// ListBillHistory returns a Bill's Payables newest month first, at most limit of them.
func ListBillHistory(db *gorm.DB, billID int64, limit int) ([]HistoryLine, error) {
	var lines []HistoryLine
	err := db.Raw(`
		SELECT payables.*, cycles.month AS month, bills.name AS bill_name,
		       bills.card_last4 AS card_last4, bills.due_day AS due_day
		FROM payables
		JOIN cycles ON cycles.id = payables.cycle_id
		JOIN bills  ON bills.id = payables.bill_id
		WHERE payables.bill_id = ?
		ORDER BY cycles.month DESC
		LIMIT ?`, billID, limit).Scan(&lines).Error
	if err != nil {
		return nil, fmt.Errorf("list history for bill %d: %w", billID, err)
	}
	return lines, nil
}

// ActorNames maps each Telegram id that has ever acted to the name it last acted under.
// The audit trail is the only place a person's name is written down — there is no users
// table, because the allowlist is configuration — so a report that wants to say who paid
// something reads it from there.
func ActorNames(db *gorm.DB) (map[int64]string, error) {
	var rows []struct {
		ActorTelegramID int64
		ActorName       string
	}
	err := db.Raw(`
		SELECT actor_telegram_id, actor_name
		FROM events
		WHERE id IN (SELECT MAX(id) FROM events GROUP BY actor_telegram_id)`).Scan(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("list actor names: %w", err)
	}

	names := make(map[int64]string, len(rows))
	for _, row := range rows {
		names[row.ActorTelegramID] = row.ActorName
	}
	return names, nil
}
