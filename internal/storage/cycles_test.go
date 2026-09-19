package storage_test

import (
	"testing"
	"time"

	"github.com/kevin-jake/bills-bot/internal/storage"
	"github.com/kevin-jake/bills-bot/internal/storage/storagetest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func createCycle(t *testing.T, db *gorm.DB, month string, closed bool) storage.Cycle {
	t.Helper()
	c := storage.Cycle{Month: month, OpenedAt: time.Now().UTC()}
	if closed {
		now := time.Now().UTC()
		c.ClosedAt = &now
	}
	require.NoError(t, storage.CreateCycle(db, &c))
	return c
}

func TestLatestCyclePrefersAnOpenMonthOverALaterClosedOne(t *testing.T) {
	db := storagetest.Open(t)

	none, err := storage.LatestCycle(db)
	require.NoError(t, err)
	assert.Nil(t, none, "no cycle yet is an answer, not an error")

	august := createCycle(t, db, "2026-08", false)
	createCycle(t, db, "2026-09", true)

	latest, err := storage.LatestCycle(db)
	require.NoError(t, err)
	require.NotNil(t, latest)
	assert.Equal(t, august.ID, latest.ID)

	october := createCycle(t, db, "2026-10", false)
	latest, err = storage.LatestCycle(db)
	require.NoError(t, err)
	assert.Equal(t, october.ID, latest.ID, "among open cycles the newest month wins")

	open, err := storage.ListOpenCycles(db)
	require.NoError(t, err)
	require.Len(t, open, 2)
	assert.Equal(t, "2026-08", open[0].Month, "oldest open month first")
}

func TestCycleMonthIsUnique(t *testing.T) {
	db := storagetest.Open(t)
	createCycle(t, db, "2026-09", false)

	err := storage.CreateCycle(db, &storage.Cycle{Month: "2026-09", OpenedAt: time.Now().UTC()})

	require.Error(t, err)
}

func TestPayableLinesComeInBoardOrderWithTheBillName(t *testing.T) {
	db := storagetest.Open(t)
	cycle := createCycle(t, db, "2026-09", false)

	bills, err := storage.ListActiveBills(db)
	require.NoError(t, err)
	// Insert in reverse, so the order can only come from the query.
	for i := len(bills) - 1; i >= 0; i-- {
		require.NoError(t, storage.CreatePayable(db, &storage.Payable{
			CycleID: cycle.ID, BillID: bills[i].ID, Status: "due", Channel: bills[i].Channel,
			CardName: bills[i].CardName,
		}))
	}

	lines, err := storage.ListPayableLines(db, cycle.ID)

	require.NoError(t, err)
	require.Len(t, lines, 16)
	assert.Equal(t, "UnionBank CC", lines[0].BillName)
	assert.Equal(t, "BDO JCB CC", lines[1].BillName)
	assert.Equal(t, "GCash funds", lines[15].BillName)
	assert.Nil(t, lines[0].AmountCents, "a new payable's amount is unknown")
	assert.NotZero(t, lines[0].ID)
	assert.NotZero(t, lines[0].SectionID)
}

func TestSetBoardMessage(t *testing.T) {
	db := storagetest.Open(t)
	cycle := createCycle(t, db, "2026-09", false)

	require.NoError(t, storage.SetBoardMessage(db, cycle.ID, -100123, 55))

	found, err := storage.FindCycleByMonth(db, "2026-09")
	require.NoError(t, err)
	require.NotNil(t, found.BoardChatID)
	require.NotNil(t, found.BoardMessageID)
	assert.Equal(t, int64(-100123), *found.BoardChatID)
	assert.Equal(t, 55, *found.BoardMessageID)
}

func TestUpdatePayableStateWritesNullsBack(t *testing.T) {
	db := storagetest.Open(t)
	cycle := createCycle(t, db, "2026-09", false)
	bills, err := storage.ListActiveBills(db)
	require.NoError(t, err)
	p := storage.Payable{CycleID: cycle.ID, BillID: bills[0].ID, Status: "due", Channel: bills[0].Channel}
	require.NoError(t, storage.CreatePayable(db, &p))

	zero, paidBy, paidAt := int64(0), int64(0), time.Now().UTC()
	p.AmountCents, p.Status, p.PaidBy, p.PaidAt = &zero, "paid", &paidBy, &paidAt
	require.NoError(t, storage.UpdatePayableState(db, &p))

	found, err := storage.FindPayableLine(db, p.ID)
	require.NoError(t, err)
	require.NotNil(t, found)
	assert.Equal(t, "UnionBank CC", found.BillName)
	assert.Equal(t, "paid", found.Status)
	require.NotNil(t, found.AmountCents)
	assert.Equal(t, int64(0), *found.AmountCents, "zero is stored as zero, not as unknown")
	require.NotNil(t, found.PaidBy)

	amount := int64(249900)
	p.AmountCents, p.Status, p.PaidBy, p.PaidAt = &amount, "due", nil, nil
	require.NoError(t, storage.UpdatePayableState(db, &p))

	found, err = storage.FindPayableLine(db, p.ID)
	require.NoError(t, err)
	assert.Equal(t, "due", found.Status)
	assert.Equal(t, int64(249900), *found.AmountCents)
	assert.Nil(t, found.PaidBy, "clearing who paid writes NULL")
	assert.Nil(t, found.PaidAt)
}

