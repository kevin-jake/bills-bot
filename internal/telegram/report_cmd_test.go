package telegram

import (
	"strings"
	"testing"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// aSeptemberWorkedThrough opens September, enters two amounts, pays one of them and sends
// a transfer, which is enough for every report to have something to say.
func aSeptemberWorkedThrough(t *testing.T, bot *Bot, sender *fakeSender) {
	t.Helper()
	command(t, bot, sender, "/newmonth")
	bot.handleMessage(message(groupChatID, kevinID, "supergroup", "rcbc jcb 103431.23"))
	bot.handleMessage(message(groupChatID, sheenaID, "supergroup", "paid batelec 3000"))
	command(t, bot, sender, "/transfer sheena bpi 205601.58")
}

func TestHistoryShowsWhatOneBillHasComeTo(t *testing.T) {
	bot, sender := newTestBot(t)
	aSeptemberWorkedThrough(t, bot, sender)

	text := command(t, bot, sender, "/history rcbc jcb")

	assert.Contains(t, text, "📈 <b>RCBC JCB CC ••1006</b>")
	assert.Contains(t, text, "<i>1 month · average ₱103,431.23</i>")
	assert.Contains(t, text, "Sep 2026 · ₱103,431.23 · ⏳ funded", "the transfer funded it")
	assert.Equal(t, tgbotapi.ModeHTML, sender.messages[len(sender.messages)-1].ParseMode)
}

func TestHistoryNamesWhoeverPaid(t *testing.T) {
	bot, sender := newTestBot(t)
	aSeptemberWorkedThrough(t, bot, sender)

	// The day is whatever day the test runs: the tracker stamps paid_at with the real
	// clock, deliberately, rather than with the bot's injectable one.
	assert.Regexp(t, `Sep 2026 · ₱3,000\.00 · ✓ \d+ Sep \(Someone\)`,
		command(t, bot, sender, "/history batelec"))
}

func TestHistoryOfABillNoMonthHasYet(t *testing.T) {
	bot, sender := newTestBot(t)

	assert.Contains(t, command(t, bot, sender, "/history batelec"),
		"<i>No month has this bill on it yet.</i>")
}

func TestHistoryAsksWhichBillWhenTheNameFitsSeveral(t *testing.T) {
	bot, sender := newTestBot(t)

	text := command(t, bot, sender, "/history bdo")

	assert.Contains(t, text, "“bdo” could be")
	assert.Contains(t, text, "<b>BDO JCB CC ••5994</b>")
	assert.Contains(t, text, "<b>BDO Home Loan</b>")
}

func TestHistoryOfSomethingThatIsNotABill(t *testing.T) {
	bot, sender := newTestBot(t)

	assert.Contains(t, command(t, bot, sender, "/history spotify"), "No bill matches “spotify”")
	assert.Contains(t, command(t, bot, sender, "/history"), "/history &lt;bill&gt;")
}

func TestSummaryTotsUpTheMonthPeopleAreWorkingOn(t *testing.T) {
	bot, sender := newTestBot(t)
	aSeptemberWorkedThrough(t, bot, sender)

	text := command(t, bot, sender, "/summary")

	assert.Contains(t, text, "📊 <b>September 2026</b>")
	assert.Contains(t, text, "<i>1 of 16 paid · 14 no amount yet</i>")
	assert.Contains(t, text, "RCBC · ₱103,431.23 (1 unknown) · 0 of 2 paid")
	assert.Contains(t, text, "Kevin · ₱3,000.00 (5 unknown) · 1 of 6 paid")
	assert.Contains(t, text, "Sheena BPI · need ₱103,431.23 <i>(tentative, 4 unknown)</i> · "+
		"sent ₱205,601.58 (+₱102,170.35)")
	assert.Contains(t, text, "Sheena PSBank · need ₱0.00 <i>(tentative, 1 unknown)</i> · not sent")
	assert.Contains(t, text, "<b>To settle ₱106,431.23</b> <i>(13 unknown)</i>")
	assert.Contains(t, text, "<b>Cash out ₱3,000.00</b> <i>(5 unknown)</i>")
}

func TestSummaryOfAMonthThatWasNamed(t *testing.T) {
	bot, sender := newTestBot(t)
	aSeptemberWorkedThrough(t, bot, sender)

	assert.Contains(t, command(t, bot, sender, "/summary 2026-09"), "📊 <b>September 2026</b>")
	assert.Contains(t, command(t, bot, sender, "/summary 2026-07"), "⚠️ That month has not been opened")
	assert.Contains(t, command(t, bot, sender, "/summary wat"), "⚠️ A month is written YYYY-MM")
}

func TestSummaryBeforeAnyMonthIsOpen(t *testing.T) {
	bot, sender := newTestBot(t)

	assert.Contains(t, command(t, bot, sender, "/summary"), "No month has been opened yet.")
}

func TestExportSendsEveryPayableAsACSVFile(t *testing.T) {
	bot, sender := newTestBot(t)
	aSeptemberWorkedThrough(t, bot, sender)

	bot.handleMessage(message(groupChatID, kevinID, "supergroup", "/export"))

	require.Len(t, sender.documents, 1)
	doc := sender.documents[0]
	assert.Equal(t, groupChatID, doc.ChatID)
	assert.Equal(t, "September 2026.", doc.Caption)
	file, ok := doc.File.(tgbotapi.FileBytes)
	require.True(t, ok)
	assert.Equal(t, "bills_export.csv", file.Name)

	rows := strings.Split(strings.TrimRight(string(file.Bytes), "\n"), "\n")
	require.Len(t, rows, 17, "a header and every bill of the month")
	assert.Equal(t, "month,section,bill,channel,card,amount,status,paid_at,paid_by", rows[0])
	assert.Contains(t, rows, "2026-09,RCBC,RCBC JCB CC,Sheena BPI,,103431.23,funded,,")
	assert.Contains(t, rows, "2026-09,BDO,BDO JCB CC,Sheena BDO,,,due,,")
	assert.Contains(t, rows, "2026-09,Utilities,Internet PLDT,Charged to card,RCBC Visa Airmiles,,due,,")
}

func TestExportTransfersSendsTheAccountsInstead(t *testing.T) {
	bot, sender := newTestBot(t)
	aSeptemberWorkedThrough(t, bot, sender)

	bot.handleMessage(message(groupChatID, kevinID, "supergroup", "/export transfers"))

	require.Len(t, sender.documents, 1)
	file, ok := sender.documents[0].File.(tgbotapi.FileBytes)
	require.True(t, ok)
	assert.Equal(t, "transfers_export.csv", file.Name)

	rows := strings.Split(strings.TrimRight(string(file.Bytes), "\n"), "\n")
	require.Len(t, rows, 4, "a header and sheena's three accounts")
	assert.Equal(t, "month,channel,required,sent,sent_at,sent_by", rows[0])
	assert.Contains(t, rows[1], "2026-09,Sheena BDO,0.00,,,")
	assert.Contains(t, rows[2], "2026-09,Sheena BPI,103431.23,205601.58,")
	assert.Contains(t, rows[2], ",Someone")
}

func TestExportBeforeAnyMonthIsOpenSendsNothing(t *testing.T) {
	bot, sender := newTestBot(t)

	assert.Contains(t, command(t, bot, sender, "/export"), "There is nothing to export yet.")
	assert.Empty(t, sender.documents)
}

func TestExportOfSomethingItCannotExport(t *testing.T) {
	bot, sender := newTestBot(t)
	aSeptemberWorkedThrough(t, bot, sender)

	assert.Contains(t, command(t, bot, sender, "/export payables"), "<code>/export transfers</code>")
	assert.Empty(t, sender.documents)
}
