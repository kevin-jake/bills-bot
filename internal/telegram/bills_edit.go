package telegram

import (
	"html"
	"log"
	"strconv"
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/kevin-jake/bills-bot/internal/board"
	"github.com/kevin-jake/bills-bot/internal/domain"
	"github.com/kevin-jake/bills-bot/internal/parse"
	"github.com/kevin-jake/bills-bot/internal/tracker"
)

// Changing the standing list is typed rather than tapped. A Bill is named the loose way a
// shortcut names one, so "bpi cc" is enough, and what is being set follows a pipe, because
// names contain spaces. The one exception is archiving, which is asked about twice: it
// deletes the Bill's unpaid lines, and restoring brings them back blank.

// renameBill gives a Bill a new name, everywhere and in every month.
func (b *Bot) renameBill(message *tgbotapi.Message, args []string) {
	typed, name, ok := b.twoParts(message, args, "rename &lt;bill&gt; | &lt;new name&gt;")
	if !ok {
		return
	}
	bill, ok := b.resolveBill(message.Chat.ID, typed, listed)
	if !ok {
		return
	}

	change, err := b.tracker.RenameBill(actorOf(message), bill.ID, name)
	if err != nil {
		b.refuse(message.Chat.ID, err)
		return
	}

	text := "Renamed <b>" + html.EscapeString(change.Before.DisplayName()) + "</b> to <b>" +
		html.EscapeString(change.After.DisplayName()) + "</b>, in every month."
	if change.Charges > 0 {
		text += " " + capitalise(plural(change.Charges, "bill")) + " charged to it now says so."
	}
	b.announce(message.Chat.ID, text, change)
}

// aliasBill records the other names a Bill answers to, so that a typed shortcut finds it
// by whatever it is actually called in the house.
func (b *Bot) aliasBill(message *tgbotapi.Message, args []string) {
	typed, list, ok := b.twoParts(message, args, "alias &lt;bill&gt; | &lt;other name&gt;, &lt;…&gt;")
	if !ok {
		return
	}
	bill, ok := b.resolveBill(message.Chat.ID, typed, listed)
	if !ok {
		return
	}

	aliases := domain.ParseAliases(list)
	change, err := b.tracker.SetBillAliases(actorOf(message), bill.ID, aliases)
	if err != nil {
		b.refuse(message.Chat.ID, err)
		return
	}

	name := "<b>" + html.EscapeString(change.After.DisplayName()) + "</b>"
	if len(aliases) == 0 {
		b.announce(message.Chat.ID, name+" now answers to its name alone.", change)
		return
	}
	bold := make([]string, 0, len(aliases))
	for _, alias := range aliases {
		bold = append(bold, "<b>"+html.EscapeString(alias)+"</b>")
	}
	b.announce(message.Chat.ID, name+" also answers to "+strings.Join(bold, ", ")+".", change)
}

// moveBill lists a Bill under a different heading, at the end of it.
func (b *Bot) moveBill(message *tgbotapi.Message, args []string) {
	typed, section, ok := b.twoParts(message, args, "move &lt;bill&gt; | &lt;section&gt;")
	if !ok {
		return
	}
	bill, ok := b.resolveBill(message.Chat.ID, typed, listed)
	if !ok {
		return
	}

	change, err := b.tracker.MoveBill(actorOf(message), bill.ID, section)
	if err != nil {
		b.refuse(message.Chat.ID, err)
		return
	}
	b.announce(message.Chat.ID, "<b>"+html.EscapeString(change.After.DisplayName())+
		"</b> is now listed under <b>"+html.EscapeString(change.Section)+"</b>, last.", change)
}

