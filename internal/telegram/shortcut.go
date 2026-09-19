package telegram

import (
	"errors"
	"html"
	"log"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/kevin-jake/bills-bot/internal/board"
	"github.com/kevin-jake/bills-bot/internal/domain"
	"github.com/kevin-jake/bills-bot/internal/parse"
	"github.com/kevin-jake/bills-bot/internal/tracker"
)

// matchActions carries a shortcut's verb through a "Which one?" button.
var matchActions = map[parse.Verb]board.MatchAction{
	parse.VerbSetAmount: board.MatchSetAmount,
	parse.VerbPaid:      board.MatchPaid,
	parse.VerbUndo:      board.MatchUndo,
}

// handleShortcut acts on a typed shortcut such as "pldt 2499" or "paid bpi cc". Anything
// that does not read as one is conversation and is left alone.
func (b *Bot) handleShortcut(message *tgbotapi.Message) {
	s, ok := parse.Parse(message.Text, domain.MonthOf(b.now()))
	if !ok {
		return
	}
	sections, err := b.tracker.BillList()
	if err != nil {
		log.Printf("failed to read the bill list for a shortcut: %v", err)
		return
	}
	var candidates []parse.Candidate
	for _, section := range sections {
		for _, bill := range section.Bills {
			candidates = append(candidates, parse.Candidate{
				ID: bill.ID, Name: bill.Name, Aliases: bill.Aliases, Last4: bill.CardLast4})
		}
	}

	matches := parse.MatchBill(s.Name, candidates)
	if len(matches) == 0 {
		// A bare name and number is also how ordinary chat can end, so it is answered only
		// when it looks like a bill mistyped rather than a sentence.
		if s.Explicit || parse.Mentions(s.Name, candidates) {
			b.replyHTML(message, "No bill matches “"+html.EscapeString(s.Name)+
				"”. <code>/bills</code> lists them.")
		}
		return
	}

	snaps, ok := b.shortcutCycles(message, s)
	if !ok {
		return
	}

	var payables []domain.Payable
	for _, match := range matches {
		if p, found := choosePayable(match.ID, s.Verb, snaps); found {
			payables = append(payables, p)
		}
	}
	switch len(payables) {
	case 0:
		b.replyHTML(message, "<b>"+html.EscapeString(matches[0].Name)+"</b> is not on the board for "+
			snaps[len(snaps)-1].Cycle.Month.Title()+".")
	case 1:
		text, change, err := b.applyShortcut(actorOf(message), payables[0], matchActions[s.Verb], s.Amount)
		b.replyHTML(message, text)
		if err == nil {
			b.showChange(change)
		}
	default:
		b.askWhichOne(message, s, payables, snaps)
	}
}

// shortcutCycles returns the Cycles a shortcut may reach, oldest first: the month it
// named, or every open one. With none open it falls back to the latest, so that "undo …"
// can still reopen a month that has just closed.
func (b *Bot) shortcutCycles(message *tgbotapi.Message, s parse.Shortcut) ([]domain.Snapshot, bool) {
	if s.Month != nil {
		snap, err := b.tracker.MonthSnapshot(*s.Month)
		switch {
		case errors.Is(err, tracker.ErrCycleUnknown):
			b.replyHTML(message, "⚠️ "+s.Month.Title()+" has not been opened.")
			return nil, false
		case err != nil:
			log.Printf("failed to read %s for a shortcut: %v", s.Month, err)
			b.replyHTML(message, "I could not read that month just now. Nothing was changed.")
			return nil, false
		}
		return []domain.Snapshot{snap}, true
	}

	snaps, err := b.tracker.OpenSnapshots()
	if err == nil && len(snaps) == 0 {
		var snap domain.Snapshot
		snap, err = b.tracker.CurrentSnapshot()
		snaps = []domain.Snapshot{snap}
	}
	switch {
	case errors.Is(err, tracker.ErrNoCycle):
		b.replyHTML(message, "No month has been opened yet. <code>/newmonth</code> opens this one.")
		return nil, false
	case err != nil:
		log.Printf("failed to read the open cycles for a shortcut: %v", err)
		b.replyHTML(message, "I could not read the board just now. Nothing was changed.")
		return nil, false
	}
	return snaps, true
}

// choosePayable picks which month's Payable of a Bill a shortcut means, from snaps oldest
// first. An amount or a payment goes to the newest month where the Bill is still unpaid,
// since that is the one being worked through; an undo goes to the newest month where
// something has been entered, since only there is there anything to take back. Failing
// either, the newest month the Bill is in.
func choosePayable(billID int64, verb parse.Verb, snaps []domain.Snapshot) (domain.Payable, bool) {
	var fallback *domain.Payable
	for i := len(snaps) - 1; i >= 0; i-- {
		for _, p := range snaps[i].Payables {
			if p.BillID != billID {
				continue
			}
			if verb == parse.VerbUndo && p.AmountKnown() || verb != parse.VerbUndo && !p.Paid() {
				return p, true
			}
			if fallback == nil {
				fallback = &p
			}
		}
	}
	if fallback == nil {
		return domain.Payable{}, false
	}
	return *fallback, true
}

