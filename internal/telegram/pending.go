package telegram

import (
	"errors"
	"fmt"
	"html"
	"log"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/kevin-jake/bills-bot/internal/domain"
	"github.com/kevin-jake/bills-bot/internal/tracker"
)

// pendingTTL is how long the bot waits for an answer. Long enough to go and open a banking
// app for the figure; short enough that a number typed tomorrow is not taken as the answer.
const pendingTTL = 10 * time.Minute

// pendingKind is what the bot is waiting for someone to type.
type pendingKind int

const (
	awaitAmount pendingKind = iota + 1
	awaitTransferAmount
)

// pendingKey identifies whose answer the bot is waiting for. It is per person, so Kevin and
// Sheena can each be asked something at the same time without either answering for the other.
type pendingKey struct {
	chatID int64
	userID int64
}

// pending is a question the bot has asked and not yet had answered.
type pending struct {
	kind pendingKind
	// payableID is what an awaitAmount question is about.
	payableID int64
	// cycleID and channel are what an awaitTransferAmount question is about.
	cycleID int64
	channel domain.Channel
	// subject names what was asked about, for the wording of the prompt's final edit, so a
	// question can be closed off even when what it asked about has since gone.
	subject     string
	promptMsgID int
	expiresAt   time.Time
}

// askAmount posts the question for a Payable's amount and waits for the asker's reply.
func (b *Bot) askAmount(chatID int64, from *tgbotapi.User, p domain.Payable, snap domain.Snapshot) error {
	text := fmt.Sprintf(`%s, amount for <b>%s</b> (%s)?`,
		mention(from), html.EscapeString(p.BillName), snap.Cycle.Month.Title())
	if p.AmountKnown() {
		text += " It is " + domain.FormatPesos(*p.AmountCents) + " now."
	}
	text += " Reply with a number, 0 for nothing due, or /cancel."

	return b.ask(chatID, from, text, &pending{kind: awaitAmount, payableID: p.ID, subject: p.BillName})
}

// mention names a person in a way that notifies them.
func mention(user *tgbotapi.User) string {
	return fmt.Sprintf(`<a href="tg://user?id=%d">%s</a>`, user.ID, html.EscapeString(user.FirstName))
}

// ask posts a question to one person and waits for their reply. Any question already
// waiting for the same person is closed off, since they have moved on.
func (b *Bot) ask(chatID int64, from *tgbotapi.User, text string, next *pending) error {
	msg := tgbotapi.NewMessage(chatID, text)
	msg.ParseMode = tgbotapi.ModeHTML
	// Selective opens the reply box only for the person mentioned, not for everyone.
	msg.ReplyMarkup = tgbotapi.ForceReply{ForceReply: true, Selective: true}
	sent, err := b.sender.Send(msg)
	if err != nil {
		return err
	}

	key := pendingKey{chatID: chatID, userID: from.ID}
	next.promptMsgID = sent.MessageID
	next.expiresAt = b.now().Add(pendingTTL)

	b.mu.Lock()
	previous := b.pending[key]
	b.pending[key] = next
	b.mu.Unlock()

	if previous != nil {
		b.closePrompt(chatID, previous, "<i>Never mind — answering a newer question instead.</i>")
	}
	return nil
}

// handlePending treats a message as the answer to a question the bot asked its sender, and
// reports whether it did. A question that has expired is closed off and the message is left
// for whatever comes after.
func (b *Bot) handlePending(message *tgbotapi.Message) bool {
	key := pendingKey{chatID: message.Chat.ID, userID: message.From.ID}
	p, expired := b.lookupPending(key)
	if p == nil {
		return false
	}
	if expired {
		b.closePrompt(key.chatID, p, fmt.Sprintf(
			"<i>⌛ No answer came for <b>%s</b>, so I stopped waiting. Start again from the board.</i>",
			html.EscapeString(p.subject)))
		// Someone replying to the stale question deserves to know why nothing happened;
		// anyone else was just chatting.
		if message.ReplyToMessage != nil && message.ReplyToMessage.MessageID == p.promptMsgID {
			b.send(key.chatID, "That question expired, so I did not use your answer. "+
				"Tap the bill on the board to try again.")
			return true
		}
		return false
	}
	if message.Text == "" {
		return false
	}

	switch p.kind {
	case awaitAmount:
		b.answerAmount(message, key, p)
	case awaitTransferAmount:
		b.answerTransfer(message, key, p)
	}
	return true
}

