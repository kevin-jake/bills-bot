package tracker_test

import (
	"testing"

	"github.com/kevin-jake/bills-bot/internal/domain"
	"github.com/kevin-jake/bills-bot/internal/tracker"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// payableOf finds a Bill's Payable within a month that is already open.
func payableOf(t *testing.T, tr *tracker.Tracker, month domain.Month, name string) domain.Payable {
	t.Helper()
	snap, err := tr.MonthSnapshot(month)
	require.NoError(t, err)
	for _, p := range snap.Payables {
		if p.BillName == name {
			return p
		}
	}
	t.Fatalf("no payable for %s in %s", name, month)
	return domain.Payable{}
}

func TestHistoryReadsABillAcrossTheMonthsNewestFirst(t *testing.T) {
	tr, _ := newTracker(t)
	for _, month := range []domain.Month{august, september} {
		_, _, err := tr.OpenCycle(kevin, month)
		require.NoError(t, err)
	}

	_, err := tr.SetAmount(kevin, payableOf(t, tr, august, "RCBC JCB CC").ID, 9800000)
	require.NoError(t, err)
	paid := payableOf(t, tr, august, "RCBC JCB CC")
	_, err = tr.MarkPaid(sheena, paid.ID)
	require.NoError(t, err)
	_, err = tr.SetAmount(kevin, payableOf(t, tr, september, "RCBC JCB CC").ID, 10343123)
	require.NoError(t, err)

	history, err := tr.History(paid.BillID, 12)

	require.NoError(t, err)
	assert.Equal(t, "RCBC JCB CC ••1006", history.Bill, "named as the bill reads now")
	require.Len(t, history.Entries, 2)
	assert.Equal(t, september, history.Entries[0].Month)
	assert.Equal(t, int64(10343123), *history.Entries[0].Payable.AmountCents)
	assert.Equal(t, "", history.Entries[0].PaidBy, "nobody has paid September's")
	assert.Equal(t, august, history.Entries[1].Month)
	assert.Equal(t, "Sheena", history.Entries[1].PaidBy, "read from the audit trail")

	average, months := history.Average()
	assert.Equal(t, int64(10071561), average)
	assert.Equal(t, 2, months)
}

func TestHistoryOfAMonthTickedOffByNobody(t *testing.T) {
	tr, _ := newTracker(t)
	p := payableNamed(t, tr, "Water")
	_, err := tr.SetAmount(kevin, p.ID, 0)
	require.NoError(t, err)

	history, err := tr.History(p.BillID, 12)

	require.NoError(t, err)
	require.Len(t, history.Entries, 1)
	assert.Equal(t, "nobody", history.Entries[0].PaidBy, "a zero amount pays itself")
}

func TestHistoryOfABillThatDoesNotExist(t *testing.T) {
	tr, _ := newTracker(t)

	_, err := tr.History(12345, 12)

	require.ErrorIs(t, err, tracker.ErrBillUnknown)
}

func TestAllBillsKeepArchivedOnesSoAHistoryCanStillBeAskedFor(t *testing.T) {
	tr, db := newTracker(t)

	bills, err := tr.AllBills()
	require.NoError(t, err)
	require.Len(t, bills, 16)

	require.NoError(t, db.Exec("UPDATE bills SET archived_at = datetime('now') WHERE name = 'Bahay'").Error)

	bills, err = tr.AllBills()
	require.NoError(t, err)
	assert.Len(t, bills, 16, "an archived bill is still a bill a report may be asked about")
	list, err := tr.BillList()
	require.NoError(t, err)
	counted := 0
	for _, section := range list {
		counted += len(section.Bills)
	}
	assert.Equal(t, 15, counted, "but it has left the standing list")
}

func TestAllSnapshotsReadEveryMonthOldestFirst(t *testing.T) {
	tr, _ := newTracker(t)

	snaps, err := tr.AllSnapshots()
	require.NoError(t, err)
	assert.Empty(t, snaps, "no month opened yet is an answer, not an error")

	for _, month := range []domain.Month{september, august} {
		_, _, err := tr.OpenCycle(kevin, month)
		require.NoError(t, err)
	}

	snaps, err = tr.AllSnapshots()

	require.NoError(t, err)
	require.Len(t, snaps, 2)
	assert.Equal(t, august, snaps[0].Cycle.Month)
	assert.Len(t, snaps[0].Payables, 16, "each month is read whole, sections and all")
	assert.Len(t, snaps[0].Sections, 9)
}

func TestActorNamesComeFromTheAuditTrail(t *testing.T) {
	tr, _ := newTracker(t)
	p := payableNamed(t, tr, "Batelec")
	_, err := tr.SetAmount(sheena, p.ID, 300000)
	require.NoError(t, err)

	names, err := tr.ActorNames()

	require.NoError(t, err)
	assert.Equal(t, "Kevin", names.Of(&kevin.TelegramID), "opening the month was Kevin's")
	assert.Equal(t, "Sheena", names.Of(&sheena.TelegramID))
}