// rechannelBill changes who pays a Bill and out of which account, which moves it between
// the Board's transfer lines from this month on.
func (b *Bot) rechannelBill(message *tgbotapi.Message, args []string) {
	parts, ok := splitParts(strings.Join(args, " "), 2, 3)
	if !ok || parts[0] == "" {
		b.usage(message.Chat.ID, "channel &lt;bill&gt; | &lt;channel&gt; [| &lt;card&gt;]")
		return
	}
	channel, err := domain.ParseChannel(parts[1])
	if err != nil {
		b.sendHTML(message.Chat.ID, "⚠️ "+html.EscapeString(capitalise(err.Error()))+".")
		return
	}
	bill, ok := b.resolveBill(message.Chat.ID, parts[0], listed)
	if !ok {
		return
	}
	var card string
	if len(parts) == 3 {
		card = parts[2]
	}

	change, err := b.tracker.SetBillChannel(actorOf(message), bill.ID, channel, card)
	if err != nil {
		b.refuse(message.Chat.ID, err)
		return
	}
	b.announce(message.Chat.ID, "<b>"+html.EscapeString(change.After.DisplayName())+
		"</b> is now paid "+html.EscapeString(change.After.ChannelLabel())+
		". Months already closed keep <i>"+html.EscapeString(change.Before.ChannelLabel())+
		"</i>, which is what actually paid them.", change)
}

// recardBill records the four digits printed on a card, which is what tells two cards from
// the same bank apart when one of them is queried.
func (b *Bot) recardBill(message *tgbotapi.Message, args []string) {
	typed, last4, ok := b.twoParts(message, args, "card &lt;bill&gt; | &lt;last 4 digits&gt;")
	if !ok {
		return
	}
	bill, ok := b.resolveBill(message.Chat.ID, typed, listed)
	if !ok {
		return
	}

	change, err := b.tracker.SetCardLast4(actorOf(message), bill.ID, last4)
	if err != nil {
		b.refuse(message.Chat.ID, err)
		return
	}
	if change.After.CardLast4 == "" {
		b.announce(message.Chat.ID, "<b>"+html.EscapeString(change.After.Name)+
			"</b> no longer carries any digits.", change)
		return
	}
	b.announce(message.Chat.ID, "<b>"+html.EscapeString(change.After.DisplayName())+
		"</b>, in every month.", change)
}

// redueBill records the day of the month a Bill falls due, which is what the 15th's
// reminder sorts by and marks overdue.
func (b *Bot) redueBill(message *tgbotapi.Message, args []string) {
	typed, typedDay, ok := b.twoParts(message, args, "due &lt;bill&gt; | &lt;day of the month&gt;")
	if !ok {
		return
	}
	day, err := strconv.Atoi(strings.TrimSpace(typedDay))
	if err != nil {
		b.refuse(message.Chat.ID, domain.ErrDueDayInvalid)
		return
	}
	bill, ok := b.resolveBill(message.Chat.ID, typed, listed)
	if !ok {
		return
	}

	change, err := b.tracker.SetDueDay(actorOf(message), bill.ID, day)
	if err != nil {
		b.refuse(message.Chat.ID, err)
		return
	}
	name := "<b>" + html.EscapeString(change.After.DisplayName()) + "</b>"
	if change.After.DueDay == 0 {
		b.announce(message.Chat.ID, name+" has no fixed day any more.", change)
		return
	}
	b.announce(message.Chat.ID, name+" is "+change.After.DueLabel()+
		" from now on, and on the last day of a month too short for it.", change)
}

// reorder moves a Bill within its Section, or a Section within the Board.
func (b *Bot) reorder(message *tgbotapi.Message, args []string) {
	if len(args) > 0 && strings.EqualFold(args[0], "section") {
		b.reorderSection(message, args[1:])
		return
	}

	typed, where, ok := b.twoParts(message, args, "order &lt;bill&gt; | up|down|top|bottom|&lt;n&gt;")
	if !ok {
		return
	}
	to, err := domain.ParsePlacement(where)
	if err != nil {
		b.refuse(message.Chat.ID, err)
		return
	}
	bill, ok := b.resolveBill(message.Chat.ID, typed, listed)
	if !ok {
		return
	}

	change, err := b.tracker.ReorderBill(actorOf(message), bill.ID, to)
	if err != nil {
		b.refuse(message.Chat.ID, err)
		return
	}
	b.announce(message.Chat.ID, "<b>"+html.EscapeString(change.After.DisplayName())+
		"</b> is now "+ordinal(change.After.DisplayOrder)+" under <b>"+
		html.EscapeString(change.Section)+"</b>.", change)
}

