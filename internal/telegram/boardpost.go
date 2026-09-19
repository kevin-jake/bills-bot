package telegram

import (
	"errors"
	"html"
	"log"
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/kevin-jake/bills-bot/internal/board"
	"github.com/kevin-jake/bills-bot/internal/domain"
	"github.com/kevin-jake/bills-bot/internal/tracker"
)

// handleNewMonth opens a Cycle and posts its Board. With no argument it opens the current
// month in Manila; "/newmonth 2026-10" opens that one instead.
func (b *Bot) handleNewMonth(message *tgbotapi.Message, args []string) {
	chatID := message.Chat.ID
	current := domain.MonthOf(b.now())

	month := current
	if len(args) > 0 {
		parsed, err := domain.ParseMonth(args[0])
		if err != nil {
			b.sendHTML(chatID, "⚠️ "+html.EscapeString(capitalise(err.Error())+"."))
			return
		}
		month = parsed
	}
	// A typo such as 2062-09 would open a month nobody can close or delete, so only a month
	// that has begun, or the one about to, may be opened.
	if month.After(current.Next()) {
		b.sendHTML(chatID, "⚠️ "+month.Title()+" is too far ahead. I can open up to "+
			current.Next().Title()+".")
		return
	}

	snap, opened, err := b.tracker.OpenCycle(actorOf(message), month)
	if err != nil {
		log.Printf("failed to open %s: %v", month, err)
		b.send(chatID, "Something went wrong opening that month. Nothing was changed.")
		return
	}

	if !opened {
		state := "is already open"
		if snap.Cycle.Closed() {
			state = "already exists and is closed"
		}
		b.sendHTML(chatID, "📋 <b>"+month.Title()+"</b> "+state+". "+
			"<code>/board "+month.String()+"</code> posts its board again.")
		return
	}

	if err := b.postBoard(snap); err != nil {
		log.Printf("opened %s but failed to post its board: %v", month, err)
		b.sendHTML(chatID, "Opened <b>"+month.Title()+"</b>, but I could not post its board. "+
			"<code>/board</code> tries again.")
	}
}

// handleBoard posts a Cycle's Board afresh at the bottom of the chat, which is what someone
// wants when the pinned one has scrolled out of sight. The previous copy is retired so that
// only one Board per Cycle carries buttons.
func (b *Bot) handleBoard(message *tgbotapi.Message, args []string) {
	chatID := message.Chat.ID

	var snap domain.Snapshot
	var err error
	if len(args) > 0 {
		month, parseErr := domain.ParseMonth(args[0])
		if parseErr != nil {
			b.sendHTML(chatID, "⚠️ "+html.EscapeString(capitalise(parseErr.Error())+"."))
			return
		}
		snap, err = b.tracker.MonthSnapshot(month)
	} else {
		snap, err = b.tracker.CurrentSnapshot()
	}

	switch {
	case errors.Is(err, tracker.ErrNoCycle):
		b.sendHTML(chatID, "No month has been opened yet. <code>/newmonth</code> opens this one.")
		return
	case errors.Is(err, tracker.ErrCycleUnknown):
		b.sendHTML(chatID, "⚠️ "+html.EscapeString(capitalise(err.Error()))+
			". <code>/newmonth</code> opens a month.")
		return
	case err != nil:
		log.Printf("failed to read a cycle for /board: %v", err)
		b.send(chatID, "I could not read the board just now. Try again in a moment.")
		return
	}

	if err := b.postBoard(snap); err != nil {
		log.Printf("failed to post the board for cycle %d: %v", snap.Cycle.ID, err)
		b.send(chatID, "I could not post the board just now. Try again in a moment.")
	}
}

