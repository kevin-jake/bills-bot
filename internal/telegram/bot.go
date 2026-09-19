// Package telegram is the bot's only interface to the outside world: it receives
// updates, decides who is allowed to act, and routes commands. Business rules live
// elsewhere; this package translates between Telegram and the rest of the bot.
package telegram

import (
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/kevin-jake/bills-bot/internal/config"
	"github.com/kevin-jake/bills-bot/internal/tracker"
)

// messageSender is the slice of the Telegram API the bot actually uses, so tests can
// substitute a fake without talking to Telegram.
type messageSender interface {
	Send(c tgbotapi.Chattable) (tgbotapi.Message, error)
	Request(c tgbotapi.Chattable) (*tgbotapi.APIResponse, error)
}

// Bot receives Telegram updates and acts on the ones it is allowed to act on.
type Bot struct {
	api     *tgbotapi.BotAPI
	sender  messageSender
	cfg     *config.Config
	tracker *tracker.Tracker
	// now is the clock /newmonth reads the current month from and questions expire by,
	// replaceable in tests.
	now func() time.Time

	// mu guards pending, which the scheduler will sweep from its own goroutine.
	mu      sync.Mutex
	pending map[pendingKey]*pending
}

// pollTimeout is how long Telegram holds a getUpdates long poll open waiting for news.
const pollTimeout = 60 * time.Second

// newHTTPClient returns the client the bot talks to Telegram through. The library's
// default client has no deadline at all, so a connection that dies silently mid-poll
// leaves the bot deaf until TCP notices; this one gives up a little after the long poll
// should have answered, and pings idle HTTP/2 connections so dead ones are found early.
func newHTTPClient() *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.HTTP2 = &http.HTTP2Config{
		SendPingTimeout: 30 * time.Second,
		PingTimeout:     15 * time.Second,
	}
	return &http.Client{
		Transport: transport,
		// Well clear of pollTimeout, so a slow but healthy poll is never cut off just as
		// it returns.
		Timeout: pollTimeout + 15*time.Second,
	}
}

// New connects to Telegram and returns a ready bot.
func New(cfg *config.Config, tracker *tracker.Tracker) (*Bot, error) {
	api, err := tgbotapi.NewBotAPIWithClient(cfg.TelegramToken, tgbotapi.APIEndpoint, newHTTPClient())
	if err != nil {
		return nil, err
	}
	return &Bot{api: api, sender: api, cfg: cfg, tracker: tracker, now: time.Now,
		pending: map[pendingKey]*pending{}}, nil
}

// newBotForTest builds a Bot with no live Telegram connection.
func newBotForTest(sender messageSender, cfg *config.Config, tracker *tracker.Tracker) *Bot {
	return &Bot{sender: sender, cfg: cfg, tracker: tracker, now: time.Now,
		pending: map[pendingKey]*pending{}}
}

const startText = "📋 <b>Bills</b>\n\n" +
	"I keep this month's bill list as a single pinned message, so it works like the " +
	"sticky note: one glance to see what is left, one tap to strike something through.\n\n" +
	"<code>/newmonth</code> opens this month with every amount blank and pins its board.\n" +
	"<code>/board</code> posts the board again when the pinned one has scrolled away.\n" +
	"Tap a bill on the board to enter its amount, mark it paid, or undo the last change to it; " +
	"0 means nothing is due and ticks it off. Once everything is paid the month closes.\n" +
	"<code>/transfer</code> records money sent into one of Sheena's accounts, which marks " +
	"that account's bills ⏳ funded.\n" +
	"<code>/bills</code> shows the standing list — what we pay every month, and who pays it.\n\n" +
	"You can also just type: <code>pldt 2499</code> or <code>PLDT = 2,499</code> enters an amount, " +
	"<code>paid bpi cc</code> (or <code>paid bpi cc 5000</code>) marks it paid, and " +
	"<code>undo pldt</code> takes back the last change. Add a month such as <code>aug</code> " +
	"or <code>2026-08</code> at the end to reach an older one."

