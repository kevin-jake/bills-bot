package storage

import (
	"fmt"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// JobRun marks one scheduled job as done. The key carries the month it was done for
// ("open:2026-10"), so a job that should happen once a month leaves one row a month and a
// bot that was restarted, or was down over the hour it should have run, can tell what it
// still owes without keeping any state of its own.
type JobRun struct {
	JobKey string    `gorm:"primaryKey;column:job_key"`
	RanAt  time.Time `gorm:"column:ran_at"`
}

// TableName pins the table.
func (JobRun) TableName() string { return "job_runs" }

// JobDone reports whether a job has already run.
func JobDone(db *gorm.DB, key string) (bool, error) {
	var count int64
	if err := db.Model(&JobRun{}).Where("job_key = ?", key).Count(&count).Error; err != nil {
		return false, fmt.Errorf("check job %q: %w", key, err)
	}
	return count > 0, nil
}

// ClaimJob records a job as done and reports whether this call is the one that recorded it.
// The primary key does the deciding, so two bots against one database cannot both act.
func ClaimJob(db *gorm.DB, key string, at time.Time) (bool, error) {
	result := db.Clauses(clause.OnConflict{DoNothing: true}).
		Create(&JobRun{JobKey: key, RanAt: at})
	if result.Error != nil {
		return false, fmt.Errorf("claim job %q: %w", key, result.Error)
	}
	return result.RowsAffected == 1, nil
}
