package report_test

import (
	"strings"
	"testing"
	"time"

	"github.com/kevin-jake/bills-bot/internal/domain"
	"github.com/kevin-jake/bills-bot/internal/report"
	"github.com/stretchr/testify/assert"
)

func TestSummary(t *testing.T) {
	want := strings.Join([]string{
		"📊 <b>September 2026</b>",
		"<i>1 of 4 paid · 1 no amount yet</i>",
		"",
		"<b>By section</b>",
		"BDO · ₱16,639.31 (1 unknown) · 0 of 2 paid",
		"Utilities · ₱5,939.50 · 1 of 2 paid",
		"",
		"<b>By channel</b>",
		"Kevin · ₱840.50 · 1 of 1 paid",
		"Sheena BDO · ₱16,639.31 (1 unknown) · 0 of 2 paid",
		"Charged to card · ₱5,099.00 · 0 of 1 paid",
		"",
		"<b>Transfers</b>",
		"Sheena BDO · need ₱16,639.31 <i>(tentative, 1 unknown)</i> · sent ₱17,000.00 (+₱360.69)",
		"",
		"<b>To settle ₱17,479.81</b> <i>(1 unknown)</i>",
		"<i>plus ₱5,099.00 charged to cards (already inside those card balances)</i>",
		"<b>Cash out ₱840.50</b>",
		"<i>what actually leaves the household: the bills Kevin pays himself</i>",
	}, "\n")

	assert.Equal(t, want, report.Summary(september()))
}

func TestSummaryOfAClosedMonthSaysSoInsteadOfCounting(t *testing.T) {
	snap := september()
	closed := time.Date(2026, time.September, 16, 18, 0, 0, 0, domain.Manila)
	snap.Cycle.ClosedAt = &closed

	assert.Contains(t, report.Summary(snap), "<i>✅ closed — all paid on 16 Sep</i>")
}

func TestSummaryOfAnAccountNothingWasSentInto(t *testing.T) {
	snap := september()
	snap.Transfers = nil

	assert.Contains(t, report.Summary(snap), "Sheena BDO · need ₱16,639.31 <i>(tentative, 1 unknown)</i> · not sent")
}

func TestSummaryOfAMonthWithShortfallAndNothingUnknown(t *testing.T) {
	snap := september()
	snap.Payables[0].AmountCents = cents(1000000)
	snap.Payables[0].Status = domain.StatusFunded

	text := report.Summary(snap)
	assert.Contains(t, text, "Sheena BDO · need ₱26,639.31 · sent ₱17,000.00 (−₱9,639.31)")
	assert.Contains(t, text, "<b>To settle ₱27,479.81</b>\n")
}

func TestSummaryOfAMonthWithNoBills(t *testing.T) {
	snap := domain.Snapshot{Cycle: domain.Cycle{ID: 8, Month: month(2026, time.October)}}

	assert.Equal(t, strings.Join([]string{
		"📊 <b>October 2026</b>",
		"<i>0 of 0 paid</i>",
		"",
		"<i>No bills were on the list that month.</i>",
	}, "\n"), report.Summary(snap))
}
