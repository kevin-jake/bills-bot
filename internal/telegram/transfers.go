package telegram

import (
	"errors"
	"fmt"
	"html"
	"log"
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/kevin-jake/bills-bot/internal/board"
	"github.com/kevin-jake/bills-bot/internal/domain"
	"github.com/kevin-jake/bills-bot/internal/tracker"
)

// transferUsage is shown whenever /transfer's arguments do not parse.
const transferUsage = "💸 <b>Transfers</b>\n\n" +
	"<code>/transfer</code> — pick an account and I ask how much\n" +
	"<code>/transfer &lt;account&gt; &lt;amount&gt;</code> — record it straight away\n" +
	"<code>/transfer YYYY-MM &lt;account&gt; &lt;amount&gt;</code> — for a month other than the current one\n\n" +
	"Accounts: <code>bdo</code>, <code>bpi</code>, <code>psbank</code>\n\n" +
	"<i>Example:</i> <code>/transfer bpi 213,205.11</code>"

// showTransferPicker swaps the Board's buttons for one per account of Sheena's, the same
// way a bill's menu does.
func (b *Bot) showTransferPicker(query *tgbotapi.CallbackQuery, cycleID int64) {
	snap, ok := b.openCycle(query, cycleID)
	if !ok {
		return
	}
	rows := board.TransferPicker(snap)
	if len(rows) == 0 {
		b.alert(query.ID, "Nothing in "+snap.Cycle.Month.Title()+" is paid through Sheena's accounts.")
		return
	}
	rows = append(rows, []board.Button{
		{Text: "« Back", Data: board.Callback{Kind: board.KindBack, ID: cycleID}.Encode()},
	})

	edit := tgbotapi.NewEditMessageReplyMarkup(snap.Cycle.BoardChatID, snap.Cycle.BoardMessageID,
		*markupOf(rows))
	if _, err := b.sender.Request(edit); err != nil && !isNotModified(err) {
		log.Printf("failed to show the transfer picker for cycle %d: %v", cycleID, err)
		b.answer(query.ID, "I could not open the transfers just now.")
		return
	}
	b.answer(query.ID, "Which account did the money go into?")
}

// startTransfer asks the person who tapped how much went into an account, and puts the
// Board's own buttons back.
func (b *Bot) startTransfer(query *tgbotapi.CallbackQuery, cycleID int64, channel domain.Channel) {
	snap, ok := b.openCycle(query, cycleID)
	if !ok {
		return
	}
	line, found := transferLine(snap, channel)
	if !found {
		b.alert(query.ID, "Nothing in "+snap.Cycle.Month.Title()+" is paid through "+channel.Label()+".")
		return
	}

	if err := b.askTransfer(query.Message.Chat.ID, query.From, snap, line); err != nil {
		log.Printf("failed to ask for the %s transfer of cycle %d: %v", channel, cycleID, err)
		b.answer(query.ID, "I could not ask just now. Try again in a moment.")
		return
	}
	b.refreshBoard(snap)
	b.answer(query.ID, "")
}

// askTransfer posts the question for what went into an account, saying what it needs so
// the figure can be checked against the banking app.
func (b *Bot) askTransfer(chatID int64, from *tgbotapi.User, snap domain.Snapshot, line domain.TransferLine) error {
	text := fmt.Sprintf("%s, how much went into <b>%s</b> for %s? It needs %s",
		mention(from), line.Channel.Label(), snap.Cycle.Month.Title(), domain.FormatPesos(line.Need.Cents))
	if line.Tentative() {
		text += " so far, with " + plural(line.Need.Unknown, "amount") + " still unknown"
	}
	text += "."
	if line.Sent != nil {
		text += " " + domain.FormatPesos(line.Sent.SentCents) + " is recorded as sent."
	}
	text += " Reply with a number, or /cancel."

	return b.ask(chatID, from, text, &pending{
		kind:    awaitTransferAmount,
		cycleID: snap.Cycle.ID,
		channel: line.Channel,
		subject: line.Channel.Label() + " transfer",
	})
}

