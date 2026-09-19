package telegram

import (
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// say sends text as Kevin in the group.
func say(bot *Bot, text string) {
	bot.handleMessage(message(groupChatID, kevinID, "supergroup", text))
}

// tap delivers a button tap as userID in chatID.
func tap(bot *Bot, chatID, userID int64, data string) {
	bot.handleCallback(&tgbotapi.CallbackQuery{
		ID:      "query",
		From:    &tgbotapi.User{ID: userID},
		Message: &tgbotapi.Message{Chat: &tgbotapi.Chat{ID: chatID}},
		Data:    data,
	})
}

func TestNewMonthPostsAndPinsABoardWithEveryBillUnknown(t *testing.T) {
	bot, sender := newTestBot(t)

	say(bot, "/newmonth")

	require.Len(t, sender.messages, 1, "the board is the whole reply")
	posted := sender.messages[0]
	assert.Equal(t, groupChatID, posted.ChatID)
	assert.Equal(t, tgbotapi.ModeHTML, posted.ParseMode)
	assert.True(t, strings.HasPrefix(posted.Text, "📋 <b>Bills — September 2026</b>\n"+
		"<i>0 of 16 paid · 16 no amount yet</i>"))
	assert.Contains(t, posted.Text, "? BDO JCB CC · — · Sheena BDO")
	assert.Contains(t, posted.Text, "? Internet PLDT · — · → RCBC Visa Airmiles")
	assert.Equal(t, 16, strings.Count(posted.Text, "\n? "), "every bill opens unknown")
	assert.Contains(t, posted.Text, "<b>To settle ₱0.00</b> <i>(15 unknown)</i>")
	assert.Contains(t, posted.Text, "Sheena PSBank: need ₱0.00 <i>(tentative, 1 unknown)</i> · not sent")

	markup, ok := posted.ReplyMarkup.(tgbotapi.InlineKeyboardMarkup)
	require.True(t, ok, "the board carries its buttons")
	require.Len(t, markup.InlineKeyboard, 9, "16 bills two to a row, then refresh and transfer")
	assert.Equal(t, "? UnionBank CC", markup.InlineKeyboard[0][0].Text)
	last := markup.InlineKeyboard[8]
	assert.Equal(t, "🔄 Refresh", last[0].Text)

	pins := requestsOf[tgbotapi.PinChatMessageConfig](sender)
	require.Len(t, pins, 1)
	assert.Equal(t, groupChatID, pins[0].ChatID)
	assert.Equal(t, 1, pins[0].MessageID, "the message just posted is the one pinned")
	assert.True(t, pins[0].DisableNotification, "pinning should not ping the household")

	snap, err := bot.tracker.CurrentSnapshot()
	require.NoError(t, err)
	assert.Equal(t, groupChatID, snap.Cycle.BoardChatID)
	assert.Equal(t, 1, snap.Cycle.BoardMessageID, "the board's location is stored")
}

func TestNewMonthTwiceSaysTheMonthExists(t *testing.T) {
	bot, sender := newTestBot(t)
	say(bot, "/newmonth")

	say(bot, "/newmonth")

	require.Len(t, sender.messages, 2)
	assert.Equal(t, "📋 <b>September 2026</b> is already open. "+
		"<code>/board 2026-09</code> posts its board again.", sender.lastText())
	assert.Len(t, requestsOf[tgbotapi.PinChatMessageConfig](sender), 1, "nothing is pinned twice")
}

func TestNewMonthForAClosedMonthSaysSo(t *testing.T) {
	bot, sender, db := newTestBotWithDB(t)
	say(bot, "/newmonth")
	snap, err := bot.tracker.CurrentSnapshot()
	require.NoError(t, err)
	closeCycle(t, db, snap.Cycle.ID)

	say(bot, "/newmonth 2026-09")

	assert.Contains(t, sender.lastText(), "already exists and is closed")
}

func TestNewMonthWithAMonthArgument(t *testing.T) {
	tests := []struct {
		name string
		arg  string
		want string
	}{
		{"next month may be opened early", "2026-10", "📋 <b>Bills — October 2026</b>"},
		{"a past month may be opened late", "2026-08", "📋 <b>Bills — August 2026</b>"},
		{"too far ahead", "2026-11", "November 2026 is too far ahead. I can open up to October 2026."},
		{"not a month", "sept", "A month is written YYYY-MM"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bot, sender := newTestBot(t)

			say(bot, "/newmonth "+tt.arg)

			require.Len(t, sender.messages, 1)
			assert.Contains(t, sender.lastText(), tt.want)
		})
	}
}

