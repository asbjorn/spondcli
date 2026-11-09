package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/asbjorn/spondcli/internal/roster"
)

// memberList is the members table. It keeps its own scroll offset so a mouse
// click can be mapped to a row exactly.
type memberList struct {
	rows   []roster.Row
	cursor int
	offset int // first visible row
	width  int
	height int // total lines, header included
}

// memberListHeader is the number of lines above the first row.
const memberListHeader = 2

var (
	listHeaderSty   = lipgloss.NewStyle().Bold(true).Foreground(accent)
	listSelectedSty = lipgloss.NewStyle().Foreground(lipgloss.Color("#FFFFFF")).Background(accent)
)

func (l *memberList) setRows(rows []roster.Row) {
	l.rows = rows
	l.cursor = min(l.cursor, max(len(rows)-1, 0))
	l.ensureVisible()
}

func (l *memberList) setSize(width, height int) {
	l.width, l.height = width, height
	l.ensureVisible()
}

func (l *memberList) visibleRows() int {
	return max(l.height-memberListHeader, 1)
}

func (l *memberList) move(n int) {
	if len(l.rows) == 0 {
		return
	}
	l.cursor = min(max(l.cursor+n, 0), len(l.rows)-1)
	l.ensureVisible()
}

func (l *memberList) ensureVisible() {
	visible := l.visibleRows()
	switch {
	case l.cursor < l.offset:
		l.offset = l.cursor
	case l.cursor >= l.offset+visible:
		l.offset = l.cursor - visible + 1
	}
	l.offset = min(max(l.offset, 0), max(len(l.rows)-visible, 0))
}

// rowAt maps a line within the list (0 = header) to a row index.
func (l memberList) rowAt(line int) (int, bool) {
	i := l.offset + line - memberListHeader
	if line < memberListHeader || i >= len(l.rows) || i >= l.offset+l.visibleRows() {
		return 0, false
	}
	return i, true
}

func (l memberList) selected() (roster.Row, bool) {
	if len(l.rows) == 0 {
		return roster.Row{}, false
	}
	return l.rows[l.cursor], true
}

type column struct {
	title string
	frac  float64
	cell  func(roster.Row) string
}

var memberColumnsSpec = []column{
	{"Name", 0.18, func(r roster.Row) string { return r.Member.Name() }},
	{"Type", 0.14, func(r roster.Row) string { return memberKind(r.Member) }},
	{"Email", 0.18, func(r roster.Row) string { return r.Member.Email }},
	{"Phone", 0.11, func(r roster.Row) string { return r.Member.PhoneNumber }},
	{"Subgroups", 0.15, func(r roster.Row) string { return r.Subgroups }},
	{"Guardians", 0.24, func(r roster.Row) string { return r.Guardians }},
}

func (l memberList) columnWidths() []int {
	usable := max(l.width-len(memberColumnsSpec)-1, len(memberColumnsSpec)*6)
	widths := make([]int, len(memberColumnsSpec))
	for i, c := range memberColumnsSpec {
		widths[i] = max(int(float64(usable)*c.frac), 6)
	}
	return widths
}

func (l memberList) renderLine(cells []string, widths []int) string {
	parts := make([]string, len(cells))
	for i, c := range cells {
		parts[i] = fit(strings.ReplaceAll(c, "\n", " "), widths[i])
	}
	return " " + strings.Join(parts, " ")
}

// fit truncates or pads s to exactly w terminal cells, on one line.
func fit(s string, w int) string {
	s = ansi.Truncate(s, w, "…")
	if n := ansi.StringWidth(s); n < w {
		s += strings.Repeat(" ", w-n)
	}
	return s
}

func (l memberList) view() string {
	widths := l.columnWidths()
	titles := make([]string, len(memberColumnsSpec))
	for i, c := range memberColumnsSpec {
		titles[i] = c.title
	}
	lines := []string{
		listHeaderSty.Render(fit(l.renderLine(titles, widths), l.width)),
		dimSty.Render(strings.Repeat("─", max(l.width, 1))),
	}
	end := min(l.offset+l.visibleRows(), len(l.rows))
	for i := l.offset; i < end; i++ {
		cells := make([]string, len(memberColumnsSpec))
		for j, c := range memberColumnsSpec {
			cells[j] = c.cell(l.rows[i])
		}
		line := fit(l.renderLine(cells, widths), l.width)
		if i == l.cursor {
			line = listSelectedSty.Render(line)
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}
