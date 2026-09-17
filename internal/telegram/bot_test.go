package telegram

import (
	"testing"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/kevin-jake/bills-bot/internal/config"
	"github.com/kevin-jake/bills-bot/internal/storage/storagetest"
	"github.com/kevin-jake/bills-bot/internal/tracker"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	groupChatID  = int64(-1001234567890)
	kevinID      = int64(111)
	sheenaID     = int64(222)
	strangerID   = int64(999)
	privateChat  = int64(111)
	otherGroupID = int64(-1009999999999)
)

type fakeSender struct {
	messages []tgbotapi.MessageConfig
	requests []tgbotapi.Chattable
}

func (s *fakeSender) Send(c tgbotapi.Chattable) (tgbotapi.Message, error) {
	if msg, ok := c.(tgbotapi.MessageConfig); ok {
		s.messages = append(s.messages, msg)
	}
	return tgbotapi.Message{}, nil
}

func (s *fakeSender) Request(c tgbotapi.Chattable) (*tgbotapi.APIResponse, error) {
	s.requests = append(s.requests, c)
	return &tgbotapi.APIResponse{Ok: true}, nil
}

func (s *fakeSender) lastText() string {
	if len(s.messages) == 0 {
		return ""
	}
	return s.messages[len(s.messages)-1].Text
}

// newTestBot wires a bot over a real migrated database, seeded standing list and all, so
// that a command test exercises the same path as production minus Telegram itself.
func newTestBot(t *testing.T) (*Bot, *fakeSender) {
	t.Helper()

	sender := &fakeSender{}
	cfg := &config.Config{
		GroupChatID:    groupChatID,
		AllowedUserIDs: map[int64]bool{kevinID: true, sheenaID: true},
	}
	return newBotForTest(sender, cfg, tracker.New(storagetest.Open(t))), sender
}

// message builds an update as Telegram would deliver it. chatType matters because the
// bot treats a private chat differently from a group it does not serve.
func message(chatID, userID int64, chatType, text string) *tgbotapi.Message {
	return &tgbotapi.Message{
		From: &tgbotapi.User{ID: userID, FirstName: "Someone"},
		Chat: &tgbotapi.Chat{ID: chatID, Type: chatType},
		Text: text,
	}
}

func TestStartInTheGroupAnswers(t *testing.T) {
	bot, sender := newTestBot(t)

	bot.handleMessage(message(groupChatID, kevinID, "supergroup", "/start"))

	require.Len(t, sender.messages, 1)
	assert.Contains(t, sender.lastText(), "Bills")
	assert.Equal(t, tgbotapi.ModeHTML, sender.messages[0].ParseMode)
	assert.Equal(t, groupChatID, sender.messages[0].ChatID)
}

func TestBothAllowlistedUsersMayAct(t *testing.T) {
	for _, userID := range []int64{kevinID, sheenaID} {
		bot, sender := newTestBot(t)

		bot.handleMessage(message(groupChatID, userID, "supergroup", "/start"))

		assert.Len(t, sender.messages, 1, "user %d should be served", userID)
	}
}

func TestStrangerInTheGroupIsIgnoredSilently(t *testing.T) {
	bot, sender := newTestBot(t)

	bot.handleMessage(message(groupChatID, strangerID, "supergroup", "/start"))

	assert.Empty(t, sender.messages, "a stranger gets no reply at all, not even a refusal")
}

func TestPrivateChatStartIsRefusedWithAnExplanation(t *testing.T) {
	bot, sender := newTestBot(t)

	bot.handleMessage(message(privateChat, kevinID, "private", "/start"))

	require.Len(t, sender.messages, 1)
	assert.Contains(t, sender.lastText(), "only works inside")
}

func TestPrivateChatIgnoresEverythingExceptStart(t *testing.T) {
	bot, sender := newTestBot(t)

	bot.handleMessage(message(privateChat, kevinID, "private", "/board"))
	bot.handleMessage(message(privateChat, kevinID, "private", "hello"))

	assert.Empty(t, sender.messages)
}

func TestAnotherGroupIsIgnoredEntirely(t *testing.T) {
	bot, sender := newTestBot(t)

	bot.handleMessage(message(otherGroupID, kevinID, "supergroup", "/start"))

	assert.Empty(t, sender.messages, "the bot serves exactly one group")
}

func TestNonCommandChatterIsIgnored(t *testing.T) {
	bot, sender := newTestBot(t)

	bot.handleMessage(message(groupChatID, kevinID, "supergroup", "what time is dinner"))

	assert.Empty(t, sender.messages, "ordinary conversation must not be answered")
}

func TestUnknownCommandIsAnswered(t *testing.T) {
	bot, sender := newTestBot(t)

	bot.handleMessage(message(groupChatID, kevinID, "supergroup", "/nope"))

	require.Len(t, sender.messages, 1)
	assert.Contains(t, sender.lastText(), "do not know that command")
}

func TestMessageWithoutSenderOrChatIsIgnored(t *testing.T) {
	bot, sender := newTestBot(t)

	bot.handleMessage(&tgbotapi.Message{Text: "/start"})
	bot.handleMessage(&tgbotapi.Message{From: &tgbotapi.User{ID: kevinID}, Text: "/start"})

	assert.Empty(t, sender.messages)
}

func TestParseCommand(t *testing.T) {
	tests := []struct {
		name    string
		text    string
		command string
		args    []string
	}{
		{"plain command", "/start", "start", []string{}},
		{"addressed to the bot", "/start@kevin_bills_bot", "start", []string{}},
		{"with arguments", "/summary 2026-09", "summary", []string{"2026-09"}},
		{"uppercase is normalised", "/START", "start", []string{}},
		{"leading whitespace", "  /start", "start", []string{}},
		{"not a command", "paid bpi cc", "", nil},
		{"empty", "", "", nil},
		{"lone slash", "/", "", nil},
		{"slash with only a mention", "/@somebot", "", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			command, args := parseCommand(tt.text)

			assert.Equal(t, tt.command, command)
			if tt.args == nil {
				assert.Nil(t, args)
			} else {
				assert.Equal(t, tt.args, args)
			}
		})
	}
}
