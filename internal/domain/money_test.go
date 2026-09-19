package domain_test

import (
	"testing"

	"github.com/kevin-jake/bills-bot/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseAmount(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  int64
	}{
		{"whole pesos", "2499", 249900},
		{"with centavos", "2930.83", 293083},
		{"one decimal is tens of centavos", "16639.3", 1663930},
		{"thousands commas", "103,431.23", 10343123},
		{"peso sign", "₱2,499", 249900},
		{"php prefix", "PHP 2499.00", 249900},
		{"zero means nothing due", "0", 0},
		{"surrounding space", "  59965.24 ", 5996524},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := domain.ParseAmount(tt.input)

			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestParseAmountRefusesWhatIsNotAnAmount(t *testing.T) {
	for _, input := range []string{
		"", "abc", "-100", "1.234", ".50", "12.", "1e5", "₱", "10 000", "9999999999999999",
	} {
		_, err := domain.ParseAmount(input)

		assert.ErrorIs(t, err, domain.ErrAmountInvalid, "input %q", input)
	}
}

func TestFormatPesos(t *testing.T) {
	tests := []struct {
		cents int64
		want  string
	}{
		{0, "₱0.00"},
		{5, "₱0.05"},
		{249900, "₱2,499.00"},
		{293083, "₱2,930.83"},
		{10343123, "₱103,431.23"},
		{100000000, "₱1,000,000.00"},
		{-760353, "−₱7,603.53"},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, domain.FormatPesos(tt.cents), "cents %d", tt.cents)
	}
}

func TestFormatPesosRoundTripsThroughParseAmount(t *testing.T) {
	for _, cents := range []int64{0, 1, 99, 100, 123456789} {
		parsed, err := domain.ParseAmount(domain.FormatPesos(cents))

		require.NoError(t, err)
		assert.Equal(t, cents, parsed)
	}
}
