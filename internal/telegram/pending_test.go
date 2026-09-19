package telegram

import (
	"strings"
	"testing"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/kevin-jake/bills-bot/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// openSeptember opens September and returns its Payable for the named Bill.
func openSeptember(t *testing.T, bot *Bot, billName string) domain.Payable {
	t.Helper()
	say(bot, "/newmonth")
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

// reply sends text as userID in the group, as a reply to the message with replyTo.
func reply(bot *Bot, userID int64, replyTo int, text string) {
	msg := message(groupChatID, userID, "supergroup", text)
	msg.From.FirstName = map[int64]string{kevinID: "Kevin", sheenaID: "Sheena"}[userID]
	msg.MessageID = replyTo + 100
	msg.ReplyToMessage = &tgbotapi.Message{MessageID: replyTo}
	bot.handleMessage(msg)
}

// askFor taps a Payable's Set amount as userID and returns the prompt's message id.
func askFor(t *testing.T, bot *Bot, sender *fakeSender, userID int64, p domain.Payable) int {
	t.Helper()
	bot.handleCallback(&tgbotapi.CallbackQuery{
		ID:      "query",
		From:    &tgbotapi.User{ID: userID, FirstName: "Kevin"},
		Message: &tgbotapi.Message{Chat: &tgbotapi.Chat{ID: groupChatID}},
		Data:    "a:" + itoa(p.ID),
	})
	require.NotEmpty(t, sender.messages)
	require.IsType(t, tgbotapi.ForceReply{}, sender.messages[len(sender.messages)-1].ReplyMarkup,
		"the last message sent is the question")
	return sender.nextID
}

// lastEdit returns the most recent text edit made to message id.
func lastEdit(t *testing.T, sender *fakeSender, id int) tgbotapi.EditMessageTextConfig {
	t.Helper()
	edits := requestsOf[tgbotapi.EditMessageTextConfig](sender)
	for i := len(edits) - 1; i >= 0; i-- {
		if edits[i].MessageID == id {
			return edits[i]
		}
	}
	t.Fatalf("message %d was never edited", id)
	return tgbotapi.EditMessageTextConfig{}
}

func TestTappingABillShowsItsMenuOnTheBoard(t *testing.T) {
	bot, sender := newTestBot(t)
	p := openSeptember(t, bot, "RCBC JCB CC")

	tap(bot, groupChatID, sheenaID, "b:"+itoa(p.ID))

	menus := requestsOf[tgbotapi.EditMessageReplyMarkupConfig](sender)
	require.Len(t, menus, 1)
	assert.Equal(t, 1, menus[0].MessageID, "the board itself shows the menu")
	require.NotNil(t, menus[0].ReplyMarkup)
	keyboard := menus[0].ReplyMarkup.InlineKeyboard
	require.Len(t, keyboard, 2)
	assert.Equal(t, "💰 Set amount", keyboard[0][0].Text)
	assert.Equal(t, "a:"+itoa(p.ID), *keyboard[0][0].CallbackData)
	assert.Equal(t, "k:"+itoa(p.CycleID), *keyboard[1][0].CallbackData)

	answers := requestsOf[tgbotapi.CallbackConfig](sender)
	require.Len(t, answers, 1)
	assert.Equal(t, "? RCBC JCB CC · no amount yet · Sheena BPI", answers[0].Text)
}

func TestBackPutsTheBoardsButtonsBack(t *testing.T) {
	bot, sender := newTestBot(t)
	p := openSeptember(t, bot, "Water")
	tap(bot, groupChatID, kevinID, "b:"+itoa(p.ID))

	tap(bot, groupChatID, kevinID, "k:"+itoa(p.CycleID))

	edit := lastEdit(t, sender, 1)
	require.NotNil(t, edit.ReplyMarkup)
	assert.Len(t, edit.ReplyMarkup.InlineKeyboard, 9, "the whole board keyboard again")
}

func TestSetAmountAsksTheTapperByName(t *testing.T) {
	bot, sender := newTestBot(t)
	p := openSeptember(t, bot, "RCBC JCB CC")

	askFor(t, bot, sender, kevinID, p)

	prompt := sender.messages[len(sender.messages)-1]
	assert.Equal(t, groupChatID, prompt.ChatID)
	assert.Equal(t, tgbotapi.ModeHTML, prompt.ParseMode)
	assert.Equal(t, `<a href="tg://user?id=111">Kevin</a>, amount for <b>RCBC JCB CC</b> `+
		`(September 2026)? Reply with a number, 0 for nothing due, or /cancel.`, prompt.Text)
	assert.Equal(t, tgbotapi.ForceReply{ForceReply: true, Selective: true}, prompt.ReplyMarkup)

	edit := lastEdit(t, sender, 1)
	assert.Len(t, edit.ReplyMarkup.InlineKeyboard, 9, "the menu closes once the question is asked")
}

func TestReplyingWithAnAmountEditsTheBoardLine(t *testing.T) {
	bot, sender := newTestBot(t)
	p := openSeptember(t, bot, "Internet PLDT")
	prompt := askFor(t, bot, sender, kevinID, p)
	sent := len(sender.messages)

	reply(bot, kevinID, prompt, "2,499")

	require.Len(t, sender.messages, sent+1, "the answer is confirmed with one reply")
	confirmation := sender.messages[sent]
	assert.Equal(t, "✓ <b>Internet PLDT</b> set to ₱2,499.00. — Kevin", confirmation.Text)
	assert.Equal(t, prompt+100, confirmation.ReplyToMessageID, "it replies to the typed amount")
	board := lastEdit(t, sender, 1)
	assert.Contains(t, board.Text, "☐ Internet PLDT · ₱2,499.00 · → RCBC Visa Airmiles")
	assert.Contains(t, board.Text, "15 no amount yet")
	assert.Contains(t, board.Text, "plus ₱2,499.00 charged to cards")
	assert.Equal(t, "✓ <b>Internet PLDT</b> set to ₱2,499.00. — Kevin", lastEdit(t, sender, prompt).Text)

	snap, err := bot.tracker.CurrentSnapshot()
	require.NoError(t, err)
	for _, got := range snap.Payables {
		if got.ID == p.ID {
			require.True(t, got.AmountKnown())
			assert.Equal(t, int64(249900), *got.AmountCents)
		}
	}
}

func TestReplyingZeroStrikesTheLineThrough(t *testing.T) {
	bot, sender := newTestBot(t)
	p := openSeptember(t, bot, "Water")
	prompt := askFor(t, bot, sender, kevinID, p)

	reply(bot, kevinID, prompt, "0")

	board := lastEdit(t, sender, 1)
	assert.Contains(t, board.Text, "✓ <s>Water · ₱0.00 · Kevin</s>")
	assert.Contains(t, board.Text, "1 of 16 paid")
	assert.Equal(t, "✓ <b>Water</b>: nothing due this month, so it is marked paid. — Kevin",
		lastEdit(t, sender, prompt).Text)
	var buttons []string
	for _, row := range board.ReplyMarkup.InlineKeyboard {
		for _, button := range row {
			buttons = append(buttons, button.Text)
		}
	}
	assert.Contains(t, buttons, "✓ Water", "its button shows it paid too")
}

func TestAnAnswerThatIsNotAnAmountKeepsTheQuestionOpen(t *testing.T) {
	bot, sender := newTestBot(t)
	p := openSeptember(t, bot, "Batelec")
	prompt := askFor(t, bot, sender, kevinID, p)

	reply(bot, kevinID, prompt, "about 3k")

	assert.Equal(t, "⚠️ That is not an amount; write it like 2499, 2,499.50 or ₱2499. "+
		"Try again, or /cancel.", sender.lastText())

	reply(bot, kevinID, prompt, "₱3,120.50")

	assert.Contains(t, lastEdit(t, sender, 1).Text, "☐ Batelec · ₱3,120.50 · Kevin")
}

func TestOnlyTheAskedPersonAnswers(t *testing.T) {
	bot, sender := newTestBot(t)
	p := openSeptember(t, bot, "Batelec")
	askFor(t, bot, sender, kevinID, p)
	sent := len(sender.messages)

	bot.handleMessage(message(groupChatID, sheenaID, "supergroup", "1500"))

	assert.Len(t, sender.messages, sent, "Sheena was not asked, so her number is chatter")
	snap, err := bot.tracker.CurrentSnapshot()
	require.NoError(t, err)
	assert.Equal(t, 16, snap.UnknownCount())
}

func TestCancelWithdrawsTheQuestion(t *testing.T) {
	bot, sender := newTestBot(t)
	p := openSeptember(t, bot, "Batelec")
	prompt := askFor(t, bot, sender, kevinID, p)

	say(bot, "/cancel")

	assert.Equal(t, "<i>Cancelled — <b>Batelec</b> was not changed.</i>", lastEdit(t, sender, prompt).Text)
	sent := len(sender.messages)
	edits := len(requestsOf[tgbotapi.EditMessageTextConfig](sender))

	reply(bot, kevinID, prompt, "2499")

	assert.Len(t, sender.messages, sent, "a number after /cancel is ordinary chatter")
	assert.Len(t, requestsOf[tgbotapi.EditMessageTextConfig](sender), edits)
}

func TestCancelWithNothingWaiting(t *testing.T) {
	bot, sender := newTestBot(t)

	say(bot, "/cancel")

	assert.Equal(t, "There is nothing to cancel.", sender.lastText())
}

func TestOtherCommandsStillWorkWhileAQuestionWaits(t *testing.T) {
	bot, sender := newTestBot(t)
	p := openSeptember(t, bot, "Batelec")
	prompt := askFor(t, bot, sender, kevinID, p)

	say(bot, "/bills")
	reply(bot, kevinID, prompt, "100")

	assert.Contains(t, lastEdit(t, sender, 1).Text, "☐ Batelec · ₱100.00 · Kevin",
		"the question outlives an unrelated command")
}

func TestAQuestionExpiresAfterTenMinutes(t *testing.T) {
	bot, sender := newTestBot(t)
	p := openSeptember(t, bot, "Batelec")
	prompt := askFor(t, bot, sender, kevinID, p)
	bot.now = func() time.Time { return midSeptember.Add(pendingTTL + time.Second) }

	reply(bot, kevinID, prompt, "2499")

	assert.Contains(t, lastEdit(t, sender, prompt).Text, "so I stopped waiting")
	assert.Contains(t, sender.lastText(), "That question expired")
	snap, err := bot.tracker.CurrentSnapshot()
	require.NoError(t, err)
	assert.Equal(t, 16, snap.UnknownCount(), "a late answer changes nothing")
}

func TestExpiryIsQuietForUnrelatedChatter(t *testing.T) {
	bot, sender := newTestBot(t)
	p := openSeptember(t, bot, "Batelec")
	prompt := askFor(t, bot, sender, kevinID, p)
	sent := len(sender.messages)
	bot.now = func() time.Time { return midSeptember.Add(time.Hour) }

	say(bot, "dinner at 7?")

	assert.Len(t, sender.messages, sent)
	assert.Contains(t, lastEdit(t, sender, prompt).Text, "so I stopped waiting",
		"the stale question is still closed off")
}

func TestANewQuestionReplacesTheOld(t *testing.T) {
	bot, sender := newTestBot(t)
	openSeptember(t, bot, "Batelec")
	snap, err := bot.tracker.CurrentSnapshot()
	require.NoError(t, err)
	var batelec, water domain.Payable
	for _, p := range snap.Payables {
		switch p.BillName {
		case "Batelec":
			batelec = p
		case "Water":
			water = p
		}
	}
	first := askFor(t, bot, sender, kevinID, batelec)

	second := askFor(t, bot, sender, kevinID, water)
	reply(bot, kevinID, second, "450")

	assert.Contains(t, lastEdit(t, sender, first).Text, "answering a newer question")
	board := lastEdit(t, sender, 1).Text
	assert.Contains(t, board, "☐ Water · ₱450.00 · Kevin")
	assert.Contains(t, board, "? Batelec · — · Kevin")
}

func TestChangingAnAmountSaysTheCurrentOne(t *testing.T) {
	bot, sender := newTestBot(t)
	p := openSeptember(t, bot, "Batelec")
	reply(bot, kevinID, askFor(t, bot, sender, kevinID, p), "100")

	askFor(t, bot, sender, kevinID, p)

	assert.Contains(t, sender.lastText(), "? It is ₱100.00 now. Reply with")
}

func TestButtonsForAClosedMonthOrAMissingBillAreRefused(t *testing.T) {
	bot, sender, db := newTestBotWithDB(t)
	p := openSeptember(t, bot, "Batelec")

	tap(bot, groupChatID, kevinID, "b:99999")
	closeCycle(t, db, p.CycleID)
	tap(bot, groupChatID, kevinID, "a:"+itoa(p.ID))

	answers := requestsOf[tgbotapi.CallbackConfig](sender)
	require.Len(t, answers, 2)
	assert.True(t, answers[0].ShowAlert)
	assert.Contains(t, answers[0].Text, "no longer on the board")
	assert.True(t, answers[1].ShowAlert)
	assert.Equal(t, "September 2026 is closed, so its bills cannot be changed.", answers[1].Text)
	for _, m := range sender.messages {
		assert.False(t, strings.Contains(m.Text, "amount for"), "no question is asked")
	}
}

func TestAnAnswerForAMonthClosedMeanwhileIsRefused(t *testing.T) {
	bot, sender, db := newTestBotWithDB(t)
	p := openSeptember(t, bot, "Batelec")
	prompt := askFor(t, bot, sender, kevinID, p)
	closeCycle(t, db, p.CycleID)

	reply(bot, kevinID, prompt, "100")

	assert.Equal(t, "⚠️ That month is closed: September 2026, so <b>Batelec</b> was not changed.",
		lastEdit(t, sender, prompt).Text)
}
