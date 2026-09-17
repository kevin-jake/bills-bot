package domain_test

import (
	"testing"

	"github.com/kevin-jake/bills-bot/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateBill(t *testing.T) {
	tests := []struct {
		name     string
		billName string
		channel  domain.Channel
		cardName string
		wantErr  error
	}{
		{"an ordinary bill", "Batelec", domain.KevinDirect, "", nil},
		{"one of sheena's", "BDO Home Loan", domain.SheenaBDO, "", nil},
		{"charged to a named card", "Internet PLDT", domain.ChargedToCard, "RCBC Visa Airmiles", nil},
		{"no name", "  ", domain.KevinDirect, "", domain.ErrNameRequired},
		{"card without a name", "Internet PLDT", domain.ChargedToCard, "", domain.ErrCardNameRequired},
		{"card name on another channel", "Water", domain.KevinDirect, "BPI CC", domain.ErrCardNameUnwanted},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := domain.ValidateBill(tt.billName, tt.channel, tt.cardName)

			if tt.wantErr == nil {
				assert.NoError(t, err)
				return
			}
			require.ErrorIs(t, err, tt.wantErr)
		})
	}
}

func TestValidateBillRefusesAnUnknownChannel(t *testing.T) {
	err := domain.ValidateBill("Netflix", domain.Channel("venmo"), "")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "venmo")
}

func TestValidateSectionNeedsAName(t *testing.T) {
	require.NoError(t, domain.ValidateSection("Utilities"))
	require.ErrorIs(t, domain.ValidateSection("   "), domain.ErrNameRequired)
}

func TestBillChannelLabelNamesTheCard(t *testing.T) {
	bill := domain.Bill{Name: "Internet PLDT", Channel: domain.ChargedToCard, CardName: "RCBC Visa Airmiles"}

	assert.Equal(t, "→ RCBC Visa Airmiles", bill.ChannelLabel())
}