// postBoard sends a Cycle's Board to the group, remembers where it went, pins it and
// retires any earlier copy. The Board always goes to the configured group, whichever chat
// asked, because that is where the household looks for it.
func (b *Bot) postBoard(snap domain.Snapshot) error {
	chatID := b.cfg.GroupChatID

	msg := tgbotapi.NewMessage(chatID, board.Render(snap))
	msg.ParseMode = tgbotapi.ModeHTML
	if markup := keyboardMarkup(snap); markup != nil {
		msg.ReplyMarkup = *markup
	}
	sent, err := b.sender.Send(msg)
	if err != nil {
		return err
	}

	if err := b.tracker.RecordBoard(snap.Cycle.ID, chatID, sent.MessageID); err != nil {
		return err
	}

	if snap.Cycle.HasBoard() && snap.Cycle.BoardMessageID != sent.MessageID {
		b.retireBoard(snap.Cycle.BoardChatID, snap.Cycle.BoardMessageID)
	}

	// A closed Cycle's Board is a record, not a checklist, so it is not pinned over the
	// month people are working on.
	if snap.Cycle.Closed() {
		return nil
	}
	pin := tgbotapi.PinChatMessageConfig{
		ChatID: chatID, MessageID: sent.MessageID, DisableNotification: true,
	}
	if _, err := b.sender.Request(pin); err != nil {
		log.Printf("failed to pin the board for cycle %d: %v", snap.Cycle.ID, err)
		b.send(chatID, "I posted the board but could not pin it. Make me an admin with "+
			"the Pin messages permission, then send /board again.")
	}
	return nil
}

// retireBoard unpins and deletes an earlier copy of a Board. Either can fail harmlessly:
// Telegram refuses to delete a bot's message after 48 hours, and the copy may already have
// been removed by hand. A copy left behind still works, because every button re-reads the
// Cycle and edits the current Board rather than the message it was tapped on.
func (b *Bot) retireBoard(chatID int64, messageID int) {
	unpin := tgbotapi.UnpinChatMessageConfig{ChatID: chatID, MessageID: messageID}
	if _, err := b.sender.Request(unpin); err != nil {
		log.Printf("could not unpin the previous board %d: %v", messageID, err)
	}
	if _, err := b.sender.Request(tgbotapi.NewDeleteMessage(chatID, messageID)); err != nil {
		log.Printf("could not delete the previous board %d: %v", messageID, err)
	}
}

// refreshBoard edits a Cycle's Board in place to match snap. A Cycle whose Board was never
// posted has nothing to edit.
func (b *Bot) refreshBoard(snap domain.Snapshot) {
	if !snap.Cycle.HasBoard() {
		return
	}

	edit := tgbotapi.NewEditMessageText(snap.Cycle.BoardChatID, snap.Cycle.BoardMessageID,
		board.Render(snap))
	edit.ParseMode = tgbotapi.ModeHTML
	// Leaving the markup out removes the buttons, which is what a closed Cycle wants.
	edit.ReplyMarkup = keyboardMarkup(snap)

	if _, err := b.sender.Request(edit); err != nil && !isNotModified(err) {
		log.Printf("failed to refresh the board for cycle %d: %v", snap.Cycle.ID, err)
	}
}

// refreshOpenBoards brings every open Cycle's Board up to date, after a change to the
// standing list that reaches into them.
func (b *Bot) refreshOpenBoards() {
	snaps, err := b.tracker.OpenSnapshots()
	if err != nil {
		log.Printf("failed to read open cycles to refresh their boards: %v", err)
		return
	}
	for _, snap := range snaps {
		b.refreshBoard(snap)
	}
}

// isNotModified recognises Telegram refusing an edit that would change nothing, which is
// what a Refresh tap on an up-to-date Board produces. It is not a failure.
func isNotModified(err error) bool {
	return strings.Contains(err.Error(), "message is not modified")
}

// keyboardMarkup converts the Board's buttons into Telegram's type, or nil for none.
func keyboardMarkup(snap domain.Snapshot) *tgbotapi.InlineKeyboardMarkup {
	rows := board.Keyboard(snap)
	if len(rows) == 0 {
		return nil
	}

	markup := tgbotapi.InlineKeyboardMarkup{}
	for _, row := range rows {
		buttons := make([]tgbotapi.InlineKeyboardButton, len(row))
		for i, button := range row {
			buttons[i] = tgbotapi.NewInlineKeyboardButtonData(button.Text, button.Data)
		}
		markup.InlineKeyboard = append(markup.InlineKeyboard, buttons)
	}
	return &markup
}
