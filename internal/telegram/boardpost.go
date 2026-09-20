package telegram

import (
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

	snap, ok := b.snapshotFor(chatID, args)
	if !ok {
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
	b.pinBoard(snap.Cycle.ID, chatID, sent.MessageID)
	return nil
}

// pinBoard pins a Board quietly, and says so when the bot lacks the right to.
func (b *Bot) pinBoard(cycleID, chatID int64, messageID int) {
	pin := tgbotapi.PinChatMessageConfig{
		ChatID: chatID, MessageID: messageID, DisableNotification: true,
	}
	if _, err := b.sender.Request(pin); err != nil {
		log.Printf("failed to pin the board for cycle %d: %v", cycleID, err)
		b.send(chatID, "I could not pin the board. Make me an admin with "+
			"the Pin messages permission, then send /board again.")
	}
}

// showChange brings the Board up to date after a Payable changed, and marks the moment a
// month closes or reopens. A closed month's Board is unpinned, so the pin is left for the
// month still being worked on, and the announcement carries an Undo for the payment that
// closed it, since the closed Board itself has no buttons left to tap.
func (b *Bot) showChange(change tracker.Change) {
	b.refreshBoard(change.Snap)
	cycle := change.Snap.Cycle
	month := "<b>" + cycle.Month.Title() + "</b>"

	switch {
	case change.Closed:
		if cycle.HasBoard() {
			unpin := tgbotapi.UnpinChatMessageConfig{ChatID: cycle.BoardChatID, MessageID: cycle.BoardMessageID}
			if _, err := b.sender.Request(unpin); err != nil {
				log.Printf("could not unpin the board of closed cycle %d: %v", cycle.ID, err)
			}
		}
		msg := tgbotapi.NewMessage(b.cfg.GroupChatID, "✅ "+month+" is all paid, so I closed it.")
		msg.ParseMode = tgbotapi.ModeHTML
		msg.ReplyMarkup = *markupOf([][]board.Button{{{
			Text: "↩ Undo " + change.After.BillName,
			Data: board.Callback{Kind: board.KindUndo, ID: change.After.ID}.Encode(),
		}}})
		if _, err := b.sender.Send(msg); err != nil {
			log.Printf("failed to announce that cycle %d closed: %v", cycle.ID, err)
		}
	case change.Reopened:
		if cycle.HasBoard() {
			b.pinBoard(cycle.ID, cycle.BoardChatID, cycle.BoardMessageID)
		}
		b.sendHTML(b.cfg.GroupChatID, "📋 "+month+" is open again: <b>"+
			html.EscapeString(change.After.BillName)+"</b> is no longer paid.")
	}
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
	return markupOf(board.Keyboard(snap))
}

// markupOf converts rows of buttons into Telegram's type, or nil for none.
func markupOf(rows [][]board.Button) *tgbotapi.InlineKeyboardMarkup {
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