// answerTransfer records a typed Transfer amount. Something that is not an amount, or is
// nothing, is refused and the question stays open.
func (b *Bot) answerTransfer(message *tgbotapi.Message, key pendingKey, p *pending) {
	cents, err := domain.ParseAmount(message.Text)
	if err == nil {
		err = domain.ValidateTransfer(cents)
	}
	if err != nil {
		b.sendHTML(key.chatID, "⚠️ "+html.EscapeString(capitalise(err.Error()))+
			". Try again, or /cancel.")
		return
	}

	b.dropPending(key, p)
	change, err := b.tracker.RecordTransfer(actorOf(message), p.cycleID, p.channel, cents)
	if refusal := transferRefusal(err); refusal != "" {
		b.closePrompt(key.chatID, p, "⚠️ "+html.EscapeString(refusal)+
			", so the <b>"+html.EscapeString(p.subject)+"</b> was not recorded.")
		return
	}
	if err != nil {
		log.Printf("failed to record the %s transfer of cycle %d: %v", p.channel, p.cycleID, err)
		b.closePrompt(key.chatID, p, "Something went wrong writing that down. "+
			"The <b>"+html.EscapeString(p.subject)+"</b> was not recorded.")
		return
	}

	// As with a bill's amount, the question keeps the record and a reply confirms it.
	text := transferRecordedText(change, message.From.FirstName)
	b.closePrompt(key.chatID, p, text)
	b.replyHTML(message, text)
	b.refreshBoard(change.Snap)
}

// undoTransfer removes a Transfer, putting the bills it funded back to due.
func (b *Bot) undoTransfer(query *tgbotapi.CallbackQuery, transferID int64) {
	change, err := b.tracker.UndoTransfer(actorOfUser(query.From), transferID)
	switch {
	case errors.Is(err, tracker.ErrTransferUnknown):
		b.alert(query.ID, "That transfer is no longer recorded. Tap 🔄 Refresh.")
		return
	case errors.Is(err, tracker.ErrCycleClosed):
		b.alert(query.ID, capitalise(err.Error())+", so its transfers cannot be changed.")
		return
	case err != nil:
		log.Printf("failed to undo transfer %d: %v", transferID, err)
		b.answer(query.ID, "Something went wrong. Nothing was changed.")
		return
	}

	b.refreshBoard(change.Snap)
	b.answer(query.ID, fmt.Sprintf("↩ The %s transfer of %s is undone.",
		change.Channel.Label(), domain.FormatPesos(change.Before.SentCents)))
}

// handleTransfer routes /transfer. With no arguments it offers the accounts as buttons;
// with an account and an amount it records the Transfer at once.
func (b *Bot) handleTransfer(message *tgbotapi.Message, args []string) {
	chatID := message.Chat.ID

	var month *domain.Month
	if len(args) > 0 {
		if parsed, err := domain.ParseMonth(args[0]); err == nil {
			month, args = &parsed, args[1:]
		}
	}

	var snap domain.Snapshot
	var err error
	if month != nil {
		snap, err = b.tracker.MonthSnapshot(*month)
	} else {
		snap, err = b.tracker.CurrentSnapshot()
	}
	switch {
	case errors.Is(err, tracker.ErrNoCycle):
		b.sendHTML(chatID, "No month has been opened yet. <code>/newmonth</code> opens this one.")
		return
	case errors.Is(err, tracker.ErrCycleUnknown):
		b.sendHTML(chatID, "⚠️ "+html.EscapeString(capitalise(err.Error()))+".")
		return
	case err != nil:
		log.Printf("failed to read a cycle for /transfer: %v", err)
		b.send(chatID, "I could not read the month just now. Try again in a moment.")
		return
	case snap.Cycle.Closed():
		b.sendHTML(chatID, "⚠️ <b>"+snap.Cycle.Month.Title()+"</b> is closed, so its transfers cannot be changed.")
		return
	}

	if len(args) == 0 {
		b.sendTransferPicker(chatID, snap)
		return
	}
	if len(args) < 2 {
		b.sendHTML(chatID, transferUsage)
		return
	}

	cents, err := domain.ParseAmount(args[len(args)-1])
	if err != nil {
		b.sendHTML(chatID, "⚠️ "+html.EscapeString(capitalise(err.Error()))+".")
		return
	}
	channel, err := domain.ParseChannel(strings.Join(args[:len(args)-1], " "))
	if err != nil {
		b.sendHTML(chatID, "⚠️ "+html.EscapeString(capitalise(err.Error()))+".\n\n"+transferUsage)
		return
	}

	change, err := b.tracker.RecordTransfer(actorOf(message), snap.Cycle.ID, channel, cents)
	if refusal := transferRefusal(err); refusal != "" {
		b.sendHTML(chatID, "⚠️ "+html.EscapeString(refusal)+".")
		return
	}
	if err != nil {
		log.Printf("failed to record the %s transfer of cycle %d: %v", channel, snap.Cycle.ID, err)
		b.send(chatID, "Something went wrong writing that down. Nothing was changed.")
		return
	}
	b.sendHTML(chatID, transferRecordedText(change, message.From.FirstName))
	b.refreshBoard(change.Snap)
}

