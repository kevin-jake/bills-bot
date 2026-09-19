package telegram

import (
	"log"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/kevin-jake/bills-bot/internal/board"
)

// handleCallback acts on a tap on one of the Board's buttons. Every tap is answered, even a
// refused one, because Telegram keeps a spinner on the button until it is.
func (b *Bot) handleCallback(query *tgbotapi.CallbackQuery) {
	if query.From == nil || query.Message == nil || query.Message.Chat == nil {
		b.answer(query.ID, "")
		return
	}
	// The same gate as messages, and just as quiet: a stranger's tap is answered with
	// nothing, so the spinner stops without the bot saying anything about itself.
	if query.Message.Chat.ID != b.cfg.GroupChatID || !b.cfg.IsAllowed(query.From.ID) {
		log.Printf("ignoring a button tap from user %d in chat %d", query.From.ID, query.Message.Chat.ID)
		b.answer(query.ID, "")
		return
	}

	callback, err := board.DecodeCallback(query.Data)
	if err != nil {
		log.Printf("ignoring unrecognised button data %q", query.Data)
		b.answer(query.ID, "")
		return
	}

	switch callback.Kind {
	case board.KindRefresh:
		snap, err := b.tracker.Snapshot(callback.ID)
		if err != nil {
			log.Printf("failed to read cycle %d to refresh its board: %v", callback.ID, err)
			b.answer(query.ID, "I could not read that month just now.")
			return
		}
		b.refreshBoard(snap)
		b.answer(query.ID, "Board is up to date.")
	default:
		// Setting amounts, marking paid and recording Transfers are still being built.
		b.answer(query.ID, "That button does not work yet.")
	}
}

func (b *Bot) answer(queryID, text string) {
	if _, err := b.sender.Request(tgbotapi.NewCallback(queryID, text)); err != nil {
		log.Printf("failed to answer button tap: %v", err)
	}
}
