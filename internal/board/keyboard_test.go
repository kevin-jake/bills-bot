package board_test

import (
	"testing"

	"github.com/kevin-jake/bills-bot/internal/board"
	"github.com/kevin-jake/bills-bot/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestKeyboardHasAButtonPerBillTwoToARow(t *testing.T) {
	rows := board.Keyboard(fourBills())

	require.Len(t, rows, 3)
	assert.Equal(t, []board.Button{
		{Text: "? BDO JCB CC", Data: "b:11"},
		{Text: "⏳ BDO Home Loan", Data: "b:12"},
	}, rows[0])
	assert.Equal(t, []board.Button{
		{Text: "☐ Internet PLDT", Data: "b:13"},
		{Text: "✓ Water & Sewer", Data: "b:14"},
	}, rows[1])
	assert.Equal(t, []board.Button{
		{Text: "🔄 Refresh", Data: "r:7"},
		{Text: "💸 Transfer", Data: "t:7"},
	}, rows[2])
}

func TestKeyboardClipsLongNamesAndLeavesAnOddBillAlone(t *testing.T) {
	snap := domain.Snapshot{
		Cycle:    domain.Cycle{ID: 1},
		Sections: []domain.Section{{ID: 1, Name: "Misc"}},
		Payables: []domain.Payable{{ID: 5, SectionID: 1, Status: domain.StatusDue,
			BillName: "An extraordinarily long bill name for a button"}},
	}

	rows := board.Keyboard(snap)

	require.Len(t, rows, 2)
	require.Len(t, rows[0], 1)
	assert.Equal(t, "? An extraordinarily long bil…", rows[0][0].Text)
}

func TestCallbackRoundTrips(t *testing.T) {
	for _, cb := range []board.Callback{
		{Kind: board.KindMenu, ID: 42},
		{Kind: board.KindRefresh, ID: 1},
		{Kind: board.KindTransfer, ID: 99999},
		{Kind: board.KindSetAmount, ID: 7},
		{Kind: board.KindBack, ID: 3},
	} {
		decoded, err := board.DecodeCallback(cb.Encode())

		require.NoError(t, err)
		assert.Equal(t, cb, decoded)
		assert.LessOrEqual(t, len(cb.Encode()), 20, "callback data stays short")
	}
}

func TestDecodeCallbackRefusesWhatTheBotDidNotWrite(t *testing.T) {
	for _, data := range []string{"", "b", "b:", "b:x", "b:-1", "b:0", "z:1", "refresh"} {
		_, err := board.DecodeCallback(data)

		assert.ErrorIs(t, err, board.ErrCallbackInvalid, "data %q", data)
	}
}

func TestMenuOffersToSetOrChangeTheAmountAndToGoBack(t *testing.T) {
	unknown := domain.Payable{ID: 42, CycleID: 3, BillName: "Water"}
	amount := int64(0)
	known := domain.Payable{ID: 42, CycleID: 3, BillName: "Water", AmountCents: &amount}

	rows := board.Menu(unknown)

	assert.Equal(t, [][]board.Button{
		{{Text: "💰 Set amount", Data: "a:42"}},
		{{Text: "« Back", Data: "k:3"}},
	}, rows)
	assert.Equal(t, "💰 Change amount", board.Menu(known)[0][0].Text)
}
