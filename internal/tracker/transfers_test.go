package tracker_test

import (
	"testing"

	"github.com/kevin-jake/bills-bot/internal/domain"
	"github.com/kevin-jake/bills-bot/internal/tracker"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// statuses maps each Bill in snap to its Payable's status.
func statuses(snap domain.Snapshot) map[string]domain.Status {
	found := map[string]domain.Status{}
	for _, p := range snap.Payables {
		found[p.BillName] = p.Status
	}
	return found
}

func TestRecordTransferFundsOnlyTheDuePayablesOnItsChannel(t *testing.T) {
	tr, db := newTracker(t)
	homeLoan := payableNamed(t, tr, "BDO Home Loan")
	unionpay := payableNamed(t, tr, "BDO Unionpay CC")
	_, err := tr.SetAmount(kevin, unionpay.ID, 0)
	require.NoError(t, err)

	change, err := tr.RecordTransfer(kevin, homeLoan.CycleID, domain.SheenaBDO, 2000000)

	require.NoError(t, err)
	assert.Nil(t, change.Before)
	require.NotNil(t, change.After)
	assert.Equal(t, int64(2000000), change.After.SentCents)
	assert.Equal(t, int64(111), change.After.SentBy)
	assert.Len(t, change.Moved, 2, "JCB and the home loan; Unionpay was already paid")

	got := statuses(change.Snap)
	assert.Equal(t, domain.StatusFunded, got["BDO Home Loan"])
	assert.Equal(t, domain.StatusFunded, got["BDO JCB CC"], "an unknown amount is funded too")
	assert.Equal(t, domain.StatusPaid, got["BDO Unionpay CC"])
	assert.Equal(t, domain.StatusDue, got["RCBC JCB CC"], "another channel is untouched")
	require.Len(t, change.Snap.Transfers, 1)

	var funds int64
	require.NoError(t, db.Raw("SELECT COUNT(*) FROM events WHERE action = 'transfer.fund' AND transfer_id = ?",
		change.After.ID).Scan(&funds).Error)
	assert.Equal(t, int64(2), funds, "an event for each bill funded")
}

func TestRecordingATransferAgainReplacesTheAmount(t *testing.T) {
	tr, _ := newTracker(t)
	p := payableNamed(t, tr, "PSBank Car Loan")
	first, err := tr.RecordTransfer(kevin, p.CycleID, domain.SheenaPSBank, 2000000)
	require.NoError(t, err)

	second, err := tr.RecordTransfer(sheena, p.CycleID, domain.SheenaPSBank, 2451500)

	require.NoError(t, err)
	require.NotNil(t, second.Before)
	assert.Equal(t, int64(2000000), second.Before.SentCents)
	assert.Equal(t, first.After.ID, second.After.ID, "still one transfer per channel")
	assert.Equal(t, int64(2451500), second.After.SentCents)
	assert.Empty(t, second.Moved, "nothing was left due")
	require.Len(t, second.Snap.Transfers, 1)
}

func TestUndoTransferPutsFundedBackToDueAndLeavesPaidAlone(t *testing.T) {
	tr, db := newTracker(t)
	homeLoan := payableNamed(t, tr, "BDO Home Loan")
	_, err := tr.SetAmount(kevin, homeLoan.ID, 1663931)
	require.NoError(t, err)
	recorded, err := tr.RecordTransfer(kevin, homeLoan.CycleID, domain.SheenaBDO, 2000000)
	require.NoError(t, err)
	_, err = tr.MarkPaid(sheena, homeLoan.ID)
	require.NoError(t, err)

	change, err := tr.UndoTransfer(kevin, recorded.After.ID)

	require.NoError(t, err)
	assert.Nil(t, change.After)
	assert.Empty(t, change.Snap.Transfers)
	got := statuses(change.Snap)
	assert.Equal(t, domain.StatusPaid, got["BDO Home Loan"])
	assert.Equal(t, domain.StatusDue, got["BDO JCB CC"])
	assert.Equal(t, domain.StatusDue, got["BDO Unionpay CC"])
	assert.Len(t, change.Moved, 2)

	_, err = tr.UndoTransfer(kevin, recorded.After.ID)
	assert.ErrorIs(t, err, tracker.ErrTransferUnknown)

	var undos int64
	require.NoError(t, db.Raw("SELECT COUNT(*) FROM events WHERE action = 'transfer.undo'").
		Scan(&undos).Error)
	assert.Equal(t, int64(1), undos)
}

func TestRecordTransferRefusals(t *testing.T) {
	tr, db := newTracker(t)
	p := payableNamed(t, tr, "Water")

	_, err := tr.RecordTransfer(kevin, p.CycleID, domain.KevinDirect, 100)
	assert.ErrorIs(t, err, tracker.ErrNotSheenaChannel)
	_, err = tr.RecordTransfer(kevin, p.CycleID, domain.SheenaBDO, 0)
	assert.ErrorIs(t, err, domain.ErrTransferEmpty)
	_, err = tr.RecordTransfer(kevin, 9999, domain.SheenaBDO, 100)
	assert.ErrorIs(t, err, tracker.ErrCycleUnknown)

	require.NoError(t, db.Exec("UPDATE cycles SET closed_at = CURRENT_TIMESTAMP WHERE id = ?",
		p.CycleID).Error)
	_, err = tr.RecordTransfer(kevin, p.CycleID, domain.SheenaBDO, 100)
	assert.ErrorIs(t, err, tracker.ErrCycleClosed)

	var transfers int64
	require.NoError(t, db.Raw("SELECT COUNT(*) FROM transfers").Scan(&transfers).Error)
	assert.Zero(t, transfers)
}

func TestRecordTransferRefusesAnAccountNothingIsPaidThrough(t *testing.T) {
	tr, db := newTracker(t)
	p := payableNamed(t, tr, "PSBank Car Loan")
	require.NoError(t, db.Exec("DELETE FROM payables WHERE id = ?", p.ID).Error)

	_, err := tr.RecordTransfer(kevin, p.CycleID, domain.SheenaPSBank, 100)

	assert.ErrorIs(t, err, tracker.ErrChannelUnused)
}

func TestSetAmountOnAFundedChannelAfterAnAutoPayGoesBackToFunded(t *testing.T) {
	tr, _ := newTracker(t)
	p := payableNamed(t, tr, "HSBC CC")
	_, err := tr.SetAmount(kevin, p.ID, 0)
	require.NoError(t, err)
	_, err = tr.RecordTransfer(kevin, p.CycleID, domain.SheenaBPI, 100)
	require.NoError(t, err)

	change, err := tr.SetAmount(kevin, p.ID, 500000)

	require.NoError(t, err)
	assert.Equal(t, domain.StatusFunded, change.After.Status)
}
