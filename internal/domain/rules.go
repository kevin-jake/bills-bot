package domain

import (
	"errors"
	"time"
)

// Errors the rules return. They are worded to be shown straight back to whoever tapped.
var (
	// ErrAmountNegative is returned for an amount below zero. ParseAmount never produces
	// one; this guards the rule against a caller that did not go through it.
	ErrAmountNegative = errors.New("an amount cannot be negative")
	// ErrAmountUnknown refuses to mark a Payable paid before anyone knows what it came to.
	ErrAmountUnknown = errors.New("set an amount first")
	// ErrAlreadyPaid refuses to mark a Payable paid twice.
	ErrAlreadyPaid = errors.New("that is already paid")
	// ErrTransferEmpty refuses a Transfer of nothing, which would fund bills with no money.
	ErrTransferEmpty = errors.New("a transfer has to be more than ₱0.00")
)

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
		p.PaidAt, p.PaidBy = nil, nil
		p.Status = UnpaidStatus(p.Channel, transferSent)
	}
	return p, nil
}

// MarkPaid records that a person settled p. An unknown amount is refused, because the
// history would then say something was paid without saying what; zero is a known amount.
func MarkPaid(p Payable, by int64, now time.Time) (Payable, error) {
	switch {
	case p.Paid():
		return Payable{}, ErrAlreadyPaid
	case !p.AmountKnown():
		return Payable{}, ErrAmountUnknown
	}
	p.Status, p.PaidAt, p.PaidBy = StatusPaid, &now, &by
	return p, nil
}

// UnpaidStatus is where a Payable that is not paid stands: funded when a Transfer has gone
// into its channel this Cycle, otherwise due. Only Sheena's channels are ever funded.
func UnpaidStatus(c Channel, transferSent bool) Status {
	if transferSent && c.IsSheena() {
		return StatusFunded
	}
	return StatusDue
}

// FitToTransfers settles the status of a Payable put back to an earlier state, since the
// Transfers may have changed since then: an unpaid Payable is funded exactly when its
// channel's Transfer has been sent. A paid one is left alone.
func FitToTransfers(p Payable, transferSent bool) Payable {
	if !p.Paid() {
		p.Status = UnpaidStatus(p.Channel, transferSent)
	}
	return p
}

// ValidateTransfer checks the amount of a Transfer about to be recorded.
func ValidateTransfer(cents int64) error {
	switch {
	case cents < 0:
		return ErrAmountNegative
	case cents == 0:
		return ErrTransferEmpty
	}
	return nil
}