func TestFindPayableLineAndTransferReturnNilWhenMissing(t *testing.T) {
	db := storagetest.Open(t)
	cycle := createCycle(t, db, "2026-09", false)

	line, err := storage.FindPayableLine(db, 12345)
	require.NoError(t, err)
	assert.Nil(t, line)

	transfer, err := storage.FindTransfer(db, cycle.ID, "sheena_bpi")
	require.NoError(t, err)
	assert.Nil(t, transfer)

	require.NoError(t, db.Create(&storage.Transfer{
		CycleID: cycle.ID, Channel: "sheena_bpi", SentCents: 100, SentAt: time.Now().UTC(), SentBy: 111,
	}).Error)
	transfer, err = storage.FindTransfer(db, cycle.ID, "sheena_bpi")
	require.NoError(t, err)
	require.NotNil(t, transfer)
	assert.Equal(t, int64(100), transfer.SentCents)
}

func TestSetCycleClosedClosesAndReopens(t *testing.T) {
	db := storagetest.Open(t)
	cycle := createCycle(t, db, "2026-09", false)
	at := time.Date(2026, 9, 16, 2, 0, 0, 0, time.UTC)

	require.NoError(t, storage.SetCycleClosed(db, cycle.ID, &at))
	found, err := storage.FindCycle(db, cycle.ID)
	require.NoError(t, err)
	require.NotNil(t, found.ClosedAt)
	assert.True(t, at.Equal(*found.ClosedAt))

	require.NoError(t, storage.SetCycleClosed(db, cycle.ID, nil))
	found, err = storage.FindCycle(db, cycle.ID)
	require.NoError(t, err)
	assert.Nil(t, found.ClosedAt, "reopening writes NULL")
}

func TestSaveTransferInsertsThenUpdatesAndDeleteRemoves(t *testing.T) {
	db := storagetest.Open(t)
	cycle := createCycle(t, db, "2026-09", false)
	transfer := storage.Transfer{
		CycleID: cycle.ID, Channel: "sheena_bdo", SentCents: 100, SentAt: time.Now().UTC(), SentBy: 111,
	}

	require.NoError(t, storage.SaveTransfer(db, &transfer))
	require.NotZero(t, transfer.ID)
	transfer.SentCents, transfer.SentBy = 250, 222
	require.NoError(t, storage.SaveTransfer(db, &transfer))

	found, err := storage.FindTransferByID(db, transfer.ID)
	require.NoError(t, err)
	require.NotNil(t, found)
	assert.Equal(t, int64(250), found.SentCents)
	assert.Equal(t, int64(222), found.SentBy)

	require.NoError(t, storage.DeleteTransfer(db, transfer.ID))
	found, err = storage.FindTransferByID(db, transfer.ID)
	require.NoError(t, err)
	assert.Nil(t, found)
}

func TestLatestPayableEventPassesOverOtherKinds(t *testing.T) {
	db := storagetest.Open(t)
	cycle := createCycle(t, db, "2026-09", false)
	bills, err := storage.ListActiveBills(db)
	require.NoError(t, err)
	p := storage.Payable{CycleID: cycle.ID, BillID: bills[0].ID, Status: "due", Channel: bills[0].Channel}
	require.NoError(t, storage.CreatePayable(db, &p))
	payableID := p.ID

	found, err := storage.LatestPayableEvent(db, payableID)
	require.NoError(t, err)
	assert.Nil(t, found, "nothing done yet")

	for _, action := range []string{"payable.set_amount", "payable.mark_paid", "transfer.fund"} {
		require.NoError(t, storage.AppendEvent(db, &storage.Event{
			OccurredAt: time.Now().UTC(), ActorName: "Kevin", Action: action, PayableID: &payableID,
		}))
	}

	found, err = storage.LatestPayableEvent(db, payableID)
	require.NoError(t, err)
	require.NotNil(t, found)
	assert.Equal(t, "payable.mark_paid", found.Action)
}