func TestNewMonthFollowsManilaNotUTC(t *testing.T) {
	bot, sender := newTestBot(t)
	// 1 October 2026, 07:00 in Manila, which is still 30 September in UTC.
	bot.now = func() time.Time { return time.Date(2026, 9, 30, 23, 0, 0, 0, time.UTC) }

	say(bot, "/newmonth")

	assert.Contains(t, sender.lastText(), "Bills — October 2026")
}

func TestNewMonthStillOpensWhenPinningIsRefused(t *testing.T) {
	bot, sender := newTestBot(t)
	sender.refusePins = true

	say(bot, "/newmonth")

	require.Len(t, sender.messages, 2)
	assert.Contains(t, sender.messages[0].Text, "Bills — September 2026")
	assert.Contains(t, sender.lastText(), "could not pin it")
	_, err := bot.tracker.CurrentSnapshot()
	assert.NoError(t, err, "the month is open regardless")
}

func TestBoardWithNoMonthOpenPointsAtNewMonth(t *testing.T) {
	bot, sender := newTestBot(t)

	say(bot, "/board")

	require.Len(t, sender.messages, 1)
	assert.Contains(t, sender.lastText(), "<code>/newmonth</code>")
	assert.Empty(t, requestsOf[tgbotapi.PinChatMessageConfig](sender))
}

func TestBoardForAMonthNeverOpened(t *testing.T) {
	bot, sender := newTestBot(t)

	say(bot, "/board 2026-07")

	assert.Contains(t, sender.lastText(), "That month has not been opened: July 2026")
}

func TestBoardRepostsAndRetiresTheOldCopy(t *testing.T) {
	bot, sender := newTestBot(t)
	say(bot, "/newmonth")

	say(bot, "/board")

	require.Len(t, sender.messages, 2)
	assert.Contains(t, sender.lastText(), "Bills — September 2026")

	pins := requestsOf[tgbotapi.PinChatMessageConfig](sender)
	require.Len(t, pins, 2)
	assert.Equal(t, 2, pins[1].MessageID)

	unpins := requestsOf[tgbotapi.UnpinChatMessageConfig](sender)
	require.Len(t, unpins, 1)
	assert.Equal(t, 1, unpins[0].MessageID, "the earlier board is unpinned")
	deletes := requestsOf[tgbotapi.DeleteMessageConfig](sender)
	require.Len(t, deletes, 1)
	assert.Equal(t, 1, deletes[0].MessageID, "and removed, so only one copy has buttons")

	snap, err := bot.tracker.CurrentSnapshot()
	require.NoError(t, err)
	assert.Equal(t, 2, snap.Cycle.BoardMessageID)
}

func TestBoardForAClosedMonthIsPostedButNotPinned(t *testing.T) {
	bot, sender, db := newTestBotWithDB(t)
	say(bot, "/newmonth")
	snap, err := bot.tracker.CurrentSnapshot()
	require.NoError(t, err)
	closeCycle(t, db, snap.Cycle.ID)

	say(bot, "/board 2026-09")

	assert.Contains(t, sender.lastText(), "✅ <b>CLOSED</b>")
	assert.Nil(t, sender.messages[len(sender.messages)-1].ReplyMarkup, "a closed board has no buttons")
	assert.Len(t, requestsOf[tgbotapi.PinChatMessageConfig](sender), 1, "only the open board was pinned")
}

