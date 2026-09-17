package domain

import (
	"errors"
	"fmt"
	"strings"
)

// Section is a free-form heading the Board groups Bills under, in a fixed display order.
type Section struct {
	ID           int64
	Name         string
	DisplayOrder int
}

// Bill is a standing list entry: a thing the household pays every month. It carries no
// amount, because an amount belongs to a Payable within one Cycle.
type Bill struct {
	ID           int64
	Name         string
	Aliases      []string
	SectionID    int64
	Channel      Channel
	CardName     string
	DisplayOrder int
	Archived     bool
}

// ChannelLabel writes how this Bill is paid, naming the card when it is charged to one.
func (b Bill) ChannelLabel() string {
	return ChannelLabel(b.Channel, b.CardName)
}

// Errors a person can provoke by typing something the rules refuse. They are worded to be
// shown straight back to whoever typed it.
var (
	ErrNameRequired     = errors.New("a name is required")
	ErrCardNameRequired = errors.New("a bill charged to a card needs the card's name")
	ErrCardNameUnwanted = errors.New("only a bill charged to a card carries a card name")
)

// ValidateBill checks a proposed Bill against the rules the schema also enforces, so that
// a person gets a sentence rather than a constraint error.
func ValidateBill(name string, channel Channel, cardName string) error {
	if strings.TrimSpace(name) == "" {
		return ErrNameRequired
	}
	if !channel.Valid() {
		return fmt.Errorf("%q is not a payment channel", channel)
	}
	hasCard := strings.TrimSpace(cardName) != ""
	if channel.RequiresCard() && !hasCard {
		return ErrCardNameRequired
	}
	if !channel.RequiresCard() && hasCard {
		return ErrCardNameUnwanted
	}
	return nil
}

// ValidateSection checks a proposed Section name.
func ValidateSection(name string) error {
	if strings.TrimSpace(name) == "" {
		return ErrNameRequired
	}
	return nil
}
