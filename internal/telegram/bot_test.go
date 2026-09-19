package telegram

import (
	"errors"
	"net/http"
	"testing"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/kevin-jake/bills-bot/internal/config"
	"github.com/kevin-jake/bills-bot/internal/storage/storagetest"
	"github.com/kevin-jake/bills-bot/internal/tracker"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
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
	// nextID numbers sent messages as Telegram would, so a Board's message id can be traced.
	nextID int
	// refusePins makes pinning fail, as it does when the bot is not an admin.
	refusePins bool
	// editErr is returned from every edit, to imitate Telegram refusing one.
	editErr error
}

func (s *fakeSender) Send(c tgbotapi.Chattable) (tgbotapi.Message, error) {
	s.nextID++
	if msg, ok := c.(tgbotapi.MessageConfig); ok {
		s.messages = append(s.messages, msg)
	}
	return tgbotapi.Message{MessageID: s.nextID}, nil
}

func (s *fakeSender) Request(c tgbotapi.Chattable) (*tgbotapi.APIResponse, error) {
	s.requests = append(s.requests, c)
	if _, ok := c.(tgbotapi.PinChatMessageConfig); ok && s.refusePins {
		return nil, errors.New("Bad Request: not enough rights to manage pinned messages in the chat")
	}
	if _, ok := c.(tgbotapi.EditMessageTextConfig); ok && s.editErr != nil {
		return nil, s.editErr
	}
	return &tgbotapi.APIResponse{Ok: true}, nil
}

// requestsOf returns the requests of one type, in the order they were made.
func requestsOf[T tgbotapi.Chattable](s *fakeSender) []T {
	var found []T
	for _, r := range s.requests {
		if typed, ok := r.(T); ok {
			found = append(found, typed)
		}
	}
	return found
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
	bot, sender, _ := newTestBotWithDB(t)
	return bot, sender
}

// newTestBotWithDB is newTestBot for tests that must set up state no command can reach yet.
func newTestBotWithDB(t *testing.T) (*Bot, *fakeSender, *gorm.DB) {
	t.Helper()

	db := storagetest.Open(t)
	sender := &fakeSender{}
	cfg := &config.Config{
		GroupChatID:    groupChatID,
		AllowedUserIDs: map[int64]bool{kevinID: true, sheenaID: true},
	}
	bot := newBotForTest(sender, cfg, tracker.New(db))
	bot.now = func() time.Time { return midSeptember }
	return bot, sender, db
}

// midSeptember is the test clock: 17 September 2026, 10:00 in Manila.
var midSeptember = time.Date(2026, 9, 17, 2, 0, 0, 0, time.UTC)

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

func TestHTTPClientOutlastsLongPoll(t *testing.T) {
	client := newHTTPClient()

	// No deadline would let a silently dead connection hang the poll forever; one at or
	// just past the long poll would cut healthy polls off as they return.
	assert.Greater(t, client.Timeout, pollTimeout+5*time.Second)

	transport, ok := client.Transport.(*http.Transport)
	require.True(t, ok)
	require.NotNil(t, transport.HTTP2)
	assert.NotZero(t, transport.HTTP2.SendPingTimeout)
}
