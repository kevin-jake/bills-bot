package telegram

import (
	"errors"
	"fmt"
	"html"
	"log"
	"strings"
	"unicode"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/kevin-jake/bills-bot/internal/domain"
	"github.com/kevin-jake/bills-bot/internal/tracker"
)

// billsUsage is shown whenever the arguments do not parse. The pipes matter: Section and
// Bill names contain spaces ("BDO Home Loan", "RCBC Visa Airmiles"), so splitting on them
// would cut names in half.
const billsUsage = "📋 <b>Bills</b>\n\n" +
	"<code>/bills</code> — the standing list\n" +
	"<code>/bills add &lt;name&gt; | &lt;section&gt; | &lt;channel&gt; [| &lt;card&gt;]</code>\n" +
	"<code>/bills section &lt;name&gt;</code> — a new heading, at the end\n\n" +
	"Channels: <code>kevin</code>, <code>sheena bdo</code>, <code>sheena bpi</code>, " +
	"<code>sheena psbank</code>, <code>card</code>\n" +
	"A bill on <code>card</code> names the card it lands on; no other channel may.\n\n" +
	"<i>Example:</i> <code>/bills add Netflix | Utilities | card | RCBC Visa Airmiles</code>"

// handleBills routes /bills. With no arguments it prints the standing list, which is the
// sticky note without any month's amounts on it.
func (b *Bot) handleBills(message *tgbotapi.Message, args []string) {
	if len(args) == 0 {
		b.sendBillList(message.Chat.ID)
		return
	}

	switch strings.ToLower(args[0]) {
	case "add":
		b.addBill(message, args[1:])
	case "section":
		b.addSection(message, args[1:])
	default:
		b.sendHTML(message.Chat.ID, billsUsage)
	}
}

func (b *Bot) sendBillList(chatID int64) {
	list, err := b.tracker.BillList()
	if err != nil {
		log.Printf("failed to read the bill list: %v", err)
		b.send(chatID, "I could not read the bill list just now. Try again in a moment.")
		return
	}
	b.sendHTML(chatID, renderBillList(list))
}

func (b *Bot) addSection(message *tgbotapi.Message, args []string) {
	name := strings.TrimSpace(strings.Join(args, " "))
	if name == "" {
		b.sendHTML(message.Chat.ID, billsUsage)
		return
	}

	section, err := b.tracker.AddSection(actorOf(message), name)
	if err != nil {
		b.refuse(message.Chat.ID, err)
		return
	}
	b.sendHTML(message.Chat.ID, fmt.Sprintf(
		"Added section <b>%s</b>, last in the order. Put a bill under it with "+
			"<code>/bills add &lt;name&gt; | %s | &lt;channel&gt;</code>.",
		html.EscapeString(section.Name), html.EscapeString(section.Name)))
}

func (b *Bot) addBill(message *tgbotapi.Message, args []string) {
	spec, err := parseBillSpec(strings.Join(args, " "))
	if err != nil {
		b.sendHTML(message.Chat.ID, html.EscapeString(err.Error())+"\n\n"+billsUsage)
		return
	}

	bill, err := b.tracker.AddBill(actorOf(message), spec)
	if err != nil {
		b.refuse(message.Chat.ID, err)
		return
	}
	b.sendHTML(message.Chat.ID, fmt.Sprintf("Added <b>%s</b> · %s, under <b>%s</b>.",
		html.EscapeString(bill.Name), html.EscapeString(bill.ChannelLabel()),
		html.EscapeString(spec.Section)))

	// The new Bill joined every open Cycle, so their Boards are now a line short.
	b.refreshOpenBoards()
}

// parseBillSpec reads "name | section | channel [| card]".
func parseBillSpec(raw string) (tracker.BillSpec, error) {
	parts := strings.Split(raw, "|")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	if len(parts) < 3 || len(parts) > 4 {
		return tracker.BillSpec{}, errors.New("I need a name, a section and a channel, separated by |")
	}

	channel, err := domain.ParseChannel(parts[2])
	if err != nil {
		return tracker.BillSpec{}, err
	}

	spec := tracker.BillSpec{Name: parts[0], Section: parts[1], Channel: channel}
	if len(parts) == 4 {
		spec.CardName = parts[3]
	}
	return spec, nil
}

// refuse turns a rules error into a sentence for whoever typed the command. Anything that
// is not a rules error is a fault of the bot's, so it is logged rather than shown.
func (b *Bot) refuse(chatID int64, err error) {
	for _, known := range []error{
		domain.ErrNameRequired, domain.ErrCardNameRequired, domain.ErrCardNameUnwanted,
		tracker.ErrSectionUnknown, tracker.ErrSectionExists, tracker.ErrBillExists,
	} {
		if errors.Is(err, known) {
			b.sendHTML(chatID, "⚠️ "+html.EscapeString(capitalise(err.Error())))
			return
		}
	}
	log.Printf("bills command failed: %v", err)
	b.send(chatID, "Something went wrong writing that down. Nothing was changed.")
}

// capitalise raises the first letter, because a wrapped rules error is read as a sentence
// by whoever typed the command rather than as a Go error string.
func capitalise(s string) string {
	runes := []rune(s)
	if len(runes) == 0 {
		return s
	}
	runes[0] = unicode.ToUpper(runes[0])
	return string(runes)
}

// renderBillList lays the standing list out the way the sticky note groups it: a heading
// per Section, then each Bill with how it is paid.
func renderBillList(list []tracker.SectionBills) string {
	if len(list) == 0 {
		return "📋 <b>Bills</b>\n\n<i>The standing list is empty.</i>\n\n" + billsUsage
	}

	bills := 0
	for _, section := range list {
		bills += len(section.Bills)
	}

	var out strings.Builder
	fmt.Fprintf(&out, "📋 <b>Bills</b>\n<i>%s in %s</i>\n",
		plural(bills, "bill"), plural(len(list), "section"))

	for _, section := range list {
		fmt.Fprintf(&out, "\n<b>%s</b>\n", html.EscapeString(section.Section.Name))
		for _, bill := range section.Bills {
			fmt.Fprintf(&out, "• %s · %s\n",
				html.EscapeString(bill.Name), html.EscapeString(bill.ChannelLabel()))
		}
	}
	return strings.TrimRight(out.String(), "\n")
}

func plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

// actorOf records who asked. FirstName is what Telegram gives without extra lookups, and
// it is what the household calls each other anyway.
func actorOf(message *tgbotapi.Message) tracker.Actor {
	return tracker.Actor{TelegramID: message.From.ID, Name: message.From.FirstName}
}
