package telegram

import (
	"strings"
	"testing"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// bills runs a /bills command as Kevin in the group and returns what the bot said.
func bills(t *testing.T, bot *Bot, sender *fakeSender, text string) string {
	t.Helper()

	before := len(sender.messages)
	bot.handleMessage(message(groupChatID, kevinID, "supergroup", text))
	require.Len(t, sender.messages, before+1, "%q should be answered exactly once", text)
	return sender.lastText()
}

func TestBillsPrintsTheStickyNoteGroupedBySection(t *testing.T) {
	bot, sender := newTestBot(t)

	text := bills(t, bot, sender, "/bills")

	assert.Equal(t, tgbotapi.ModeHTML, sender.messages[0].ParseMode)
	assert.Contains(t, text, "16 bills in 9 sections")
	assert.Contains(t, text, "<b>BDO</b>\n• BDO JCB CC · Sheena BDO\n"+
		"• BDO Unionpay CC · Sheena BDO\n• BDO Home Loan · Sheena BDO")
	assert.Contains(t, text, "• UnionBank CC · Kevin")
	assert.Contains(t, text, "• PSBank Car Loan · Sheena PSBank")
	assert.Contains(t, text, "• Internet PLDT · → RCBC Visa Airmiles")
	assert.Contains(t, text, "• BPI Wealth Builder · Kevin")
}

func TestBillsListsSectionsInDisplayOrder(t *testing.T) {
	bot, sender := newTestBot(t)

	text := bills(t, bot, sender, "/bills")

	order := []string{
		"<b>UnionBank</b>", "<b>BDO</b>", "<b>RCBC</b>", "<b>BPI</b>", "<b>HSBC</b>",
		"<b>PSBank</b>", "<b>Bahay</b>", "<b>Investment</b>", "<b>Utilities</b>",
	}
	at := 0
	for _, heading := range order {
		found := indexAfter(text, heading, at)
		require.GreaterOrEqual(t, found, 0, "%s should appear after the section before it", heading)
		at = found
	}
}

func TestBillsAddPutsABillOnTheList(t *testing.T) {
	bot, sender := newTestBot(t)

	added := bills(t, bot, sender, "/bills add Netflix | Utilities | kevin")

	assert.Contains(t, added, "Added <b>Netflix</b> · Kevin, under <b>Utilities</b>.")
	assert.Contains(t, bills(t, bot, sender, "/bills"), "• Netflix · Kevin")
	assert.Contains(t, bills(t, bot, sender, "/bills"), "17 bills in 9 sections")
}

func TestBillsAddChargedToACardNamesTheCard(t *testing.T) {
	bot, sender := newTestBot(t)

	added := bills(t, bot, sender, "/bills add Spotify | Utilities | card | RCBC Visa Airmiles")

	assert.Contains(t, added, "Added <b>Spotify</b> · → RCBC Visa Airmiles")
	assert.Contains(t, bills(t, bot, sender, "/bills"), "• Spotify · → RCBC Visa Airmiles")
}

func TestBillsAddAcceptsChannelsAsAPersonWritesThem(t *testing.T) {
	tests := []struct {
		name    string
		command string
		want    string
	}{
		{"spoken", "/bills add A | Utilities | Sheena BDO", "· Sheena BDO"},
		{"bank alone", "/bills add B | Utilities | bpi", "· Sheena BPI"},
		{"stored value", "/bills add C | Utilities | sheena_psbank", "· Sheena PSBank"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bot, sender := newTestBot(t)

			assert.Contains(t, bills(t, bot, sender, tt.command), tt.want)
		})
	}
}

func TestBillsAddRefusals(t *testing.T) {
	tests := []struct {
		name    string
		command string
		want    string
	}{
		{"a name already taken", "/bills add water | Utilities | kevin", "⚠️ There is already a bill"},
		{"an unknown section", "/bills add Netflix | Streaming | kevin", "⚠️ There is no section with that name: Streaming"},
		{"an unknown channel", "/bills add Netflix | Utilities | venmo", "is not a payment channel"},
		{"too few fields", "/bills add Netflix | Utilities", "separated by |"},
		{"too many fields", "/bills add A | B | kevin | C | D", "separated by |"},
		{"a card with no card named", "/bills add Netflix | Utilities | card", "⚠️ A bill charged to a card needs the card"},
		{"a card name where none belongs", "/bills add N | Utilities | kevin | BPI CC", "⚠️ Only a bill charged to a card"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bot, sender := newTestBot(t)

			assert.Contains(t, bills(t, bot, sender, tt.command), tt.want)
			assert.Contains(t, bills(t, bot, sender, "/bills"), "16 bills",
				"a refused bill must not reach the list")
		})
	}
}

func TestBillsSectionAppendsAHeading(t *testing.T) {
	bot, sender := newTestBot(t)

	added := bills(t, bot, sender, "/bills section Subscriptions")
	assert.Contains(t, added, "Added section <b>Subscriptions</b>")

	assert.NotContains(t, bills(t, bot, sender, "/bills"), "Subscriptions",
		"an empty section is not a heading worth printing")

	bills(t, bot, sender, "/bills add Netflix | Subscriptions | kevin")
	text := bills(t, bot, sender, "/bills")
	assert.Contains(t, text, "<b>Subscriptions</b>\n• Netflix · Kevin")
	assert.Contains(t, text, "17 bills in 10 sections")
}

func TestBillsSectionRefusesADuplicate(t *testing.T) {
	bot, sender := newTestBot(t)

	assert.Contains(t, bills(t, bot, sender, "/bills section utilities"), "⚠️ There is already a section")
}

func TestBillsShowsUsageForAnythingElse(t *testing.T) {
	bot, sender := newTestBot(t)

	for _, command := range []string{"/bills wat", "/bills add", "/bills section"} {
		assert.Contains(t, bills(t, bot, sender, command), "/bills add", "command %q", command)
	}
}

func TestBillsEscapesNamesSoTheyCannotBreakTheMarkup(t *testing.T) {
	bot, sender := newTestBot(t)

	bills(t, bot, sender, "/bills add <b>Netflix</b> & co | Utilities | kevin")
	text := bills(t, bot, sender, "/bills")

	assert.Contains(t, text, "• &lt;b&gt;Netflix&lt;/b&gt; &amp; co · Kevin")
}

// indexAfter reports where needle next appears in haystack at or after from, or -1.
func indexAfter(haystack, needle string, from int) int {
	if from >= len(haystack) {
		return -1
	}
	found := strings.Index(haystack[from:], needle)
	if found < 0 {
		return -1
	}
	return from + found
}
