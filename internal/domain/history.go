package domain

// History is what one Bill has come to over the past months: the same Bill read across
// Cycles rather than one Cycle read across Bills, which is the other way the household
// looks at the sticky note.
type History struct {
	// Bill is the Bill's name as it reads now, digits and all, even for months paid before
	// it was renamed or its card reissued.
	Bill string
	// Entries are the months the Bill has been in, newest first.
	Entries []HistoryEntry
}

// HistoryEntry is one month of a Bill's History.
type HistoryEntry struct {
	Month   Month
	Payable Payable
	// PaidBy names whoever settled it, empty while it is unpaid and "nobody" for the months
	// it came to nothing and ticked itself off.
	PaidBy string
}

// Average is what the Bill has come to in the months it has an amount for, and how many
// months those are. A month whose amount was never entered is left out of both rather than
// counted as zero, which would drag the average down for no reason. A month entered as
// zero is real and counts.
func (h History) Average() (cents int64, months int) {
	var total int64
	for _, entry := range h.Entries {
		if entry.Payable.AmountKnown() {
			total += *entry.Payable.AmountCents
			months++
		}
	}
	if months == 0 {
		return 0, 0
	}
	return total / int64(months), months
}

// Names maps each Telegram id that has acted to the name it acts under. There is no users
// table — the allowlist is configuration and the audit trail carries the names — so a
// report that wants to say who paid something is given this to look them up in.
type Names map[int64]string

// Of names whoever an id belongs to. A Payable nobody has paid has no id at all; the id 0
// is the bot ticking off a month that came to nothing, which is nobody in particular.
func (n Names) Of(telegramID *int64) string {
	switch {
	case telegramID == nil:
		return ""
	case *telegramID == 0:
		return "nobody"
	case n[*telegramID] != "":
		return n[*telegramID]
	}
	return "someone"
}
