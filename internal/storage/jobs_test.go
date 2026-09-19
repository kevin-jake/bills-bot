package storage_test

import (
	"testing"
	"time"

	"github.com/kevin-jake/bills-bot/internal/storage"
	"github.com/kevin-jake/bills-bot/internal/storage/storagetest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAJobIsClaimedExactlyOnce(t *testing.T) {
	db := storagetest.Open(t)
	at := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)

	done, err := storage.JobDone(db, "open:2026-10")
	require.NoError(t, err)
	assert.False(t, done)

	claimed, err := storage.ClaimJob(db, "open:2026-10", at)
	require.NoError(t, err)
	assert.True(t, claimed, "the first caller does the work")

	claimed, err = storage.ClaimJob(db, "open:2026-10", at.Add(time.Minute))
	require.NoError(t, err)
	assert.False(t, claimed, "the second finds it already done")

	done, err = storage.JobDone(db, "open:2026-10")
	require.NoError(t, err)
	assert.True(t, done)
}

func TestJobsOfDifferentMonthsAreSeparate(t *testing.T) {
	db := storagetest.Open(t)
	at := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)

	_, err := storage.ClaimJob(db, "open:2026-10", at)
	require.NoError(t, err)

	claimed, err := storage.ClaimJob(db, "open:2026-11", at.AddDate(0, 1, 0))
	require.NoError(t, err)
	assert.True(t, claimed, "next month is a job of its own")

	claimed, err = storage.ClaimJob(db, "remind:2026-10", at)
	require.NoError(t, err)
	assert.True(t, claimed, "and so is the reminder")
}
