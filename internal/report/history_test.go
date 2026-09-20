package report_test

import (
	"strings"
	"testing"
	"time"

	"github.com/kevin-jake/bills-bot/internal/domain"
	"github.com/kevin-jake/bills-bot/internal/report"
	"github.com/stretchr/testify/assert"
)

// threeMonths is a bill that was paid, then funded but not yet paid, then never entered —
// one row of each kind a history can show.
func threeMonths() domain.History {
	paidAt := time.Date(2026, time.September, 15, 9, 3, 0, 0, domain.Manila)
	return domain.History{
		Bill: "RCBC JCB CC ••1006",
		Entries: []domain.HistoryEntry{
			{Month: month(2026, time.September), PaidBy: "Sheena", Payable: domain.Payable{
				AmountCents: cents(10343123), Status: domain.StatusPaid, PaidAt: &paidAt}},
			{Month: month(2026, time.August), Payable: domain.Payable{
				AmountCents: cents(9800000), Status: domain.StatusFunded}},
			{Month: month(2026, time.July), Payable: domain.Payable{Status: domain.StatusDue}},
		},
	}
}

func TestHistory(t *testing.T) {
	want := strings.Join([]string{
		"📈 <b>RCBC JCB CC ••1006</b>",
		"<i>3 months · average ₱100,715.61 over the 2 with an amount</i>",
		"",
		"Sep 2026 · ₱103,431.23 · ✓ 15 Sep (Sheena)",
		"Aug 2026 · ₱98,000.00 · ⏳ funded",
		"Jul 2026 · — · ? no amount yet",
	}, "\n")

	assert.Equal(t, want, report.History(threeMonths()))
}

func TestHistoryOfMonthsThatAllHaveAmounts(t *testing.T) {
	h := threeMonths()
	h.Entries[2].Payable.AmountCents = cents(9000000)

	assert.Contains(t, report.History(h), "<i>3 months · average ₱97,143.74</i>")
	assert.Contains(t, report.History(h), "Jul 2026 · ₱90,000.00 · ☐ due")
}

func TestHistoryOfAMonthNobodyEnteredAnythingIn(t *testing.T) {
	h := domain.History{Bill: "Batelec", Entries: threeMonths().Entries[2:]}

	assert.Contains(t, report.History(h), "<i>1 month · no amounts entered yet</i>")
}

func TestHistoryOfAZeroMonthTickedOffByNobody(t *testing.T) {
	paidAt := time.Date(2026, time.September, 1, 8, 0, 0, 0, domain.Manila)
	h := domain.History{Bill: "Batelec", Entries: []domain.HistoryEntry{
		{Month: month(2026, time.September), PaidBy: "nobody", Payable: domain.Payable{
			AmountCents: cents(0), Status: domain.StatusPaid, PaidAt: &paidAt}},
	}}

	assert.Contains(t, report.History(h), "Sep 2026 · ₱0.00 · ✓ 1 Sep (nobody)")
}

func TestHistoryOfABillNoMonthHasYet(t *testing.T) {
	assert.Equal(t, "📈 <b>Netflix &amp; chill</b>\n\n<i>No month has this bill on it yet.</i>",
		report.History(domain.History{Bill: "Netflix & chill"}))
}
