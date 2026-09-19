package telegram

import (
	"testing"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// askTransferFor taps an account in the picker as Kevin and returns the prompt's id.
func askTransferFor(t *testing.T, bot *Bot, sender *fakeSender, cycleID int64, code string) int {
	t.Helper()
	bot.handleCallback(&tgbotapi.CallbackQuery{
		ID:      "query",
		From:    &tgbotapi.User{ID: kevinID, FirstName: "Kevin"},
		Message: &tgbotapi.Message{Chat: &tgbotapi.Chat{ID: groupChatID}},
		Data:    "tc:" + itoa(cycleID) + ":" + code,
	})
	require.IsType(t, tgbotapi.ForceReply{}, sender.messages[len(sender.messages)-1].ReplyMarkup,
		"the last message sent is the question")
	return sender.nextID
}

func TestTransferButtonShowsTheAccountsOnTheBoard(t *testing.T) {
	bot, sender := newTestBot(t)
	p := openSeptember(t, bot, "BDO Home Loan")

	tap(bot, groupChatID, kevinID, "t:"+itoa(p.CycleID))

	menus := requestsOf[tgbotapi.EditMessageReplyMarkupConfig](sender)
	require.Len(t, menus, 1)
	assert.Equal(t, 1, menus[0].MessageID)
	keyboard := menus[0].ReplyMarkup.InlineKeyboard
	require.Len(t, keyboard, 4, "BDO, BPI, PSBank, then back")
	assert.Equal(t, "💸 Sheena BDO · need ₱0.00?", keyboard[0][0].Text)
	assert.Equal(t, "tc:"+itoa(p.CycleID)+":bdo", *keyboard[0][0].CallbackData)
	assert.Equal(t, "k:"+itoa(p.CycleID), *keyboard[3][0].CallbackData)
	assert.Equal(t, "Which account did the money go into?", lastAnswer(t, sender).Text)
}

func TestRecordingATransferFundsTheAccountsBills(t *testing.T) {
	bot, sender := newTestBot(t)
	p := openSeptember(t, bot, "BDO Home Loan")
	setAmount(t, bot, sender, p, "16,639.31")

	prompt := askTransferFor(t, bot, sender, p.CycleID, "bdo")

	assert.Equal(t, `<a href="tg://user?id=111">Kevin</a>, how much went into <b>Sheena BDO</b> for `+
		`September 2026? It needs ₱16,639.31 so far, with 2 amounts still unknown. `+
		`Reply with a number, or /cancel.`, sender.lastText())

	sent := len(sender.messages)
	reply(bot, kevinID, prompt, "17000")

	recorded := "✓ ₱17,000.00 sent to <b>Sheena BDO</b> for September 2026, funding 3 bills. — Kevin"
	assert.Equal(t, recorded, lastEdit(t, sender, prompt).Text)
	require.Len(t, sender.messages, sent+1, "the transfer is confirmed with one reply")
	assert.Equal(t, recorded, sender.messages[sent].Text)
	assert.Equal(t, prompt+100, sender.messages[sent].ReplyToMessageID, "it replies to the typed amount")
	board := lastEdit(t, sender, 1).Text
	assert.Contains(t, board, "⏳ BDO Home Loan · ₱16,639.31 · Sheena BDO")
	assert.Contains(t, board, "? BDO JCB CC · — · Sheena BDO", "an unknown amount still shows ?")
	assert.Contains(t, board, "Sheena BDO: need ₱16,639.31 <i>(tentative, 2 unknown)</i> · "+
		"sent ₱17,000.00 (+₱360.69) ⏳")
}

func TestATransferOfNothingKeepsTheQuestionOpen(t *testing.T) {
	bot, sender := newTestBot(t)
	p := openSeptember(t, bot, "BDO Home Loan")
	prompt := askTransferFor(t, bot, sender, p.CycleID, "bdo")

	reply(bot, kevinID, prompt, "0")

	assert.Equal(t, "⚠️ A transfer has to be more than ₱0.00. Try again, or /cancel.", sender.lastText())
	reply(bot, kevinID, prompt, "500")
	assert.Contains(t, lastEdit(t, sender, prompt).Text, "✓ ₱500.00 sent to <b>Sheena BDO</b>")
}

func TestUndoTransferFromThePicker(t *testing.T) {
	bot, sender := newTestBot(t)
	p := openSeptember(t, bot, "BDO Home Loan")
	say(bot, "/transfer bdo 500")
	snap, err := bot.tracker.CurrentSnapshot()
	require.NoError(t, err)
	require.Len(t, snap.Transfers, 1)

	tap(bot, groupChatID, kevinID, "t:"+itoa(p.CycleID))
	menus := requestsOf[tgbotapi.EditMessageReplyMarkupConfig](sender)
	bdo := menus[len(menus)-1].ReplyMarkup.InlineKeyboard[0]
	require.Len(t, bdo, 2)
	assert.Equal(t, "💸 Sheena BDO · sent ₱500.00", bdo[0].Text)
	assert.Equal(t, "tu:"+itoa(snap.Transfers[0].ID), *bdo[1].CallbackData)

	tap(bot, groupChatID, kevinID, *bdo[1].CallbackData)

	assert.Equal(t, "↩ The Sheena BDO transfer of ₱500.00 is undone.", lastAnswer(t, sender).Text)
	board := lastEdit(t, sender, 1).Text
	assert.Contains(t, board, "Sheena BDO: need ₱0.00 <i>(tentative, 3 unknown)</i> · not sent")
	assert.NotContains(t, board, "⏳")
}

func TestTransferCommand(t *testing.T) {
	tests := []struct {
		name string
		text string
		want string
	}{
		{"records straight away", "/transfer bpi 213,205.11",
			"✓ ₱213,205.11 sent to <b>Sheena BPI</b> for September 2026, funding 5 bills. — Someone"},
		{"takes the spoken account name", "/transfer sheena psbank 24515",
			"✓ ₱24,515.00 sent to <b>Sheena PSBank</b>"},
		{"names the month", "/transfer 2026-09 bdo 100", "sent to <b>Sheena BDO</b> for September 2026"},
		{"refuses Kevin's own channel", "/transfer kevin 100", "Only Sheena&#39;s accounts take transfers, not Kevin."},
		{"refuses an unknown account", "/transfer gcash 100", "is not a payment channel"},
		{"refuses a bad amount", "/transfer bdo lots", "That is not an amount"},
		{"refuses nothing", "/transfer bdo 0", "A transfer has to be more than ₱0.00."},
		{"explains itself", "/transfer bdo", "<code>/transfer &lt;account&gt; &lt;amount&gt;</code>"},
		{"refuses a month never opened", "/transfer 2026-07 bdo 100", "That month has not been opened: July 2026"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bot, sender := newTestBot(t)
			openSeptember(t, bot, "BDO Home Loan")

			say(bot, tt.text)

			assert.Contains(t, sender.lastText(), tt.want)
		})
	}
}

