package board_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/kevin-jake/bills-bot/internal/board"
	"github.com/kevin-jake/bills-bot/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func cents(v int64) *int64 { return &v }

// fourBills is the smallest Cycle that exercises every marker and both footers.
func fourBills() domain.Snapshot {
	return domain.Snapshot{
		Cycle: domain.Cycle{ID: 7, Month: domain.Month{Year: 2026, Month: time.September}},
		Sections: []domain.Section{
			{ID: 1, Name: "BDO", DisplayOrder: 1},
			{ID: 2, Name: "HSBC", DisplayOrder: 2},
			{ID: 3, Name: "Utilities", DisplayOrder: 3},
		},
		Payables: []domain.Payable{
			{ID: 11, SectionID: 1, BillName: "BDO JCB CC", Channel: domain.SheenaBDO,
				Status: domain.StatusDue},
			{ID: 12, SectionID: 1, BillName: "BDO Home Loan", Channel: domain.SheenaBDO,
				AmountCents: cents(1663931), Status: domain.StatusFunded},
			{ID: 13, SectionID: 3, BillName: "Internet PLDT", Channel: domain.ChargedToCard,
				CardName: "RCBC Visa Airmiles", AmountCents: cents(509900), Status: domain.StatusDue},
			{ID: 14, SectionID: 3, BillName: "Water & Sewer", Channel: domain.KevinDirect,
				AmountCents: cents(84050), Status: domain.StatusPaid},
		},
		Transfers: []domain.Transfer{{ID: 3, Channel: domain.SheenaBDO, SentCents: 1700000}},
	}
}

func TestRenderFull(t *testing.T) {
	want := strings.Join([]string{
		"📋 <b>Bills — September 2026</b>",
		"<i>1 of 4 paid · 1 no amount yet</i>",
		"",
		"<b>BDO</b>",
		"? BDO JCB CC · — · Sheena BDO",
		"⏳ BDO Home Loan · ₱16,639.31 · Sheena BDO",
		"<i>subtotal ₱16,639.31 (1 unknown)</i>",
		"",
		"<b>Utilities</b>",
		"☐ Internet PLDT · ₱5,099.00 · → RCBC Visa Airmiles",
		"✓ <s>Water &amp; Sewer · ₱840.50 · Kevin</s>",
		"<i>subtotal ₱5,939.50</i>",
		"",
		"<b>To settle ₱17,479.81</b> <i>(1 unknown)</i>",
		"<i>plus ₱5,099.00 charged to cards (already inside those card balances)</i>",
		"",
		"<b>Transfers</b>",
		"Sheena BDO: need ₱16,639.31 <i>(tentative, 1 unknown)</i> · sent ₱17,000.00 (+₱360.69) ⏳",
	}, "\n")

	assert.Equal(t, want, board.Render(fourBills()))
}

func TestRenderCompactShortensLabelsAndDropsSubtotals(t *testing.T) {
	text := board.RenderMode(fourBills(), board.Compact)

	assert.Contains(t, text, "? BDO JCB CC · — · S-BDO")
	assert.Contains(t, text, "☐ Internet PLDT · ₱5,099.00 · →card")
	assert.Contains(t, text, "✓ <s>Water &amp; Sewer · ₱840.50 · K</s>")
	assert.NotContains(t, text, "subtotal")
	assert.Contains(t, text, "<b>Transfers</b>")
}

func TestRenderTransferFooter(t *testing.T) {
	tests := []struct {
		name     string
		sent     []domain.Transfer
		allPaid  bool
		wantLine string
	}{
		{"not sent", nil, false, "Sheena BPI: need ₱1,000.00 · not sent"},
		{"sent exactly, still unpaid",
			[]domain.Transfer{{Channel: domain.SheenaBPI, SentCents: 100000}}, false,
			"Sheena BPI: need ₱1,000.00 · sent ₱1,000.00 ⏳"},
		{"short",
			[]domain.Transfer{{Channel: domain.SheenaBPI, SentCents: 90000}}, false,
			"Sheena BPI: need ₱1,000.00 · sent ₱900.00 (−₱100.00) ⏳"},
		{"all paid",
			[]domain.Transfer{{Channel: domain.SheenaBPI, SentCents: 100000}}, true,
			"Sheena BPI: need ₱1,000.00 · sent ₱1,000.00 ✓"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status := domain.StatusFunded
			if tt.allPaid {
				status = domain.StatusPaid
			}
			snap := domain.Snapshot{
				Sections: []domain.Section{{ID: 1, Name: "BPI"}},
				Payables: []domain.Payable{{ID: 1, SectionID: 1, BillName: "BPI CC",
					Channel: domain.SheenaBPI, AmountCents: cents(100000), Status: status}},
				Transfers: tt.sent,
			}

			assert.Contains(t, board.Render(snap), "\n"+tt.wantLine)
		})
	}
}

func TestRenderLeavesOutFootersWithNothingToSay(t *testing.T) {
	snap := domain.Snapshot{
		Sections: []domain.Section{{ID: 1, Name: "Utilities"}},
		Payables: []domain.Payable{{ID: 1, SectionID: 1, BillName: "Water",
			Channel: domain.KevinDirect, Status: domain.StatusDue}},
	}

	text := board.Render(snap)

	assert.NotContains(t, text, "charged to cards")
	assert.NotContains(t, text, "Transfers")
}

func TestRenderClosedCycle(t *testing.T) {
	snap := fourBills()
	closed := time.Date(2026, 9, 15, 17, 0, 0, 0, time.UTC) // 16 Sep, 01:00 in Manila
	snap.Cycle.ClosedAt = &closed

	lines := strings.Split(board.Render(snap), "\n")

	assert.Equal(t, "✅ <b>CLOSED</b> — all paid on 16 Sep", lines[1])
	assert.Nil(t, board.Keyboard(snap), "a closed cycle has no buttons")
}

// crowded builds a Cycle long enough that the Full layout cannot fit in one message.
func crowded(bills int) domain.Snapshot {
	snap := domain.Snapshot{Sections: []domain.Section{{ID: 1, Name: "Everything"}}}
	for i := 1; i <= bills; i++ {
		snap.Payables = append(snap.Payables, domain.Payable{
			ID: int64(i), SectionID: 1, Channel: domain.SheenaPSBank, AmountCents: cents(123456789),
			BillName: fmt.Sprintf("A rather long bill name number %03d", i), Status: domain.StatusDue,
		})
	}
	return snap
}

func TestRenderFallsBackToCompactWhenFullIsTooLong(t *testing.T) {
	snap := crowded(62)
	require.Greater(t, len([]rune(board.RenderMode(snap, board.Full))), board.Limit)
	require.LessOrEqual(t, len([]rune(board.RenderMode(snap, board.Compact))), board.Limit)

	assert.Equal(t, board.RenderMode(snap, board.Compact), board.Render(snap))
}

func TestRenderTruncatesWhenEvenCompactIsTooLong(t *testing.T) {
	snap := crowded(200)

	text := board.Render(snap)

	assert.LessOrEqual(t, len([]rune(text)), board.Limit)
	assert.True(t, strings.HasSuffix(text, "<i>… use /summary for the rest</i>"))
	assert.Contains(t, text, "<b>To settle", "the totals survive truncation")
	assert.NotContains(t, text, "Transfers")
}
