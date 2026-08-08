package styles

import (
	"math"
	"strings"

	lg "charm.land/lipgloss/v2"
)

// PadCenter centres every line of s within width columns, padding with spaces.
func PadCenter(width int, s string) string {
	return PadHorizontal(width, lg.Center, s)
}

// PadHorizontal is lipgloss.PlaceHorizontal for the three positions we use.
func PadHorizontal(width int, pos lg.Position, s string) string {
	lines := strings.Split(s, "\n")

	contentWidth := 0
	for _, line := range lines {
		if w := lg.Width(line); w > contentWidth {
			contentWidth = w
		}
	}
	gap := width - contentWidth
	if gap <= 0 {
		return s
	}

	var b strings.Builder
	b.Grow(len(s) + len(lines)*(gap+1))

	for i, line := range lines {
		if i > 0 {
			b.WriteByte('\n')
		}
		total := gap + max(0, contentWidth-lg.Width(line))

		// Left and Right are not the centre formula with the position plugged in:
		// lipgloss special-cases both, and in the opposite direction to what that
		// formula gives. Following the formula instead mirrors the layout.
		var left int
		switch pos {
		case lg.Left:
			left = 0
		case lg.Right:
			left = total
		default:
			left = total - int(math.Round(float64(total)*float64(pos)))
		}

		b.WriteString(spaces(left))
		b.WriteString(line)
		b.WriteString(spaces(total - left))
	}
	return b.String()
}

const padCacheWidth = 512

var padCache = strings.Repeat(" ", padCacheWidth)

func spaces(n int) string {
	if n <= 0 {
		return ""
	}
	if n <= padCacheWidth {
		return padCache[:n]
	}
	return strings.Repeat(" ", n)
}

// PadVertical is lipgloss.PlaceVertical without the styled-whitespace machinery.
func PadVertical(height int, pos lg.Position, s string) string {
	contentHeight := strings.Count(s, "\n") + 1
	gap := height - contentHeight
	if gap <= 0 {
		return s
	}

	width := lg.Width(s)
	empty := spaces(width)

	var b strings.Builder
	b.Grow(len(s) + gap*(width+1))

	switch pos {
	case lg.Top:
		b.WriteString(s)
		for range gap {
			b.WriteByte('\n')
			b.WriteString(empty)
		}
	case lg.Bottom:
		for range gap {
			b.WriteString(empty)
			b.WriteByte('\n')
		}
		b.WriteString(s)
	default:
		split := int(math.Round(float64(gap) * float64(pos)))
		top := gap - split
		for range top {
			b.WriteString(empty)
			b.WriteByte('\n')
		}
		b.WriteString(s)
		for range gap - top {
			b.WriteByte('\n')
			b.WriteString(empty)
		}
	}
	return b.String()
}

// Place is lipgloss.Place without the styled-whitespace machinery.
func Place(width, height int, hPos, vPos lg.Position, s string) string {
	return PadVertical(height, vPos, PadHorizontal(width, hPos, s))
}
