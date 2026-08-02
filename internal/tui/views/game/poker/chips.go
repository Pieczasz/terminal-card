package poker

import (
	"fmt"
	"strings"

	lg "charm.land/lipgloss/v2"
)

// chipGlyph is a single casino chip. Colour carries the denomination, so the
// raise rack doubles as the legend for the stacks drawn beside each seat.
const chipGlyph = "●"

// chipDenoms are the chips a player can push forward, largest first. Values map
// onto the 25/50 blinds so a raise can always be built out of whole chips.
var chipDenoms = []struct {
	Value uint
	Style lg.Style
}{
	{100, lg.NewStyle().Foreground(lg.Color("#EDEDED"))},
	{50, lg.NewStyle().Foreground(lg.Color("#5B8DEF"))},
	{25, lg.NewStyle().Foreground(lg.Color("#6FBF73"))},
	{10, lg.NewStyle().Foreground(lg.Color("#CC4444"))},
}

// renderChipStack draws amount as coloured chips, largest denomination first.
// Change below the smallest chip is left out: the exact number is always printed
// next to the stack anyway.
func renderChipStack(amount uint) string {
	parts := make([]string, 0, len(chipDenoms))
	for _, d := range chipDenoms {
		count := amount / d.Value
		if count == 0 {
			continue
		}
		amount %= d.Value
		parts = append(parts, d.Style.Render(fmt.Sprintf("%s%d", chipGlyph, count)))
	}
	return strings.Join(parts, " ")
}

// renderChipRack is the raise prompt's keypad: which key pushes which chip.
func renderChipRack() string {
	parts := make([]string, 0, len(chipDenoms))
	for i, d := range chipDenoms {
		parts = append(parts, fmt.Sprintf("%s %s",
			d.Style.Render(fmt.Sprintf("%s%d", chipGlyph, d.Value)),
			dimStyle.Render(fmt.Sprintf("[%d]", i+1)),
		))
	}
	return strings.Join(parts, "  ")
}

// chipForKey maps a rack key to its denomination. Keys run largest to smallest,
// matching the order the chips are drawn in.
func chipForKey(key string) (uint, bool) {
	for i, d := range chipDenoms {
		if key == fmt.Sprint(i+1) {
			return d.Value, true
		}
	}
	return 0, false
}

// smallestChip is the step for nudging a raise up or down by hand.
func smallestChip() uint {
	return chipDenoms[len(chipDenoms)-1].Value
}
