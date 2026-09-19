package domain_test

import (
	"testing"
	"time"

	"github.com/kevin-jake/bills-bot/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSetAmount(t *testing.T) {
	now := time.Date(2026, 9, 15, 2, 0, 0, 0, time.UTC)
	earlier := now.Add(-time.Hour)
	kevin := int64(111)

	unknownDue := domain.Payable{Channel: domain.SheenaBPI, Status: domain.StatusDue}
	funded := domain.Payable{Channel: domain.SheenaBPI, Status: domain.StatusFunded, AmountCents: cents(500)}
	autoPaid := domain.Payable{
		Channel: domain.SheenaBPI, Status: domain.StatusPaid, AmountCents: cents(0),
		PaidAt: &earlier, PaidBy: cents(domain.AutoPaidBy),
	}
	personPaid := domain.Payable{
		Channel: domain.KevinDirect, Status: domain.StatusPaid, AmountCents: cents(500),
		PaidAt: &earlier, PaidBy: &kevin,
	}

	tests := []struct {
		name         string
		from         domain.Payable
		cents        int64
		transferSent bool
		wantStatus   domain.Status
		wantPaidBy   *int64
		wantPaidAt   *time.Time
	}{
		{"a positive amount leaves due alone", unknownDue, 249900, false, domain.StatusDue, nil, nil},
		{"a positive amount leaves funded alone", funded, 249900, false, domain.StatusFunded, nil, nil},
		{"zero pays at once, by nobody", unknownDue, 0, false, domain.StatusPaid, cents(domain.AutoPaidBy), &now},
		{"zero pays a funded payable too", funded, 0, true, domain.StatusPaid, cents(domain.AutoPaidBy), &now},
		{"a positive amount undoes an auto-pay", autoPaid, 249900, false, domain.StatusDue, nil, nil},
		{"…back to funded when a transfer covers it", autoPaid, 249900, true, domain.StatusFunded, nil, nil},
		{"a correction to a paid payable keeps it paid", personPaid, 249900, false, domain.StatusPaid, &kevin, &earlier},
		{"zero on a paid payable keeps who paid it", personPaid, 0, false, domain.StatusPaid, &kevin, &earlier},
		{"zero again on an auto-paid payable keeps its time", autoPaid, 0, false, domain.StatusPaid, cents(domain.AutoPaidBy), &earlier},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := domain.SetAmount(tt.from, tt.cents, now, tt.transferSent)

			require.NoError(t, err)
			require.NotNil(t, got.AmountCents)
			assert.Equal(t, tt.cents, *got.AmountCents)
			assert.Equal(t, tt.wantStatus, got.Status)
			assert.Equal(t, tt.wantPaidBy, got.PaidBy)
			assert.Equal(t, tt.wantPaidAt, got.PaidAt)
		})
	}
}

func TestSetAmountNeverFundsAChannelWithoutTransfers(t *testing.T) {
	autoPaid := domain.Payable{
		Channel: domain.KevinDirect, Status: domain.StatusPaid, AmountCents: cents(0),
		PaidBy: cents(domain.AutoPaidBy),
	}

	got, err := domain.SetAmount(autoPaid, 100, time.Now(), true)

	require.NoError(t, err)
	assert.Equal(t, domain.StatusDue, got.Status, "Kevin's own bills are never funded")
}

func TestSetAmountRefusesANegativeAmount(t *testing.T) {
	_, err := domain.SetAmount(domain.Payable{Status: domain.StatusDue}, -1, time.Now(), false)

	assert.ErrorIs(t, err, domain.ErrAmountNegative)
}

func TestSetAmountDoesNotAliasTheCallersAmount(t *testing.T) {
	from := domain.Payable{Status: domain.StatusDue, AmountCents: cents(100)}

	_, err := domain.SetAmount(from, 200, time.Now(), false)

	require.NoError(t, err)
	assert.Equal(t, int64(100), *from.AmountCents)
}
