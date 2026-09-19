package domain_test

import (
	"testing"
	"time"

	"github.com/kevin-jake/bills-bot/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMonthOfUsesManilaNotUTC(t *testing.T) {
	tests := []struct {
		name string
		at   time.Time
		want string
	}{
		{"midnight on the 1st in manila is still august in utc",
			time.Date(2026, 8, 31, 16, 0, 0, 0, time.UTC), "2026-09"},
		{"one second earlier is august in manila",
			time.Date(2026, 8, 31, 15, 59, 59, 0, time.UTC), "2026-08"},
		{"new year", time.Date(2026, 12, 31, 16, 30, 0, 0, time.UTC), "2027-01"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, domain.MonthOf(tt.at).String())
		})
	}
}

func TestParseMonth(t *testing.T) {
	month, err := domain.ParseMonth("2026-09")

	require.NoError(t, err)
	assert.Equal(t, domain.Month{Year: 2026, Month: time.September}, month)
	assert.Equal(t, "2026-09", month.String())
	assert.Equal(t, "September 2026", month.Title())
}

func TestParseMonthRefusesOtherShapes(t *testing.T) {
	for _, input := range []string{"", "2026-9", "2026-13", "09-2026", "sept", "2026-09-01"} {
		_, err := domain.ParseMonth(input)

		assert.ErrorIs(t, err, domain.ErrMonthInvalid, "input %q", input)
	}
}

func TestMonthArithmetic(t *testing.T) {
	december := domain.Month{Year: 2026, Month: time.December}

	assert.Equal(t, domain.Month{Year: 2027, Month: time.January}, december.Next())
	assert.True(t, december.Next().After(december))
	assert.False(t, december.After(december))
	assert.Equal(t, time.Date(2026, 12, 1, 0, 0, 0, 0, domain.Manila), december.Start())
}
