package telegram

import (
	"testing"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/kevin-jake/bills-bot/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// typed sends text as Kevin in the group, as message 500, and returns what the bot sent
// back, if anything.
func typed(bot *Bot, sender *fakeSender, text string) []tgbotapi.MessageConfig {
	before := len(sender.messages)
	msg := message(groupChatID, kevinID, "supergroup", text)
	msg.MessageID = 500
	msg.From.FirstName = "Kevin"
	bot.handleMessage(msg)
	return sender.messages[before:]
}

// payable reads the current state of the Payable for billName in the current month.
func payable(t *testing.T, bot *Bot, billName string) domain.Payable {
	t.Helper()
	snap, err := bot.tracker.CurrentSnapshot()
	require.NoError(t, err)
	for _, p := range snap.Payables {
		if p.BillName == billName {
			return p
		}
	}
	t.Fatalf("no payable for %s", billName)
	return domain.Payable{}
}

func TestTypingANameAndAmountSetsIt(t *testing.T) {
	bot, sender := newTestBot(t)
	openSeptember(t, bot, "RCBC JCB CC")

	sent := typed(bot, sender, "rcbc jcb 103431.23")

	require.Len(t, sent, 1)
	assert.Equal(t, "✓ <b>RCBC JCB CC</b> set to ₱103,431.23. — Kevin", sent[0].Text)
	assert.Equal(t, 500, sent[0].ReplyToMessageID, "it answers the message it read")
	assert.Contains(t, lastEdit(t, sender, 1).Text, "☐ RCBC JCB CC · ₱103,431.23 · Sheena BPI")
}

func TestTypedShortcutsFromThePlan(t *testing.T) {
	tests := []struct {
		text  string
		bill  string
		reply string
		check func(t *testing.T, p domain.Payable)
	}{
		{"PLDT = 2,499", "Internet PLDT", "✓ <b>Internet PLDT</b> set to ₱2,499.00. — Kevin",
			func(t *testing.T, p domain.Payable) { assert.Equal(t, int64(249900), *p.AmountCents) }},
		{"batelec 0", "Batelec", "✓ <b>Batelec</b>: nothing due this month, so it is marked paid. — Kevin",
			func(t *testing.T, p domain.Payable) { assert.True(t, p.AutoPaid()) }},
		{"paid bpi cc 5000", "BPI CC", "✓ <b>BPI CC</b> · ₱5,000.00 is paid. — Kevin",
			func(t *testing.T, p domain.Payable) {
				assert.True(t, p.Paid())
				assert.Equal(t, kevinID, *p.PaidBy)
			}},
	}
	for _, tt := range tests {
		t.Run(tt.text, func(t *testing.T) {
			bot, sender := newTestBot(t)
			openSeptember(t, bot, tt.bill)

			sent := typed(bot, sender, tt.text)

			require.Len(t, sent, 1)
			assert.Equal(t, tt.reply, sent[0].Text)
			tt.check(t, payable(t, bot, tt.bill))
		})
	}
}

func TestPaidWithoutAnAmountNeedsOneFirst(t *testing.T) {
	bot, sender := newTestBot(t)
	openSeptember(t, bot, "BPI CC")

	sent := typed(bot, sender, "paid bpi cc")

	require.Len(t, sent, 1)
	assert.Contains(t, sent[0].Text, "has no amount yet")
	assert.False(t, payable(t, bot, "BPI CC").Paid())

	typed(bot, sender, "bpi cc 1200")
	sent = typed(bot, sender, "bpi cc paid")

	require.Len(t, sent, 1)
	assert.Equal(t, "✓ <b>BPI CC</b> · ₱1,200.00 is paid. — Kevin", sent[0].Text)
	assert.Contains(t, lastEdit(t, sender, 1).Text, "✓ <s>BPI CC · ₱1,200.00 · Sheena BPI</s>")
}

func TestUndoTakesBackTheLastTypedChange(t *testing.T) {
	bot, sender := newTestBot(t)
	openSeptember(t, bot, "Internet PLDT")
	typed(bot, sender, "pldt 2499")

	sent := typed(bot, sender, "undo pldt")

	require.Len(t, sent, 1)
	assert.Equal(t, "↩ Undone: ? Internet PLDT · no amount yet · → RCBC Visa Airmiles — Kevin", sent[0].Text)
	assert.False(t, payable(t, bot, "Internet PLDT").AmountKnown())

	sent = typed(bot, sender, "undo batelec")
	require.Len(t, sent, 1)
	assert.Equal(t, "⚠️ There is nothing to undo on <b>Batelec</b>.", sent[0].Text)
}

func TestConversationIsLeftAlone(t *testing.T) {
	bot, sender := newTestBot(t)
	openSeptember(t, bot, "Batelec")

	for _, text := range []string{"hello", "see you at 5", "call me in 5", "dinner at 7 pm"} {
		assert.Empty(t, typed(bot, sender, text), "%q is not a shortcut", text)
	}
}

func TestANameNothingMatchesIsSaidOnlyWhenItLooksLikeABill(t *testing.T) {
	bot, sender := newTestBot(t)
	openSeptember(t, bot, "Batelec")

	sent := typed(bot, sender, "paid meralco")
	require.Len(t, sent, 1)
	assert.Equal(t, "No bill matches “meralco”. <code>/bills</code> lists them.", sent[0].Text)

	sent = typed(bot, sender, "bdo jbc 5000")
	require.Len(t, sent, 1, "a mistyped bill is still answered")
	assert.Contains(t, sent[0].Text, "No bill matches")
}

func TestAnAmbiguousNameAsksWhichOne(t *testing.T) {
	bot, sender := newTestBot(t)
	loan := openSeptember(t, bot, "BDO Home Loan")

	sent := typed(bot, sender, "bdo 5000")

	require.Len(t, sent, 1)
	assert.Equal(t, "Which one did you mean by “bdo”?", sent[0].Text)
	markup, ok := sent[0].ReplyMarkup.(tgbotapi.InlineKeyboardMarkup)
	require.True(t, ok)
	require.Len(t, markup.InlineKeyboard, 4, "three BDO bills and a way out")
	assert.Equal(t, "BDO Home Loan", markup.InlineKeyboard[2][0].Text)
	assert.Equal(t, "m:"+itoa(loan.ID)+":s:500000", *markup.InlineKeyboard[2][0].CallbackData)
	assert.Equal(t, "x", *markup.InlineKeyboard[3][0].CallbackData)
	assert.False(t, payable(t, bot, "BDO Home Loan").AmountKnown(), "nothing changes until one is picked")

	questionID := sender.nextID
	bot.handleCallback(&tgbotapi.CallbackQuery{
		ID:      "query",
		From:    &tgbotapi.User{ID: sheenaID, FirstName: "Sheena"},
		Message: &tgbotapi.Message{MessageID: questionID, Chat: &tgbotapi.Chat{ID: groupChatID}},
		Data:    *markup.InlineKeyboard[2][0].CallbackData,
	})

	assert.Equal(t, "✓ <b>BDO Home Loan</b> set to ₱5,000.00. — Sheena", lastEdit(t, sender, questionID).Text)
	assert.Nil(t, lastEdit(t, sender, questionID).ReplyMarkup, "the question's buttons go")
	assert.Contains(t, lastEdit(t, sender, 1).Text, "☐ BDO Home Loan · ₱5,000.00 · Sheena BDO")
	assert.Equal(t, int64(500000), *payable(t, bot, "BDO Home Loan").AmountCents)
}

func TestNoneOfTheseChangesNothing(t *testing.T) {
	bot, sender := newTestBot(t)
	openSeptember(t, bot, "BPI CC")
	typed(bot, sender, "paid bpi")
	questionID := sender.nextID

	bot.handleCallback(&tgbotapi.CallbackQuery{
		ID:      "query",
		From:    &tgbotapi.User{ID: kevinID},
		Message: &tgbotapi.Message{MessageID: questionID, Chat: &tgbotapi.Chat{ID: groupChatID}},
		Data:    "x",
	})

	assert.Equal(t, "<i>Never mind — nothing was changed.</i>", lastEdit(t, sender, questionID).Text)
}

func TestAMonthAtTheEndPicksThatMonth(t *testing.T) {
	bot, sender := newTestBot(t)
	say(bot, "/newmonth 2026-08")
	openSeptember(t, bot, "Water")

	sent := typed(bot, sender, "water 512 aug")

	require.Len(t, sent, 1)
	august, err := bot.tracker.MonthSnapshot(domain.Month{Year: 2026, Month: 8})
	require.NoError(t, err)
	for _, p := range august.Payables {
		if p.BillName == "Water" {
			assert.Equal(t, int64(51200), *p.AmountCents, "August's water, not September's")
		}
	}
	assert.False(t, payable(t, bot, "Water").AmountKnown())

	sent = typed(bot, sender, "water 512 jul")
	require.Len(t, sent, 1)
	assert.Equal(t, "⚠️ July 2026 has not been opened.", sent[0].Text)
}

func TestTheNewestUnpaidMonthIsChosen(t *testing.T) {
	bot, sender := newTestBot(t)
	say(bot, "/newmonth 2026-08")
	openSeptember(t, bot, "Water")

	typed(bot, sender, "water 0")
	assert.True(t, payable(t, bot, "Water").Paid(), "September is the newest month it is unpaid in")

	typed(bot, sender, "water 300")
	assert.Equal(t, int64(0), *payable(t, bot, "Water").AmountCents, "September is paid, so left alone")
	august, err := bot.tracker.MonthSnapshot(domain.Month{Year: 2026, Month: 8})
	require.NoError(t, err)
	for _, p := range august.Payables {
		if p.BillName == "Water" {
			assert.Equal(t, int64(30000), *p.AmountCents, "August is where Water is still unpaid")
		}
	}
}

func TestShortcutsBeforeAnyMonthIsOpened(t *testing.T) {
	bot, sender := newTestBot(t)

	sent := typed(bot, sender, "pldt 2499")

	require.Len(t, sent, 1)
	assert.Contains(t, sent[0].Text, "No month has been opened yet")
}

func TestAnOpenQuestionTakesTheMessageBeforeShortcuts(t *testing.T) {
	bot, sender := newTestBot(t)
	p := openSeptember(t, bot, "Batelec")
	askFor(t, bot, sender, kevinID, p)

	sent := typed(bot, sender, "pldt 2499")

	require.Len(t, sent, 1)
	assert.Contains(t, sent[0].Text, "not an amount", "it is read as the answer, and refused")
	assert.False(t, payable(t, bot, "Internet PLDT").AmountKnown())
}

func TestStrangersCannotUseShortcuts(t *testing.T) {
	bot, sender := newTestBot(t)
	openSeptember(t, bot, "Internet PLDT")

	bot.handleMessage(message(groupChatID, strangerID, "supergroup", "pldt 2499"))

	assert.False(t, payable(t, bot, "Internet PLDT").AmountKnown())
	assert.Len(t, sender.messages, 1, "only the board was ever sent")
}
