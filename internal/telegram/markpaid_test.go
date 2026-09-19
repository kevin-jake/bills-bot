package telegram

import (
	"testing"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/kevin-jake/bills-bot/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setAmount enters an amount for p as Kevin, through the prompt as a person would.
func setAmount(t *testing.T, bot *Bot, sender *fakeSender, p domain.Payable, amount string) {
	t.Helper()
	reply(bot, kevinID, askFor(t, bot, sender, kevinID, p), amount)
}

// lastAnswer is the text of the most recent answer to a tap.
func lastAnswer(t *testing.T, sender *fakeSender) tgbotapi.CallbackConfig {
	t.Helper()
	answers := requestsOf[tgbotapi.CallbackConfig](sender)
	require.NotEmpty(t, answers)
	return answers[len(answers)-1]
}

func TestTheMenuOffersMarkPaidOnceTheAmountIsKnown(t *testing.T) {
	bot, sender := newTestBot(t)
	p := openSeptember(t, bot, "Batelec")
	setAmount(t, bot, sender, p, "3120.50")

	tap(bot, groupChatID, kevinID, "b:"+itoa(p.ID))

	menus := requestsOf[tgbotapi.EditMessageReplyMarkupConfig](sender)
	require.NotEmpty(t, menus)
	row := menus[len(menus)-1].ReplyMarkup.InlineKeyboard[0]
	require.Len(t, row, 3)
	assert.Equal(t, "✓ Mark paid", row[1].Text)
	assert.Equal(t, "p:"+itoa(p.ID), *row[1].CallbackData)
	assert.Equal(t, "u:"+itoa(p.ID), *row[2].CallbackData)
}

func TestMarkPaidStrikesTheLineThrough(t *testing.T) {
	bot, sender := newTestBot(t)
	p := openSeptember(t, bot, "Batelec")
	setAmount(t, bot, sender, p, "3120.50")

	tap(bot, groupChatID, sheenaID, "p:"+itoa(p.ID))

	board := lastEdit(t, sender, 1)
	assert.Contains(t, board.Text, "✓ <s>Batelec · ₱3,120.50 · Kevin</s>")
	assert.Contains(t, board.Text, "1 of 16 paid")
	require.NotNil(t, board.ReplyMarkup, "the board's own buttons come back")
	assert.Equal(t, "✓ Batelec is paid.", lastAnswer(t, sender).Text)

	snap, err := bot.tracker.CurrentSnapshot()
	require.NoError(t, err)
	for _, got := range snap.Payables {
		if got.ID == p.ID {
			require.NotNil(t, got.PaidBy)
			assert.Equal(t, sheenaID, *got.PaidBy, "whoever tapped paid it")
		}
	}
}

func TestMarkPaidRefusalsAreAlerts(t *testing.T) {
	bot, sender := newTestBot(t)
	p := openSeptember(t, bot, "Batelec")

	tap(bot, groupChatID, kevinID, "p:"+itoa(p.ID))

	answer := lastAnswer(t, sender)
	assert.True(t, answer.ShowAlert)
	assert.Equal(t, "Set an amount first.", answer.Text)

	setAmount(t, bot, sender, p, "100")
	tap(bot, groupChatID, kevinID, "p:"+itoa(p.ID))
	tap(bot, groupChatID, kevinID, "p:"+itoa(p.ID))

	answer = lastAnswer(t, sender)
	assert.True(t, answer.ShowAlert)
	assert.Equal(t, "That is already paid.", answer.Text)
}

func TestUndoPutsTheLineBack(t *testing.T) {
	bot, sender := newTestBot(t)
	p := openSeptember(t, bot, "Batelec")
	setAmount(t, bot, sender, p, "100")
	tap(bot, groupChatID, kevinID, "p:"+itoa(p.ID))

	tap(bot, groupChatID, kevinID, "u:"+itoa(p.ID))

	assert.Contains(t, lastEdit(t, sender, 1).Text, "☐ Batelec · ₱100.00 · Kevin")
	assert.Equal(t, "↩ Undone: ☐ Batelec · ₱100.00 · Kevin", lastAnswer(t, sender).Text)
}

func TestUndoWithNothingToUndoIsAnAlert(t *testing.T) {
	bot, sender := newTestBot(t)
	p := openSeptember(t, bot, "Batelec")

	tap(bot, groupChatID, kevinID, "u:"+itoa(p.ID))

	answer := lastAnswer(t, sender)
	assert.True(t, answer.ShowAlert)
	assert.Equal(t, "There is nothing to undo: Batelec.", answer.Text)
}

// payAll enters zero for every bill in September, which pays each at once.
func payAll(t *testing.T, bot *Bot, sender *fakeSender) domain.Payable {
	t.Helper()
	snap, err := bot.tracker.CurrentSnapshot()
	require.NoError(t, err)
	for _, p := range snap.Payables {
		setAmount(t, bot, sender, p, "0")
	}
	return snap.Payables[len(snap.Payables)-1]
}

func TestPayingTheLastBillClosesTheMonthAndUnpinsItsBoard(t *testing.T) {
	bot, sender := newTestBot(t)
	openSeptember(t, bot, "Batelec")

	last := payAll(t, bot, sender)

	board := lastEdit(t, sender, 1)
	// The close is stamped by the tracker's own clock, not the bot's test clock.
	assert.Contains(t, board.Text, "✅ <b>CLOSED</b> — all paid on ")
	assert.Nil(t, board.ReplyMarkup, "a closed board has nothing to tap")

	unpins := requestsOf[tgbotapi.UnpinChatMessageConfig](sender)
	require.Len(t, unpins, 1)
	assert.Equal(t, 1, unpins[0].MessageID)

	var closing *tgbotapi.MessageConfig
	for i := range sender.messages {
		if sender.messages[i].Text == "✅ <b>September 2026</b> is all paid, so I closed it." {
			closing = &sender.messages[i]
		}
	}
	require.NotNil(t, closing, "the group is told")
	markup, ok := closing.ReplyMarkup.(tgbotapi.InlineKeyboardMarkup)
	require.True(t, ok)
	assert.Equal(t, "↩ Undo "+last.BillName, markup.InlineKeyboard[0][0].Text)
	assert.Equal(t, "u:"+itoa(last.ID), *markup.InlineKeyboard[0][0].CallbackData)
}

func TestUndoFromTheClosingMessageReopensTheMonth(t *testing.T) {
	bot, sender := newTestBot(t)
	openSeptember(t, bot, "Batelec")
	last := payAll(t, bot, sender)
	closingID := sender.nextID
	pins := len(requestsOf[tgbotapi.PinChatMessageConfig](sender))

	bot.handleCallback(&tgbotapi.CallbackQuery{
		ID:      "query",
		From:    &tgbotapi.User{ID: sheenaID, FirstName: "Sheena"},
		Message: &tgbotapi.Message{MessageID: closingID, Chat: &tgbotapi.Chat{ID: groupChatID}},
		Data:    "u:" + itoa(last.ID),
	})

	board := lastEdit(t, sender, 1)
	assert.Contains(t, board.Text, "15 of 16 paid")
	assert.NotNil(t, board.ReplyMarkup, "the buttons are back")

	allPins := requestsOf[tgbotapi.PinChatMessageConfig](sender)
	require.Len(t, allPins, pins+1)
	assert.Equal(t, 1, allPins[len(allPins)-1].MessageID, "the board is pinned again")
	assert.Equal(t, "📋 <b>September 2026</b> is open again: <b>"+last.BillName+
		"</b> is no longer paid.", sender.lastText())

	markups := requestsOf[tgbotapi.EditMessageReplyMarkupConfig](sender)
	require.NotEmpty(t, markups)
	removed := markups[len(markups)-1]
	assert.Equal(t, closingID, removed.MessageID)
	assert.Empty(t, removed.ReplyMarkup.InlineKeyboard, "the closing message's Undo has served")

	snap, err := bot.tracker.CurrentSnapshot()
	require.NoError(t, err)
	assert.False(t, snap.Cycle.Closed())
}
