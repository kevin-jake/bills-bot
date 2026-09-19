package board

import (
	"errors"
	"strconv"
	"strings"

	"github.com/kevin-jake/bills-bot/internal/domain"
)

// Kind is what a button asks for. The values are one or two letters because Telegram allows
// a button 64 bytes of callback data and the plan keeps well under 20.
type Kind string

const (
	// KindMenu opens one Payable's menu. Its id is the Payable's.
	KindMenu Kind = "b"
	// KindRefresh redraws a Cycle's Board. Its id is the Cycle's.
	KindRefresh Kind = "r"
	// KindTransfer picks which of Sheena's accounts a Transfer went into. Its id is the Cycle's.
	KindTransfer Kind = "t"
	// KindSetAmount asks for a Payable's amount. Its id is the Payable's.
	KindSetAmount Kind = "a"
	// KindBack closes a Payable's menu and puts the Board's buttons back. Its id is the Cycle's.
	KindBack Kind = "k"
	// KindMarkPaid marks a Payable paid. Its id is the Payable's.
	KindMarkPaid Kind = "p"
	// KindUndo takes back the last thing done to a Payable. Its id is the Payable's.
	KindUndo Kind = "u"
	// KindTransferChannel asks what was sent into one of Sheena's accounts. Its id is the
	// Cycle's and its Arg names the account, as ChannelCode writes it.
	KindTransferChannel Kind = "tc"
	// KindTransferUndo removes a recorded Transfer. Its id is the Transfer's.
	KindTransferUndo Kind = "tu"
)

// ErrCallbackInvalid is returned for callback data the bot did not write, or wrote under
// an older layout.
var ErrCallbackInvalid = errors.New("unrecognised button")

// Callback is what a button carries back when tapped.
type Callback struct {
	Kind Kind
	ID   int64
	// Arg is set only for KindTransferChannel, which also has to say which account.
	Arg string
}

// Encode writes the callback as button data, e.g. "b:42" or "tc:7:bdo".
func (c Callback) Encode() string {
	data := string(c.Kind) + ":" + strconv.FormatInt(c.ID, 10)
	if c.Arg != "" {
		data += ":" + c.Arg
	}
	return data
}

// DecodeCallback reads button data written by Encode.
func DecodeCallback(data string) (Callback, error) {
	parts := strings.Split(data, ":")
	if len(parts) < 2 {
		return Callback{}, ErrCallbackInvalid
	}
	kind := Kind(parts[0])
	switch kind {
	case KindMenu, KindRefresh, KindTransfer, KindSetAmount, KindBack, KindMarkPaid, KindUndo,
		KindTransferUndo:
		if len(parts) != 2 {
			return Callback{}, ErrCallbackInvalid
		}
	case KindTransferChannel:
		if len(parts) != 3 {
			return Callback{}, ErrCallbackInvalid
		}
		if _, ok := ChannelFromCode(parts[2]); !ok {
			return Callback{}, ErrCallbackInvalid
		}
	default:
		return Callback{}, ErrCallbackInvalid
	}
	id, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil || id <= 0 {
		return Callback{}, ErrCallbackInvalid
	}
	callback := Callback{Kind: kind, ID: id}
	if len(parts) == 3 {
		callback.Arg = parts[2]
	}
	return callback, nil
}

// channelCodes are the short names Sheena's accounts go by in callback data.
var channelCodes = map[domain.Channel]string{
	domain.SheenaBDO:    "bdo",
	domain.SheenaBPI:    "bpi",
	domain.SheenaPSBank: "psb",
}

// ChannelCode is how a callback names one of Sheena's accounts.
func ChannelCode(c domain.Channel) string { return channelCodes[c] }

// ChannelFromCode reads a name written by ChannelCode.
func ChannelFromCode(code string) (domain.Channel, bool) {
	for channel, known := range channelCodes {
		if known == code {
			return channel, true
		}
	}
	return "", false
}

// Button is one inline button, independent of the Telegram library's type for it.
type Button struct {
	Text string
	Data string
}

// maxButtonName keeps two buttons side by side legible on a phone.
const maxButtonName = 28

// Keyboard returns the Board's buttons in rows: one per bill, two to a row, in Board order,
// then Refresh and Transfer. A closed Cycle has nothing left to tap, so it gets none.
func Keyboard(snap domain.Snapshot) [][]Button {
	if snap.Cycle.Closed() {
		return nil
	}

	var rows [][]Button
	var row []Button
	for _, group := range snap.SectionGroups() {
		for _, p := range group.Payables {
			row = append(row, Button{
				Text: Marker(p) + " " + clip(p.BillName, maxButtonName),
				Data: Callback{Kind: KindMenu, ID: p.ID}.Encode(),
			})
			if len(row) == 2 {
				rows = append(rows, row)
				row = nil
			}
		}
	}
	if len(row) > 0 {
		rows = append(rows, row)
	}

	return append(rows, []Button{
		{Text: "🔄 Refresh", Data: Callback{Kind: KindRefresh, ID: snap.Cycle.ID}.Encode()},
		{Text: "💸 Transfer", Data: Callback{Kind: KindTransfer, ID: snap.Cycle.ID}.Encode()},
	})
}

// Menu returns the buttons for one Payable, shown in place of the Board's own until someone
// picks an action or goes back. Mark paid is offered only when it would be accepted: once
// the amount is known, and while the Payable is unpaid.
func Menu(p domain.Payable) [][]Button {
	setAmount := "💰 Set amount"
	if p.AmountKnown() {
		setAmount = "💰 Change amount"
	}
	actions := []Button{{Text: setAmount, Data: Callback{Kind: KindSetAmount, ID: p.ID}.Encode()}}
	if p.AmountKnown() && !p.Paid() {
		actions = append(actions, Button{Text: "✓ Mark paid", Data: Callback{Kind: KindMarkPaid, ID: p.ID}.Encode()})
	}
	actions = append(actions, Button{Text: "↩ Undo", Data: Callback{Kind: KindUndo, ID: p.ID}.Encode()})
	return [][]Button{
		actions,
		{{Text: "« Back", Data: Callback{Kind: KindBack, ID: p.CycleID}.Encode()}},
	}
}

// TransferPicker returns one row per account of Sheena's that the Cycle uses: a button to
// record what was sent into it, and, once something has been, one to undo that.
func TransferPicker(snap domain.Snapshot) [][]Button {
	var rows [][]Button
	for _, line := range snap.TransferLines() {
		record := Button{
			Text: "💸 " + line.Channel.Label() + " · need " + domain.FormatPesos(line.Need.Cents),
			Data: Callback{Kind: KindTransferChannel, ID: snap.Cycle.ID, Arg: ChannelCode(line.Channel)}.Encode(),
		}
		if line.Tentative() {
			record.Text += "?"
		}
		if line.Sent == nil {
			rows = append(rows, []Button{record})
			continue
		}
		record.Text = "💸 " + line.Channel.Label() + " · sent " + domain.FormatPesos(line.Sent.SentCents)
		rows = append(rows, []Button{record, {
			Text: "↩ Undo",
			Data: Callback{Kind: KindTransferUndo, ID: line.Sent.ID}.Encode(),
		}})
	}
	return rows
}

// clip shortens a name to max runes, marking the cut.
func clip(name string, max int) string {
	runes := []rune(name)
	if len(runes) <= max {
		return name
	}
	return string(runes[:max-1]) + "…"
}
