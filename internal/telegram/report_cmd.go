package telegram

import (
	"errors"
	"html"
	"log"
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/kevin-jake/bills-bot/internal/domain"
	"github.com/kevin-jake/bills-bot/internal/parse"
	"github.com/kevin-jake/bills-bot/internal/report"
	"github.com/kevin-jake/bills-bot/internal/tracker"
)

// historyMonths is how far back /history looks. A year is long enough to see what a bill
// does over the seasons and short enough to read on a phone.
const historyMonths = 12

const historyUsage = "📈 <code>/history &lt;bill&gt;</code> — what one bill has come to, month by month.\n\n" +
	"<i>Example:</i> <code>/history rcbc jcb</code>"

// handleHistory answers with one Bill's last months. The Bill is named the same loose way
// the typed shortcuts name one, so that "rcbc jcb" finds it; unlike a shortcut it may also
// name a Bill that has been archived, since the months it was paid in are still true.
func (b *Bot) handleHistory(message *tgbotapi.Message, args []string) {
	typed := strings.TrimSpace(strings.Join(args, " "))
	if typed == "" {
		b.sendHTML(message.Chat.ID, historyUsage)
		return
	}

	bills, err := b.tracker.AllBills()
	if err != nil {
		log.Printf("failed to read the bills for /history: %v", err)
		b.send(message.Chat.ID, "I could not read the bill list just now. Try again in a moment.")
		return
	}

	candidates := make([]parse.Candidate, 0, len(bills))
	byID := make(map[int64]domain.Bill, len(bills))
	for _, bill := range bills {
		candidates = append(candidates, parse.Candidate{
			ID: bill.ID, Name: bill.Name, Aliases: bill.Aliases, Last4: bill.CardLast4})
		byID[bill.ID] = bill
	}

	matches := parse.MatchBill(typed, candidates)
	switch len(matches) {
	case 0:
		b.sendHTML(message.Chat.ID, "No bill matches “"+html.EscapeString(typed)+
			"”. <code>/bills</code> lists them.")
		return
	case 1:
	default:
		// A history is a whole message rather than a tap on a line, so a tie is answered
		// with the names to choose from rather than with buttons: typing one more word is
		// quicker than a round trip through a keyboard.
		var names []string
		for _, match := range matches {
			names = append(names, "<b>"+html.EscapeString(byID[match.ID].DisplayName())+"</b>")
		}
		b.sendHTML(message.Chat.ID, "“"+html.EscapeString(typed)+"” could be "+
			strings.Join(names, " or ")+". Which one?")
		return
	}

	history, err := b.tracker.History(matches[0].ID, historyMonths)
	if err != nil {
		log.Printf("failed to read the history of bill %d: %v", matches[0].ID, err)
		b.send(message.Chat.ID, "I could not read that bill's history just now. Try again in a moment.")
		return
	}
	b.sendHTML(message.Chat.ID, report.History(history))
}

// handleSummary tots up one month. With no argument it is the month people are working
// on, which is what the Board's "… use /summary for the rest" points at.
func (b *Bot) handleSummary(message *tgbotapi.Message, args []string) {
	snap, ok := b.snapshotFor(message.Chat.ID, args)
	if !ok {
		return
	}
	b.sendHTML(message.Chat.ID, report.Summary(snap))
}

// snapshotFor reads the month an optional argument names, or the current one, and says why
// it could not when it could not. It is the reading /board and /summary share.
func (b *Bot) snapshotFor(chatID int64, args []string) (domain.Snapshot, bool) {
	var snap domain.Snapshot
	var err error
	if len(args) > 0 {
		month, parseErr := domain.ParseMonth(args[0])
		if parseErr != nil {
			b.sendHTML(chatID, "⚠️ "+html.EscapeString(capitalise(parseErr.Error())+"."))
			return snap, false
		}
		snap, err = b.tracker.MonthSnapshot(month)
	} else {
		snap, err = b.tracker.CurrentSnapshot()
	}

	switch {
	case errors.Is(err, tracker.ErrNoCycle):
		b.sendHTML(chatID, "No month has been opened yet. <code>/newmonth</code> opens this one.")
		return snap, false
	case errors.Is(err, tracker.ErrCycleUnknown):
		b.sendHTML(chatID, "⚠️ "+html.EscapeString(capitalise(err.Error()))+
			". <code>/newmonth</code> opens a month.")
		return snap, false
	case err != nil:
		log.Printf("failed to read a cycle: %v", err)
		b.send(chatID, "I could not read that month just now. Try again in a moment.")
		return snap, false
	}
	return snap, true
}

// handleExport sends the whole database out as a CSV file: every Payable of every month,
// or with "transfers" every account's month. It is the answer to ADR 0001 keeping the
// bot's SQLite as the system of record — nothing is locked in.
func (b *Bot) handleExport(message *tgbotapi.Message, args []string) {
	chatID := message.Chat.ID
	transfers := len(args) > 0 && strings.EqualFold(args[0], "transfers")
	if len(args) > 0 && !transfers {
		b.sendHTML(chatID, "📤 <code>/export</code> — every bill of every month as a CSV file.\n"+
			"<code>/export transfers</code> — the transfers instead.")
		return
	}

	snaps, err := b.tracker.AllSnapshots()
	if err != nil {
		log.Printf("failed to read the cycles for /export: %v", err)
		b.send(chatID, "I could not read the months just now. Try again in a moment.")
		return
	}
	if len(snaps) == 0 {
		b.sendHTML(chatID, "There is nothing to export yet. <code>/newmonth</code> opens a month.")
		return
	}
	names, err := b.tracker.ActorNames()
	if err != nil {
		log.Printf("failed to read the actor names for /export: %v", err)
		b.send(chatID, "I could not read the months just now. Try again in a moment.")
		return
	}

	name, data := report.PayablesFile, report.PayablesCSV(snaps, names)
	if transfers {
		name, data = report.TransfersFile, report.TransfersCSV(snaps, names)
	}

	doc := tgbotapi.NewDocument(chatID, tgbotapi.FileBytes{Name: name, Bytes: data})
	doc.Caption = exportCaption(snaps)
	if _, err := b.sender.Send(doc); err != nil {
		log.Printf("failed to send %s: %v", name, err)
		b.send(chatID, "I could not send the file just now. Try again in a moment.")
	}
}

// exportCaption says what the file covers, so that a short file is obviously a short
// history rather than a failed export.
func exportCaption(snaps []domain.Snapshot) string {
	first := snaps[0].Cycle.Month
	last := snaps[len(snaps)-1].Cycle.Month
	if first == last {
		return first.Title() + "."
	}
	return plural(len(snaps), "month") + ", " + first.Title() + " to " + last.Title() + "."
}