func TestTransferCommandWithNoArgumentsOffersTheAccounts(t *testing.T) {
	bot, sender := newTestBot(t)
	p := openSeptember(t, bot, "BDO Home Loan")

	say(bot, "/transfer")

	last := sender.messages[len(sender.messages)-1]
	assert.Equal(t, "💸 Which account did money go into for <b>September 2026</b>?", last.Text)
	markup, ok := last.ReplyMarkup.(tgbotapi.InlineKeyboardMarkup)
	require.True(t, ok)
	require.Len(t, markup.InlineKeyboard, 3, "no Back: this is not the board")
	assert.Equal(t, "tc:"+itoa(p.CycleID)+":psb", *markup.InlineKeyboard[2][0].CallbackData)
}

func TestTransfersForAClosedMonthAreRefused(t *testing.T) {
	bot, sender, db := newTestBotWithDB(t)
	p := openSeptember(t, bot, "BDO Home Loan")
	closeCycle(t, db, p.CycleID)

	say(bot, "/transfer 2026-09 bdo 100")
	assert.Contains(t, sender.lastText(), "is closed, so its transfers cannot be changed")

	tap(bot, groupChatID, kevinID, "t:"+itoa(p.CycleID))
	answer := lastAnswer(t, sender)
	assert.True(t, answer.ShowAlert)
	assert.Equal(t, "September 2026 is closed, so its transfers cannot be changed.", answer.Text)
}

func TestSetAmountAfterZeroOnAFundedAccountShowsFunded(t *testing.T) {
	bot, sender := newTestBot(t)
	p := openSeptember(t, bot, "HSBC CC")
	setAmount(t, bot, sender, p, "0")
	say(bot, "/transfer bpi 100")

	setAmount(t, bot, sender, p, "5000")

	assert.Contains(t, lastEdit(t, sender, 1).Text, "⏳ HSBC CC · ₱5,000.00 · Sheena BPI")
}
