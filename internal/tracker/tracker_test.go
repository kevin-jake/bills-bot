package tracker_test

import (
	"testing"

	"github.com/kevin-jake/bills-bot/internal/domain"
	"github.com/kevin-jake/bills-bot/internal/storage/storagetest"
	"github.com/kevin-jake/bills-bot/internal/tracker"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

var kevin = tracker.Actor{TelegramID: 111, Name: "Kevin"}

func newTracker(t *testing.T) (*tracker.Tracker, *gorm.DB) {
	t.Helper()
	db := storagetest.Open(t)
	return tracker.New(db), db
}

func TestBillListGroupsTheStickyNoteBySection(t *testing.T) {
	tr, _ := newTracker(t)

	list, err := tr.BillList()

	require.NoError(t, err)
	require.Len(t, list, 9, "every seeded section has at least one bill")
	assert.Equal(t, "UnionBank", list[0].Section.Name, "sections come in display order")
	assert.Equal(t, "Utilities", list[8].Section.Name)

	bdo := list[1]
	assert.Equal(t, "BDO", bdo.Section.Name)
	names := make([]string, len(bdo.Bills))
	for i, b := range bdo.Bills {
		names[i] = b.Name
	}
	assert.Equal(t, []string{"BDO JCB CC", "BDO Unionpay CC", "BDO Home Loan"}, names,
		"bills keep the order they are listed in within a section")
}

func TestBillListCarriesTheCardDigitsAndDueDaysFromTheStickyNote(t *testing.T) {
	tr, _ := newTracker(t)

	list, err := tr.BillList()
	require.NoError(t, err)

	bills := map[string]domain.Bill{}
	for _, section := range list {
		for _, bill := range section.Bills {
			bills[bill.Name] = bill
		}
	}

	assert.Equal(t, "7577", bills["BPI Visa CC"].CardLast4)
	assert.Equal(t, 28, bills["BPI Visa CC"].DueDay)
	assert.Equal(t, "9361", bills["HSBC Mastercard CC"].CardLast4)
	assert.Equal(t, 24, bills["HSBC Mastercard CC"].DueDay)
	assert.Equal(t, "2943", bills["Unionbank Mastercard CC"].CardLast4)
	assert.Equal(t, 5, bills["BDO JCB CC"].DueDay)

	loan := bills["PSBank Car Loan"]
	assert.Empty(t, loan.CardLast4, "a loan is not a card")
	assert.Equal(t, 19, loan.DueDay)
	assert.Equal(t, 21, bills["Bahay"].DueDay)
	assert.Equal(t, 25, bills["BDO Home Loan"].DueDay)

	water := bills["Water"]
	assert.Empty(t, water.CardLast4)
	assert.Zero(t, water.DueDay, "a utility has no fixed day")
}

func TestBillListLabelsHowEachBillIsPaid(t *testing.T) {
	tr, _ := newTracker(t)

	list, err := tr.BillList()
	require.NoError(t, err)

	labels := map[string]string{}
	for _, section := range list {
		for _, bill := range section.Bills {
			labels[bill.Name] = bill.ChannelLabel()
		}
	}
	assert.Equal(t, "Kevin", labels["Unionbank Mastercard CC"])
	assert.Equal(t, "Sheena BDO", labels["BDO Home Loan"])
	assert.Equal(t, "Sheena BPI", labels["RCBC JCB CC"])
	assert.Equal(t, "Sheena PSBank", labels["PSBank Car Loan"])
	assert.Equal(t, "→ RCBC Visa Airmiles", labels["Internet PLDT"])
	assert.Equal(t, "Kevin", labels["BPI Investment"], "the investments are paid in cash")
}

func TestBillListLeavesOutEmptySections(t *testing.T) {
	tr, db := newTracker(t)
	require.NoError(t, db.Exec(
		`UPDATE bills SET archived_at = CURRENT_TIMESTAMP WHERE section_id =
		 (SELECT id FROM sections WHERE name = 'HSBC')`).Error)

	list, err := tr.BillList()

	require.NoError(t, err)
	for _, section := range list {
		assert.NotEqual(t, "HSBC", section.Section.Name,
			"a section with nothing active on it is not a heading worth printing")
	}
	assert.Len(t, list, 8)
}

func TestAddSectionAppendsToTheEnd(t *testing.T) {
	tr, _ := newTracker(t)

	added, err := tr.AddSection(kevin, "Subscriptions")

	require.NoError(t, err)
	assert.Equal(t, "Subscriptions", added.Name)
	assert.Equal(t, 10, added.DisplayOrder, "a new section goes after the nine seeded ones")
	assert.NotZero(t, added.ID)
}

func TestAddSectionRefusesADuplicateNameWhateverTheCase(t *testing.T) {
	tr, _ := newTracker(t)

	_, err := tr.AddSection(kevin, "utilities")

	require.ErrorIs(t, err, tracker.ErrSectionExists)
	assert.Contains(t, err.Error(), "Utilities", "the refusal names the section already there")
}

func TestAddSectionRefusesAnEmptyName(t *testing.T) {
	tr, _ := newTracker(t)

	_, err := tr.AddSection(kevin, "   ")

	require.ErrorIs(t, err, domain.ErrNameRequired)
}

func TestAddBillPutsItAtTheEndOfItsSection(t *testing.T) {
	tr, _ := newTracker(t)

	added, err := tr.AddBill(kevin, tracker.BillSpec{
		Name: "Netflix", Section: "Utilities", Channel: domain.KevinDirect,
	})

	require.NoError(t, err)
	assert.Equal(t, "Netflix", added.Name)
	assert.Equal(t, 5, added.DisplayOrder, "Utilities already has four bills")
	assert.Equal(t, domain.KevinDirect, added.Channel)

	list, err := tr.BillList()
	require.NoError(t, err)
	utilities := list[len(list)-1]
	require.Equal(t, "Utilities", utilities.Section.Name)
	assert.Equal(t, "Netflix", utilities.Bills[len(utilities.Bills)-1].Name)
}

func TestAddBillFindsItsSectionCaseInsensitively(t *testing.T) {
	tr, _ := newTracker(t)

	added, err := tr.AddBill(kevin, tracker.BillSpec{
		Name: "Netflix", Section: "utilities", Channel: domain.KevinDirect,
	})

	require.NoError(t, err)
	assert.NotZero(t, added.SectionID)
}

func TestAddBillChargedToACardKeepsTheCardName(t *testing.T) {
	tr, _ := newTracker(t)

	added, err := tr.AddBill(kevin, tracker.BillSpec{
		Name: "Spotify", Section: "Utilities", Channel: domain.ChargedToCard, CardName: "BPI Visa CC",
	})

	require.NoError(t, err)
	assert.Equal(t, "BPI Visa CC", added.CardName)
	assert.Equal(t, "→ BPI Visa CC", added.ChannelLabel())
}

func TestAddBillRefusals(t *testing.T) {
	tests := []struct {
		name    string
		spec    tracker.BillSpec
		wantErr error
	}{
		{
			"a name already on the list",
			tracker.BillSpec{Name: "water", Section: "Utilities", Channel: domain.KevinDirect},
			tracker.ErrBillExists,
		},
		{
			"a section that does not exist",
			tracker.BillSpec{Name: "Netflix", Section: "Streaming", Channel: domain.KevinDirect},
			tracker.ErrSectionUnknown,
		},
		{
			"no name",
			tracker.BillSpec{Name: " ", Section: "Utilities", Channel: domain.KevinDirect},
			domain.ErrNameRequired,
		},
		{
			"charged to a card with no card named",
			tracker.BillSpec{Name: "Netflix", Section: "Utilities", Channel: domain.ChargedToCard},
			domain.ErrCardNameRequired,
		},
		{
			"a card name on a channel that cannot have one",
			tracker.BillSpec{
				Name: "Netflix", Section: "Utilities",
				Channel: domain.KevinDirect, CardName: "BPI Visa CC",
			},
			domain.ErrCardNameUnwanted,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tr, db := newTracker(t)

			_, err := tr.AddBill(kevin, tt.spec)

			require.ErrorIs(t, err, tt.wantErr)

			var count int64
			require.NoError(t, db.Raw("SELECT COUNT(*) FROM bills").Scan(&count).Error)
			assert.Equal(t, int64(16), count, "a refused bill leaves the list untouched")
		})
	}
}

