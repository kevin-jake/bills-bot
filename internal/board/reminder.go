package board

import (
	"html"
	"slices"
	"strings"
	"time"

	"github.com/kevin-jake/bills-bot/internal/domain"
)

// Reminder writes the mid-month nudge: what is still unpaid, across every open Cycle,
// oldest month first. It is a shorter thing than the Board — no subtotals, no paid lines,
// nothing to tap — because it is read once and then acted on from the pinned Board.
//
// Within a month the lines come soonest due first rather than in Board order, since the
// point of the message is what to pay next. A Bill with no fixed day comes last: it cannot
// be late on any particular morning.
//
// It returns empty when nothing is outstanding, which is how the scheduler knows to stay
// quiet rather than send a reminder that says everything is fine.
func Reminder(snaps []domain.Snapshot, now time.Time) string {
	today := startOfDay(now)

	var body []string
	for _, snap := range snaps {
		var unpaid []domain.Payable
		for _, p := range snap.Payables {
			if !p.Paid() {
				unpaid = append(unpaid, p)
			}
		}
		notSent := transfersNotSent(snap)
		if len(unpaid) == 0 && len(notSent) == 0 {
			continue
		}

		slices.SortStableFunc(unpaid, func(a, b domain.Payable) int {
			return dueOrder(a) - dueOrder(b)
		})

		body = append(body, "", "<b>"+snap.Cycle.Month.Title()+"</b>")
		for _, p := range unpaid {
			body = append(body, reminderLine(p, snap.Cycle.Month, today))
		}
		if len(notSent) > 0 {
			body = append(body, "<i>Transfers not sent: "+strings.Join(notSent, ", ")+"</i>")
		}
	}
	if len(body) == 0 {
		return ""
	}
	return strings.Join(append([]string{"⏰ <b>Bills still unpaid</b>"}, body...), "\n")
}

// reminderLine writes one unpaid Bill the way the Board does, and says so when its day has
// already gone by.
func reminderLine(p domain.Payable, month domain.Month, today time.Time) string {
	line := payableLine(p, Full)
	if due := p.DueDate(month); !due.IsZero() && due.Before(today) {
		line += " ⚠️ overdue"
	}
	return line
}

// transfersNotSent names the accounts that still need money for the Cycle, which is the
// one thing on a reminder that is Kevin's to do rather than Sheena's.
func transfersNotSent(snap domain.Snapshot) []string {
	var channels []string
	for _, line := range snap.TransferLines() {
		if line.Sent == nil && !line.AllPaid {
			channels = append(channels, html.EscapeString(line.Channel.Label()))
		}
	}
	return channels
}

// dueOrder sorts a Payable by the day it falls due. A Bill with no fixed day sorts after
// every day of every month.
func dueOrder(p domain.Payable) int {
	if p.DueDay == 0 {
		return 32
	}
	return p.DueDay
}

// startOfDay is midnight in Manila on the day t falls, which is what "already gone by"
// means for a due day: a Bill due today is not overdue.
func startOfDay(t time.Time) time.Time {
	local := t.In(domain.Manila)
	return time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, domain.Manila)
}
