// Package report writes the bot's read-only answers: what one Bill has come to over the
// months, what one month comes to across the Bills, and the CSV exports. Like board it is
// pure — it is given a reading of the database and returns text — which is what lets every
// figure the household is shown be checked in a test against one worked by hand.
package report

import (
	"fmt"
	"html"
	"strings"

	"github.com/kevin-jake/bills-bot/internal/domain"
)

// History writes a Bill's last months, newest first, with what it has averaged. The
// average leaves out the months whose amount was never entered rather than counting them
// as nothing, so a month someone forgot cannot quietly halve the figure.
func History(h domain.History) string {
	name := "📈 <b>" + html.EscapeString(h.Bill) + "</b>"
	if len(h.Entries) == 0 {
		return name + "\n\n<i>No month has this bill on it yet.</i>"
	}

	lines := []string{name, "<i>" + summaryOfMonths(h) + "</i>", ""}
	for _, entry := range h.Entries {
		lines = append(lines, historyLine(entry))
	}
	return strings.Join(lines, "\n")
}

// summaryOfMonths is the line under the Bill's name: how many months are shown and what
// they average.
func summaryOfMonths(h domain.History) string {
	months := plural(len(h.Entries), "month")
	average, counted := h.Average()
	switch {
	case counted == 0:
		return months + " · no amounts entered yet"
	case counted < len(h.Entries):
		return fmt.Sprintf("%s · average %s over the %d with an amount",
			months, domain.FormatPesos(average), counted)
	}
	return months + " · average " + domain.FormatPesos(average)
}

// historyLine writes one month: what it came to, and how it stands or who settled it.
func historyLine(entry domain.HistoryEntry) string {
	amount := "—"
	if entry.Payable.AmountKnown() {
		amount = domain.FormatPesos(*entry.Payable.AmountCents)
	}
	return entry.Month.Short() + " · " + amount + " · " + standing(entry)
}

// standing says where a month's Payable got to. A paid one names the day and whoever did
// it, which is the thing someone looks a history up to settle an argument about.
func standing(entry domain.HistoryEntry) string {
	p := entry.Payable
	switch {
	case p.Paid():
		paid := "✓"
		if p.PaidAt != nil {
			paid += " " + p.PaidAt.In(domain.Manila).Format("2 Jan")
		}
		if entry.PaidBy != "" {
			paid += " (" + html.EscapeString(entry.PaidBy) + ")"
		}
		return paid
	case !p.AmountKnown():
		return "? no amount yet"
	case p.Status == domain.StatusFunded:
		return "⏳ funded"
	}
	return "☐ due"
}

func plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}
