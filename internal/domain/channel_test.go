package domain_test

import (
	"testing"

	"github.com/kevin-jake/bills-bot/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChannelsCoverTheEnum(t *testing.T) {
	channels := domain.Channels()

	require.Len(t, channels, 5)
	for _, c := range channels {
		assert.True(t, c.Valid(), "%s should be a valid channel", c)
	}
	assert.False(t, domain.Channel("venmo").Valid())
	assert.False(t, domain.Channel("").Valid())
}

func TestOnlySheenaChannelsCanBeFunded(t *testing.T) {
	funded := map[domain.Channel]bool{
		domain.KevinDirect:   false,
		domain.SheenaBDO:     true,
		domain.SheenaBPI:     true,
		domain.SheenaPSBank:  true,
		domain.ChargedToCard: false,
	}
	for channel, want := range funded {
		assert.Equal(t, want, channel.IsSheena(), "channel %s", channel)
	}
}

func TestOnlyChargedToCardRequiresACard(t *testing.T) {
	for _, c := range domain.Channels() {
		assert.Equal(t, c == domain.ChargedToCard, c.RequiresCard(), "channel %s", c)
	}
}

func TestChannelLabel(t *testing.T) {
	tests := []struct {
		name     string
		channel  domain.Channel
		cardName string
		want     string
	}{
		{"kevin pays directly", domain.KevinDirect, "", "Kevin"},
		{"one of sheena's accounts", domain.SheenaBDO, "", "Sheena BDO"},
		{"psbank keeps its capitals", domain.SheenaPSBank, "", "Sheena PSBank"},
		{"a card names the statement", domain.ChargedToCard, "RCBC Visa Airmiles", "→ RCBC Visa Airmiles"},
		{"a card with no name still reads", domain.ChargedToCard, "", "Charged to card"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, domain.ChannelLabel(tt.channel, tt.cardName))
		})
	}
}

func TestParseChannel(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  domain.Channel
	}{
		{"stored value", "kevin_direct", domain.KevinDirect},
		{"spoken value", "Kevin", domain.KevinDirect},
		{"stored sheena value", "sheena_bdo", domain.SheenaBDO},
		{"spoken sheena value", "Sheena BDO", domain.SheenaBDO},
		{"bank alone", "bpi", domain.SheenaBPI},
		{"psbank abbreviated", "psb", domain.SheenaPSBank},
		{"hyphenated", "Sheena-PSBank", domain.SheenaPSBank},
		{"card", "card", domain.ChargedToCard},
		{"card spelled out", "charged to card", domain.ChargedToCard},
		{"surrounding space", "  sheena bpi  ", domain.SheenaBPI},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := domain.ParseChannel(tt.input)

			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestParseChannelRefusesNonsenseAndSaysWhatIsAllowed(t *testing.T) {
	for _, input := range []string{"", "venmo", "sheena", "gcash"} {
		_, err := domain.ParseChannel(input)

		require.Error(t, err, "input %q", input)
		assert.Contains(t, err.Error(), "sheena psbank", "the error should list the options")
	}
}
