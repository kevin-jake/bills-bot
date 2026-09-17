package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setValidEnv(t *testing.T) {
	t.Setenv("TELEGRAM_BOT_TOKEN", "token")
	t.Setenv("TELEGRAM_GROUP_CHAT_ID", "-1001234567890")
	t.Setenv("ALLOWED_USER_IDS", "111,222")
	t.Setenv("SQLITE_PATH", "./data/bills.db")
}

func TestLoad(t *testing.T) {
	setValidEnv(t)

	cfg, err := Load()
	require.NoError(t, err)

	assert.Equal(t, "token", cfg.TelegramToken)
	assert.Equal(t, int64(-1001234567890), cfg.GroupChatID)
	assert.Equal(t, "./data/bills.db", cfg.DBPath)
	assert.True(t, cfg.SchedulerEnabled, "scheduler defaults on")
	assert.True(t, cfg.IsAllowed(111))
	assert.True(t, cfg.IsAllowed(222))
	assert.False(t, cfg.IsAllowed(333))
}

func TestLoadMissingRequired(t *testing.T) {
	for _, key := range []string{
		"TELEGRAM_BOT_TOKEN",
		"TELEGRAM_GROUP_CHAT_ID",
		"ALLOWED_USER_IDS",
		"SQLITE_PATH",
	} {
		t.Run(key, func(t *testing.T) {
			setValidEnv(t)
			t.Setenv(key, "")

			_, err := Load()
			require.Error(t, err)
			assert.Contains(t, err.Error(), key)
		})
	}
}

func TestLoadRejectsMalformed(t *testing.T) {
	tests := []struct {
		name  string
		key   string
		value string
		want  string
	}{
		{"chat id not a number", "TELEGRAM_GROUP_CHAT_ID", "not-a-number", "TELEGRAM_GROUP_CHAT_ID"},
		{"user id not a number", "ALLOWED_USER_IDS", "111,abc", "ALLOWED_USER_IDS"},
		{"only separators", "ALLOWED_USER_IDS", " , , ", "at least one"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setValidEnv(t)
			t.Setenv(tt.key, tt.value)

			_, err := Load()
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.want)
		})
	}
}

func TestSchedulerEnabledOptional(t *testing.T) {
	tests := []struct {
		raw  string
		want bool
	}{
		{"false", false},
		{"0", false},
		{"true", true},
		{"nonsense", true},
	}
	for _, tt := range tests {
		t.Run(tt.raw, func(t *testing.T) {
			setValidEnv(t)
			t.Setenv("SCHEDULER_ENABLED", tt.raw)

			cfg, err := Load()
			require.NoError(t, err)
			assert.Equal(t, tt.want, cfg.SchedulerEnabled)
		})
	}
}
