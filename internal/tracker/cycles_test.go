package tracker_test

import (
	"testing"
	"time"

	"github.com/kevin-jake/bills-bot/internal/domain"
	"github.com/kevin-jake/bills-bot/internal/tracker"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var (
	august    = domain.Month{Year: 2026, Month: time.August}
	september = domain.Month{Year: 2026, Month: time.September}
)

func TestOpenCycleCopiesTheStandingListWithEveryAmountUnknown(t *testing.T) {
	tr, _ := newTracker(t)

	snap, opened, err := tr.OpenCycle(kevin, september)

	require.NoError(t, err)
	assert.True(t, opened)
	assert.NotZero(t, snap.Cycle.ID)
	assert.Equal(t, september, snap.Cycle.Month)
	assert.False(t, snap.Cycle.Closed())
	assert.False(t, snap.Cycle.HasBoard())

	require.Len(t, snap.Payables, 16)
	for _, p := range snap.Payables {
		assert.False(t, p.AmountKnown(), "%s should open with no amount", p.BillName)
		assert.Equal(t, domain.StatusDue, p.Status, "%s should open due", p.BillName)
	}
	assert.Equal(t, "UnionBank CC", snap.Payables[0].BillName, "payables come in board order")
	assert.Len(t, snap.SectionGroups(), 9)
	assert.Equal(t, 16, snap.UnknownCount())
}

func TestOpenCycleSnapshotsTheChannelAndCard(t *testing.T) {
	tr, _ := newTracker(t)

	snap, _, err := tr.OpenCycle(kevin, september)
	require.NoError(t, err)

	byName := map[string]domain.Payable{}
	for _, p := range snap.Payables {
		byName[p.BillName] = p
	}
	assert.Equal(t, domain.SheenaPSBank, byName["PSBank Car Loan"].Channel)
	assert.Equal(t, domain.ChargedToCard, byName["Internet PLDT"].Channel)
	assert.Equal(t, "RCBC Visa Airmiles", byName["Internet PLDT"].CardName)
}

func TestOpenCycleTwiceChangesNothing(t *testing.T) {
	tr, db := newTracker(t)
	first, _, err := tr.OpenCycle(kevin, september)
	require.NoError(t, err)

	again, opened, err := tr.OpenCycle(kevin, september)

	require.NoError(t, err)
	assert.False(t, opened, "the second open reports the month already existed")
	assert.Equal(t, first.Cycle.ID, again.Cycle.ID)

	var payables, events int64
	require.NoError(t, db.Raw("SELECT COUNT(*) FROM payables").Scan(&payables).Error)
	require.NoError(t, db.Raw("SELECT COUNT(*) FROM events WHERE action = 'cycle.open'").Scan(&events).Error)
	assert.Equal(t, int64(16), payables)
	assert.Equal(t, int64(1), events)
}

func TestOpenCycleAppendsAnEventNamingTheCycle(t *testing.T) {
	tr, db := newTracker(t)

	snap, _, err := tr.OpenCycle(kevin, september)
	require.NoError(t, err)

	var row struct {
		Action    string
		ActorName string
		CycleID   *int64
		AfterJSON string
	}
	require.NoError(t, db.Raw(
		"SELECT action, actor_name, cycle_id, after_json FROM events ORDER BY id DESC LIMIT 1",
	).Scan(&row).Error)
	assert.Equal(t, "cycle.open", row.Action)
	assert.Equal(t, "Kevin", row.ActorName)
	require.NotNil(t, row.CycleID)
	assert.Equal(t, snap.Cycle.ID, *row.CycleID)
	assert.Contains(t, row.AfterJSON, `"month":"2026-09"`)
}

func TestCurrentSnapshot(t *testing.T) {
	tr, db := newTracker(t)

	_, err := tr.CurrentSnapshot()
	require.ErrorIs(t, err, tracker.ErrNoCycle)

	aug, _, err := tr.OpenCycle(kevin, august)
	require.NoError(t, err)
	sep, _, err := tr.OpenCycle(kevin, september)
	require.NoError(t, err)

	current, err := tr.CurrentSnapshot()
	require.NoError(t, err)
	assert.Equal(t, sep.Cycle.ID, current.Cycle.ID, "the newest open month is current")

	require.NoError(t, db.Exec("UPDATE cycles SET closed_at = CURRENT_TIMESTAMP WHERE id = ?",
		sep.Cycle.ID).Error)
	current, err = tr.CurrentSnapshot()
	require.NoError(t, err)
	assert.Equal(t, aug.Cycle.ID, current.Cycle.ID, "an open month outranks a later closed one")
}

func TestMonthSnapshotRefusesAMonthNeverOpened(t *testing.T) {
	tr, _ := newTracker(t)

	_, err := tr.MonthSnapshot(september)

	require.ErrorIs(t, err, tracker.ErrCycleUnknown)
	assert.Contains(t, err.Error(), "September 2026")
}

func TestRecordBoardIsReadBack(t *testing.T) {
	tr, _ := newTracker(t)
	snap, _, err := tr.OpenCycle(kevin, september)
	require.NoError(t, err)

	require.NoError(t, tr.RecordBoard(snap.Cycle.ID, -100123, 77))

	again, err := tr.Snapshot(snap.Cycle.ID)
	require.NoError(t, err)
	assert.True(t, again.Cycle.HasBoard())
	assert.Equal(t, int64(-100123), again.Cycle.BoardChatID)
	assert.Equal(t, 77, again.Cycle.BoardMessageID)
}

func TestAddBillMidCycleJoinsEveryOpenCycleButNotAClosedOne(t *testing.T) {
	tr, db := newTracker(t)
	aug, _, err := tr.OpenCycle(kevin, august)
	require.NoError(t, err)
	sep, _, err := tr.OpenCycle(kevin, september)
	require.NoError(t, err)
	require.NoError(t, db.Exec("UPDATE cycles SET closed_at = CURRENT_TIMESTAMP WHERE id = ?",
		aug.Cycle.ID).Error)

	_, err = tr.AddBill(kevin, tracker.BillSpec{
		Name: "Netflix", Section: "Utilities", Channel: domain.ChargedToCard, CardName: "BPI CC",
	})
	require.NoError(t, err)

	open, err := tr.Snapshot(sep.Cycle.ID)
	require.NoError(t, err)
	require.Len(t, open.Payables, 17)
	netflix := open.Payables[16]
	assert.Equal(t, "Netflix", netflix.BillName)
	assert.Equal(t, domain.StatusDue, netflix.Status)
	assert.False(t, netflix.AmountKnown())
	assert.Equal(t, "BPI CC", netflix.CardName)

	closed, err := tr.Snapshot(aug.Cycle.ID)
	require.NoError(t, err)
	assert.Len(t, closed.Payables, 16, "a settled month is not reopened by a new bill")
}
