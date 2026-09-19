package domain_test

import (
	"testing"

	"github.com/kevin-jake/bills-bot/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func cents(v int64) *int64 { return &v }

// september is a small Cycle with one of everything the tallies must treat differently:
// an unknown amount, a paid bill, a charge to a card and a Transfer.
func september() domain.Snapshot {
	return domain.Snapshot{
		Sections: []domain.Section{
			{ID: 1, Name: "BDO", DisplayOrder: 1},
			{ID: 2, Name: "HSBC", DisplayOrder: 2},
			{ID: 3, Name: "Utilities", DisplayOrder: 3},
		},
		Payables: []domain.Payable{
			{ID: 1, SectionID: 1, BillName: "BDO JCB CC", Channel: domain.SheenaBDO, Status: domain.StatusDue},
			{ID: 2, SectionID: 1, BillName: "BDO Home Loan", Channel: domain.SheenaBDO,
				AmountCents: cents(1663931), Status: domain.StatusFunded},
			{ID: 3, SectionID: 3, BillName: "Internet PLDT", Channel: domain.ChargedToCard,
				CardName: "RCBC Visa Airmiles", AmountCents: cents(509900), Status: domain.StatusDue},
			{ID: 4, SectionID: 3, BillName: "Batelec", Channel: domain.KevinDirect,
				AmountCents: cents(300000), Status: domain.StatusPaid},
		},
		Transfers: []domain.Transfer{{ID: 9, Channel: domain.SheenaBDO, SentCents: 2000000}},
	}
}

func TestSectionGroupsLeaveOutEmptySections(t *testing.T) {
	groups := september().SectionGroups()

	require.Len(t, groups, 2)
	assert.Equal(t, "BDO", groups[0].Section.Name)
	assert.Len(t, groups[0].Payables, 2)
	assert.Equal(t, "Utilities", groups[1].Section.Name)
	assert.Equal(t, domain.Tally{Cents: 1663931, Unknown: 1, Count: 2}, groups[0].Subtotal())
}

func TestSnapshotCounts(t *testing.T) {
	snap := september()

	assert.Equal(t, 1, snap.PaidCount())
	assert.Equal(t, 1, snap.UnknownCount())
}

func TestToSettleLeavesOutChargesToCards(t *testing.T) {
	snap := september()

	assert.Equal(t, domain.Tally{Cents: 1963931, Unknown: 1, Count: 3}, snap.ToSettle())
	assert.Equal(t, domain.Tally{Cents: 509900, Count: 1}, snap.ChargedToCards())
}

func TestTransferLinesCoverOnlySheenaChannelsInUse(t *testing.T) {
	lines := september().TransferLines()

	require.Len(t, lines, 1, "only BDO has payables among sheena's channels")
	line := lines[0]
	assert.Equal(t, domain.SheenaBDO, line.Channel)
	assert.Equal(t, domain.Tally{Cents: 1663931, Unknown: 1, Count: 2}, line.Need)
	assert.True(t, line.Tentative())
	require.NotNil(t, line.Sent)
	assert.Equal(t, int64(2000000), line.Sent.SentCents)
	assert.False(t, line.AllPaid)
}
