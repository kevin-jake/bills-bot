package telegram

import (
	"strings"
	"testing"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/kevin-jake/bills-bot/internal/board"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// boardText is the current text of the board, which every change to the standing list
// edits in place.
func boardText(t *testing.T, sender *fakeSender) string {
	t.Helper()

	edits := requestsOf[tgbotapi.EditMessageTextConfig](sender)
	require.NotEmpty(t, edits, "the board should have been redrawn")
	return edits[len(edits)-1].Text
}

func TestBillsHelpListsEverythingTheListCanBeToldToDo(t *testing.T) {
	bot, sender := newTestBot(t)

	text := command(t, bot, sender, "/bills help")

	for _, sub := range []string{"rename", "alias", "move", "channel", "card", "due",
		"order", "archive", "restore"} {
		assert.Contains(t, text, "/bills "+sub, "%s should be documented", sub)
	}
}

func TestBillsRenamesABillEverywhere(t *testing.T) {
	bot, sender := newTestBot(t)
	say(bot, "/newmonth")

	text := command(t, bot, sender, "/bills rename bpi visa | BPI Signature CC")

	assert.Contains(t, text, "Renamed <b>BPI Visa CC ••7577</b> to <b>BPI Signature CC ••7577</b>")
	assert.Contains(t, boardText(t, sender), "BPI Signature CC ••7577",
		"the open month's board is redrawn with the new name")
	assert.Contains(t, command(t, bot, sender, "/bills"), "• BPI Signature CC ••7577")
}

func TestRenamingACardRepointsWhatIsChargedToIt(t *testing.T) {
	bot, sender := newTestBot(t)

	text := command(t, bot, sender, "/bills rename rcbc visa airmiles | RCBC Airmiles")

	assert.Contains(t, text, "1 bill charged to it now says so.")
	assert.Contains(t, command(t, bot, sender, "/bills"), "• Internet PLDT · → RCBC Airmiles")
}

func TestBillsAliasLetsAShortcutFindABillByItsHouseName(t *testing.T) {
	bot, sender := newTestBot(t)
	say(bot, "/newmonth")

	assert.Contains(t, command(t, bot, sender, "/bills alias internet pldt | fibre, wifi"),
		"also answers to <b>fibre</b>, <b>wifi</b>")
	assert.Contains(t, command(t, bot, sender, "/bills"), "<i>(fibre, wifi)</i>")

	assert.Contains(t, command(t, bot, sender, "wifi 2499"), "<b>Internet PLDT</b> set to ₱2,499.00")

	assert.Contains(t, command(t, bot, sender, "/bills alias pldt |"),
		"answers to its name alone")
}

func TestBillsMoveListsABillUnderAnotherHeading(t *testing.T) {
	bot, sender := newTestBot(t)
	say(bot, "/newmonth")

	text := command(t, bot, sender, "/bills move gcash | Investment")

	assert.Contains(t, text, "<b>GCash funds</b> is now listed under <b>Investment</b>, last.")
	assert.Contains(t, boardText(t, sender), "BPI Wealth Builder · — · Kevin\n? GCash funds")
}

func TestBillsChannelMovesABillBetweenAccounts(t *testing.T) {
	bot, sender := newTestBot(t)
	say(bot, "/newmonth")

	text := command(t, bot, sender, "/bills channel batelec | sheena bdo")

	assert.Contains(t, text, "<b>Batelec</b> is now paid Sheena BDO")
	assert.Contains(t, text, "keep <i>Kevin</i>")
	assert.Contains(t, boardText(t, sender), "? Batelec · — · Sheena BDO")
}

func TestBillsChannelToACardNamesTheCard(t *testing.T) {
	bot, sender := newTestBot(t)

	assert.Contains(t, command(t, bot, sender, "/bills channel water | card"),
		"⚠️ A bill charged to a card needs the card&#39;s name")
	assert.Contains(t, command(t, bot, sender, "/bills channel water | card | BPI Visa CC"),
		"is now paid → BPI Visa CC")
}

func TestBillsCardAndDueDayAreSetAndCleared(t *testing.T) {
	bot, sender := newTestBot(t)

	assert.Contains(t, command(t, bot, sender, "/bills card bpi visa | 4321"),
		"<b>BPI Visa CC ••4321</b>")
	assert.Contains(t, command(t, bot, sender, "/bills card bpi visa | "),
		"no longer carries any digits")
	assert.Contains(t, command(t, bot, sender, "/bills card bpi visa | 12"),
		"⚠️ A card&#39;s last four digits are four numbers")

	assert.Contains(t, command(t, bot, sender, "/bills due water | 9"),
		"<b>Water</b> is due 9 from now on")
	assert.Contains(t, command(t, bot, sender, "/bills due water | 0"),
		"has no fixed day any more")
	assert.Contains(t, command(t, bot, sender, "/bills due water | soon"),
		"⚠️ A due day is a day of the month")
}

func TestBillsOrderMovesABillWithinItsSection(t *testing.T) {
	bot, sender := newTestBot(t)
	say(bot, "/newmonth")

	text := command(t, bot, sender, "/bills order bdo home loan | top")

	assert.Contains(t, text, "<b>BDO Home Loan</b> is now 1st under <b>BDO</b>.")
	assert.Contains(t, boardText(t, sender),
		"<b>BDO</b>\n? BDO Home Loan · — · Sheena BDO · due 25\n? BDO JCB CC")

	assert.Contains(t, command(t, bot, sender, "/bills order bdo home loan | down"),
		"is now 2nd under <b>BDO</b>")
	assert.Contains(t, command(t, bot, sender, "/bills order bdo home loan | sideways"),
		"⚠️ Say up, down, top, bottom, or a position such as 2")
}

func TestBillsOrderSectionMovesAHeading(t *testing.T) {
	bot, sender := newTestBot(t)
	say(bot, "/newmonth")

	text := command(t, bot, sender, "/bills order section Utilities | 1")

	assert.Contains(t, text, "<b>Utilities</b> is now 1st on the board.")
	assert.Contains(t, text, "<i>Utilities · UnionBank · BDO")
	assert.True(t, strings.Index(boardText(t, sender), "<b>Utilities</b>") <
		strings.Index(boardText(t, sender), "<b>UnionBank</b>"),
		"the board regroups in the new order")
}

func TestArchiveAsksFirstAndNamesWhatWouldGo(t *testing.T) {
	bot, sender := newTestBot(t)
	say(bot, "/newmonth")
	say(bot, "bpi visa 103431.23")

	text := command(t, bot, sender, "/bills archive bpi visa")

	assert.Contains(t, text, "🗄 Archive <b>BPI Visa CC ••7577</b>?")
	assert.Contains(t, text, "its unpaid line goes from <b>September 2026</b> "+
		"(₱103,431.23 entered, kept for a restore)")
	assert.Contains(t, text, "<code>/history</code> still shows them")

	markup, ok := sender.messages[len(sender.messages)-1].ReplyMarkup.(tgbotapi.InlineKeyboardMarkup)
	require.True(t, ok, "the question carries its buttons")
	require.Len(t, markup.InlineKeyboard, 1)
	assert.Equal(t, "🗄 Archive", markup.InlineKeyboard[0][0].Text)
	assert.Equal(t, "✖ Keep it", markup.InlineKeyboard[0][1].Text)

	assert.Contains(t, boardText(t, sender), "BPI Visa CC", "nothing has happened yet")
}

func TestArchivingTakesTheLineOffTheBoardAndKeepsTheHistory(t *testing.T) {
	bot, sender := newTestBot(t)
	say(bot, "/newmonth")
	say(bot, "batelec 550")
	say(bot, "/bills archive batelec")

	billID := archiveButtonID(t, sender)
	tap(bot, groupChatID, kevinID, board.Callback{
		Kind: board.KindBill, ID: billID, Arg: string(board.BillArchive)}.Encode())

	settled := requestsOf[tgbotapi.EditMessageTextConfig](sender)
	assert.Contains(t, settled[len(settled)-2].Text, "🗄 <b>Batelec</b> is off the standing list.")
	assert.Contains(t, settled[len(settled)-2].Text, "1 board line withdrawn")
	assert.NotContains(t, boardText(t, sender), "Batelec", "the line is off the board")
	assert.Contains(t, boardText(t, sender), "15 no amount yet", "and out of the count")

	assert.NotContains(t, command(t, bot, sender, "/bills"), "• Batelec")
	assert.Contains(t, command(t, bot, sender, "/bills archived"), "• Batelec · Kevin")
}

func TestArchivedBillsHistoryStillShowsTheMonthsItWasPaidIn(t *testing.T) {
	bot, sender := newTestBot(t)
	say(bot, "/newmonth")
	say(bot, "paid batelec 550")
	say(bot, "/bills archive batelec")
	tap(bot, groupChatID, kevinID, board.Callback{
		Kind: board.KindBill, ID: archiveButtonID(t, sender), Arg: string(board.BillArchive)}.Encode())

	text := command(t, bot, sender, "/history batelec")

	assert.Contains(t, text, "Sep 2026")
	assert.Contains(t, text, "₱550.00")
}

func TestRestoreBringsTheLineBackWithItsFigure(t *testing.T) {
	bot, sender := newTestBot(t)
	say(bot, "/newmonth")
	say(bot, "batelec 550")
	say(bot, "/bills archive batelec")
	tap(bot, groupChatID, kevinID, board.Callback{
		Kind: board.KindBill, ID: archiveButtonID(t, sender), Arg: string(board.BillArchive)}.Encode())

	text := command(t, bot, sender, "/bills restore batelec")

	assert.Contains(t, text, "<b>Batelec</b> is back on the standing list, under <b>Utilities</b>.")
	assert.Contains(t, text, "1 board line back.")
	assert.Contains(t, boardText(t, sender), "☐ Batelec · ₱550.00 · Kevin")
}

func TestNamingAnArchivedBillPointsAtTheRestore(t *testing.T) {
	bot, sender := newTestBot(t)
	say(bot, "/bills archive batelec")
	tap(bot, groupChatID, kevinID, board.Callback{
		Kind: board.KindBill, ID: archiveButtonID(t, sender), Arg: string(board.BillArchive)}.Encode())

	text := command(t, bot, sender, "/bills rename batelec | Meralco")

	assert.Contains(t, text, "<b>Batelec</b> has been archived.")
	assert.Contains(t, text, "<code>/bills restore Batelec</code>")
}

func TestKeepingItLeavesTheBillAlone(t *testing.T) {
	bot, sender := newTestBot(t)
	say(bot, "/newmonth")
	say(bot, "/bills archive batelec")

	tap(bot, groupChatID, kevinID, board.Callback{Kind: board.KindCancel}.Encode())

	settled := requestsOf[tgbotapi.EditMessageTextConfig](sender)
	assert.Contains(t, settled[len(settled)-1].Text, "Never mind")
	assert.Contains(t, command(t, bot, sender, "/bills"), "• Batelec")
}

func TestAChangeToTheListSaysWhichBillItCouldNotPick(t *testing.T) {
	bot, sender := newTestBot(t)

	assert.Contains(t, command(t, bot, sender, "/bills rename bdo | Something"),
		"could be <b>BDO JCB CC ••5994</b> or <b>BDO Unionpay CC ••1630</b>")
	assert.Contains(t, command(t, bot, sender, "/bills rename netflix | Something"),
		"No bill matches “netflix”")
	assert.Contains(t, command(t, bot, sender, "/bills restore water"),
		"No archived bill matches “water”")
}

func TestASubcommandWrittenWrongShowsHowItIsWritten(t *testing.T) {
	bot, sender := newTestBot(t)

	for _, typed := range []string{"/bills rename water", "/bills move", "/bills order",
		"/bills archive", "/bills restore", "/bills alias | nothing"} {
		assert.Contains(t, command(t, bot, sender, typed), "⚠️ <code>/bills ",
			"%q should be answered with its usage", typed)
	}
}

func TestArchivedListIsEmptyUntilSomethingLeaves(t *testing.T) {
	bot, sender := newTestBot(t)

	assert.Contains(t, command(t, bot, sender, "/bills archived"),
		"<i>Nothing has left the standing list.</i>")
}

// archiveButtonID reads the Bill the pending archive question is about.
func archiveButtonID(t *testing.T, sender *fakeSender) int64 {
	t.Helper()

	markup, ok := sender.messages[len(sender.messages)-1].ReplyMarkup.(tgbotapi.InlineKeyboardMarkup)
	require.True(t, ok, "the archive question should carry buttons")
	callback, err := board.DecodeCallback(*markup.InlineKeyboard[0][0].CallbackData)
	require.NoError(t, err)
	return callback.ID
}
