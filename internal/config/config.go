// Package config loads and validates every setting the bot needs, once, at startup.
// A missing or malformed required value is a fatal error: the bot should crash loudly
// rather than run half-configured.
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// Config is the fully validated runtime configuration.
type Config struct {
	// TelegramToken is the BotFather token.
	TelegramToken string
	// GroupChatID is the single group chat the bot serves. Everything else is ignored.
	GroupChatID int64
	// AllowedUserIDs gates who may act. Membership of the group alone is not enough.
	AllowedUserIDs map[int64]bool
	// DBPath is the SQLite file. In Docker this is on the mounted volume.
	DBPath string
	// SchedulerEnabled turns the cycle-opening and reminder jobs on. Off during local dev.
	SchedulerEnabled bool
}

// IsAllowed reports whether a Telegram user may act on the bot.
func (c *Config) IsAllowed(telegramID int64) bool {
	return c.AllowedUserIDs[telegramID]
}

// Load reads configuration from the environment.
func Load() (*Config, error) {
	token, err := requireEnv("TELEGRAM_BOT_TOKEN")
	if err != nil {
		return nil, err
	}

	rawChatID, err := requireEnv("TELEGRAM_GROUP_CHAT_ID")
	if err != nil {
		return nil, err
	}
	chatID, err := strconv.ParseInt(strings.TrimSpace(rawChatID), 10, 64)
	if err != nil {
		return nil, fmt.Errorf("TELEGRAM_GROUP_CHAT_ID: %q is not an integer", rawChatID)
	}

	rawUsers, err := requireEnv("ALLOWED_USER_IDS")
	if err != nil {
		return nil, err
	}
	allowed, err := parseUserIDs(rawUsers)
	if err != nil {
		return nil, err
	}

	dbPath, err := requireEnv("SQLITE_PATH")
	if err != nil {
		return nil, err
	}

	return &Config{
		TelegramToken:    token,
		GroupChatID:      chatID,
		AllowedUserIDs:   allowed,
		DBPath:           dbPath,
		SchedulerEnabled: optionalBool("SCHEDULER_ENABLED", true),
	}, nil
}

// parseUserIDs turns "123, 456" into a set. At least one id is required, because a bot
// nobody is allowed to use is always a misconfiguration.
func parseUserIDs(raw string) (map[int64]bool, error) {
	out := make(map[int64]bool)
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		id, err := strconv.ParseInt(part, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("ALLOWED_USER_IDS: %q is not an integer", part)
		}
		out[id] = true
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("ALLOWED_USER_IDS must list at least one Telegram user id")
	}
	return out, nil
}

func requireEnv(key string) (string, error) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return "", fmt.Errorf("environment variable %s is required", key)
	}
	return value, nil
}

// optionalBool falls back to def when unset or unparseable.
func optionalBool(key string, def bool) bool {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return def
	}
	parsed, err := strconv.ParseBool(raw)
	if err != nil {
		return def
	}
	return parsed
}
