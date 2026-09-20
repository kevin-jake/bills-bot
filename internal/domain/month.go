package domain

import (
	"errors"
	"fmt"
	"time"
)

// Manila is the household's clock. The Philippines has kept UTC+8 without daylight saving
// since 1978, so a fixed zone is exact and spares the binary a tzdata lookup that could
// fail in a slim container.
var Manila = time.FixedZone("Asia/Manila", 8*60*60)

// ErrMonthInvalid is returned for anything that does not read as YYYY-MM.
var ErrMonthInvalid = errors.New("a month is written YYYY-MM, like 2026-09")

// Month is a calendar month in Manila, which is what a Cycle covers. It is a value, so two
// Months compare with ==.
type Month struct {
	Year  int
	Month time.Month
}

// MonthOf returns the Manila month t falls in. Just after midnight on the 1st in Manila is
// still the previous day in UTC, which is exactly the case this exists to get right.
func MonthOf(t time.Time) Month {
	local := t.In(Manila)
	return Month{Year: local.Year(), Month: local.Month()}
}

// ParseMonth reads "2026-09".
func ParseMonth(s string) (Month, error) {
	parsed, err := time.ParseInLocation("2006-01", s, Manila)
	if err != nil || len(s) != 7 {
		return Month{}, ErrMonthInvalid
	}
	return Month{Year: parsed.Year(), Month: parsed.Month()}, nil
}

// String is the stored form, "2026-09", which is also the one people type.
func (m Month) String() string {
	return fmt.Sprintf("%04d-%02d", m.Year, int(m.Month))
}

// Title is how the Board names a month: "September 2026".
func (m Month) Title() string {
	return fmt.Sprintf("%s %d", m.Month, m.Year)
}

// Short is how a list of months names one: "Sep 2026". A history of twelve rows is easier
// to read down when every month is the same width.
func (m Month) Short() string {
	return fmt.Sprintf("%s %d", m.Month.String()[:3], m.Year)
}

// Start is midnight on the 1st, in Manila.
func (m Month) Start() time.Time {
	return time.Date(m.Year, m.Month, 1, 0, 0, 0, 0, Manila)
}

// Days is how many days the month has, which is what a due day past the end of a short
// month is pulled back to.
func (m Month) Days() int {
	return time.Date(m.Year, m.Month+1, 0, 0, 0, 0, 0, Manila).Day()
}

// Next is the month after m.
func (m Month) Next() Month {
	return MonthOf(m.Start().AddDate(0, 1, 0))
}

// After reports whether m comes later than other.
func (m Month) After(other Month) bool {
	return m.Start().After(other.Start())
}
