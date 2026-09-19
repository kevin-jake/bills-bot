package domain_test

import (
	"testing"
	"time"

	"github.com/kevin-jake/bills-bot/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDisplayNameCarriesACardsLastFourDigits(t *testing.T) {
	card := domain.Bill{Name: "BPI Visa CC", CardLast4: "7577"}
	loan := domain.Bill{Name: "PSBank Car Loan"}

	assert.Equal(t, "BPI Visa CC ••7577", card.DisplayName())
	assert.Equal(t, "PSBank Car Loan", loan.DisplayName(), "a loan has no digits to show")
}

func TestDueLabel(t *testing.T) {
	assert.Equal(t, "due 28", domain.Bill{DueDay: 28}.DueLabel())
	assert.Equal(t, "due 5", domain.Bill{DueDay: 5}.DueLabel())
	assert.Empty(t, domain.Bill{}.DueLabel(), "a bill with no fixed day says nothing")
}

func TestDueDateFallsOnTheDueDayInManila(t *testing.T) {
	due := domain.DueDate(domain.Month{Year: 2026, Month: time.September}, 24)

	assert.Equal(t, time.Date(2026, time.September, 24, 0, 0, 0, 0, domain.Manila), due)
}

func TestDueDatePullsADayBackIntoAShortMonth(t *testing.T) {
	// A card due on the 28th needs no help; one due on the 31st must not slip into March,
	// which would make a February payment look a day late every leap year.
	feb := domain.Month{Year: 2026, Month: time.February}

	assert.Equal(t, 28, domain.DueDate(feb, 31).Day())
	assert.Equal(t, 28, domain.DueDate(feb, 28).Day())
	assert.Equal(t, 29, domain.DueDate(domain.Month{Year: 2028, Month: time.February}, 31).Day())
	assert.Equal(t, 30, domain.DueDate(domain.Month{Year: 2026, Month: time.April}, 31).Day())
}

func TestDueDateOfABillWithNoFixedDayIsNothing(t *testing.T) {
	assert.True(t, domain.DueDate(domain.Month{Year: 2026, Month: time.September}, 0).IsZero())
}

func TestValidateCardLast4(t *testing.T) {
	tests := []struct {
		name    string
		last4   string
		wantErr error
	}{
		{"four digits", "7577", nil},
		{"leading zero", "0042", nil},
		{"none, as a loan has", "", nil},
		{"too few", "757", domain.ErrLast4Invalid},
		{"too many", "75770", domain.ErrLast4Invalid},
		{"not digits", "75x7", domain.ErrLast4Invalid},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := domain.ValidateCardLast4(tt.last4)

			if tt.wantErr == nil {
				assert.NoError(t, err)
				return
			}
			require.ErrorIs(t, err, tt.wantErr)
		})
	}
}

func TestValidateDueDay(t *testing.T) {
	assert.NoError(t, domain.ValidateDueDay(1))
	assert.NoError(t, domain.ValidateDueDay(31))
	assert.NoError(t, domain.ValidateDueDay(0), "no fixed day is allowed")
	require.ErrorIs(t, domain.ValidateDueDay(32), domain.ErrDueDayInvalid)
	require.ErrorIs(t, domain.ValidateDueDay(-1), domain.ErrDueDayInvalid)
}

func TestMonthDays(t *testing.T) {
	assert.Equal(t, 30, domain.Month{Year: 2026, Month: time.September}.Days())
	assert.Equal(t, 31, domain.Month{Year: 2026, Month: time.December}.Days())
	assert.Equal(t, 28, domain.Month{Year: 2026, Month: time.February}.Days())
	assert.Equal(t, 29, domain.Month{Year: 2028, Month: time.February}.Days())
}