func TestRefreshButtonEditsTheBoardInPlace(t *testing.T) {
	bot, sender := newTestBot(t)
	say(bot, "/newmonth")
	snap, err := bot.tracker.CurrentSnapshot()
	require.NoError(t, err)

	tap(bot, groupChatID, sheenaID, "r:"+itoa(snap.Cycle.ID))

	edits := requestsOf[tgbotapi.EditMessageTextConfig](sender)
	require.Len(t, edits, 1)
	assert.Equal(t, 1, edits[0].MessageID)
	assert.Equal(t, groupChatID, edits[0].ChatID)
	assert.Equal(t, tgbotapi.ModeHTML, edits[0].ParseMode)
	assert.Contains(t, edits[0].Text, "Bills — September 2026")
	require.NotNil(t, edits[0].ReplyMarkup)

	answers := requestsOf[tgbotapi.CallbackConfig](sender)
	require.Len(t, answers, 1)
	assert.Equal(t, "Board is up to date.", answers[0].Text)
	assert.Len(t, sender.messages, 1, "a refresh posts nothing new")
}

func TestRefreshOfAnUnchangedBoardIsNotAFailure(t *testing.T) {
	bot, sender := newTestBot(t)
	say(bot, "/newmonth")
	sender.editErr = errors.New("Bad Request: message is not modified")
	snap, err := bot.tracker.CurrentSnapshot()
	require.NoError(t, err)

	tap(bot, groupChatID, kevinID, "r:"+itoa(snap.Cycle.ID))

	answers := requestsOf[tgbotapi.CallbackConfig](sender)
	require.Len(t, answers, 1)
	assert.Equal(t, "Board is up to date.", answers[0].Text)
}

func TestButtonsNotBuiltYetAreStillAnswered(t *testing.T) {
	bot, sender := newTestBot(t)
	say(bot, "/newmonth")

	tap(bot, groupChatID, kevinID, "t:1")

	answers := requestsOf[tgbotapi.CallbackConfig](sender)
	require.Len(t, answers, 1)
	assert.Equal(t, "That button does not work yet.", answers[0].Text)
}

func TestTapsFromOutsideTheGateAreAnsweredSilently(t *testing.T) {
	tests := []struct {
		name   string
		chatID int64
		userID int64
		data   string
	}{
		{"a stranger in the group", groupChatID, strangerID, "r:1"},
		{"another chat", otherGroupID, kevinID, "r:1"},
		{"data the bot did not write", groupChatID, kevinID, "nonsense"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bot, sender := newTestBot(t)
			say(bot, "/newmonth")

			tap(bot, tt.chatID, tt.userID, tt.data)

			answers := requestsOf[tgbotapi.CallbackConfig](sender)
			require.Len(t, answers, 1, "the spinner must still stop")
			assert.Empty(t, answers[0].Text)
			assert.Empty(t, requestsOf[tgbotapi.EditMessageTextConfig](sender))
		})
	}
}

func TestAddingABillMidMonthRefreshesTheBoard(t *testing.T) {
	bot, sender := newTestBot(t)
	say(bot, "/newmonth")

	say(bot, "/bills add Netflix | Utilities | kevin")

	edits := requestsOf[tgbotapi.EditMessageTextConfig](sender)
	require.Len(t, edits, 1)
	assert.Contains(t, edits[0].Text, "? Netflix · — · Kevin")
	assert.Contains(t, edits[0].Text, "0 of 17 paid")
}

// closeCycle closes a Cycle directly. Closing arrives with marking paid, which no command
// can do yet.
func closeCycle(t *testing.T, db *gorm.DB, cycleID int64) {
	t.Helper()
	require.NoError(t, db.Exec(
		"UPDATE cycles SET closed_at = CURRENT_TIMESTAMP WHERE id = ?", cycleID).Error)
}

func itoa(id int64) string { return strconv.FormatInt(id, 10) }