// lookupPending returns the question waiting for key and whether its time has run out. An
// expired question is removed; a live one stays until it gets an answer the bot can use.
func (b *Bot) lookupPending(key pendingKey) (p *pending, expired bool) {
	b.mu.Lock()
	defer b.mu.Unlock()

	p = b.pending[key]
	if p != nil && b.now().After(p.expiresAt) {
		delete(b.pending, key)
		return p, true
	}
	return p, false
}

// answerAmount applies a typed amount. Something that is not an amount is refused and the
// question stays open, because it was most likely a typo.
func (b *Bot) answerAmount(message *tgbotapi.Message, key pendingKey, p *pending) {
	cents, err := domain.ParseAmount(message.Text)
	if err != nil {
		b.sendHTML(key.chatID, "⚠️ "+html.EscapeString(capitalise(err.Error()))+
			". Try again, or /cancel.")
		return
	}

	b.dropPending(key, p)
	change, err := b.tracker.SetAmount(actorOf(message), p.payableID, cents)
	switch {
	case errors.Is(err, tracker.ErrPayableUnknown), errors.Is(err, tracker.ErrCycleClosed):
		b.closePrompt(key.chatID, p, "⚠️ "+html.EscapeString(capitalise(err.Error()))+
			", so <b>"+html.EscapeString(p.subject)+"</b> was not changed.")
		return
	case err != nil:
		log.Printf("failed to set the amount of payable %d: %v", p.payableID, err)
		b.closePrompt(key.chatID, p, "Something went wrong writing that down. "+
			"<b>"+html.EscapeString(p.subject)+"</b> was not changed.")
		return
	}

	// The question keeps the record, and a reply to the answer confirms it where the person
	// who typed it is looking.
	text := amountSetText(change, message.From.FirstName)
	b.closePrompt(key.chatID, p, text)
	b.replyHTML(message, text)
	b.showChange(change)
}

// amountSetText confirms an entered amount, so the chat reads as a record of who entered
// what.
func amountSetText(change tracker.Change, who string) string {
	name := "<b>" + html.EscapeString(change.After.BillName) + "</b>"
	amount := domain.FormatPesos(*change.After.AmountCents)
	who = html.EscapeString(who)

	switch {
	case *change.After.AmountCents == 0 && change.After.AutoPaid():
		return "✓ " + name + ": nothing due this month, so it is marked paid. — " + who
	case change.After.Paid():
		return "✓ " + name + " changed to " + amount + ", still paid. — " + who
	}
	return "✓ " + name + " set to " + amount + ". — " + who
}

// handleCancel withdraws the question waiting for the sender, if there is one.
func (b *Bot) handleCancel(message *tgbotapi.Message) {
	key := pendingKey{chatID: message.Chat.ID, userID: message.From.ID}

	b.mu.Lock()
	p := b.pending[key]
	delete(b.pending, key)
	b.mu.Unlock()

	if p == nil {
		b.send(key.chatID, "There is nothing to cancel.")
		return
	}
	b.closePrompt(key.chatID, p, "<i>Cancelled — <b>"+html.EscapeString(p.subject)+
		"</b> was not changed.</i>")
}

// dropPending removes p, unless a newer question has already taken its place.
func (b *Bot) dropPending(key pendingKey, p *pending) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.pending[key] == p {
		delete(b.pending, key)
	}
}

// closePrompt rewrites a question once it is settled, which also takes away its reply box.
// Failing to is harmless: the question simply stays as it was.
func (b *Bot) closePrompt(chatID int64, p *pending, text string) {
	edit := tgbotapi.NewEditMessageText(chatID, p.promptMsgID, text)
	edit.ParseMode = tgbotapi.ModeHTML
	if _, err := b.sender.Request(edit); err != nil && !isNotModified(err) {
		log.Printf("failed to close the question in message %d: %v", p.promptMsgID, err)
	}
}
