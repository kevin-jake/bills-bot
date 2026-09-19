// Command bot runs the household bills Telegram bot.
package main

import (
	"log"

	"github.com/kevin-jake/bills-bot/internal/config"
	"github.com/kevin-jake/bills-bot/internal/scheduler"
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

	tr := tracker.New(db)
	bot, err := telegram.New(cfg, tr)
	if err != nil {
		log.Fatalf("telegram: %v", err)
	}

	// The scheduler runs beside the update loop, both going through the same Tracker, whose
	// mutex is what keeps a month opening at eight in the morning from crossing a tap on
	// the Board. Local work turns it off, so a dev database is not opened months ahead.
	if cfg.SchedulerEnabled {
		jobs := scheduler.New(tr, bot)
		jobs.Start()
		defer jobs.Stop()
	}

	bot.Start()
}
