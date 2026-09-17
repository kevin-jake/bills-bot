// Command bot runs the household bills Telegram bot.
package main

import (
	"log"

	"github.com/kevin-jake/bills-bot/internal/config"
	"github.com/kevin-jake/bills-bot/internal/storage"
	"github.com/kevin-jake/bills-bot/internal/telegram"
	"github.com/kevin-jake/bills-bot/internal/tracker"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("configuration: %v", err)
	}

	db, err := storage.Open(cfg.DBPath)
	if err != nil {
		log.Fatalf("database: %v", err)
	}

	migrations, err := storage.FindMigrationsDir()
	if err != nil {
		log.Fatalf("database: %v", err)
	}
	if err := storage.Migrate(db, migrations); err != nil {
		log.Fatalf("database: %v", err)
	}

	bot, err := telegram.New(cfg, tracker.New(db))
	if err != nil {
		log.Fatalf("telegram: %v", err)
	}

	bot.Start()
}
