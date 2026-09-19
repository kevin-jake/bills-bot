package domain

import (
	"errors"
	"time"
)

// ErrAmountNegative is returned for an amount below zero. ParseAmount never produces one;
// this guards the rule against a caller that did not go through it.
var ErrAmountNegative = errors.New("an amount cannot be negative")

// AutoPaidBy is the PaidBy recorded when a Payable is paid because its amount was set to
// zero rather than because someone marked it paid.
const AutoPaidBy int64 = 0

// AutoPaid reports whether the Payable is paid only because nothing was due.
func (p Payable) AutoPaid() bool {
	return p.Paid() && p.PaidBy != nil && *p.PaidBy == AutoPaidBy
}

// SetAmount applies a newly entered amount to p and returns the result. transferSent says
// whether a Transfer has already gone into p's channel this Cycle, which decides where a
// Payable lands when it stops being auto-paid.
//
//   - Zero means nothing is due, so the Payable is paid at once, by nobody in particular.
//   - A positive amount on an auto-paid Payable puts it back to owing: funded if a Transfer
//     already covers its channel, otherwise due.
//   - A positive amount on a Payable a person marked paid corrects the figure and leaves it
//     paid; one on a due or funded Payable leaves the status alone.
func SetAmount(p Payable, cents int64, now time.Time, transferSent bool) (Payable, error) {
	if cents < 0 {
		return Payable{}, ErrAmountNegative
	}
	wasAutoPaid := p.AutoPaid()
	p.AmountCents = &cents

	switch {
	case cents == 0 && !p.Paid():
		paidBy := AutoPaidBy
		p.Status, p.PaidAt, p.PaidBy = StatusPaid, &now, &paidBy
	case cents > 0 && wasAutoPaid:
		p.Status, p.PaidAt, p.PaidBy = StatusDue, nil, nil
		if transferSent && p.Channel.IsSheena() {
			p.Status = StatusFunded
		}
	}
	return p, nil
}
