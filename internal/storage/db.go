// Package storage owns the SQLite database: opening it, migrating it, and (in later
// slices) the repositories that read and write it. It holds no business rules.
package storage

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/glebarez/sqlite"
	"github.com/pressly/goose/v3"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// pragmas matter more than they look:
//   - foreign_keys is OFF by default in SQLite, so the REFERENCES clauses in the schema
//     would otherwise be decorative.
//   - WAL lets the scheduler read while a user's update is being written.
//   - busy_timeout stops a concurrent write from failing instantly.
const pragmas = "_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)"

// Open connects to the SQLite file at path with the pragmas the bot depends on.
func Open(path string) (*gorm.DB, error) {
	dsn := fmt.Sprintf("file:%s?%s", path, pragmas)

	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		return nil, fmt.Errorf("open sqlite at %s: %w", path, err)
	}

	if err := verifyPragmas(db); err != nil {
		return nil, err
	}
	return db, nil
}

// verifyPragmas fails loudly if foreign keys did not actually get enabled, rather than
// letting the bot run with silently unenforced references.
func verifyPragmas(db *gorm.DB) error {
	var on int
	if err := db.Raw("PRAGMA foreign_keys").Scan(&on).Error; err != nil {
		return fmt.Errorf("read foreign_keys pragma: %w", err)
	}
	if on != 1 {
		return fmt.Errorf("foreign_keys pragma is off; schema references would not be enforced")
	}
	return nil
}

// Migrate runs every goose migration in dir against db.
func Migrate(db *gorm.DB, dir string) error {
	sqlDB, err := db.DB()
	if err != nil {
		return fmt.Errorf("get sql.DB: %w", err)
	}
	if err := goose.SetDialect("sqlite3"); err != nil {
		return fmt.Errorf("set goose dialect: %w", err)
	}
	if err := goose.Up(sqlDB, dir); err != nil {
		return fmt.Errorf("run migrations from %s: %w", dir, err)
	}
	return nil
}

// FindMigrationsDir walks up from the working directory looking for the migrations
// folder, so tests work from any package depth and the binary works from the repo root.
func FindMigrationsDir() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("get working directory: %w", err)
	}
	for {
		candidate := filepath.Join(dir, "migrations")
		if entries, err := os.ReadDir(candidate); err == nil && len(entries) > 0 {
			return candidate, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("no migrations directory found above the working directory")
		}
		dir = parent
	}
}
