package report

import (
	"fmt"
	"html"
	"strings"

	"github.com/kevin-jake/bills-bot/internal/domain"
)

// Summary writes one month totted up every way the household asks about it: down the
// Sections as the Board groups them, across the Payment Channels as the money actually
// leaves, and then the two figures that are easy to confuse — To settle, which is the
// checklist, and Cash out, which is what the household is really out of pocket.
func Summary(snap domain.Snapshot) string {
	lines := []string{"📊 <b>" + snap.Cycle.Month.Title() + "</b>", "<i>" + progress(snap) + "</i>"}

	if len(snap.Payables) == 0 {
		return strings.Join(append(lines, "", "<i>No bills were on the list that month.</i>"), "\n")
	}

	lines = append(lines, "", "<b>By section</b>")
	for _, bySection := range snap.SectionGroups() {
		lines = append(lines, groupLine(bySection.Section.Name, bySection.Subtotal(), bySection.Payables))
	}

	lines = append(lines, "", "<b>By channel</b>")
	for _, byChannel := range snap.ChannelGroups() {
		lines = append(lines, groupLine(byChannel.Channel.Label(), byChannel.Subtotal(), byChannel.Payables))
	}

	if transfers := transferLines(snap); len(transfers) > 0 {
		lines = append(lines, append([]string{"", "<b>Transfers</b>"}, transfers...)...)
	}
	return strings.Join(append(lines, totals(snap)...), "\n")
}

// progress is the same count the Board carries at the top, so that a summary read beside
// the pinned message says the same thing.
func progress(snap domain.Snapshot) string {
	if snap.Cycle.Closed() {
		return "✅ closed — all paid on " + snap.Cycle.ClosedAt.In(domain.Manila).Format("2 Jan")
	}
	line := fmt.Sprintf("%d of %d paid", snap.PaidCount(), len(snap.Payables))
	if unknown := snap.UnknownCount(); unknown > 0 {
		line += fmt.Sprintf(" · %d no amount yet", unknown)
	}
	return line
}

// groupLine writes one heading's figures: what it comes to, and how much of it is done.
func groupLine(name string, subtotal domain.Tally, payables []domain.Payable) string {
	return fmt.Sprintf("%s · %s · %d of %d paid", html.EscapeString(name), tally(subtotal),
		domain.PaidIn(payables), len(payables))
}

// transferLines compares what each of Sheena's accounts needed with what was sent into it.
func transferLines(snap domain.Snapshot) []string {
	var lines []string
	for _, t := range snap.TransferLines() {
		line := t.Channel.Label() + " · need " + domain.FormatPesos(t.Need.Cents)
		if t.Tentative() {
			line += fmt.Sprintf(" <i>(tentative, %d unknown)</i>", t.Need.Unknown)
		}
		if t.Sent == nil {
			lines = append(lines, line+" · not sent")
			continue
		}
		line += " · sent " + domain.FormatPesos(t.Sent.SentCents)
		if diff := t.Sent.SentCents - t.Need.Cents; diff != 0 {
			line += " (" + signed(diff) + ")"
		}
		lines = append(lines, line)
	}
	return lines
}

// totals are the two figures decision 23 keeps apart: the checklist and the outflow.
func totals(snap domain.Snapshot) []string {
	settle := snap.ToSettle()
	lines := []string{"", "<b>To settle " + domain.FormatPesos(settle.Cents) + "</b>" + unknowns(settle)}

	if cards := snap.ChargedToCards(); cards.Count > 0 {
		lines = append(lines, fmt.Sprintf(
			"<i>plus %s charged to cards (already inside those card balances)</i>", tally(cards)))
	}

	cash := snap.CashOut()
	lines = append(lines, "<b>Cash out "+domain.FormatPesos(cash.Cents)+"</b>"+unknowns(cash),
		"<i>what actually leaves the household: the bills Kevin pays himself</i>")
	return lines
}

// unknowns marks a figure that is not final yet, in the italics a total is written in.
func unknowns(t domain.Tally) string {
	if t.Unknown == 0 {
		return ""
	}
	return fmt.Sprintf(" <i>(%d unknown)</i>", t.Unknown)
}

// tally writes a subtotal with its unknowns, so a figure that is not final never looks it.
func tally(t domain.Tally) string {
	text := domain.FormatPesos(t.Cents)
	if t.Unknown > 0 {
		text += fmt.Sprintf(" (%d unknown)", t.Unknown)
	}
	return text
}

// signed writes a difference with the sign a surplus or a shortfall reads by.
func signed(cents int64) string {
	if cents > 0 {
		return "+" + domain.FormatPesos(cents)
	}
	return domain.FormatPesos(cents)
}
