package parse

import (
	"testing"
	"time"

	"github.com/kevin-jake/bills-bot/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var september = domain.Month{Year: 2026, Month: time.September}

func cents(n int64) *int64 { return &n }

func month(year int, m time.Month) *domain.Month { return &domain.Month{Year: year, Month: m} }

func TestParse(t *testing.T) {
	tests := []struct {
		text string
		want Shortcut
	}{
		{"rcbc jcb 103431.23", Shortcut{Verb: VerbSetAmount, Name: "rcbc jcb", Amount: cents(10343123)}},
		{"paid bpi cc", Shortcut{Verb: VerbPaid, Name: "bpi cc", Explicit: true}},
		{"Paid BPI CC 5,000", Shortcut{Verb: VerbPaid, Name: "bpi cc", Amount: cents(500000), Explicit: true}},
		{"bpi cc paid", Shortcut{Verb: VerbPaid, Name: "bpi cc", Explicit: true}},
		{"PLDT = 2,499", Shortcut{Verb: VerbSetAmount, Name: "pldt", Amount: cents(249900), Explicit: true}},
		{"pldt=2499", Shortcut{Verb: VerbSetAmount, Name: "pldt", Amount: cents(249900), Explicit: true}},
		{"batelec 0", Shortcut{Verb: VerbSetAmount, Name: "batelec", Amount: cents(0)}},
		{"water ₱512.5", Shortcut{Verb: VerbSetAmount, Name: "water", Amount: cents(51250)}},
		{"water PHP512", Shortcut{Verb: VerbSetAmount, Name: "water", Amount: cents(51200)}},
		{"undo pldt", Shortcut{Verb: VerbUndo, Name: "pldt", Explicit: true}},
		{"bdo 5000", Shortcut{Verb: VerbSetAmount, Name: "bdo", Amount: cents(500000)}},
		{"pldt 2499 aug", Shortcut{Verb: VerbSetAmount, Name: "pldt", Amount: cents(249900),
			Month: month(2026, time.August)}},
		{"pldt 2499 2026-07", Shortcut{Verb: VerbSetAmount, Name: "pldt", Amount: cents(249900),
			Month: month(2026, time.July)}},
		{"paid pldt October", Shortcut{Verb: VerbPaid, Name: "pldt", Explicit: true,
			Month: month(2026, time.October)}},
		{"undo pldt dec", Shortcut{Verb: VerbUndo, Name: "pldt", Explicit: true,
			Month: month(2025, time.December)}},
	}
	for _, tt := range tests {
		t.Run(tt.text, func(t *testing.T) {
			got, ok := Parse(tt.text, september)

			require.True(t, ok)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestParseIgnoresConversation(t *testing.T) {
	for _, text := range []string{
		"hello",
		"",
		"what time is dinner",
		"paid",
		"undo",
		"2 500",
		"paid 2499",
		"= 2499",
		"pldt =",
		"pldt aug",
		"undo pldt 2499",
		"pldt -2499",
		"pldt 24.999",
	} {
		t.Run(text, func(t *testing.T) {
			_, ok := Parse(text, september)

			assert.False(t, ok)
		})
	}
}
