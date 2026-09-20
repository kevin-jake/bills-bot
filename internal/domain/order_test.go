package domain_test

import (
	"testing"

	"github.com/kevin-jake/bills-bot/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParsePlacementReadsAMoveAsItIsSpoken(t *testing.T) {
	tests := []struct {
		typed string
		want  domain.Placement
	}{
		{"up", domain.Placement{Step: -1}},
		{"Down", domain.Placement{Step: 1}},
		{" top ", domain.Placement{Position: 1}},
		{"first", domain.Placement{Position: 1}},
		{"3", domain.Placement{Position: 3}},
	}
	for _, tt := range tests {
		t.Run(tt.typed, func(t *testing.T) {
			got, err := domain.ParsePlacement(tt.typed)

			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}

	bottom, err := domain.ParsePlacement("bottom")
	require.NoError(t, err)
	assert.Equal(t, 4, bottom.Index(0, 5), "bottom is the end of whatever list it lands in")
}

func TestParsePlacementRefusesWhatNobodyCouldActOn(t *testing.T) {
	for _, typed := range []string{"", "sideways", "0", "-2", "2.5"} {
		_, err := domain.ParsePlacement(typed)

		assert.ErrorIs(t, err, domain.ErrPlacementInvalid, "%q is not a placement", typed)
	}
}

func TestPlacementIndexStaysInsideTheList(t *testing.T) {
	up, down := domain.Placement{Step: -1}, domain.Placement{Step: 1}

	assert.Equal(t, 1, up.Index(2, 5))
	assert.Equal(t, 0, up.Index(0, 5), "a step up from the top stays at the top")
	assert.Equal(t, 4, down.Index(4, 5), "a step down from the bottom stays at the bottom")
	assert.Equal(t, 4, domain.Placement{Position: 99}.Index(0, 5), "a position past the end is the end")
	assert.Equal(t, 0, domain.Placement{Position: 1}.Index(3, 5))
	assert.Equal(t, 0, up.Index(0, 0), "an empty list has one place to be")
}

func TestReorderSlidesTheRestAlong(t *testing.T) {
	list := []string{"a", "b", "c", "d"}

	assert.Equal(t, []string{"b", "c", "a", "d"}, domain.Reorder(list, 0, 2))
	assert.Equal(t, []string{"d", "a", "b", "c"}, domain.Reorder(list, 3, 0))
	assert.Equal(t, []string{"a", "b", "c", "d"}, domain.Reorder(list, 1, 1), "a move to where it is changes nothing")
	assert.Equal(t, list, domain.Reorder(list, 9, 0), "an index off the list is left alone")
	assert.Equal(t, []string{"a", "b", "c", "d"}, list, "the list it was given is not shuffled")
}

func TestParseAliasesReadsTheOtherNamesABillGoesBy(t *testing.T) {
	assert.Equal(t, []string{"nflx", "the telly"}, domain.ParseAliases(" nflx , the  telly "))
	assert.Equal(t, []string{"nflx"}, domain.ParseAliases("nflx,,NFLX, nflx"), "repeats are dropped")
	assert.Nil(t, domain.ParseAliases("  "), "an empty list clears the aliases")
	assert.Equal(t, "nflx,telly", domain.JoinAliases([]string{"nflx", "telly"}))
	assert.Equal(t, "", domain.JoinAliases(nil))
}
