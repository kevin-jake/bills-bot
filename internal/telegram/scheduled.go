package telegram

import (
	"html"
	"log"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/kevin-jake/bills-bot/internal/domain"
)

// The methods here are everything the scheduler may do to the group. They are the exported
// face of what a command does when someone types it, so that a job nobody asked for reaches
// the chat through exactly the same code as a tap on the Board.

// PostBoard posts a Cycle's Board to the group and pins it.
func (b *Bot) PostBoard(snap domain.Snapshot) error {
	return b.postBoard(snap)
}

// SendReminder posts the mid-month reminder to the group. It carries no buttons: what to do
// about it is done on the pinned Board, which is still where everything lives.
func (b *Bot) SendReminder(text string) error {
	msg := tgbotapi.NewMessage(b.cfg.GroupChatID, text)
	msg.ParseMode = tgbotapi.ModeHTML
	_, err := b.sender.Send(msg)
	return err
}

// SweepPending closes off questions nobody answered. Without it an unanswered question sits
// with its reply box open until the person it was asked of happens to type again, which may
// be days later and by then reads as the bot waiting for something it has forgotten.
func (b *Bot) SweepPending(now time.Time) {
	type expired struct {
		key pendingKey
		p   *pending
	}

	// The prompts are edited outside the lock: each is a network call, and the update loop
	// needs the lock back to answer whoever is typing meanwhile.
	var stale []expired
	b.mu.Lock()
	for key, p := range b.pending {
		if now.After(p.expiresAt) {
			stale = append(stale, expired{key: key, p: p})
			delete(b.pending, key)
		}
	}
	b.mu.Unlock()

	for _, e := range stale {
		log.Printf("no answer came for %q from user %d, so the question was closed",
			e.p.subject, e.key.userID)
		b.closePrompt(e.key.chatID, e.p, expiredPromptText(e.p.subject))
	}
}

// expiredPromptText is what a question becomes once its time has run out, whether the bot
// noticed on the next message or on a sweep.
func expiredPromptText(subject string) string {
	return "<i>⌛ No answer came for <b>" + html.EscapeString(subject) +
		"</b>, so I stopped waiting. Start again from the board.</i>"
}
