package tracker_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/kevin-jake/bills-bot/internal/domain"
	"github.com/kevin-jake/bills-bot/internal/tracker"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// payableNamed opens September and returns the Payable for the named Bill.
func payableNamed(t *testing.T, tr *tracker.Tracker, name string) domain.Payable {
	t.Helper()
	snap, _, err := tr.OpenCycle(kevin, september)
	require.NoError(t, err)
	for _, p := range snap.Payables {
		if p.BillName == name {
			return p
		}
	}
	t.Fatalf("no payable for %s", name)
	return domain.Payable{}
}

func TestSetAmountRecordsTheAmountAndReturnsTheFreshCycle(t *testing.T) {
	tr, _ := newTracker(t)
	p := payableNamed(t, tr, "RCBC JCB CC")

	change, err := tr.SetAmount(kevin, p.ID, 10343123)

	require.NoError(t, err)
	assert.False(t, change.Before.AmountKnown())
	require.True(t, change.After.AmountKnown())
	assert.Equal(t, int64(10343123), *change.After.AmountCents)
	assert.Equal(t, domain.StatusDue, change.After.Status)
	assert.Equal(t, 15, change.Snap.UnknownCount(), "the snapshot already shows the new amount")

	again, _, err := tr.Payable(p.ID)
	require.NoError(t, err)
	assert.Equal(t, int64(10343123), *again.AmountCents)
}

func TestSetAmountZeroPaysAtOnce(t *testing.T) {
	tr, _ := newTracker(t)
	p := payableNamed(t, tr, "Water")

	change, err := tr.SetAmount(kevin, p.ID, 0)

	require.NoError(t, err)
	assert.Equal(t, domain.StatusPaid, change.After.Status)
	assert.True(t, change.After.AutoPaid())
	assert.NotNil(t, change.After.PaidAt)
	assert.Equal(t, 1, change.Snap.PaidCount())
}

func TestSetAmountAfterAZeroGoesBackToFundedWhenATransferWasSent(t *testing.T) {
	tr, db := newTracker(t)
	p := payableNamed(t, tr, "HSBC Mastercard CC")
	_, err := tr.SetAmount(kevin, p.ID, 0)
	require.NoError(t, err)
	require.NoError(t, db.Exec(`INSERT INTO transfers (cycle_id, channel, sent_cents, sent_at, sent_by)
		VALUES (?, 'sheena_bpi', 100, CURRENT_TIMESTAMP, 111)`, p.CycleID).Error)

	change, err := tr.SetAmount(kevin, p.ID, 500000)

	require.NoError(t, err)
	assert.Equal(t, domain.StatusFunded, change.After.Status)
	assert.Nil(t, change.After.PaidBy)
	assert.Nil(t, change.After.PaidAt)
}

func TestSetAmountAppendsAnEventHoldingTheStateBeforeAndAfter(t *testing.T) {
	tr, db := newTracker(t)
	p := payableNamed(t, tr, "Batelec")

	_, err := tr.SetAmount(kevin, p.ID, 0)
	require.NoError(t, err)

	row := lastEvent(t, db)
	assert.Equal(t, "payable.set_amount", row.Action)
	assert.Equal(t, "Kevin", row.ActorName)
	assert.Equal(t, int64(111), row.ActorTelegramID)
	require.NotNil(t, row.PayableID)
	assert.Equal(t, p.ID, *row.PayableID)
	require.NotNil(t, row.CycleID)
	assert.Equal(t, p.CycleID, *row.CycleID)
	require.NotNil(t, row.BillID)
	assert.Equal(t, p.BillID, *row.BillID)

	var before, after tracker.PayableState
	require.NoError(t, json.Unmarshal([]byte(row.BeforeJSON), &before))
	require.NoError(t, json.Unmarshal([]byte(row.AfterJSON), &after))
	assert.Equal(t, tracker.PayableState{Status: "due"}, before, "undo restores exactly this")
	assert.Equal(t, "paid", after.Status)
	require.NotNil(t, after.AmountCents)
	assert.Equal(t, int64(0), *after.AmountCents)
	require.NotNil(t, after.PaidBy)
	assert.Equal(t, domain.AutoPaidBy, *after.PaidBy)
	assert.NotNil(t, after.PaidAt)
	assert.Contains(t, row.BeforeJSON, `"amount_cents":null`, "unknown is written, not left out")
}

