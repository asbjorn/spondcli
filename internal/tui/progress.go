package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

const progressWidth = 24

var (
	barFillSty  = lipgloss.NewStyle().Foreground(accent)
	barEmptySty = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#D0D0D0", Dark: "#3A3A3A"})
)

// progressBar renders a fixed-width bar. A fraction in [0, 1] fills it and
// shows a percentage; a negative fraction means the total is unknown, so a
// block sweeps back and forth, advanced by tick.
func progressBar(frac float64, tick int) string {
	cells := make([]bool, progressWidth)
	suffix := ""
	if frac >= 0 {
		frac = min(frac, 1)
		filled := int(frac * progressWidth)
		for i := range filled {
			cells[i] = true
		}
		suffix = fmt.Sprintf(" %3.0f%%", frac*100)
	} else {
		const block = 6
		span := progressWidth - block
		pos := tick % (2 * span)
		if pos > span {
			pos = 2*span - pos
		}
		for i := pos; i < pos+block; i++ {
			cells[i] = true
		}
	}

	var b strings.Builder
	for _, on := range cells {
		if on {
			b.WriteString(barFillSty.Render("█"))
		} else {
			b.WriteString(barEmptySty.Render("░"))
		}
	}
	return b.String() + dimSty.Render(suffix)
}
