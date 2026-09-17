// Package domain holds the household's vocabulary and rules, and knows nothing about
// Telegram, SQLite or any other machinery. Everything here is pure, which is what makes
// the rules cheap to test and safe to reason about.
package domain

import (
	"fmt"
	"strings"
	"unicode"
)

// Channel is who pays a Bill and out of which account. It decides whether a Bill can be
// Funded, which Transfer it counts toward, and whether its amount is already inside a
// card's balance.
type Channel string

const (
	KevinDirect   Channel = "kevin_direct"
	SheenaBDO     Channel = "sheena_bdo"
	SheenaBPI     Channel = "sheena_bpi"
	SheenaPSBank  Channel = "sheena_psbank"
	ChargedToCard Channel = "charged_to_card"
)

// Channels lists every Payment Channel, in the order the bot offers them.
func Channels() []Channel {
	return []Channel{KevinDirect, SheenaBDO, SheenaBPI, SheenaPSBank, ChargedToCard}
}

// Valid reports whether c is one of the five Payment Channels. The database enforces the
// same set, so this exists to refuse bad input before it reaches a constraint error.
func (c Channel) Valid() bool {
	switch c {
	case KevinDirect, SheenaBDO, SheenaBPI, SheenaPSBank, ChargedToCard:
		return true
	}
	return false
}

// IsSheena reports whether the Channel is one of Sheena's accounts. Those are the only
// Channels a Transfer funds, and so the only ones whose Payables can be Funded.
func (c Channel) IsSheena() bool {
	return c == SheenaBDO || c == SheenaBPI || c == SheenaPSBank
}

// RequiresCard reports whether the Channel needs the name of the card the Bill lands on.
// The schema states the same rule as a CHECK, in both directions.
func (c Channel) RequiresCard() bool {
	return c == ChargedToCard
}

// Label is how a Channel is written on the Board and in the bill list. A Bill charged to
// a card is better labelled with ChannelLabel, which can name the card.
func (c Channel) Label() string {
	switch c {
	case KevinDirect:
		return "Kevin"
	case SheenaBDO:
		return "Sheena BDO"
	case SheenaBPI:
		return "Sheena BPI"
	case SheenaPSBank:
		return "Sheena PSBank"
	case ChargedToCard:
		return "Charged to card"
	}
	return string(c)
}

// ChannelLabel writes a Channel together with the card it rides on, so that a Bill whose
// charge lands on a statement says which statement.
func ChannelLabel(c Channel, cardName string) string {
	if c == ChargedToCard && cardName != "" {
		return "→ " + cardName
	}
	return c.Label()
}

// ParseChannel reads a Channel the way a person would type it, so that both the stored
// value ("sheena_bdo") and the spoken one ("Sheena BDO", "BDO") are accepted.
func ParseChannel(s string) (Channel, error) {
	switch normaliseChannel(s) {
	case "kevin", "kevin direct":
		return KevinDirect, nil
	case "bdo", "sheena bdo":
		return SheenaBDO, nil
	case "bpi", "sheena bpi":
		return SheenaBPI, nil
	case "psbank", "psb", "sheena psbank", "sheena psb":
		return SheenaPSBank, nil
	case "card", "charged to card":
		return ChargedToCard, nil
	}
	return "", fmt.Errorf(
		"%q is not a payment channel; use kevin, sheena bdo, sheena bpi, sheena psbank or card", s)
}

// normaliseChannel lowercases and turns every run of punctuation or space into a single
// space, which is what lets "sheena_bdo", "Sheena-BDO" and "sheena bdo" all agree.
func normaliseChannel(s string) string {
	fields := strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	return strings.Join(fields, " ")
}
