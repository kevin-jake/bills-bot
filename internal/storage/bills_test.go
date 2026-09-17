package storage_test

import (
	"testing"

	"github.com/kevin-jake/bills-bot/internal/storage"
	"github.com/kevin-jake/bills-bot/internal/storage/storagetest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The seed is the sticky note. These tests pin the parts of it the rules depend on, so
// that editing migration 00002 cannot quietly change what the household sees on day one.

func TestSeedListsTheStickyNote(t *testing.T) {
	db := storagetest.Open(t)

	sections, err := storage.ListSections(db)
	require.NoError(t, err)
	names := make([]string, len(sections))
	for i, s := range sections {
		names[i] = s.Name
	}
	assert.Equal(t, []string{
		"UnionBank", "BDO", "RCBC", "BPI", "HSBC", "PSBank", "Bahay", "Investment", "Utilities",
	}, names, "sections come back in display order")

	bills, err := storage.ListActiveBills(db)
	require.NoError(t, err)
	assert.Len(t, bills, 16)
}

func TestSeedChargesOnlyPLDTToACard(t *testing.T) {
	db := storagetest.Open(t)

	bills, err := storage.ListActiveBills(db)
	require.NoError(t, err)

	charged := map[string]string{}
	for _, b := range bills {
		if b.CardName != nil {
			charged[b.Name] = *b.CardName
		}
		if b.Channel == "charged_to_card" {
			require.NotNil(t, b.CardName, "%s is charged to a card and must name it", b.Name)
		}
	}
	assert.Equal(t, map[string]string{"Internet PLDT": "RCBC Visa Airmiles"}, charged)
}

func TestSeedPaysTheInvestmentsInCash(t *testing.T) {
	db := storagetest.Open(t)

	for _, name := range []string{"BPI Investment", "BPI Wealth Builder"} {
		bill, err := storage.FindActiveBillByName(db, name)
		require.NoError(t, err)
		require.NotNil(t, bill, "%s should be seeded", name)
		assert.Equal(t, "kevin_direct", bill.Channel, "%s is paid by Kevin in cash", name)
		assert.Nil(t, bill.CardName)
	}
}

func TestFindByNameIsCaseInsensitive(t *testing.T) {
	db := storagetest.Open(t)

	section, err := storage.FindSectionByName(db, "utilities")
	require.NoError(t, err)
	require.NotNil(t, section)
	assert.Equal(t, "Utilities", section.Name)

	bill, err := storage.FindActiveBillByName(db, "bdo home loan")
	require.NoError(t, err)
	require.NotNil(t, bill)
	assert.Equal(t, "BDO Home Loan", bill.Name)
}

func TestFindByNameReturnsNothingRatherThanAnError(t *testing.T) {
	db := storagetest.Open(t)

	section, err := storage.FindSectionByName(db, "Nowhere")
	require.NoError(t, err)
	assert.Nil(t, section)

	bill, err := storage.FindActiveBillByName(db, "Netflix")
	require.NoError(t, err)
	assert.Nil(t, bill)
}

func TestArchivedBillsAreLeftOffTheStandingList(t *testing.T) {
	db := storagetest.Open(t)
	require.NoError(t, db.Exec(
		`UPDATE bills SET archived_at = CURRENT_TIMESTAMP WHERE name = 'Water'`).Error)

	bills, err := storage.ListActiveBills(db)
	require.NoError(t, err)
	assert.Len(t, bills, 15)

	found, err := storage.FindActiveBillByName(db, "Water")
	require.NoError(t, err)
	assert.Nil(t, found, "an archived bill is off the list and its name is free again")
}

func TestNextOrderStartsAtOneAndThenFollowsTheLast(t *testing.T) {
	db := storagetest.OpenEmpty(t)

	order, err := storage.NextSectionOrder(db)
	require.NoError(t, err)
	assert.Equal(t, 1, order, "the first section of an empty list is first")

	section := storage.Section{Name: "Utilities", DisplayOrder: order}
	require.NoError(t, storage.CreateSection(db, &section))
	require.NotZero(t, section.ID, "create fills in the id")

	order, err = storage.NextSectionOrder(db)
	require.NoError(t, err)
	assert.Equal(t, 2, order)

	billOrder, err := storage.NextBillOrder(db, section.ID)
	require.NoError(t, err)
	assert.Equal(t, 1, billOrder, "a section's first bill is first")

	require.NoError(t, storage.CreateBill(db, &storage.Bill{
		Name: "Water", SectionID: section.ID, Channel: "kevin_direct", DisplayOrder: billOrder,
	}))

	billOrder, err = storage.NextBillOrder(db, section.ID)
	require.NoError(t, err)
	assert.Equal(t, 2, billOrder)
}

func TestNextBillOrderCountsArchivedBills(t *testing.T) {
	db := storagetest.Open(t)
	utilities, err := storage.FindSectionByName(db, "Utilities")
	require.NoError(t, err)
	require.NoError(t, db.Exec(
		`UPDATE bills SET archived_at = CURRENT_TIMESTAMP WHERE name = 'GCash funds'`).Error)

	order, err := storage.NextBillOrder(db, utilities.ID)

	require.NoError(t, err)
	assert.Equal(t, 5, order, "an archived bill still holds its place, so orders cannot collide")
}
