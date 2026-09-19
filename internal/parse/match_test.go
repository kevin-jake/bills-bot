package parse

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// seeded is the standing list as migration 00002 writes it.
var seeded = []Candidate{
	{ID: 1, Name: "Unionbank Mastercard CC"},
	{ID: 2, Name: "BDO JCB CC"},
	{ID: 3, Name: "BDO Unionpay CC"},
	{ID: 4, Name: "BDO Home Loan"},
	{ID: 5, Name: "RCBC JCB CC"},
	{ID: 6, Name: "RCBC Visa Airmiles"},
	{ID: 7, Name: "BPI Visa CC"},
	{ID: 8, Name: "HSBC Mastercard CC"},
	{ID: 9, Name: "PSBank Car Loan"},
	{ID: 10, Name: "Bahay"},
	{ID: 11, Name: "BPI Investment"},
	{ID: 12, Name: "BPI Wealth Builder"},
	{ID: 13, Name: "Internet PLDT"},
	{ID: 14, Name: "Batelec"},
	{ID: 15, Name: "Water"},
	{ID: 16, Name: "GCash funds"},
}

func ids(candidates []Candidate) []int64 {
	var found []int64
	for _, c := range candidates {
		found = append(found, c.ID)
	}
	return found
}

func TestMatchBill(t *testing.T) {
	tests := []struct {
		name string
		want []int64
	}{
		{"rcbc jcb", []int64{5}},
		{"bpi cc", []int64{7}},
		{"BPI credit card", []int64{7}},
		{"pldt", []int64{13}},
		{"batelec", []int64{14}},
		{"bdo hl", []int64{4}},
		{"bdo home loan", []int64{4}},
		{"psb", []int64{9}},
		{"psbank", []int64{9}},
		{"rcbc air", []int64{6}},
		{"wealth", []int64{12}},
		{"gcash", []int64{16}},
		{"union", []int64{1, 3}},
		{"bdo", []int64{2, 3, 4}},
		{"bpi", []int64{7, 11, 12}},
		{"rcbc", []int64{5, 6}},
		{"jcb", []int64{2, 5}},
		{"meralco", nil},
		{"b", nil},
		{"hello", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, ids(MatchBill(tt.name, seeded)))
		})
	}
}

func TestMatchBillPrefersTheBetterFit(t *testing.T) {
	candidates := []Candidate{{ID: 1, Name: "Water"}, {ID: 2, Name: "Water Heater Loan"}}

	assert.Equal(t, []int64{1}, ids(MatchBill("water", candidates)), "exact beats a shared word")
	assert.Equal(t, []int64{2}, ids(MatchBill("water heat", candidates)))
}

func TestMatchBillReadsAliases(t *testing.T) {
	candidates := append([]Candidate{}, seeded...)
	candidates[13].Aliases = []string{"meralco", "kuryente"}

	assert.Equal(t, []int64{14}, ids(MatchBill("meralco", candidates)))
	assert.Equal(t, []int64{14}, ids(MatchBill("kury", candidates)))
}

func TestMatchBillReadsACardsLastFourDigits(t *testing.T) {
	candidates := append([]Candidate{}, seeded...)
	candidates[6].Last4 = "7577" // BPI Visa CC
	candidates[7].Last4 = "9361" // HSBC Mastercard CC

	assert.Equal(t, []int64{7}, ids(MatchBill("7577", candidates)), "the digits off a statement")
	assert.Equal(t, []int64{8}, ids(MatchBill("9361", candidates)))
	assert.Nil(t, ids(MatchBill("1234", candidates)), "digits of no card here")
}

func TestMentions(t *testing.T) {
	assert.True(t, Mentions("bdo jbc", seeded), "a mistyped bill still names its bank")
	assert.True(t, Mentions("pdlt interne", seeded))
	assert.False(t, Mentions("see you at", seeded))
	assert.False(t, Mentions("call me in", seeded), "two letters are too few to count")
}
