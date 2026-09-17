// Package telegram is the bot's only interface to the outside world: it receives
// updates, decides who is allowed to act, and routes commands. Business rules live
// elsewhere; this package translates between Telegram and the rest of the bot.
package telegram

import (
	"log"
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/kevin-jake/bills-bot/internal/config"
)

// messageSender is the slice of the Telegram API the bot actually uses, so tests can
// substitute a fake without talking to Telegram.
type messageSender interface {
	Send(c tgbotapi.Chattable) (tgbotapi.Message, error)
	Request(c tgbotapi.Chattable) (*tgbotapi.APIResponse, error)
}

// Bot receives Telegram updates and acts on the ones it is allowed to act on.
type Bot struct {
	api    *tgbotapi.BotAPI
	sender messageSender
	cfg    *config.Config
}

// New connects to Telegram and returns a ready bot.
func New(cfg *config.Config) (*Bot, error) {
	api, err := tgbotapi.NewBotAPI(cfg.TelegramToken)
	if err != nil {
		return nil, err
	}
	return &Bot{api: api, sender: api, cfg: cfg}, nil
}

// newBotForTest builds a Bot with no live Telegram connection.
func newBotForTest(sender messageSender, cfg *config.Config) *Bot {
	return &Bot{sender: sender, cfg: cfg}
}

const startText = "📋 <b>Bills</b>\n\n" +
	"I keep this month's bill list as a single pinned message, so it works like the " +
	"sticky note: one glance to see what is left, one tap to strike something through.\n\n" +
	"Nothing is set up yet. More commands arrive as the bot is built."

// Start registers the command list and consumes updates until the channel closes.
func (b *Bot) Start() {
	commands := []tgbotapi.BotCommand{
		{Command: "start", Description: "What this bot does"},
	}
	if _, err := b.sender.Request(tgbotapi.NewSetMyCommands(commands...)); err != nil {
		log.Printf("failed to register bot commands: %v", err)
	}

	update := tgbotapi.NewUpdate(0)
	update.Timeout = 60

	log.Printf("bills bot started as @%s, serving group %d", b.api.Self.UserName, b.cfg.GroupChatID)

	for u := range b.api.GetUpdatesChan(update) {
		if u.Message != nil {
			b.handleMessage(u.Message)
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

	command, args := parseCommand(message.Text)
	if command == "" {
		return
	}
	b.handleCommand(message, command, args)
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

func (b *Bot) handleCommand(message *tgbotapi.Message, command string, _ []string) {
	switch command {
	case "start":
		b.sendHTML(message.Chat.ID, startText)
	default:
		b.send(message.Chat.ID, "I do not know that command. Try /start.")
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
