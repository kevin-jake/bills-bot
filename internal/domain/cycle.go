package domain

import "time"

// Status is where a Payable stands within its Cycle. A Payable on one of Sheena's channels
// goes due → funded → paid; every other goes due → paid.
type Status string

const (
	StatusDue    Status = "due"
	StatusFunded Status = "funded"
	StatusPaid   Status = "paid"
)

// Cycle is one calendar month's copy of the Bill list.
type Cycle struct {
	ID       int64
	Month    Month
	OpenedAt time.Time
	ClosedAt *time.Time
	// BoardChatID and BoardMessageID locate the pinned Board, once one has been posted.
	BoardChatID    int64
	BoardMessageID int
}

// Closed reports whether every Payable in the Cycle has been paid and the Cycle put away.
func (c Cycle) Closed() bool { return c.ClosedAt != nil }

// HasBoard reports whether a Board message has been posted for the Cycle.
func (c Cycle) HasBoard() bool { return c.BoardMessageID != 0 }

// Payable is one Bill's obligation within one Cycle. The channel and card are a snapshot
// taken when the Payable was created, so that a later change to the Bill does not rewrite
// how a past month was paid.
type Payable struct {
	ID        int64
	CycleID   int64
	BillID    int64
	BillName  string
	SectionID int64
	Channel   Channel
	CardName  string
	// AmountCents is nil while the amount is unknown. Zero is a real amount: nothing due.
	AmountCents *int64
	Status      Status
	PaidAt      *time.Time
	PaidBy      *int64
}

// AmountKnown reports whether an amount has been entered, zero included.
func (p Payable) AmountKnown() bool { return p.AmountCents != nil }

// Paid reports whether the Payable has been settled.
func (p Payable) Paid() bool { return p.Status == StatusPaid }

// ChannelLabel writes how this Payable is paid, naming the card when it is charged to one.
func (p Payable) ChannelLabel() string {
	return ChannelLabel(p.Channel, p.CardName)
}

// Transfer is the money Kevin sent into one of Sheena's accounts for a Cycle.
type Transfer struct {
	ID        int64
	CycleID   int64
	Channel   Channel
	SentCents int64
	SentAt    time.Time
	SentBy    int64
}
