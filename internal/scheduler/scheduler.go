// Package scheduler does the things nobody types: it opens a month on the 1st, nudges the
// group mid-month about what is still unpaid, and clears away questions that were never
// answered. It owns no state. Everything it has done is written in job_runs, so a bot that
// was restarted, or was switched off over the hour a job was due, catches up rather than
// either repeating itself or skipping the month.
package scheduler

import (
	"log"
	"time"

	"github.com/kevin-jake/bills-bot/internal/board"
	"github.com/kevin-jake/bills-bot/internal/domain"
	"github.com/kevin-jake/bills-bot/internal/tracker"
)

// Cycles is the part of the Tracker the scheduler uses.
type Cycles interface {
	OpenCycle(actor tracker.Actor, month domain.Month) (domain.Snapshot, bool, error)
	OpenSnapshots() ([]domain.Snapshot, error)
	JobDone(key string) (bool, error)
	ClaimJob(key string, when time.Time) (bool, error)
}

// Poster is the part of the bot the scheduler uses: it can put a Board in the group, say
// something there, and close off questions that have gone stale.
type Poster interface {
	PostBoard(snap domain.Snapshot) error
	SendReminder(text string) error
	SweepPending(now time.Time)
}

// When the jobs fall due, in Manila. Eight in the morning is when someone is awake to see
// the Board but has not yet started paying anything.
const (
	openDay    = 1
	remindDay  = 15
	workHour   = 8
	tickPeriod = time.Minute
)

// systemActor is who the audit trail credits for work nobody asked for.
var systemActor = tracker.Actor{TelegramID: 0, Name: "system"}

// Scheduler runs the unattended jobs on a ticker.
type Scheduler struct {
	cycles Cycles
	poster Poster
	now    func() time.Time
	every  time.Duration
	stop   chan struct{}
	done   chan struct{}
}

// New returns a Scheduler that reads the real clock.
func New(cycles Cycles, poster Poster) *Scheduler {
	return &Scheduler{cycles: cycles, poster: poster, now: time.Now, every: tickPeriod}
}

// Start runs the jobs in the background until Stop. It ticks once straight away, so a bot
// that comes back up after the hour a job was due does not wait a minute to notice.
func (s *Scheduler) Start() {
	s.stop = make(chan struct{})
	s.done = make(chan struct{})

	go func() {
		defer close(s.done)
		ticker := time.NewTicker(s.every)
		defer ticker.Stop()

		s.Tick(s.now())
		for {
			select {
			case <-s.stop:
				return
			case now := <-ticker.C:
				s.Tick(now)
			}
		}
	}()
}

// Stop ends the background loop and waits for the tick in progress to finish.
func (s *Scheduler) Stop() {
	if s.stop == nil {
		return
	}
	close(s.stop)
	<-s.done
	s.stop, s.done = nil, nil
}

// Tick does whatever now says is owed. It is exported so that tests can drive it from a
// clock of their own rather than waiting for one.
func (s *Scheduler) Tick(now time.Time) {
	s.openMonth(now)
	s.remind(now)
	s.poster.SweepPending(now)
}

// openMonth opens the current month and posts its Board, once. The marker is written last:
// a crash before it costs a second attempt next minute, which opening is built to survive,
// whereas a marker written first would cost the household the month's Board altogether.
func (s *Scheduler) openMonth(now time.Time) {
	month := domain.MonthOf(now)
	if now.Before(dueAt(month, openDay)) {
		return
	}

	key := "open:" + month.String()
	if done, err := s.cycles.JobDone(key); err != nil {
		log.Printf("scheduler: could not check %s: %v", key, err)
		return
	} else if done {
		return
	}

	snap, opened, err := s.cycles.OpenCycle(systemActor, month)
	if err != nil {
		log.Printf("scheduler: could not open %s: %v", month, err)
		return
	}
	// A month someone opened by hand is left as it is, Board and all. A month that was
	// opened but whose Board never reached the group still needs posting, which is the
	// case a crash between the two leaves behind.
	if !snap.Cycle.HasBoard() {
		if err := s.poster.PostBoard(snap); err != nil {
			log.Printf("scheduler: opened %s but could not post its board: %v", month, err)
			return
		}
	}

	if _, err := s.cycles.ClaimJob(key, now); err != nil {
		log.Printf("scheduler: could not record %s: %v", key, err)
		return
	}
	if opened {
		log.Printf("scheduler: opened %s and posted its board", month)
	}
}

// remind nudges the group once a month about what is still unpaid, across every open Cycle
// rather than only the current one: an older month that is still open is the one people
// have stopped looking at.
func (s *Scheduler) remind(now time.Time) {
	month := domain.MonthOf(now)
	if now.Before(dueAt(month, remindDay)) {
		return
	}

	key := "remind:" + month.String()
	if done, err := s.cycles.JobDone(key); err != nil {
		log.Printf("scheduler: could not check %s: %v", key, err)
		return
	} else if done {
		return
	}

	snaps, err := s.cycles.OpenSnapshots()
	if err != nil {
		log.Printf("scheduler: could not read the open months to remind about: %v", err)
		return
	}

	// Nothing outstanding still claims the job: a household that is up to date on the 15th
	// should hear nothing at all this month, not be asked again every minute.
	if text := board.Reminder(snaps, now); text != "" {
		if err := s.poster.SendReminder(text); err != nil {
			log.Printf("scheduler: could not send the reminder for %s: %v", month, err)
			return
		}
	}
	if _, err := s.cycles.ClaimJob(key, now); err != nil {
		log.Printf("scheduler: could not record %s: %v", key, err)
	}
}

// dueAt is the moment a job falls due within a month: a day of it, at the working hour, in
// Manila. Comparing against it with "not before" rather than "equals" is what gives the
// catch-up: a bot that was down all morning still finds the job owed when it comes back.
func dueAt(month domain.Month, day int) time.Time {
	return time.Date(month.Year, month.Month, day, workHour, 0, 0, 0, domain.Manila)
}
