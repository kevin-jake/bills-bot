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

// payableIn gives a Bill a Payable in a month, which is what a history reads back.
func payableIn(t *testing.T, db *gorm.DB, cycleID, billID int64, amount *int64) storage.Payable {
	t.Helper()
	p := storage.Payable{CycleID: cycleID, BillID: billID, Status: "due", Channel: "kevin_direct",
		AmountCents: amount}
	require.NoError(t, storage.CreatePayable(db, &p))
	return p
}

func TestListBillHistoryComesNewestMonthFirstAndStopsAtTheLimit(t *testing.T) {
	db := storagetest.Open(t)
	bills, err := storage.ListActiveBills(db)
	require.NoError(t, err)
	bill := bills[0]

	amount := int64(249900)
	// Out of order, so that only the query can put the months right.
	for _, month := range []string{"2026-08", "2026-10", "2026-09"} {
		cycle := createCycle(t, db, month, false)
		payableIn(t, db, cycle.ID, bill.ID, &amount)
	}
	other := bills[1]
	october, err := storage.FindCycleByMonth(db, "2026-10")
	require.NoError(t, err)
	payableIn(t, db, october.ID, other.ID, nil)

	lines, err := storage.ListBillHistory(db, bill.ID, 12)

	require.NoError(t, err)
	require.Len(t, lines, 3, "only this bill's months")
	assert.Equal(t, "2026-10", lines[0].Month)
	assert.Equal(t, "2026-08", lines[2].Month)
	assert.Equal(t, bill.Name, lines[0].BillName, "the name is read live from the bill")
	require.NotNil(t, lines[0].CardLast4)
	assert.Equal(t, "2943", *lines[0].CardLast4)

	capped, err := storage.ListBillHistory(db, bill.ID, 2)
	require.NoError(t, err)
	assert.Len(t, capped, 2, "the limit cuts the oldest months off")
}

func TestListBillHistoryOfABillNoMonthHas(t *testing.T) {
	db := storagetest.Open(t)

	lines, err := storage.ListBillHistory(db, 12345, 12)

	require.NoError(t, err)
	assert.Empty(t, lines)
}

func TestActorNamesKeepTheNameEachPersonLastActedUnder(t *testing.T) {
	db := storagetest.Open(t)
	for _, e := range []storage.Event{
		{OccurredAt: time.Now().UTC(), ActorTelegramID: 111, ActorName: "Kev", Action: "bill.add"},
		{OccurredAt: time.Now().UTC(), ActorTelegramID: 222, ActorName: "Sheena", Action: "bill.add"},
		{OccurredAt: time.Now().UTC(), ActorTelegramID: 111, ActorName: "Kevin", Action: "bill.add"},
	} {
		require.NoError(t, storage.AppendEvent(db, &e))
	}

	names, err := storage.ActorNames(db)

	require.NoError(t, err)
	assert.Equal(t, map[int64]string{111: "Kevin", 222: "Sheena"}, names)
}

func TestListCyclesAndBillsCoverWhatReportsRead(t *testing.T) {
	db := storagetest.Open(t)
	createCycle(t, db, "2026-09", true)
	createCycle(t, db, "2026-08", false)

	cycles, err := storage.ListCycles(db)
	require.NoError(t, err)
	require.Len(t, cycles, 2)
	assert.Equal(t, "2026-08", cycles[0].Month, "oldest month first, closed or not")

	bills, err := storage.ListBills(db)
	require.NoError(t, err)
	archived := time.Now().UTC()
	bills[0].ArchivedAt = &archived
	require.NoError(t, db.Model(&storage.Bill{}).Where("id = ?", bills[0].ID).
		Update("archived_at", archived).Error)

	active, err := storage.ListActiveBills(db)
	require.NoError(t, err)
	all, err := storage.ListBills(db)
	require.NoError(t, err)
	assert.Len(t, active, len(all)-1, "an archived bill leaves the standing list")

	found, err := storage.FindBill(db, bills[0].ID)
	require.NoError(t, err)
	require.NotNil(t, found)
	assert.True(t, found.Archived(), "reports still find it")

	missing, err := storage.FindBill(db, 12345)
	require.NoError(t, err)
	assert.Nil(t, missing)
}
