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
		{Kind: board.KindMarkPaid, ID: 8},
		{Kind: board.KindUndo, ID: 9},
		{Kind: board.KindTransferChannel, ID: 7, Arg: "psb"},
		{Kind: board.KindTransferUndo, ID: 2},
	} {
		decoded, err := board.DecodeCallback(cb.Encode())

		require.NoError(t, err)
		assert.Equal(t, cb, decoded)
		assert.LessOrEqual(t, len(cb.Encode()), 20, "callback data stays short")
	}
}

func TestDecodeCallbackRefusesWhatTheBotDidNotWrite(t *testing.T) {
	for _, data := range []string{"", "b", "b:", "b:x", "b:-1", "b:0", "z:1", "refresh",
		"b:1:bdo", "tc:1", "tc:1:gcash", "tc:x:bdo", "tu:1:bdo"} {
		_, err := board.DecodeCallback(data)

		assert.ErrorIs(t, err, board.ErrCallbackInvalid, "data %q", data)
	}
}

func TestMenuOffersEachActionOnlyWhenItWouldBeAccepted(t *testing.T) {
	unknown := domain.Payable{ID: 42, CycleID: 3, BillName: "Water", Status: domain.StatusDue}
	known := unknown
	known.AmountCents = cents(0)
	paid := known
	paid.Status = domain.StatusPaid

	assert.Equal(t, [][]board.Button{
		{{Text: "💰 Set amount", Data: "a:42"}, {Text: "↩ Undo", Data: "u:42"}},
		{{Text: "« Back", Data: "k:3"}},
	}, board.Menu(unknown), "nothing to mark paid until the amount is known")
	assert.Equal(t, [][]board.Button{
		{{Text: "💰 Change amount", Data: "a:42"}, {Text: "✓ Mark paid", Data: "p:42"}, {Text: "↩ Undo", Data: "u:42"}},
		{{Text: "« Back", Data: "k:3"}},
	}, board.Menu(known))
	assert.Len(t, board.Menu(paid)[0], 2, "a paid bill cannot be paid again")
}

func TestTransferPickerOffersEachAccountTheCycleUses(t *testing.T) {
	snap := fourBills()
	snap.Payables = append(snap.Payables, domain.Payable{ID: 15, SectionID: 2, BillName: "HSBC CC",
		Channel: domain.SheenaBPI, AmountCents: cents(500000), Status: domain.StatusDue})

	rows := board.TransferPicker(snap)

	assert.Equal(t, [][]board.Button{
		{{Text: "💸 Sheena BDO · sent ₱17,000.00", Data: "tc:7:bdo"}, {Text: "↩ Undo", Data: "tu:3"}},
		{{Text: "💸 Sheena BPI · need ₱5,000.00", Data: "tc:7:bpi"}},
	}, rows)

	snap.Transfers = nil
	assert.Equal(t, "💸 Sheena BDO · need ₱16,639.31?", board.TransferPicker(snap)[0][0].Text,
		"a need that is not final is marked")
}
