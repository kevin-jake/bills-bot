package telegram

import (
	"errors"
	"log"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/kevin-jake/bills-bot/internal/board"
	"github.com/kevin-jake/bills-bot/internal/domain"
	"github.com/kevin-jake/bills-bot/internal/tracker"
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
	case board.KindBack:
		snap, err := b.tracker.Snapshot(callback.ID)
		if err != nil {
			log.Printf("failed to read cycle %d to put its board back: %v", callback.ID, err)
			b.answer(query.ID, "I could not read that month just now.")
			return
		}
		b.refreshBoard(snap)
		b.answer(query.ID, "")
	case board.KindMenu:
		b.showMenu(query, callback.ID)
	case board.KindSetAmount:
		b.startSetAmount(query, callback.ID)
	case board.KindMarkPaid:
		b.markPaid(query, callback.ID)
	case board.KindUndo:
		b.undoPayable(query, callback.ID)
	case board.KindTransfer:
		b.showTransferPicker(query, callback.ID)
	case board.KindTransferChannel:
		channel, _ := board.ChannelFromCode(callback.Arg)
		b.startTransfer(query, callback.ID, channel)
	case board.KindTransferUndo:
		b.undoTransfer(query, callback.ID)
	case board.KindMatch:
		b.resolveMatch(query, callback)
	case board.KindCancel:
		b.dismiss(query)
	default:
		b.answer(query.ID, "")
	}
}

// showMenu swaps the Board's buttons for one Payable's actions. It is the current Board
// that changes, whichever copy was tapped, so the menu appears where the Board is pinned.
func (b *Bot) showMenu(query *tgbotapi.CallbackQuery, payableID int64) {
	p, snap, ok := b.openPayable(query, payableID)
	if !ok {
		return
	}

	edit := tgbotapi.NewEditMessageReplyMarkup(snap.Cycle.BoardChatID, snap.Cycle.BoardMessageID,
		*markupOf(board.Menu(p)))
	if _, err := b.sender.Request(edit); err != nil && !isNotModified(err) {
		log.Printf("failed to show the menu for payable %d: %v", payableID, err)
		b.answer(query.ID, "I could not open that bill just now.")
		return
	}
	// The toast names the bill, since the menu's buttons do not.
	b.answer(query.ID, menuTitle(p))
}

// startSetAmount asks the person who tapped for the Payable's amount, and puts the Board's
// own buttons back so the other person is not left looking at someone else's menu.
func (b *Bot) startSetAmount(query *tgbotapi.CallbackQuery, payableID int64) {
	p, snap, ok := b.openPayable(query, payableID)
	if !ok {
		return
	}

	if err := b.askAmount(query.Message.Chat.ID, query.From, p, snap); err != nil {
		log.Printf("failed to ask for the amount of payable %d: %v", payableID, err)
		b.answer(query.ID, "I could not ask just now. Try again in a moment.")
		return
	}
	b.refreshBoard(snap)
	b.answer(query.ID, "")
}

// markPaid marks a Payable paid by whoever tapped.
func (b *Bot) markPaid(query *tgbotapi.CallbackQuery, payableID int64) {
	change, err := b.tracker.MarkPaid(actorOfUser(query.From), payableID)
	if !b.refusePayableChange(query, payableID, err) {
		return
	}
	b.showChange(change)
	b.answer(query.ID, "✓ "+change.After.BillName+" is paid.")
}

// undoPayable takes back the last thing done to a Payable. It is also offered on the
// message announcing that a month closed, which is how a closed month is reopened, so it
// does not refuse a closed Cycle.
func (b *Bot) undoPayable(query *tgbotapi.CallbackQuery, payableID int64) {
	change, err := b.tracker.Undo(actorOfUser(query.From), payableID)
	if !b.refusePayableChange(query, payableID, err) {
		return
	}
	b.showChange(change)

	// An Undo tapped anywhere but the Board has done its one job, so its button goes.
	if tapped := query.Message.MessageID; tapped != 0 && tapped != change.Snap.Cycle.BoardMessageID {
		b.removeButtons(query.Message.Chat.ID, tapped)
	}
	b.answer(query.ID, "↩ Undone: "+menuTitle(change.After))
}

// refusePayableChange answers a tap whose change the tracker refused, and reports whether
// the change went through.
func (b *Bot) refusePayableChange(query *tgbotapi.CallbackQuery, payableID int64, err error) bool {
	switch {
	case err == nil:
		return true
	case errors.Is(err, tracker.ErrPayableUnknown):
		b.alert(query.ID, "That bill is no longer on the board. Tap 🔄 Refresh.")
	case errors.Is(err, tracker.ErrCycleClosed):
		b.alert(query.ID, capitalise(err.Error())+", so its bills cannot be changed.")
	case errors.Is(err, tracker.ErrNothingToUndo), errors.Is(err, domain.ErrAmountUnknown),
		errors.Is(err, domain.ErrAlreadyPaid):
		b.alert(query.ID, capitalise(err.Error())+".")
	default:
		log.Printf("failed to change payable %d: %v", payableID, err)
		b.answer(query.ID, "Something went wrong. Nothing was changed.")
	}
	return false
}

// removeButtons takes the buttons off a message.
func (b *Bot) removeButtons(chatID int64, messageID int) {
	edit := tgbotapi.NewEditMessageReplyMarkup(chatID, messageID,
		tgbotapi.InlineKeyboardMarkup{InlineKeyboard: [][]tgbotapi.InlineKeyboardButton{}})
	if _, err := b.sender.Request(edit); err != nil && !isNotModified(err) {
		log.Printf("failed to remove the buttons from message %d: %v", messageID, err)
	}
}

// openPayable reads a Payable a button is about to act on, and answers the tap itself when
// the Payable cannot be acted on.
func (b *Bot) openPayable(query *tgbotapi.CallbackQuery, payableID int64) (domain.Payable, domain.Snapshot, bool) {
	p, snap, err := b.tracker.Payable(payableID)
	switch {
	case errors.Is(err, tracker.ErrPayableUnknown):
		b.alert(query.ID, "That bill is no longer on the board. Tap 🔄 Refresh.")
		return p, snap, false
	case err != nil:
		log.Printf("failed to read payable %d: %v", payableID, err)
		b.answer(query.ID, "I could not read that bill just now.")
		return p, snap, false
	case snap.Cycle.Closed():
		b.alert(query.ID, snap.Cycle.Month.Title()+" is closed, so its bills cannot be changed.")
		return p, snap, false
	}
	return p, snap, true
}

// menuTitle is one Payable's Board line in plain text.
func menuTitle(p domain.Payable) string {
	amount := "no amount yet"
	if p.AmountKnown() {
		amount = domain.FormatPesos(*p.AmountCents)
	}
	return board.Marker(p) + " " + p.BillName + " · " + amount + " · " + p.ChannelLabel()
}

func (b *Bot) answer(queryID, text string) {
	if _, err := b.sender.Request(tgbotapi.NewCallback(queryID, text)); err != nil {
		log.Printf("failed to answer button tap: %v", err)
	}
}

// alert answers a tap with a refusal the person has to dismiss, so it is not missed.
func (b *Bot) alert(queryID, text string) {
	if _, err := b.sender.Request(tgbotapi.NewCallbackWithAlert(queryID, text)); err != nil {
		log.Printf("failed to answer button tap: %v", err)
	}
}