// Start registers the command list and consumes updates until the channel closes.
func (b *Bot) Start() {
	commands := []tgbotapi.BotCommand{
		{Command: "start", Description: "What this bot does"},
		{Command: "board", Description: "Post this month's board again"},
		{Command: "newmonth", Description: "Open a month and pin its board"},
		{Command: "transfer", Description: "Record money sent into one of Sheena's accounts"},
		{Command: "bills", Description: "The standing bill list"},
		{Command: "cancel", Description: "Stop answering the question I asked you"},
	}
	if _, err := b.sender.Request(tgbotapi.NewSetMyCommands(commands...)); err != nil {
		log.Printf("failed to register bot commands: %v", err)
	}

	update := tgbotapi.NewUpdate(0)
	update.Timeout = int(pollTimeout.Seconds())

	log.Printf("bills bot started as @%s, serving group %d", b.api.Self.UserName, b.cfg.GroupChatID)

	for u := range b.api.GetUpdatesChan(update) {
		switch {
		case u.Message != nil:
			b.handleMessage(u.Message)
		case u.CallbackQuery != nil:
			b.handleCallback(u.CallbackQuery)
		}
	}
}

// handleMessage applies the gate before anything else: the bot serves exactly one group,
// and within it exactly the allowlisted people. Everyone else is met with silence rather
// than an error, so that strangers cannot probe what the bot is or who it belongs to.
func (b *Bot) handleMessage(message *tgbotapi.Message) {
	if message.From == nil || message.Chat == nil {
		return
	}

	if message.Chat.ID != b.cfg.GroupChatID {
		b.handleForeignChat(message)
		return
	}

	if !b.cfg.IsAllowed(message.From.ID) {
		log.Printf("ignoring message from non-allowlisted user %d in the group", message.From.ID)
		return
	}

	// A command always runs as a command, even mid-question, so /cancel and /board still
	// work while the bot is waiting for an amount.
	command, args := parseCommand(message.Text)
	if command != "" {
		b.handleCommand(message, command, args)
		return
	}
	if b.handlePending(message) {
		return
	}
	b.handleShortcut(message)
}

// handleForeignChat answers /start in a private chat so a confused human gets an
// explanation, and stays silent everywhere else.
func (b *Bot) handleForeignChat(message *tgbotapi.Message) {
	command, _ := parseCommand(message.Text)
	if message.Chat.IsPrivate() && command == "start" {
		b.send(message.Chat.ID, "This bot only works inside the Bills group chat it was set up for.")
		return
	}
	log.Printf("ignoring message from chat %d, which is not the configured group", message.Chat.ID)
}

func (b *Bot) handleCommand(message *tgbotapi.Message, command string, args []string) {
	switch command {
	case "start":
		b.sendHTML(message.Chat.ID, startText)
	case "board":
		b.handleBoard(message, args)
	case "newmonth":
		b.handleNewMonth(message, args)
	case "bills":
		b.handleBills(message, args)
	case "transfer":
		b.handleTransfer(message, args)
	case "cancel":
		b.handleCancel(message)
	default:
		b.send(message.Chat.ID, "I do not know that command. Try /start, /board or /bills.")
	}
}

// parseCommand pulls the command name out of a message, tolerating the @botname suffix
// Telegram adds in group chats. It deliberately does not rely on message entities, which
// keeps it straightforward to test.
func parseCommand(text string) (string, []string) {
	fields := strings.Fields(text)
	if len(fields) == 0 || !strings.HasPrefix(fields[0], "/") {
		return "", nil
	}
	name := strings.TrimPrefix(fields[0], "/")
	if at := strings.Index(name, "@"); at >= 0 {
		name = name[:at]
	}
	if name == "" {
		return "", nil
	}
	return strings.ToLower(name), fields[1:]
}

func (b *Bot) send(chatID int64, text string) {
	if _, err := b.sender.Send(tgbotapi.NewMessage(chatID, text)); err != nil {
		log.Printf("failed to send message to chat %d: %v", chatID, err)
	}
}

func (b *Bot) sendHTML(chatID int64, text string) {
	msg := tgbotapi.NewMessage(chatID, text)
	msg.ParseMode = tgbotapi.ModeHTML
	if _, err := b.sender.Send(msg); err != nil {
		log.Printf("failed to send message to chat %d: %v", chatID, err)
	}
}
