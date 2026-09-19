// Package board lays a Cycle out as the sticky note it replaces: one Telegram message, in
// HTML, with a button per bill. It is pure. It knows nothing of Telegram's API types or of
// the database, which keeps every string the household reads testable exactly.
package board

import (
	"fmt"
	"html"
	"strings"

	"github.com/kevin-jake/bills-bot/internal/domain"
)

// Limit is the most characters a Board may use. Telegram refuses a message over 4096; the
// margin absorbs the difference between how Go and Telegram count some characters.
const Limit = 4000

// Mode is how much detail a Board carries. Render tries each in turn until one fits.
type Mode int

const (
	// Full is the sticky note with subtotals and whole channel names.
	Full Mode = iota
	// Compact drops subtotals and shortens channel names.
	Compact
	// Truncated is Compact without the Transfers footer, cut short if it must be.
	Truncated
)

// Render lays out the Snapshot in the most detailed Mode that fits within Limit.
func Render(snap domain.Snapshot) string {
	for _, mode := range []Mode{Full, Compact} {
		if text := RenderMode(snap, mode); length(text) <= Limit {
			return text
		}
	}
	return RenderMode(snap, Truncated)
}

// RenderMode lays out the Snapshot in the given Mode, whatever its length.
func RenderMode(snap domain.Snapshot, mode Mode) string {
	var body []string
	body = append(body, header(snap)...)

	for _, group := range snap.SectionGroups() {
		body = append(body, "", "<b>"+html.EscapeString(group.Section.Name)+"</b>")
		for _, p := range group.Payables {
			body = append(body, payableLine(p, mode))
		}
		if mode == Full {
			body = append(body, "<i>subtotal "+tally(group.Subtotal())+"</i>")
		}
	}

	footer := totals(snap)
	if mode != Truncated {
		footer = append(footer, transfers(snap)...)
		return strings.Join(append(body, footer...), "\n")
	}

	// Truncated keeps the totals, because they are what a glance at the Board is for, and
	// gives up bill lines from the end until the whole fits.
	const more = "<i>… use /summary for the rest</i>"
	footer = append(footer, "", more)
	for len(body) > 0 && length(strings.Join(append(body, footer...), "\n")) > Limit {
		body = body[:len(body)-1]
	}
	return strings.Join(append(body, footer...), "\n")
}

func header(snap domain.Snapshot) []string {
	lines := []string{"📋 <b>Bills — " + snap.Cycle.Month.Title() + "</b>"}

	if snap.Cycle.Closed() {
		closed := snap.Cycle.ClosedAt.In(domain.Manila).Format("2 Jan")
		return append(lines, "✅ <b>CLOSED</b> — all paid on "+closed)
	}

	progress := fmt.Sprintf("%d of %d paid", snap.PaidCount(), len(snap.Payables))
	if unknown := snap.UnknownCount(); unknown > 0 {
		progress += fmt.Sprintf(" · %d no amount yet", unknown)
	}
	return append(lines, "<i>"+progress+"</i>")
}

// payableLine writes one bill. A paid line is struck through, like the sticky note.
func payableLine(p domain.Payable, mode Mode) string {
	amount := "—"
	if p.AmountKnown() {
		amount = domain.FormatPesos(*p.AmountCents)
	}
	label := p.ChannelLabel()
	if mode != Full {
		label = shortLabel(p)
	}

	line := html.EscapeString(p.BillName) + " · " + amount + " · " + html.EscapeString(label)
	if p.Paid() {
		return Marker(p) + " <s>" + line + "</s>"
	}
	return Marker(p) + " " + line
}

// Marker is the one-character state of a Payable, shared by the Board and its buttons.
// An unknown amount outranks funded: it is the thing someone still has to do.
func Marker(p domain.Payable) string {
	switch {
	case p.Paid():
		return "✓"
	case !p.AmountKnown():
		return "?"
	case p.Status == domain.StatusFunded:
		return "⏳"
	}
	return "☐"
}

func shortLabel(p domain.Payable) string {
	switch p.Channel {
	case domain.KevinDirect:
		return "K"
	case domain.SheenaBDO:
		return "S-BDO"
	case domain.SheenaBPI:
		return "S-BPI"
	case domain.SheenaPSBank:
		return "S-PSB"
	case domain.ChargedToCard:
		return "→card"
	}
	return string(p.Channel)
}

// totals writes To settle, which is labelled so rather than "Total" because it is a
// checklist figure rather than what the household spent: Kevin funds Sheena's accounts from
// a card that is itself on the list, so some money appears on the Board twice over a month.
func totals(snap domain.Snapshot) []string {
	settle := snap.ToSettle()
	line := "<b>To settle " + domain.FormatPesos(settle.Cents) + "</b>"
	if settle.Unknown > 0 {
		line += fmt.Sprintf(" <i>(%d unknown)</i>", settle.Unknown)
	}
	lines := []string{"", line}

	if cards := snap.ChargedToCards(); cards.Count > 0 {
		unknown := ""
		if cards.Unknown > 0 {
			unknown = fmt.Sprintf(", %d unknown", cards.Unknown)
		}
		lines = append(lines, fmt.Sprintf(
			"<i>plus %s charged to cards%s (already inside those card balances)</i>",
			domain.FormatPesos(cards.Cents), unknown))
	}
	return lines
}

func transfers(snap domain.Snapshot) []string {
	transferLines := snap.TransferLines()
	if len(transferLines) == 0 {
		return nil
	}

	lines := []string{"", "<b>Transfers</b>"}
	for _, t := range transferLines {
		line := t.Channel.Label() + ": need " + domain.FormatPesos(t.Need.Cents)
		if t.Tentative() {
			line += fmt.Sprintf(" <i>(tentative, %d unknown)</i>", t.Need.Unknown)
		}

		if t.Sent == nil {
			lines = append(lines, line+" · not sent")
			continue
		}
		line += " · sent " + domain.FormatPesos(t.Sent.SentCents)
		switch diff := t.Sent.SentCents - t.Need.Cents; {
		case diff > 0:
			line += " (+" + domain.FormatPesos(diff) + ")"
		case diff < 0:
			line += " (" + domain.FormatPesos(diff) + ")"
		}
		if t.AllPaid {
			line += " ✓"
		} else {
			line += " ⏳"
		}
		lines = append(lines, line)
	}
	return lines
}

// tally writes a subtotal with its unknowns, so a figure that is not final never looks it.
func tally(t domain.Tally) string {
	text := domain.FormatPesos(t.Cents)
	if t.Unknown > 0 {
		text += fmt.Sprintf(" (%d unknown)", t.Unknown)
	}
	return text
}

// length counts characters the way Telegram's limit does, closely enough: in runes, not bytes.
func length(s string) int {
	return len([]rune(s))
}
