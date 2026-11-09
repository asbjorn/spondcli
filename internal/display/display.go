// Package display holds small text-formatting helpers shared by the
// command line and the interactive UI.
package display

import (
	"fmt"
	"strings"
	"time"
)

func Time(s string) string {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return s
	}
	return t.Local().Format("Mon 2 Jan 2006 15:04")
}

func FirstLine(s string, width int) string {
	s, _, _ = strings.Cut(s, "\n")
	if width > 1 && len([]rune(s)) > width {
		s = string([]rune(s)[:width-1]) + "…"
	}
	return s
}

func Plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}
