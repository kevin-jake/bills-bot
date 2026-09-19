package domain

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
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
	ID        int64
	Name      string
	Aliases   []string
	SectionID int64
	Channel   Channel
	CardName  string
	// CardLast4 is the four digits printed on the card, empty for a Bill that is not one.
	// It is what tells two cards from the same bank apart when one of them is queried at
	// the bank, and it is kept out of the name so that a reissued card needs no rename.
	CardLast4 string
	// DueDay is the day of the month the Bill falls due, or 0 for a Bill with no fixed day.
	// A day past the end of a short month means that month's last day; see DueDate.
	DueDay       int
	DisplayOrder int
	Archived     bool
}

// ChannelLabel writes how this Bill is paid, naming the card when it is charged to one.
func (b Bill) ChannelLabel() string {
	return ChannelLabel(b.Channel, b.CardName)
}

// DisplayName writes the Bill as the household reads it: the name, then a card's last four
// digits. Only what is shown carries the digits; matching a typed name never sees them.
func (b Bill) DisplayName() string { return DisplayName(b.Name, b.CardLast4) }

// DueLabel writes when the Bill falls due, or empty when it has no fixed day.
func (b Bill) DueLabel() string { return DueLabel(b.DueDay) }

// DisplayName joins a Bill's name to a card's last four digits.
func DisplayName(name, last4 string) string {
	if last4 == "" {
		return name
	}
	return name + " ••" + last4
}

// DueLabel writes a day of the month as the Board says it, "due 28". Day 0 has no label,
// because a Bill without a fixed day is not late on any particular morning.
func DueLabel(day int) string {
	if day == 0 {
		return ""
	}
	return "due " + strconv.Itoa(day)
}

// DueDate is the moment a Bill falls due within a month, in Manila: midnight at the start
// of its due day. A day the month is too short for lands on the month's last day, so a card
// due on the 31st is due on 28 February rather than slipping into March.
func DueDate(month Month, day int) time.Time {
	if day < 1 {
		return time.Time{}
	}
	if last := month.Days(); day > last {
		day = last
	}
	return time.Date(month.Year, month.Month, day, 0, 0, 0, 0, Manila)
}

// Errors a person can provoke by typing something the rules refuse. They are worded to be
// shown straight back to whoever typed it.
var (
	ErrNameRequired     = errors.New("a name is required")
	ErrCardNameRequired = errors.New("a bill charged to a card needs the card's name")
	ErrCardNameUnwanted = errors.New("only a bill charged to a card carries a card name")
	ErrLast4Invalid     = errors.New("a card's last four digits are four numbers, like 7577")
	ErrDueDayInvalid    = errors.New("a due day is a day of the month, 1 to 31")
)

// ValidateCardLast4 checks a proposed set of digits. Empty is allowed: a loan has none.
func ValidateCardLast4(last4 string) error {
	last4 = strings.TrimSpace(last4)
	if last4 == "" {
		return nil
	}
	if len(last4) != 4 || strings.IndexFunc(last4, func(r rune) bool { return r < '0' || r > '9' }) >= 0 {
		return ErrLast4Invalid
	}
	return nil
}

// ValidateDueDay checks a proposed due day. Zero is allowed: it means no fixed day.
func ValidateDueDay(day int) error {
	if day < 0 || day > 31 {
		return ErrDueDayInvalid
	}
	return nil
}

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