func TestMutationsAppendAnAuditEvent(t *testing.T) {
	tr, db := newTracker(t)

	section, err := tr.AddSection(kevin, "Subscriptions")
	require.NoError(t, err)
	bill, err := tr.AddBill(kevin, tracker.BillSpec{
		Name: "Netflix", Section: "Subscriptions", Channel: domain.KevinDirect,
	})
	require.NoError(t, err)

	type row struct {
		Action          string
		ActorTelegramID int64
		ActorName       string
		BillID          *int64
		AfterJSON       string
	}
	var events []row
	require.NoError(t, db.Raw(
		`SELECT action, actor_telegram_id, actor_name, bill_id, after_json
		 FROM events ORDER BY id`).Scan(&events).Error)

	require.Len(t, events, 2)
	assert.Equal(t, "section.add", events[0].Action)
	assert.Equal(t, kevin.TelegramID, events[0].ActorTelegramID)
	assert.Equal(t, "Kevin", events[0].ActorName)
	assert.Contains(t, events[0].AfterJSON, section.Name)

	assert.Equal(t, "bill.add", events[1].Action)
	require.NotNil(t, events[1].BillID, "a bill event points at its bill")
	assert.Equal(t, bill.ID, *events[1].BillID)
	assert.Contains(t, events[1].AfterJSON, "Netflix")
}

func TestARefusedMutationLeavesNoEventBehind(t *testing.T) {
	tr, db := newTracker(t)

	_, err := tr.AddBill(kevin, tracker.BillSpec{
		Name: "Netflix", Section: "Streaming", Channel: domain.KevinDirect,
	})
	require.Error(t, err)

	var count int64
	require.NoError(t, db.Raw("SELECT COUNT(*) FROM events").Scan(&count).Error)
	assert.Zero(t, count, "the transaction rolls the event back with the row")
}