func TestSetAmountRefusals(t *testing.T) {
	tr, db := newTracker(t)
	p := payableNamed(t, tr, "Water")

	_, err := tr.SetAmount(kevin, 99999, 100)
	assert.ErrorIs(t, err, tracker.ErrPayableUnknown)

	require.NoError(t, db.Exec("UPDATE cycles SET closed_at = CURRENT_TIMESTAMP WHERE id = ?",
		p.CycleID).Error)
	_, err = tr.SetAmount(kevin, p.ID, 100)
	assert.ErrorIs(t, err, tracker.ErrCycleClosed)
	assert.ErrorContains(t, err, "September 2026")

	var events int64
	require.NoError(t, db.Raw("SELECT COUNT(*) FROM events WHERE action = 'payable.set_amount'").
		Scan(&events).Error)
	assert.Zero(t, events, "a refusal leaves no trace")
}

func TestPayableRefusesAnIDItDoesNotKnow(t *testing.T) {
	tr, _ := newTracker(t)

	_, _, err := tr.Payable(42)

	assert.ErrorIs(t, err, tracker.ErrPayableUnknown)
}

type eventRow struct {
	Action          string
	ActorName       string
	ActorTelegramID int64
	CycleID         *int64
	BillID          *int64
	PayableID       *int64
	BeforeJSON      string
	AfterJSON       string
	OccurredAt      time.Time
}

func lastEvent(t *testing.T, db *gorm.DB) eventRow {
	t.Helper()
	var row eventRow
	require.NoError(t, db.Raw(`SELECT action, actor_name, actor_telegram_id, cycle_id, bill_id,
		payable_id, before_json, after_json, occurred_at FROM events ORDER BY id DESC LIMIT 1`).
		Scan(&row).Error)
	return row
}

var sheena = tracker.Actor{TelegramID: 222, Name: "Sheena"}

func TestMarkPaidRecordsWhoPaid(t *testing.T) {
	tr, db := newTracker(t)
	p := payableNamed(t, tr, "Batelec")
	_, err := tr.SetAmount(kevin, p.ID, 312050)
	require.NoError(t, err)

	change, err := tr.MarkPaid(sheena, p.ID)

	require.NoError(t, err)
	assert.Equal(t, domain.StatusPaid, change.After.Status)
	require.NotNil(t, change.After.PaidBy)
	assert.Equal(t, int64(222), *change.After.PaidBy)
	assert.Equal(t, 1, change.Snap.PaidCount())
	assert.False(t, change.Closed, "fifteen bills are still unpaid")
	assert.Equal(t, "payable.mark_paid", lastEvent(t, db).Action)
}

func TestMarkPaidRefusals(t *testing.T) {
	tr, db := newTracker(t)
	p := payableNamed(t, tr, "Batelec")

	_, err := tr.MarkPaid(kevin, p.ID)
	assert.ErrorIs(t, err, domain.ErrAmountUnknown)

	_, err = tr.SetAmount(kevin, p.ID, 100)
	require.NoError(t, err)
	_, err = tr.MarkPaid(kevin, p.ID)
	require.NoError(t, err)
	_, err = tr.MarkPaid(kevin, p.ID)
	assert.ErrorIs(t, err, domain.ErrAlreadyPaid)

	var events int64
	require.NoError(t, db.Raw("SELECT COUNT(*) FROM events WHERE action = 'payable.mark_paid'").
		Scan(&events).Error)
	assert.Equal(t, int64(1), events, "only the payment that happened is recorded")
}

func TestUndoRestoresTheStateBeforeTheLastChange(t *testing.T) {
	tr, db := newTracker(t)
	p := payableNamed(t, tr, "Batelec")
	_, err := tr.SetAmount(kevin, p.ID, 100)
	require.NoError(t, err)
	_, err = tr.SetAmount(kevin, p.ID, 200)
	require.NoError(t, err)
	_, err = tr.MarkPaid(kevin, p.ID)
	require.NoError(t, err)

	change, err := tr.Undo(sheena, p.ID)

	require.NoError(t, err)
	assert.Equal(t, domain.StatusDue, change.After.Status)
	assert.Equal(t, int64(200), *change.After.AmountCents)
	assert.Nil(t, change.After.PaidBy)
	assert.Nil(t, change.After.PaidAt)
	row := lastEvent(t, db)
	assert.Equal(t, "payable.undo", row.Action)
	assert.Equal(t, "Sheena", row.ActorName)

	again, err := tr.Undo(sheena, p.ID)

	require.NoError(t, err)
	assert.Equal(t, domain.StatusPaid, again.After.Status, "a second undo takes back the first")
}

