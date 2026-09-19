// Package parse reads the free-text shortcuts people type in the group instead of tapping,
// such as "pldt 2499" or "paid bpi cc", and matches the bill they name. It is pure: it
// knows nothing of Telegram or the database, so every case is a table test.
package parse

import (
	"strings"
	"time"
	"unicode"

	"github.com/kevin-jake/bills-bot/internal/domain"
)

// Verb is what a shortcut asks to do to the bill it names.
type Verb int

const (
	// VerbSetAmount enters an amount: "pldt 2499", "PLDT = 2,499".
	VerbSetAmount Verb = iota + 1
	// VerbPaid marks a bill paid, entering its amount first when one is given:
	// "paid bpi cc", "paid bpi cc 5000", "bpi cc paid".
	VerbPaid
	// VerbUndo takes back the last thing done to a bill: "undo pldt".
	VerbUndo
)

// Shortcut is one message read as an instruction.
type Shortcut struct {
	Verb Verb
	// Name is the bill as typed, lowercased, for MatchBill.
	Name string
	// Amount is set when the message carried one. VerbUndo never does.
	Amount *int64
	// Month is set when the message ended in a month, "2026-08" or "aug", which picks that
	// month's Cycle over the one the bot would otherwise choose.
	Month *domain.Month
	// Explicit reports that the message said what it wanted in so many words — a verb, or
	// "=" — rather than being a name followed by a number, which ordinary chat can also be.
	Explicit bool
}

// Parse reads text as a shortcut, and reports false for anything that is not one, which
// the bot then ignores as conversation. current is the month it is now, against which a
// bare month name such as "aug" is read: the latest such month no later than next month.
//
// The grammar, case-insensitive, each form optionally followed by a month:
//
//	paid <name> [<amount>]
//	<name> paid
//	undo <name>
//	<name> <amount>
//	<name> = <amount>
func Parse(text string, current domain.Month) (Shortcut, bool) {
	tokens := strings.Fields(strings.ToLower(strings.ReplaceAll(text, "=", " = ")))

	var s Shortcut
	if len(tokens) > 2 {
		if month, ok := parseMonth(tokens[len(tokens)-1], current); ok {
			s.Month = &month
			tokens = tokens[:len(tokens)-1]
		}
	}
	if len(tokens) < 2 {
		return Shortcut{}, false
	}

	first, last := tokens[0], tokens[len(tokens)-1]
	var name []string
	switch {
	case first == "paid":
		s.Verb, s.Explicit = VerbPaid, true
		name = tokens[1:]
		if cents, ok := amountOf(last); ok && len(name) > 1 {
			s.Amount = &cents
			name = name[:len(name)-1]
		}
	case first == "undo":
		s.Verb, s.Explicit = VerbUndo, true
		name = tokens[1:]
	case last == "paid":
		s.Verb, s.Explicit = VerbPaid, true
		name = tokens[:len(tokens)-1]
	default:
		cents, ok := amountOf(last)
		if !ok {
			return Shortcut{}, false
		}
		s.Verb, s.Amount = VerbSetAmount, &cents
		name = tokens[:len(tokens)-1]
		if len(name) > 0 && name[len(name)-1] == "=" {
			s.Explicit = true
			name = name[:len(name)-1]
		}
	}

	// A name has to have a letter in it: "2 500" is arithmetic, not a bill.
	s.Name = strings.Join(name, " ")
	if !strings.ContainsFunc(s.Name, unicode.IsLetter) || strings.Contains(s.Name, "=") {
		return Shortcut{}, false
	}
	// An amount left inside an undo's name means the message was something else.
	if s.Verb == VerbUndo {
		if _, ok := amountOf(name[len(name)-1]); ok {
			return Shortcut{}, false
		}
	}
	return s, true
}

// amountOf reads one token as an amount, with or without ₱ or PHP in front.
func amountOf(token string) (int64, bool) {
	cents, err := domain.ParseAmount(token)
	return cents, err == nil
}

// parseMonth reads "2026-08", or a month's name or three-letter abbreviation.
func parseMonth(token string, current domain.Month) (domain.Month, bool) {
	if month, err := domain.ParseMonth(token); err == nil {
		return month, true
	}
	for m := time.January; m <= time.December; m++ {
		full := strings.ToLower(m.String())
		if token != full && token != full[:3] {
			continue
		}
		// The latest such month that is not after next month, since that is as far ahead
		// as a month can be opened.
		month := domain.Month{Year: current.Year, Month: m}
		if month.After(current.Next()) {
			month.Year--
		}
		return month, true
	}
	return domain.Month{}, false
}
