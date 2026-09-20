package domain_test

import (
	"testing"
	"time"

	"github.com/kevin-jake/bills-bot/internal/domain"
	"github.com/stretchr/testify/assert"
)

func TestHistoryAverage(t *testing.T) {
	month := func(m time.Month) domain.Month { return domain.Month{Year: 2026, Month: m} }
	entry := func(m time.Month, amount *int64) domain.HistoryEntry {
		return domain.HistoryEntry{Month: month(m), Payable: domain.Payable{AmountCents: amount}}
	}

	tests := []struct {
		name    string
		entries []domain.HistoryEntry
		cents   int64
		months  int
	}{
		{name: "no months at all"},
		{
			name:    "months without an amount are left out rather than counted as nothing",
			entries: []domain.HistoryEntry{entry(time.September, cents(300000)), entry(time.August, nil)},
			cents:   300000,
			months:  1,
		},
		{
			name: "a month entered as zero is a real amount and counts",
			entries: []domain.HistoryEntry{
				entry(time.September, cents(300000)), entry(time.August, cents(0))},
			cents:  150000,
			months: 2,
		},
		{
			name:    "nothing has an amount yet",
			entries: []domain.HistoryEntry{entry(time.September, nil)},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			average, months := domain.History{Bill: "Batelec", Entries: tt.entries}.Average()

			assert.Equal(t, tt.cents, average)
			assert.Equal(t, tt.months, months)
		})
	}
}

func TestNamesOfWhoeverActed(t *testing.T) {
	names := domain.Names{111: "Kevin"}
	id := func(v int64) *int64 { return &v }

	assert.Equal(t, "Kevin", names.Of(id(111)))
	assert.Equal(t, "", names.Of(nil), "nobody has paid it")
	assert.Equal(t, "nobody", names.Of(id(0)), "a zero amount ticks itself off")
	assert.Equal(t, "someone", names.Of(id(222)), "acted before the audit trail knew a name")
}

func TestMonthShort(t *testing.T) {
	assert.Equal(t, "Sep 2026", domain.Month{Year: 2026, Month: time.September}.Short())
}