func TestUndoCanGoBackToAnUnknownAmount(t *testing.T) {
	tr, _ := newTracker(t)
	p := payableNamed(t, tr, "Water")
	_, err := tr.SetAmount(kevin, p.ID, 0)
	require.NoError(t, err)

	change, err := tr.Undo(kevin, p.ID)

	require.NoError(t, err)
	assert.False(t, change.After.AmountKnown())
	assert.Equal(t, domain.StatusDue, change.After.Status)
}

func TestUndoWithNothingDoneIsRefused(t *testing.T) {
	tr, _ := newTracker(t)
	p := payableNamed(t, tr, "Water")

	_, err := tr.Undo(kevin, p.ID)

	assert.ErrorIs(t, err, tracker.ErrNothingToUndo)
}

func TestUndoFitsTheStatusToTheTransfersAsTheyNowStand(t *testing.T) {
	tr, _ := newTracker(t)
	p := payableNamed(t, tr, "BDO Home Loan")
	_, err := tr.SetAmount(kevin, p.ID, 1663931)
	require.NoError(t, err)
	_, err = tr.RecordTransfer(kevin, p.CycleID, domain.SheenaBDO, 2000000)
	require.NoError(t, err)

	// The amount was set while the channel was unfunded, but the Transfer still stands.
	change, err := tr.Undo(sheena, p.ID)

	require.NoError(t, err)
	assert.False(t, change.After.AmountKnown())
	assert.Equal(t, domain.StatusFunded, change.After.Status)
}

func TestUndoingAPaymentOnAFundedChannelLeavesItFunded(t *testing.T) {
	tr, _ := newTracker(t)
	p := payableNamed(t, tr, "BDO Home Loan")
	_, err := tr.SetAmount(kevin, p.ID, 1663931)
	require.NoError(t, err)
	_, err = tr.RecordTransfer(kevin, p.CycleID, domain.SheenaBDO, 2000000)
	require.NoError(t, err)
	_, err = tr.MarkPaid(sheena, p.ID)
	require.NoError(t, err)

	change, err := tr.Undo(sheena, p.ID)

	require.NoError(t, err)
	assert.Equal(t, domain.StatusFunded, change.After.Status)
	assert.Equal(t, int64(1663931), *change.After.AmountCents)
}

// payEverything sets every Payable in the Cycle to zero, which pays each at once.
func payEverything(t *testing.T, tr *tracker.Tracker, snap domain.Snapshot) tracker.Change {
	t.Helper()
	var last tracker.Change
	for _, p := range snap.Payables {
		change, err := tr.SetAmount(kevin, p.ID, 0)
		require.NoError(t, err)
		last = change
	}
	return last
}

func TestPayingTheLastBillClosesTheCycle(t *testing.T) {
	tr, db := newTracker(t)
	snap, _, err := tr.OpenCycle(kevin, september)
	require.NoError(t, err)

	last := payEverything(t, tr, snap)

	assert.True(t, last.Closed)
	assert.True(t, last.Snap.Cycle.Closed())
	assert.Equal(t, "cycle.close", lastEvent(t, db).Action)
	current, err := tr.Snapshot(snap.Cycle.ID)
	require.NoError(t, err)
	assert.True(t, current.Cycle.Closed(), "closing is written down")

	var closes int64
	require.NoError(t, db.Raw("SELECT COUNT(*) FROM events WHERE action = 'cycle.close'").
		Scan(&closes).Error)
	assert.Equal(t, int64(1), closes, "only the last payment closes it")
}

func TestUndoReopensAClosedCycle(t *testing.T) {
	tr, db := newTracker(t)
	snap, _, err := tr.OpenCycle(kevin, september)
	require.NoError(t, err)
	last := payEverything(t, tr, snap)

	change, err := tr.Undo(kevin, last.After.ID)

	require.NoError(t, err)
	assert.True(t, change.Reopened)
	assert.False(t, change.Snap.Cycle.Closed())
	assert.Equal(t, "cycle.reopen", lastEvent(t, db).Action)

	_, err = tr.SetAmount(kevin, last.After.ID, 0)
	require.NoError(t, err, "a reopened month takes amounts again")
}

func TestMarkPaidAndSetAmountRefuseAClosedCycle(t *testing.T) {
	tr, _ := newTracker(t)
	snap, _, err := tr.OpenCycle(kevin, september)
	require.NoError(t, err)
	last := payEverything(t, tr, snap)

	_, err = tr.SetAmount(kevin, last.After.ID, 100)
	assert.ErrorIs(t, err, tracker.ErrCycleClosed)
	_, err = tr.MarkPaid(kevin, last.After.ID)
	assert.ErrorIs(t, err, tracker.ErrCycleClosed)
}