// reorderSection moves a heading in the Board's running order, which every month follows.
func (b *Bot) reorderSection(message *tgbotapi.Message, args []string) {
	name, where, ok := b.twoParts(message, args, "order section &lt;name&gt; | up|down|&lt;n&gt;")
	if !ok {
		return
	}
	to, err := domain.ParsePlacement(where)
	if err != nil {
		b.refuse(message.Chat.ID, err)
		return
	}

	change, err := b.tracker.ReorderSection(actorOf(message), name, to)
	if err != nil {
		b.refuse(message.Chat.ID, err)
		return
	}

	headings := make([]string, 0, len(change.Sections))
	for _, section := range change.Sections {
		headings = append(headings, html.EscapeString(section.Name))
	}
	b.sendHTML(message.Chat.ID, "<b>"+html.EscapeString(change.Section.Name)+"</b> is now "+
		ordinal(change.Section.DisplayOrder)+" on the board.\n\n<i>"+
		strings.Join(headings, " · ")+"</i>")
	for _, snap := range change.Snaps {
		b.refreshBoard(snap)
	}
}

// askArchiveBill asks before taking a Bill off the list. Archiving is reversible, but it
// withdraws lines from the open Boards and can settle a month outright, and the question
// is also what shows which Bill a loose name matched before anything happens to it.
func (b *Bot) askArchiveBill(message *tgbotapi.Message, args []string) {
	typed := strings.TrimSpace(strings.Join(args, " "))
	if typed == "" {
		b.usage(message.Chat.ID, "archive &lt;bill&gt;")
		return
	}
	bill, ok := b.resolveBill(message.Chat.ID, typed, listed)
	if !ok {
		return
	}

	msg := tgbotapi.NewMessage(message.Chat.ID, "🗄 Archive <b>"+
		html.EscapeString(bill.DisplayName())+"</b>?\n\n"+b.archiveCost(bill)+
		"\n\nMonths it was paid in keep it, and <code>/history</code> still shows them.")
	msg.ParseMode = tgbotapi.ModeHTML
	msg.ReplyToMessageID = message.MessageID
	msg.ReplyMarkup = *markupOf([][]board.Button{{
		{Text: "🗄 Archive", Data: board.Callback{
			Kind: board.KindBill, ID: bill.ID, Arg: string(board.BillArchive)}.Encode()},
		{Text: "✖ Keep it", Data: board.Callback{Kind: board.KindCancel}.Encode()},
	}})
	if _, err := b.sender.Send(msg); err != nil {
		log.Printf("failed to ask about archiving bill %d: %v", bill.ID, err)
	}
}

// archiveCost says what archiving would take off the open Boards, naming any figure that
// would go with it, since that is the part nobody can get back.
func (b *Bot) archiveCost(bill domain.Bill) string {
	snaps, err := b.tracker.OpenSnapshots()
	if err != nil {
		log.Printf("failed to read the open cycles to price an archive: %v", err)
		return "It leaves the standing list, and its unpaid lines leave the open months."
	}

	var going []string
	for _, snap := range snaps {
		for _, p := range snap.Payables {
			if p.BillID != bill.ID || p.Paid() {
				continue
			}
			line := "<b>" + snap.Cycle.Month.Title() + "</b>"
			if p.AmountKnown() {
				line += " (" + domain.FormatPesos(*p.AmountCents) + " entered, kept for a restore)"
			}
			going = append(going, line)
		}
	}
	if len(going) == 0 {
		return "It leaves the standing list. No open month has an unpaid line for it, " +
			"so no board changes."
	}
	return "It leaves the standing list, and its unpaid line goes from " +
		strings.Join(going, " and ") + "."
}

// confirmArchive takes a Bill off the list once someone has said yes.
func (b *Bot) confirmArchive(query *tgbotapi.CallbackQuery, billID int64) {
	change, err := b.tracker.ArchiveBill(actorOfUser(query.From), billID)
	if err != nil {
		b.settleQuestion(query, refusalText(err, "Nothing was archived."))
		b.answer(query.ID, "")
		return
	}

	name := "<b>" + html.EscapeString(change.After.DisplayName()) + "</b>"
	text := "🗄 " + name + " is off the standing list. — " +
		html.EscapeString(query.From.FirstName)
	if change.Lines < 0 {
		text += "\n<i>" + plural(-change.Lines, "board line") + " withdrawn. <code>/bills restore " +
			html.EscapeString(change.After.Name) + "</code> puts it back, figures and all.</i>"
	}
	b.settleQuestion(query, text)
	b.showBillChange(change)
	b.answer(query.ID, "🗄 "+change.After.Name+" is archived.")
}

