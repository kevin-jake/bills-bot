package report_test

import (
	"strings"
	"testing"
	"time"

	"github.com/kevin-jake/bills-bot/internal/domain"
	"github.com/kevin-jake/bills-bot/internal/report"
	"github.com/stretchr/testify/assert"
)

// august is a second, shorter month, so that the exports are seen to run across Cycles in
// month order rather than over whichever one they were handed.
func august() domain.Snapshot {
	return domain.Snapshot{
		Cycle:    domain.Cycle{ID: 6, Month: month(2026, time.August)},
		Sections: []domain.Section{{ID: 3, Name: "Utilities", DisplayOrder: 3}},
		Payables: []domain.Payable{
			{ID: 5, SectionID: 3, BillName: "Batelec", Channel: domain.KevinDirect,
				AmountCents: cents(0), Status: domain.StatusPaid,
				PaidAt: at(time.August, 1, 8, 0), PaidBy: id(0)},
		},
	}
}

func TestPayablesCSV(t *testing.T) {
	want := strings.Join([]string{
		"month,section,bill,channel,card,amount,status,paid_at,paid_by",
		"2026-08,Utilities,Batelec,Kevin,,0.00,paid,2026-08-01 08:00,nobody",
		"2026-09,BDO,BDO JCB CC,Sheena BDO,,,due,,",
		"2026-09,BDO,BDO Home Loan,Sheena BDO,,16639.31,funded,,",
		"2026-09,Utilities,Internet PLDT,Charged to card,RCBC Visa Airmiles,5099.00,due,,",
		"2026-09,Utilities,Water & Sewer,Kevin,,840.50,paid,2026-09-15 09:03,Sheena",
		"",
	}, "\n")

	assert.Equal(t, want, string(report.PayablesCSV([]domain.Snapshot{august(), september()}, names)))
}

func TestTransfersCSVCoversTheAccountsInUseWhetherOrNotMoneyWasSent(t *testing.T) {
	september := september()
	september.Payables = append(september.Payables, domain.Payable{ID: 15, SectionID: 2,
		BillName: "HSBC Mastercard CC", Channel: domain.SheenaBPI,
		AmountCents: cents(936100), Status: domain.StatusDue})

	want := strings.Join([]string{
		"month,channel,required,sent,sent_at,sent_by",
		"2026-09,Sheena BDO,16639.31,17000.00,2026-09-14 20:15,Kevin",
		"2026-09,Sheena BPI,9361.00,,,",
		"",
	}, "\n")

	assert.Equal(t, want, string(report.TransfersCSV([]domain.Snapshot{august(), september}, names)))
}

func TestCSVOfNoMonthsIsItsHeaderAlone(t *testing.T) {
	assert.Equal(t, "month,section,bill,channel,card,amount,status,paid_at,paid_by\n",
		string(report.PayablesCSV(nil, nil)))
	assert.Equal(t, "month,channel,required,sent,sent_at,sent_by\n",
		string(report.TransfersCSV(nil, nil)))
}
