package domain

import (
	"errors"
	"strconv"
	"strings"
)

// ErrPlacementInvalid is returned for a reorder nobody could act on.
var ErrPlacementInvalid = errors.New("say up, down, top, bottom, or a position such as 2")

// Placement is where a reorder puts something within the list it is already in. It is
// written the way it is spoken — "up", "bottom", "2" — because counting positions on a
// phone is work, and moving a line one place is usually all that is wanted.
type Placement struct {
	// Position is a 1-based place in the list, or 0 when the move is a step.
	Position int
	// Step is -1 for one place up and +1 for one place down, or 0 for a Position.
	Step int
}

// endOfList stands for "last", whatever the list turns out to be; Index clamps it.
const endOfList = 1 << 30

// ParsePlacement reads a placement as a person types it.
func ParsePlacement(s string) (Placement, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "up":
		return Placement{Step: -1}, nil
	case "down":
		return Placement{Step: 1}, nil
	case "top", "first":
		return Placement{Position: 1}, nil
	case "bottom", "last", "end":
		return Placement{Position: endOfList}, nil
	}
	position, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil || position < 1 {
		return Placement{}, ErrPlacementInvalid
	}
	return Placement{Position: position}, nil
}

// Index is the 0-based place something now at index at lands in a list of n, clamped to
// the list: a step off either end, or a position past it, stays where the list stops.
func (p Placement) Index(at, n int) int {
	if n == 0 {
		return 0
	}
	target := p.Position - 1
	if p.Step != 0 {
		target = at + p.Step
	}
	return min(max(target, 0), n-1)
}

// Reorder moves the item at from to index to, sliding everything between them along. It
// returns a new slice, so the caller can compare it with the order it had before.
func Reorder[T any](items []T, from, to int) []T {
	if from < 0 || from >= len(items) || to < 0 || to >= len(items) {
		return items
	}
	moved := items[from]
	rest := make([]T, 0, len(items))
	rest = append(rest, items[:from]...)
	rest = append(rest, items[from+1:]...)

	out := make([]T, 0, len(items))
	out = append(out, rest[:to]...)
	out = append(out, moved)
	return append(out, rest[to:]...)
}