// restoreBill puts an archived Bill back, with a blank line in every open month.
func (b *Bot) restoreBill(message *tgbotapi.Message, args []string) {
	typed := strings.TrimSpace(strings.Join(args, " "))
	if typed == "" {
		b.usage(message.Chat.ID, "restore &lt;bill&gt;")
		return
	}
	bill, ok := b.resolveBill(message.Chat.ID, typed, archived)
	if !ok {
		return
	}

	change, err := b.tracker.RestoreBill(actorOf(message), bill.ID)
	if err != nil {
		b.refuse(message.Chat.ID, err)
		return
	}
	text := "<b>" + html.EscapeString(change.After.DisplayName()) +
		"</b> is back on the standing list, under <b>" + html.EscapeString(change.Section) + "</b>."
	if change.Lines > 0 {
		text += " " + capitalise(plural(change.Lines, "board line")) + " back."
	}
	b.announce(message.Chat.ID, text, change)
}

// sendArchivedList prints what has left the standing list, which is where a restore starts.
func (b *Bot) sendArchivedList(chatID int64) {
	bills, err := b.tracker.AllBills()
	if err != nil {
		log.Printf("failed to read the bills for /bills archived: %v", err)
		b.send(chatID, "I could not read the bill list just now. Try again in a moment.")
		return
	}

	var out strings.Builder
	out.WriteString("🗄 <b>Archived bills</b>\n")
	gone := 0
	for _, bill := range bills {
		if !bill.Archived {
			continue
		}
		gone++
		out.WriteString(billLine(bill) + "\n")
	}
	if gone == 0 {
		b.sendHTML(chatID, "🗄 <b>Archived bills</b>\n\n<i>Nothing has left the standing list.</i>")
		return
	}
	b.sendHTML(chatID, out.String()+"\n<i>"+plural(gone, "bill")+
		" · <code>/bills restore &lt;bill&gt;</code> puts one back</i>")
}

// announce confirms a change to the standing list, brings the open Boards up to date and
// says so when the change was the thing that finished a month.
func (b *Bot) announce(chatID int64, text string, change tracker.BillChange) {
	b.sendHTML(chatID, text)
	b.showBillChange(change)
}

// showBillChange redraws the Boards the change reached and marks any month it settled or
// put back to owing. A closed month's Board is unpinned, so the pin is left for the month
// still being worked on; a reopened one takes the pin back.
func (b *Bot) showBillChange(change tracker.BillChange) {
	for _, snap := range change.Snaps {
		b.refreshBoard(snap)
	}
	for _, snap := range change.Closed {
		if snap.Cycle.HasBoard() {
			unpin := tgbotapi.UnpinChatMessageConfig{
				ChatID: snap.Cycle.BoardChatID, MessageID: snap.Cycle.BoardMessageID}
			if _, err := b.sender.Request(unpin); err != nil {
				log.Printf("could not unpin the board of closed cycle %d: %v", snap.Cycle.ID, err)
			}
		}
		b.sendHTML(b.cfg.GroupChatID, "✅ <b>"+snap.Cycle.Month.Title()+
			"</b> has nothing left to pay, so I closed it.")
	}
	for _, snap := range change.Reopened {
		if snap.Cycle.HasBoard() {
			b.pinBoard(snap.Cycle.ID, snap.Cycle.BoardChatID, snap.Cycle.BoardMessageID)
		}
		b.sendHTML(b.cfg.GroupChatID, "📋 <b>"+snap.Cycle.Month.Title()+
			"</b> is open again: it has something to pay once more.")
	}
}

// billScope is which Bills a typed name may reach.
type billScope int

const (
	// listed is the standing list: what the household pays now.
	listed billScope = iota
	// archived is only what has left it, which is what a restore names.
	archived
	// everything covers both, for a report about a month already gone.
	everything
)

