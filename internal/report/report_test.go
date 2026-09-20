package report_test

import (
	"time"

	"github.com/kevin-jake/bills-bot/internal/domain"
)

func cents(v int64) *int64 { return &v }

func id(v int64) *int64 { return &v }

// at is a moment in Manila, the zone every date the household reads is written in.
func at(m time.Month, day, hour, minute int) *time.Time {
	t := time.Date(2026, m, day, hour, minute, 0, 0, domain.Manila)
	return &t
}

func month(y int, m time.Month) domain.Month { return domain.Month{Year: y, Month: m} }

// september is the smallest Cycle that has one of everything a report treats differently:
// an amount nobody has entered, a bill Kevin pays himself, one of Sheena's accounts with a
// Transfer into it, and a charge that rides on a card.
func september() domain.Snapshot {
	return domain.Snapshot{
		Cycle: domain.Cycle{ID: 7, Month: month(2026, time.September)},
		Sections: []domain.Section{
			{ID: 1, Name: "BDO", DisplayOrder: 1},
			{ID: 2, Name: "HSBC", DisplayOrder: 2},
			{ID: 3, Name: "Utilities", DisplayOrder: 3},
		},
		Payables: []domain.Payable{
			{ID: 11, SectionID: 1, BillName: "BDO JCB CC", CardLast4: "5994", DueDay: 5,
				Channel: domain.SheenaBDO, Status: domain.StatusDue},
			{ID: 12, SectionID: 1, BillName: "BDO Home Loan", DueDay: 25,
				Channel: domain.SheenaBDO, AmountCents: cents(1663931), Status: domain.StatusFunded},
			{ID: 13, SectionID: 3, BillName: "Internet PLDT", Channel: domain.ChargedToCard,
				CardName: "RCBC Visa Airmiles", AmountCents: cents(509900), Status: domain.StatusDue},
			{ID: 14, SectionID: 3, BillName: "Water & Sewer", Channel: domain.KevinDirect,
				AmountCents: cents(84050), Status: domain.StatusPaid,
				PaidAt: at(time.September, 15, 9, 3), PaidBy: id(222)},
		},
		Transfers: []domain.Transfer{{ID: 3, CycleID: 7, Channel: domain.SheenaBDO,
			SentCents: 1700000, SentAt: *at(time.September, 14, 20, 15), SentBy: 111}},
	}
}

// names are the two people in the group, as the audit trail knows them.
var names = domain.Names{111: "Kevin", 222: "Sheena"}
