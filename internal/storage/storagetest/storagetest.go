// Package storagetest gives tests in any package a migrated, throwaway database.
// It lives apart from storage so that production code never imports testing.
package storagetest

import (
	"path/filepath"
	"testing"

	"github.com/kevin-jake/bills-bot/internal/storage"
	"github.com/pressly/goose/v3"
	"gorm.io/gorm"
)

// Open returns a migrated database on disk, removed when the test ends. It uses a real
// file rather than :memory: so pooling and pragmas behave as they do in production.
func Open(t *testing.T) *gorm.DB {
	t.Helper()

	goose.SetLogger(goose.NopLogger())

	db, err := storage.Open(filepath.Join(t.TempDir(), "bills.db"))
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}

	dir, err := storage.FindMigrationsDir()
	if err != nil {
		t.Fatalf("locate migrations: %v", err)
	}
	if err := storage.Migrate(db, dir); err != nil {
		t.Fatalf("migrate test database: %v", err)
	}
	return db
}