// sendTransferPicker posts the accounts as buttons in a message of their own, for when
// /transfer is typed rather than tapped.
func (b *Bot) sendTransferPicker(chatID int64, snap domain.Snapshot) {
	rows := board.TransferPicker(snap)
	if len(rows) == 0 {
		b.sendHTML(chatID, "Nothing in <b>"+snap.Cycle.Month.Title()+"</b> is paid through Sheena's accounts.")
		return
	}
	msg := tgbotapi.NewMessage(chatID, "💸 Which account did money go into for <b>"+
		snap.Cycle.Month.Title()+"</b>?")
	msg.ParseMode = tgbotapi.ModeHTML
	msg.ReplyMarkup = *markupOf(rows)
	if _, err := b.sender.Send(msg); err != nil {
		log.Printf("failed to send the transfer picker to chat %d: %v", chatID, err)
	}
}

// openCycle reads a Cycle a button is about to act on, and answers the tap itself when the
// Cycle cannot be acted on.
func (b *Bot) openCycle(query *tgbotapi.CallbackQuery, cycleID int64) (domain.Snapshot, bool) {
	snap, err := b.tracker.Snapshot(cycleID)
	switch {
	case err != nil:
		log.Printf("failed to read cycle %d: %v", cycleID, err)
		b.answer(query.ID, "I could not read that month just now.")
		return snap, false
	case snap.Cycle.Closed():
		b.alert(query.ID, snap.Cycle.Month.Title()+" is closed, so its transfers cannot be changed.")
		return snap, false
	}
	return snap, true
}

// transferRefusal words a refusal from RecordTransfer for the person who asked, or returns
// "" for success or a failure that is not theirs to fix.
func transferRefusal(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, tracker.ErrNotSheenaChannel), errors.Is(err, tracker.ErrChannelUnused),
		errors.Is(err, tracker.ErrCycleClosed), errors.Is(err, tracker.ErrCycleUnknown),
		errors.Is(err, domain.ErrTransferEmpty), errors.Is(err, domain.ErrAmountNegative):
		return capitalise(err.Error())
	}
	return ""
}

// transferRecordedText is the record left in the chat of a Transfer being recorded.
func transferRecordedText(change tracker.TransferChange, who string) string {
	account := "<b>" + change.Channel.Label() + "</b>"
	sent := domain.FormatPesos(change.After.SentCents)
	month := change.Snap.Cycle.Month.Title()

	text := "✓ " + sent + " sent to " + account + " for " + month
	if change.Before != nil {
		text = "✓ Transfer to " + account + " for " + month + " changed from " +
			domain.FormatPesos(change.Before.SentCents) + " to " + sent
	}
	if funded := len(change.Moved); funded > 0 {
		text += ", funding " + plural(funded, "bill")
	}
	return text + ". — " + html.EscapeString(who)
}

func transferLine(snap domain.Snapshot, channel domain.Channel) (domain.TransferLine, bool) {
	for _, line := range snap.TransferLines() {
		if line.Channel == channel {
			return line, true
		}
	}
	return domain.TransferLine{}, false
}
