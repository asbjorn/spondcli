package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// checklist is a small multi-select used for subgroups and admin roles.
type checklist struct {
	title  string
	items  []checkItem
	cursor int
}

type checkItem struct {
	id, label string
	on        bool
}

func newChecklist(title string, items []checkItem) checklist {
	return checklist{title: title, items: items}
}

func (c checklist) selected() []string {
	ids := []string{}
	for _, it := range c.items {
		if it.on {
			ids = append(ids, it.id)
		}
	}
	return ids
}

// update handles navigation and toggling; it reports whether the key was used.
func (c *checklist) update(key tea.KeyMsg) bool {
	switch key.String() {
	case "up", "k":
		c.cursor = max(c.cursor-1, 0)
	case "down", "j", "tab":
		c.cursor = min(c.cursor+1, len(c.items)-1)
	case " ", "x":
		if len(c.items) > 0 {
			c.items[c.cursor].on = !c.items[c.cursor].on
		}
	default:
		return false
	}
	return true
}

func (c checklist) view() string {
	lines := []string{sectionSty.Render(c.title)}
	for i, it := range c.items {
		box := "[ ]"
		if it.on {
			box = acceptedSty.Render("[x]")
		}
		marker := "  "
		if i == c.cursor {
			marker = keySty.Render("› ")
		}
		lines = append(lines, marker+box+" "+it.label)
	}
	return strings.Join(lines, "\n")
}
