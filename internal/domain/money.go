package domain

import (
	"errors"
	"regexp"
	"strconv"
	"strings"
)

// Amounts are whole centavos in an int64 everywhere below the Telegram layer. Floats would
// make ₱0.10 + ₱0.20 disagree with the bank's statement, and the sticky note is summed by
// the household by hand, so the bot's figures have to match to the centavo.

// ErrAmountInvalid is returned for anything that does not read as a peso amount.
var ErrAmountInvalid = errors.New("that is not an amount; write it like 2499, 2,499.50 or ₱2499")

// amountPattern is the grammar the free-text shortcuts will share: digits with optional
// thousands commas and at most two decimal places. A sign is never accepted.
var amountPattern = regexp.MustCompile(`^\d[\d,]*(\.\d{1,2})?$`)

// maxAmountDigits keeps the whole-peso part well inside int64 once it is multiplied into
// centavos. No household bill has fifteen digits.
const maxAmountDigits = 15

// ParseAmount reads an amount as a person types it, with or without ₱ or PHP in front, and
// returns it in centavos. Zero is a valid amount: it means nothing is due this month.
func ParseAmount(s string) (int64, error) {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.TrimPrefix(s, "₱")
	s = strings.TrimPrefix(s, "php")
	s = strings.TrimSpace(s)
	if !amountPattern.MatchString(s) {
		return 0, ErrAmountInvalid
	}

	s = strings.ReplaceAll(s, ",", "")
	whole, fraction, _ := strings.Cut(s, ".")
	if len(whole) > maxAmountDigits {
		return 0, ErrAmountInvalid
	}
	pesos, err := strconv.ParseInt(whole, 10, 64)
	if err != nil {
		return 0, ErrAmountInvalid
	}

	// "5" after the point is fifty centavos, not five.
	for len(fraction) < 2 {
		fraction += "0"
	}
	centavos, err := strconv.ParseInt(fraction, 10, 64)
	if err != nil {
		return 0, ErrAmountInvalid
	}
	return pesos*100 + centavos, nil
}

// FormatPesos writes centavos the way the sticky note does: ₱103,431.23.
func FormatPesos(cents int64) string {
	sign := ""
	if cents < 0 {
		sign = "−"
		cents = -cents
	}
	whole := strconv.FormatInt(cents/100, 10)

	var grouped strings.Builder
	for i, digit := range whole {
		if i > 0 && (len(whole)-i)%3 == 0 {
			grouped.WriteByte(',')
		}
		grouped.WriteRune(digit)
	}

	fraction := strconv.FormatInt(cents%100, 10)
	if len(fraction) == 1 {
		fraction = "0" + fraction
	}
	return sign + "₱" + grouped.String() + "." + fraction
}
