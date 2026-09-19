package tracker

import (
	"time"

	"github.com/kevin-jake/bills-bot/internal/storage"
)

// JobDone reports whether a scheduled job has already run.
func (t *Tracker) JobDone(key string) (bool, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	return storage.JobDone(t.db, key)
}

// ClaimJob records a scheduled job as done at when, and reports whether this call is the
// one that recorded it. The scheduler claims a job only after the work it stands for has
// succeeded, so a crash in between costs a repeat rather than a month nobody opened.
func (t *Tracker) ClaimJob(key string, when time.Time) (bool, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	return storage.ClaimJob(t.db, key, when)
}
