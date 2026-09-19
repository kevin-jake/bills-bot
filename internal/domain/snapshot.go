package domain

// Snapshot is a consistent view of one Cycle: everything the Board, the reminder and the
// reports need, read in one transaction. The sums are derived here rather than stored, so
// that no figure the household sees can drift from the Payables it is made of.
type Snapshot struct {
	Cycle Cycle
	// Sections are every Section in display order, whether or not the Cycle uses them.
	Sections []Section
	// Payables come in Board order: by Section display order, then Bill display order.
	Payables  []Payable
	Transfers []Transfer
}

// Tally sums a set of Payables. Unknown amounts cannot be summed, so they are counted
// instead, which is what lets the Board say "₱19,570.14 (1 unknown)" rather than a
// figure that looks final but is not.
type Tally struct {
	Cents   int64
	Unknown int
	Count   int
}

// Add counts p into the tally.
func (t *Tally) Add(p Payable) {
	t.Count++
	if p.AmountCents == nil {
		t.Unknown++
		return
	}
	t.Cents += *p.AmountCents
}

// SectionPayables is one Section heading on the Board and the Payables under it.
type SectionPayables struct {
	Section  Section
	Payables []Payable
}

// Subtotal sums every Payable in the Section.
func (s SectionPayables) Subtotal() Tally {
	var tally Tally
	for _, p := range s.Payables {
		tally.Add(p)
	}
	return tally
}

// SectionGroups groups the Payables under their Sections in display order. A Section with no
// Payables in this Cycle is left out: an empty heading is noise.
func (s Snapshot) SectionGroups() []SectionPayables {
	bySection := make(map[int64][]Payable)
	for _, p := range s.Payables {
		bySection[p.SectionID] = append(bySection[p.SectionID], p)
	}

	groups := make([]SectionPayables, 0, len(s.Sections))
	for _, section := range s.Sections {
		if payables := bySection[section.ID]; len(payables) > 0 {
			groups = append(groups, SectionPayables{Section: section, Payables: payables})
		}
	}
	return groups
}

// PaidCount is how many Payables have been paid.
func (s Snapshot) PaidCount() int {
	paid := 0
	for _, p := range s.Payables {
		if p.Paid() {
			paid++
		}
	}
	return paid
}

// AllPaid reports whether nothing is left to pay, which is when a Cycle closes. A Cycle
// with no Payables at all has nothing left to pay either.
func (s Snapshot) AllPaid() bool {
	return s.PaidCount() == len(s.Payables)
}

// UnknownCount is how many Payables still have no amount.
func (s Snapshot) UnknownCount() int {
	unknown := 0
	for _, p := range s.Payables {
		if !p.AmountKnown() {
			unknown++
		}
	}
	return unknown
}

// ToSettle sums every Payable except those charged to a card. A charge already sits inside
// the balance of the card it lands on, and that card is itself a Bill here, so counting
// both would count the same money twice.
func (s Snapshot) ToSettle() Tally {
	var tally Tally
	for _, p := range s.Payables {
		if p.Channel != ChargedToCard {
			tally.Add(p)
		}
	}
	return tally
}

// ChargedToCards sums the Payables ToSettle leaves out, so the Board can show them apart.
func (s Snapshot) ChargedToCards() Tally {
	var tally Tally
	for _, p := range s.Payables {
		if p.Channel == ChargedToCard {
			tally.Add(p)
		}
	}
	return tally
}

// TransferLine compares what one of Sheena's accounts needs for the Cycle with what was
// sent into it.
type TransferLine struct {
	Channel Channel
	// Need sums every Payable on the channel, paid or not: it is what the account needed
	// for the month as a whole.
	Need Tally
	// Sent is nil until Kevin records a Transfer.
	Sent *Transfer
	// AllPaid reports whether Sheena has paid everything on the channel.
	AllPaid bool
}

// Tentative reports whether Need is not final yet, because an amount is still unknown.
func (l TransferLine) Tentative() bool { return l.Need.Unknown > 0 }

// TransferLines returns one line per Sheena channel that has a Payable in the Cycle, in
// channel order.
func (s Snapshot) TransferLines() []TransferLine {
	var lines []TransferLine
	for _, channel := range Channels() {
		if !channel.IsSheena() {
			continue
		}

		line := TransferLine{Channel: channel, AllPaid: true}
		for _, p := range s.Payables {
			if p.Channel != channel {
				continue
			}
			line.Need.Add(p)
			if !p.Paid() {
				line.AllPaid = false
			}
		}
		if line.Need.Count == 0 {
			continue
		}

		for i := range s.Transfers {
			if s.Transfers[i].Channel == channel {
				line.Sent = &s.Transfers[i]
			}
		}
		lines = append(lines, line)
	}
	return lines
}
