package storage_test

import (
	"testing"

	"github.com/kevin-jake/bills-bot/internal/storage/storagetest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOpenForTestAppliesSchema(t *testing.T) {
	db := storagetest.Open(t)

	for _, table := range []string{
		"sections", "bills", "cycles", "payables", "transfers", "events", "job_runs",
	} {
		var name string
		err := db.Raw(
			"SELECT name FROM sqlite_master WHERE type = 'table' AND name = ?", table,
		).Scan(&name).Error
		require.NoError(t, err)
		assert.Equal(t, table, name, "table %s should exist after migration", table)
	}
}

func TestForeignKeysAreEnforced(t *testing.T) {
	db := storagetest.OpenEmpty(t)

	// section_id 999 does not exist, so this must be rejected rather than silently stored.
	err := db.Exec(`INSERT INTO bills (name, section_id, channel, display_order)
	                VALUES ('Orphan', 999, 'kevin_direct', 1)`).Error

	require.Error(t, err)
	assert.Contains(t, err.Error(), "FOREIGN KEY")
}

func TestChannelCheckConstraint(t *testing.T) {
	db := storagetest.OpenEmpty(t)
	require.NoError(t, db.Exec(
		`INSERT INTO sections (id, name, display_order) VALUES (1, 'BDO', 1)`).Error)

	t.Run("rejects an unknown channel", func(t *testing.T) {
		err := db.Exec(`INSERT INTO bills (name, section_id, channel, display_order)
		                VALUES ('Nonsense', 1, 'venmo', 1)`).Error
		require.Error(t, err)
	})

	t.Run("charged_to_card requires a card name", func(t *testing.T) {
		err := db.Exec(`INSERT INTO bills (name, section_id, channel, display_order)
		                VALUES ('PLDT', 1, 'charged_to_card', 1)`).Error
		require.Error(t, err)
	})

	t.Run("other channels must not carry a card name", func(t *testing.T) {
		err := db.Exec(`INSERT INTO bills (name, section_id, channel, card_name, display_order)
		                VALUES ('Water', 1, 'kevin_direct', 'BPI Visa CC', 1)`).Error
		require.Error(t, err)
	})

	t.Run("accepts a well formed bill", func(t *testing.T) {
		require.NoError(t, db.Exec(`INSERT INTO bills (name, section_id, channel, display_order)
		                            VALUES ('BDO Home Loan', 1, 'sheena_bdo', 1)`).Error)
	})
}

func TestActiveBillNamesAreUniqueButArchivedNamesAreFree(t *testing.T) {
	db := storagetest.OpenEmpty(t)
	require.NoError(t, db.Exec(
		`INSERT INTO sections (id, name, display_order) VALUES (1, 'Utilities', 1)`).Error)
	require.NoError(t, db.Exec(`INSERT INTO bills (name, section_id, channel, display_order)
	                            VALUES ('Water', 1, 'kevin_direct', 1)`).Error)

	t.Run("duplicate active name is rejected, case-insensitively", func(t *testing.T) {
		err := db.Exec(`INSERT INTO bills (name, section_id, channel, display_order)
		                VALUES ('water', 1, 'kevin_direct', 2)`).Error
		require.Error(t, err)
	})

	t.Run("name is free again once the original is archived", func(t *testing.T) {
		require.NoError(t, db.Exec(
			`UPDATE bills SET archived_at = CURRENT_TIMESTAMP WHERE name = 'Water'`).Error)

		require.NoError(t, db.Exec(`INSERT INTO bills (name, section_id, channel, display_order)
		                            VALUES ('Water', 1, 'kevin_direct', 2)`).Error)
	})
}

func TestOnePayablePerBillPerCycle(t *testing.T) {
	db := storagetest.OpenEmpty(t)
	require.NoError(t, db.Exec(
		`INSERT INTO sections (id, name, display_order) VALUES (1, 'Utilities', 1)`).Error)
	require.NoError(t, db.Exec(`INSERT INTO bills (id, name, section_id, channel, display_order)
	                            VALUES (1, 'Batelec', 1, 'kevin_direct', 1)`).Error)
	require.NoError(t, db.Exec(`INSERT INTO cycles (id, month, opened_at)
	                            VALUES (1, '2026-09', CURRENT_TIMESTAMP)`).Error)
	require.NoError(t, db.Exec(`INSERT INTO payables (cycle_id, bill_id, channel)
	                            VALUES (1, 1, 'kevin_direct')`).Error)

	err := db.Exec(`INSERT INTO payables (cycle_id, bill_id, channel)
	                VALUES (1, 1, 'kevin_direct')`).Error
	require.Error(t, err)
}

func TestTransfersAreOnePerChannelPerCycleAndSheenaOnly(t *testing.T) {
	db := storagetest.Open(t)
	require.NoError(t, db.Exec(`INSERT INTO cycles (id, month, opened_at)
	                            VALUES (1, '2026-09', CURRENT_TIMESTAMP)`).Error)

	require.NoError(t, db.Exec(`INSERT INTO transfers (cycle_id, channel, sent_cents, sent_at, sent_by)
	                            VALUES (1, 'sheena_bdo', 1957014, CURRENT_TIMESTAMP, 111)`).Error)

	t.Run("second transfer on the same channel is rejected", func(t *testing.T) {
		err := db.Exec(`INSERT INTO transfers (cycle_id, channel, sent_cents, sent_at, sent_by)
		                VALUES (1, 'sheena_bdo', 100, CURRENT_TIMESTAMP, 111)`).Error
		require.Error(t, err)
	})

	t.Run("kevin_direct cannot have a transfer", func(t *testing.T) {
		err := db.Exec(`INSERT INTO transfers (cycle_id, channel, sent_cents, sent_at, sent_by)
		                VALUES (1, 'kevin_direct', 100, CURRENT_TIMESTAMP, 111)`).Error
		require.Error(t, err)
	})
}

func TestPayableAmountMayBeUnknownButNeverNegative(t *testing.T) {
	db := storagetest.OpenEmpty(t)
	require.NoError(t, db.Exec(
		`INSERT INTO sections (id, name, display_order) VALUES (1, 'Utilities', 1)`).Error)
	require.NoError(t, db.Exec(`INSERT INTO bills (id, name, section_id, channel, display_order)
	                            VALUES (1, 'Batelec', 1, 'kevin_direct', 1)`).Error)
	require.NoError(t, db.Exec(`INSERT INTO cycles (id, month, opened_at)
	                            VALUES (1, '2026-09', CURRENT_TIMESTAMP)`).Error)

	t.Run("unknown amount is allowed", func(t *testing.T) {
		require.NoError(t, db.Exec(`INSERT INTO payables (cycle_id, bill_id, channel)
		                            VALUES (1, 1, 'kevin_direct')`).Error)

		var count int64
		require.NoError(t, db.Raw(
			"SELECT COUNT(*) FROM payables WHERE amount_cents IS NULL").Scan(&count).Error)
		assert.Equal(t, int64(1), count)
	})

	t.Run("negative amount is rejected", func(t *testing.T) {
		err := db.Exec(`INSERT INTO payables (cycle_id, bill_id, amount_cents, channel)
		                VALUES (1, 1, -1, 'kevin_direct')`).Error
		require.Error(t, err)
	})
}
