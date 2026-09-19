package telegram

import (
	"testing"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// These are the three things the scheduler may do to the group. Whether it does them at
// the right moment is the scheduler's own test; this is about what the group then sees.

func TestSweepClosesAQuestionNobodyAnswered(t *testing.T) {
	bot, sender := newTestBot(t)
	p := openSeptember(t, bot, "Batelec")
	prompt := askFor(t, bot, sender, kevinID, p)

	bot.SweepPending(midSeptember.Add(pendingTTL + time.Second))

	assert.Contains(t, lastEdit(t, sender, prompt).Text, "so I stopped waiting")
	assert.Contains(t, lastEdit(t, sender, prompt).Text, "Batelec")

	// The question is gone, so a number typed afterwards is read as a bill shortcut rather
	// than as the answer it is too late to be.
	bot.mu.Lock()
	waiting := len(bot.pending)
	bot.mu.Unlock()
	assert.Zero(t, waiting)
}

func TestSweepLeavesALiveQuestionAlone(t *testing.T) {
	bot, sender := newTestBot(t)
	p := openSeptember(t, bot, "Batelec")
	prompt := askFor(t, bot, sender, kevinID, p)
	edits := len(requestsOf[tgbotapi.EditMessageTextConfig](sender))

	bot.SweepPending(midSeptember.Add(pendingTTL - time.Minute))

	assert.Len(t, requestsOf[tgbotapi.EditMessageTextConfig](sender), edits,
		"nothing was rewritten")

	reply(bot, kevinID, prompt, "2499")
	assert.Contains(t, lastEdit(t, sender, 1).Text, "☐ Batelec · ₱2,499.00 · Kevin",
		"the answer still lands")
}

func TestSendReminderGoesToTheGroupAsHTML(t *testing.T) {
	bot, sender := newTestBot(t)

	require.NoError(t, bot.SendReminder("⏰ <b>Bills still unpaid</b>"))

	require.Len(t, sender.messages, 1)
	assert.Equal(t, groupChatID, sender.messages[0].ChatID)
	assert.Equal(t, tgbotapi.ModeHTML, sender.messages[0].ParseMode)
	assert.Equal(t, "⏰ <b>Bills still unpaid</b>", sender.messages[0].Text)
	assert.Nil(t, sender.messages[0].ReplyMarkup, "the reminder carries no buttons")
}

func TestPostBoardFromTheSchedulerPinsLikeTheCommand(t *testing.T) {
	bot, sender := newTestBot(t)
	openSeptember(t, bot, "Batelec")

	current, err := bot.tracker.CurrentSnapshot()
	require.NoError(t, err)
	require.NoError(t, bot.PostBoard(current))

	assert.Contains(t, sender.lastText(), "📋 <b>Bills — September 2026</b>")
	assert.NotEmpty(t, requestsOf[tgbotapi.PinChatMessageConfig](sender), "the board is pinned")
}