// askWhichOne offers the Payables a shortcut could have meant as buttons, each carrying
// what the shortcut asked for, so one tap finishes the job. The month is named only when
// more than one is open, since otherwise it goes without saying.
func (b *Bot) askWhichOne(message *tgbotapi.Message, s parse.Shortcut, payables []domain.Payable,
	snaps []domain.Snapshot) {
	months := map[int64]domain.Month{}
	if len(snaps) > 1 {
		for _, snap := range snaps {
			months[snap.Cycle.ID] = snap.Cycle.Month
		}
	}

	arg := board.MatchArg(matchActions[s.Verb], s.Amount)
	var rows [][]board.Button
	for _, p := range payables {
		text := p.BillName
		if month, ok := months[p.CycleID]; ok {
			text += " · " + month.Title()
		}
		rows = append(rows, []board.Button{{
			Text: text,
			Data: board.Callback{Kind: board.KindMatch, ID: p.ID, Arg: arg}.Encode(),
		}})
	}
	rows = append(rows, []board.Button{{Text: "✖ None of these", Data: board.Callback{Kind: board.KindCancel}.Encode()}})

	msg := tgbotapi.NewMessage(message.Chat.ID, "Which one did you mean by “"+html.EscapeString(s.Name)+"”?")
	msg.ParseMode = tgbotapi.ModeHTML
	msg.ReplyToMessageID = message.MessageID
	msg.ReplyMarkup = *markupOf(rows)
	if _, err := b.sender.Send(msg); err != nil {
		log.Printf("failed to ask which bill was meant: %v", err)
	}
}

// resolveMatch finishes a shortcut once someone has said which bill it meant. The question
// becomes the outcome, so the chat reads as a record of what was done.
func (b *Bot) resolveMatch(query *tgbotapi.CallbackQuery, callback board.Callback) {
	action, cents, err := board.ParseMatchArg(callback.Arg)
	if err != nil {
		b.answer(query.ID, "")
		return
	}
	p, _, err := b.tracker.Payable(callback.ID)
	if err != nil {
		p = domain.Payable{ID: callback.ID}
	}

	text, change, err := b.applyShortcut(actorOfUser(query.From), p, action, cents)
	b.settleQuestion(query, text)
	if err == nil {
		b.showChange(change)
	}
	b.answer(query.ID, "")
}

// dismiss answers "✖ None of these" by settling the question it was on.
func (b *Bot) dismiss(query *tgbotapi.CallbackQuery) {
	b.settleQuestion(query, "<i>Never mind — nothing was changed.</i>")
	b.answer(query.ID, "")
}

// settleQuestion rewrites the message a button was tapped on, dropping its buttons.
func (b *Bot) settleQuestion(query *tgbotapi.CallbackQuery, text string) {
	edit := tgbotapi.NewEditMessageText(query.Message.Chat.ID, query.Message.MessageID, text)
	edit.ParseMode = tgbotapi.ModeHTML
	if _, err := b.sender.Request(edit); err != nil && !isNotModified(err) {
		log.Printf("failed to settle the question in message %d: %v", query.Message.MessageID, err)
	}
}

// applyShortcut does what a shortcut asked to one Payable, and returns what to say about
// it: the outcome on success, the refusal otherwise.
func (b *Bot) applyShortcut(actor tracker.Actor, p domain.Payable, action board.MatchAction,
	cents *int64) (string, tracker.Change, error) {
	who := html.EscapeString(actor.Name)

	var change tracker.Change
	var err error
	switch action {
	case board.MatchSetAmount:
		if change, err = b.tracker.SetAmount(actor, p.ID, *cents); err == nil {
			return amountSetText(change, actor.Name), change, nil
		}
	case board.MatchPaid:
		// "paid bpi cc 5000" enters the amount, then pays it, unless entering it already
		// did: zero pays itself, and a bill already paid only has its figure corrected.
		if cents != nil {
			if change, err = b.tracker.SetAmount(actor, p.ID, *cents); err == nil && change.After.Paid() {
				return amountSetText(change, actor.Name), change, nil
			}
		}
		if err == nil {
			change, err = b.tracker.MarkPaid(actor, p.ID)
		}
		if err == nil {
			return "✓ <b>" + html.EscapeString(change.After.BillName) + "</b> · " +
				domain.FormatPesos(*change.After.AmountCents) + " is paid. — " + who, change, nil
		}
	case board.MatchUndo:
		if change, err = b.tracker.Undo(actor, p.ID); err == nil {
			return "↩ Undone: " + html.EscapeString(menuTitle(change.After)) + " — " + who, change, nil
		}
	}
	return shortcutRefusal(err, p), change, err
}

// shortcutRefusal says why a shortcut changed nothing.
func shortcutRefusal(err error, p domain.Payable) string {
	name := "<b>" + html.EscapeString(p.BillName) + "</b>"
	switch {
	case errors.Is(err, tracker.ErrPayableUnknown):
		return "⚠️ That bill is no longer on the board. Nothing was changed."
	case errors.Is(err, tracker.ErrCycleClosed):
		return "⚠️ " + html.EscapeString(capitalise(err.Error())) + ", so " + name + " was not changed."
	case errors.Is(err, domain.ErrAmountUnknown):
		return "⚠️ " + name + " has no amount yet. Give it one as you pay it, like <code>paid " +
			html.EscapeString(p.BillName) + " 2499</code>."
	case errors.Is(err, domain.ErrAlreadyPaid):
		return "⚠️ " + name + " is already paid."
	case errors.Is(err, tracker.ErrNothingToUndo):
		return "⚠️ There is nothing to undo on " + name + "."
	default:
		log.Printf("failed to apply a shortcut to payable %d: %v", p.ID, err)
		return "Something went wrong. " + name + " was not changed."
	}
}

// replyHTML answers a message as a reply to it, so it is clear which message was meant.
func (b *Bot) replyHTML(message *tgbotapi.Message, text string) {
	msg := tgbotapi.NewMessage(message.Chat.ID, text)
	msg.ParseMode = tgbotapi.ModeHTML
	msg.ReplyToMessageID = message.MessageID
	if _, err := b.sender.Send(msg); err != nil {
		log.Printf("failed to reply in chat %d: %v", message.Chat.ID, err)
	}
}
