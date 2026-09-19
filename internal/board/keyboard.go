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
)

// ErrCallbackInvalid is returned for callback data the bot did not write, or wrote under
// an older layout.
var ErrCallbackInvalid = errors.New("unrecognised button")

// Callback is what a button carries back when tapped.
type Callback struct {
	Kind Kind
	ID   int64
}

// Encode writes the callback as button data, e.g. "b:42".
func (c Callback) Encode() string {
	return string(c.Kind) + ":" + strconv.FormatInt(c.ID, 10)
}

// DecodeCallback reads button data written by Encode.
func DecodeCallback(data string) (Callback, error) {
	kind, rawID, ok := strings.Cut(data, ":")
	if !ok {
		return Callback{}, ErrCallbackInvalid
	}
	switch Kind(kind) {
	case KindMenu, KindRefresh, KindTransfer:
	default:
		return Callback{}, ErrCallbackInvalid
	}
	id, err := strconv.ParseInt(rawID, 10, 64)
	if err != nil || id <= 0 {
		return Callback{}, ErrCallbackInvalid
	}
	return Callback{Kind: Kind(kind), ID: id}, nil
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

// clip shortens a name to max runes, marking the cut.
func clip(name string, max int) string {
	runes := []rune(name)
	if len(runes) <= max {
		return name
	}
	return string(runes[:max-1]) + "…"
}
