package scheduler_test

import (
	"errors"
	"testing"
	"time"

	"github.com/kevin-jake/bills-bot/internal/domain"
	"github.com/kevin-jake/bills-bot/internal/scheduler"
	"github.com/kevin-jake/bills-bot/internal/storage/storagetest"
	"github.com/kevin-jake/bills-bot/internal/tracker"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// The tests drive Tick from a clock of their own against a real Tracker, because what is
// being tested is mostly what the database remembers between ticks and between restarts.

// fakePoster records what the scheduler asked the group to be shown.
type fakePoster struct {
	boards    []domain.Snapshot
	reminders []string
	swept     []time.Time
	postErr   error
}

func (f *fakePoster) PostBoard(snap domain.Snapshot) error {
	if f.postErr != nil {
		return f.postErr
	}
	f.boards = append(f.boards, snap)
	return nil
}

func (f *fakePoster) SendReminder(text string) error {
	f.reminders = append(f.reminders, text)
	return nil
}

func (f *fakePoster) SweepPending(now time.Time) { f.swept = append(f.swept, now) }

func manila(day, hour, minute int) time.Time {
	return time.Date(2026, time.September, day, hour, minute, 0, 0, domain.Manila)
}

func newScheduler(t *testing.T) (*scheduler.Scheduler, *tracker.Tracker, *fakePoster, *gorm.DB) {
	t.Helper()
	db := storagetest.Open(t)
	tr := tracker.New(db)
	poster := &fakePoster{}
	return scheduler.New(tr, poster), tr, poster, db
}

func TestNothingHappensBeforeEightOnTheFirst(t *testing.T) {
	jobs, tr, poster, _ := newScheduler(t)

	jobs.Tick(manila(1, 7, 59))

	assert.Empty(t, poster.boards, "the month opens at eight, not at midnight")
	_, err := tr.MonthSnapshot(domain.Month{Year: 2026, Month: time.September})
	require.ErrorIs(t, err, tracker.ErrCycleUnknown)
}

func TestTheMonthOpensOnceAtEightOnTheFirst(t *testing.T) {
	jobs, tr, poster, _ := newScheduler(t)

	jobs.Tick(manila(1, 8, 0))
	jobs.Tick(manila(1, 8, 1))
	jobs.Tick(manila(2, 8, 0))

	require.Len(t, poster.boards, 1, "one board a month, however often it ticks")
	assert.Equal(t, "September 2026", poster.boards[0].Cycle.Month.Title())

	snap, err := tr.MonthSnapshot(domain.Month{Year: 2026, Month: time.September})
	require.NoError(t, err)
	assert.Len(t, snap.Payables, 16, "the standing list was copied in")
	assert.Equal(t, 16, snap.UnknownCount(), "every amount starts blank")
}

func TestARestartDoesNotOpenTheMonthAgain(t *testing.T) {
	jobs, tr, poster, db := newScheduler(t)
	jobs.Tick(manila(1, 8, 0))
	require.Len(t, poster.boards, 1)

	// A restart is a new scheduler over the same database, which is the only place the
	// first one wrote down what it had done.
	afterRestart := &fakePoster{}
	scheduler.New(tracker.New(db), afterRestart).Tick(manila(1, 9, 30))

	assert.Empty(t, afterRestart.boards)
	done, err := tr.JobDone("open:2026-09")
	require.NoError(t, err)
	assert.True(t, done)
}

func TestABotThatWasDownOpensTheMonthLate(t *testing.T) {
	jobs, _, poster, _ := newScheduler(t)

	// Nothing ran on the 1st; the bot comes back on the 3rd.
	jobs.Tick(manila(3, 14, 12))

	require.Len(t, poster.boards, 1, "a missed morning is caught up, not skipped")
}

func TestAMonthOpenedByHandIsLeftAlone(t *testing.T) {
	jobs, tr, poster, _ := newScheduler(t)
	september := domain.Month{Year: 2026, Month: time.September}
	_, opened, err := tr.OpenCycle(tracker.Actor{TelegramID: 111, Name: "Kevin"}, september)
	require.NoError(t, err)
	require.True(t, opened)
	snap, err := tr.MonthSnapshot(september)
	require.NoError(t, err)
	require.NoError(t, tr.RecordBoard(snap.Cycle.ID, -100, 42))

	jobs.Tick(manila(1, 8, 0))

	assert.Empty(t, poster.boards, "its board is already in the group")
	done, err := tr.JobDone("open:2026-09")
	require.NoError(t, err)
	assert.True(t, done, "the job is still done, so it is not tried again every minute")
}

func TestAMonthOpenedWithoutItsBoardStillGetsOne(t *testing.T) {
	jobs, tr, poster, _ := newScheduler(t)
	september := domain.Month{Year: 2026, Month: time.September}
	_, _, err := tr.OpenCycle(tracker.Actor{TelegramID: 111, Name: "Kevin"}, september)
	require.NoError(t, err)

	jobs.Tick(manila(1, 8, 0))

	require.Len(t, poster.boards, 1, "a cycle whose board never reached the group")
}

func TestAFailedBoardIsTriedAgainNextTick(t *testing.T) {
	jobs, tr, poster, _ := newScheduler(t)
	poster.postErr = errors.New("telegram is down")

	jobs.Tick(manila(1, 8, 0))

	done, err := tr.JobDone("open:2026-09")
	require.NoError(t, err)
	assert.False(t, done, "the job is not claimed until the board is posted")

	poster.postErr = nil
	jobs.Tick(manila(1, 8, 1))
	assert.Len(t, poster.boards, 1)
}

func TestTheReminderComesOnceOnTheFifteenth(t *testing.T) {
	jobs, tr, poster, _ := newScheduler(t)
	jobs.Tick(manila(1, 8, 0))

	jobs.Tick(manila(15, 7, 59))
	assert.Empty(t, poster.reminders, "not before eight")

	jobs.Tick(manila(15, 8, 0))
	jobs.Tick(manila(15, 8, 1))
	jobs.Tick(manila(16, 8, 0))

	require.Len(t, poster.reminders, 1)
	assert.Contains(t, poster.reminders[0], "⏰ <b>Bills still unpaid</b>")
	assert.Contains(t, poster.reminders[0], "<b>September 2026</b>")

	done, err := tr.JobDone("remind:2026-09")
	require.NoError(t, err)
	assert.True(t, done)
}

func TestNothingUnpaidMeansNoReminderAtAll(t *testing.T) {
	jobs, tr, poster, _ := newScheduler(t)
	jobs.Tick(manila(1, 8, 0))

	// Nothing due on every bill closes the month, which leaves nothing to nag about.
	snap, err := tr.MonthSnapshot(domain.Month{Year: 2026, Month: time.September})
	require.NoError(t, err)
	kevin := tracker.Actor{TelegramID: 111, Name: "Kevin"}
	for _, p := range snap.Payables {
		_, err := tr.SetAmount(kevin, p.ID, 0)
		require.NoError(t, err)
	}

	jobs.Tick(manila(15, 8, 0))

	assert.Empty(t, poster.reminders, "a household that is up to date hears nothing")
	done, err := tr.JobDone("remind:2026-09")
	require.NoError(t, err)
	assert.True(t, done, "and is not asked again a minute later")
}

func TestEveryTickSweepsUnansweredQuestions(t *testing.T) {
	jobs, _, poster, _ := newScheduler(t)

	jobs.Tick(manila(1, 7, 0))
	jobs.Tick(manila(1, 8, 0))

	assert.Equal(t, []time.Time{manila(1, 7, 0), manila(1, 8, 0)}, poster.swept,
		"questions expire whatever else the hour holds")
}

func TestStartAndStopRunAtLeastOneTick(t *testing.T) {
	_, tr, poster, _ := newScheduler(t)
	jobs := scheduler.New(tr, poster)

	jobs.Start()
	jobs.Stop()

	assert.NotEmpty(t, poster.swept, "the first tick happens straight away, not a minute later")
}