// resolveBill finds the one Bill a typed name means, and says why it could not when it
// could not. A tie is answered with the names to choose from rather than with buttons:
// typing one more word is quicker than a round trip through a keyboard.
func (b *Bot) resolveBill(chatID int64, typed string, scope billScope) (domain.Bill, bool) {
	bills, err := b.tracker.AllBills()
	if err != nil {
		log.Printf("failed to read the bills to match %q: %v", typed, err)
		b.send(chatID, "I could not read the bill list just now. Try again in a moment.")
		return domain.Bill{}, false
	}

	matches, byID := matchBills(typed, bills, scope)
	switch len(matches) {
	case 1:
		return byID[matches[0].ID], true
	case 0:
		b.sendHTML(chatID, noBillText(typed, bills, scope))
		return domain.Bill{}, false
	}

	var names []string
	for _, match := range matches {
		names = append(names, "<b>"+html.EscapeString(byID[match.ID].DisplayName())+"</b>")
	}
	b.sendHTML(chatID, "“"+html.EscapeString(typed)+"” could be "+
		strings.Join(names, " or ")+". Which one?")
	return domain.Bill{}, false
}

// matchBills scores a typed name against the Bills in scope, and hands back the Bills the
// matches stand for.
func matchBills(typed string, bills []domain.Bill, scope billScope) ([]parse.Candidate, map[int64]domain.Bill) {
	candidates := make([]parse.Candidate, 0, len(bills))
	byID := make(map[int64]domain.Bill, len(bills))
	for _, bill := range bills {
		if scope == listed && bill.Archived || scope == archived && !bill.Archived {
			continue
		}
		candidates = append(candidates, parse.Candidate{
			ID: bill.ID, Name: bill.Name, Aliases: bill.Aliases, Last4: bill.CardLast4})
		byID[bill.ID] = bill
	}
	return parse.MatchBill(typed, candidates), byID
}

// noBillText says nothing matched, and points at the archive when the name is on it: a
// bill that has left the list is the commonest reason a name that used to work stops.
func noBillText(typed string, bills []domain.Bill, scope billScope) string {
	quoted := "“" + html.EscapeString(typed) + "”"
	if scope == listed {
		if gone, byID := matchBills(typed, bills, archived); len(gone) == 1 {
			name := html.EscapeString(byID[gone[0].ID].Name)
			return "<b>" + name + "</b> has been archived. <code>/bills restore " + name +
				"</code> puts it back on the list."
		}
	}
	if scope == archived {
		return "No archived bill matches " + quoted +
			". <code>/bills archived</code> lists what has left the list."
	}
	return "No bill matches " + quoted + ". <code>/bills</code> lists them."
}

// twoParts reads a subcommand written as "<bill> | <something>", and shows how it is
// written when it is not. The pipe keeps names that contain spaces whole.
func (b *Bot) twoParts(message *tgbotapi.Message, args []string, usage string) (string, string, bool) {
	parts, ok := splitParts(strings.Join(args, " "), 2)
	if !ok || parts[0] == "" {
		b.usage(message.Chat.ID, usage)
		return "", "", false
	}
	return parts[0], parts[1], true
}

// splitParts cuts a subcommand's argument on the pipe into one of the shapes it may take.
func splitParts(raw string, want ...int) ([]string, bool) {
	parts := strings.Split(raw, "|")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	for _, n := range want {
		if len(parts) == n {
			return parts, true
		}
	}
	return nil, false
}

// usage shows how one subcommand is written, rather than the whole of /bills help.
func (b *Bot) usage(chatID int64, line string) {
	b.sendHTML(chatID, "⚠️ <code>/bills "+line+"</code>\n\n<i>/bills help shows the rest.</i>")
}

// ordinal writes a position the way a person reads it off a list.
func ordinal(n int) string {
	suffix := "th"
	switch {
	case n%100 >= 11 && n%100 <= 13:
	case n%10 == 1:
		suffix = "st"
	case n%10 == 2:
		suffix = "nd"
	case n%10 == 3:
		suffix = "rd"
	}
	return strconv.Itoa(n) + suffix
}
