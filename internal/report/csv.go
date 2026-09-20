package report

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"strconv"
	"time"

	"github.com/kevin-jake/bills-bot/internal/domain"
)

// The exports are the way out of the database that ADR 0001 promises in return for the
// bot being the system of record. They are written for a spreadsheet rather than for
// reading in the chat: plain numbers with no ₱ or thousands commas, dates in Manila, one
// row per Payable or per account, every month the bot has ever opened.

// PayablesFile and TransfersFile are the names the documents arrive under.
const (
	PayablesFile  = "bills_export.csv"
	TransfersFile = "transfers_export.csv"
)

// PayablesCSV writes every Payable of every Cycle, oldest month first and in Board order
// within a month.
func PayablesCSV(snaps []domain.Snapshot, names domain.Names) []byte {
	rows := [][]string{{"month", "section", "bill", "channel", "card", "amount", "status",
		"paid_at", "paid_by"}}
	for _, snap := range snaps {
		for _, p := range snap.Payables {
			rows = append(rows, []string{
				snap.Cycle.Month.String(),
				snap.SectionName(p.SectionID),
				p.BillName,
				p.Channel.Label(),
				p.CardName,
				csvAmount(p.AmountCents),
				string(p.Status),
				csvTime(p.PaidAt),
				names.Of(p.PaidBy),
			})
		}
	}
	return write(rows)
}

// TransfersCSV writes one row per account per month that had anything on it, whether or
// not the money was sent. Required is what the account's Payables came to; a month with an
// amount still missing is counted as far as it is known, which the summary marks tentative
// and a spreadsheet cannot.
func TransfersCSV(snaps []domain.Snapshot, names domain.Names) []byte {
	rows := [][]string{{"month", "channel", "required", "sent", "sent_at", "sent_by"}}
	for _, snap := range snaps {
		for _, line := range snap.TransferLines() {
			row := []string{snap.Cycle.Month.String(), line.Channel.Label(),
				csvCents(line.Need.Cents), "", "", ""}
			if line.Sent != nil {
				sentBy := line.Sent.SentBy
				row[3] = csvCents(line.Sent.SentCents)
				row[4] = csvTime(&line.Sent.SentAt)
				row[5] = names.Of(&sentBy)
			}
			rows = append(rows, row)
		}
	}
	return write(rows)
}

func write(rows [][]string) []byte {
	var out bytes.Buffer
	writer := csv.NewWriter(&out)
	// Every row is built above rather than read from anywhere, and writing to a
	// bytes.Buffer cannot fail, so there is no error here to do anything about.
	_ = writer.WriteAll(rows)
	return out.Bytes()
}

// csvAmount writes an amount as a spreadsheet reads one, and leaves the cell empty when
// the amount was never entered. Empty is not zero: a zero row means nothing was due.
func csvAmount(cents *int64) string {
	if cents == nil {
		return ""
	}
	return csvCents(*cents)
}

func csvCents(cents int64) string {
	sign := ""
	if cents < 0 {
		sign, cents = "-", -cents
	}
	return sign + strconv.FormatInt(cents/100, 10) + "." + fmt.Sprintf("%02d", cents%100)
}

// csvTime writes a moment in Manila, since every other date the household sees is in
// Manila and a spreadsheet has nowhere to put a zone.
func csvTime(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.In(domain.Manila).Format("2006-01-02 15:04")
}
