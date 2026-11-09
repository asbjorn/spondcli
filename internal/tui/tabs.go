package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/asbjorn/spondcli/internal/display"
	"github.com/asbjorn/spondcli/internal/spondapi"
)

// tab is one of the top-level sections, in the order the app shows them.
type tab int

const (
	tabHome tab = iota
	tabAlerts
	tabGroups
	tabMessages
)

var tabNames = [...]string{"Home", "Alerts", "Groups", "Messages"}

// tabBorder draws a tab whose bottom edge is the line under the tab bar;
// the active tab leaves it open so it joins the pane below.
func tabBorder(active bool) lipgloss.Border {
	b := lipgloss.RoundedBorder()
	if active {
		b.Bottom, b.BottomLeft, b.BottomRight = " ", "┘", "└"
	} else {
		b.BottomLeft, b.BottomRight = "┴", "┴"
	}
	return b
}

var (
	tabSty = lipgloss.NewStyle().Border(tabBorder(false)).
		BorderForeground(subtle).BorderBottomForeground(accent).
		Padding(0, 1).Foreground(subtle)
	activeTabSty = lipgloss.NewStyle().Border(tabBorder(true)).BorderForeground(accent).
			Padding(0, 1).Bold(true).Foreground(lipgloss.Color("#FFFFFF")).Background(accent)
	badgeSty     = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#FFFFFF")).Background(errColor).Padding(0, 1)
	tabLineSty   = lipgloss.NewStyle().Foreground(accent)
	paneTitleSty = lipgloss.NewStyle().Bold(true).Foreground(accent).Padding(0, 1)
)

// tabBarHeight is the tab bar's height: top border, labels, bottom line.
const tabBarHeight = 3

// tabLabel is "1 Home", with an unread count when there is one.
func (m model) tabLabel(t tab) string {
	label := fmt.Sprintf("%d %s", int(t)+1, tabNames[t])
	n := 0
	switch t {
	case tabAlerts:
		n = m.alerts.unread()
	case tabMessages:
		n = m.chat.unread()
	}
	if n > 0 {
		label += " " + badgeSty.Render(fmt.Sprint(n))
	}
	return label
}

// logo is the app name left of the tabs, standing on the tab bar's line.
func logo() string {
	name := " " + headerSty.Render("Spond") + " "
	w := lipgloss.Width(name)
	return lipgloss.JoinVertical(lipgloss.Left, strings.Repeat(" ", w), name, tabLineSty.Render(strings.Repeat("─", w)))
}

// tabBar draws the app name, the tabs and who is signed in, above a line
// that the active tab opens into.
func (m model) tabBar() string {
	parts := []string{logo()}
	for t := range tabNames {
		sty := tabSty
		if tab(t) == m.tab {
			sty = activeTabSty
		}
		parts = append(parts, sty.Render(m.tabLabel(tab(t))))
	}
	bar := lipgloss.JoinHorizontal(lipgloss.Bottom, parts...)
	gap := m.width - lipgloss.Width(bar)
	if gap <= 0 {
		return bar
	}
	who := ""
	if m.me != nil {
		who = strings.TrimSpace(m.me.FirstName+" "+m.me.LastName) + " "
	}
	who = dimSty.Width(gap).Align(lipgloss.Right).Render(display.FirstLine(who, gap))
	fill := lipgloss.JoinVertical(lipgloss.Left, strings.Repeat(" ", gap), who, tabLineSty.Render(strings.Repeat("─", gap)))
	return lipgloss.JoinHorizontal(lipgloss.Bottom, bar, fill)
}

// tabAt maps a column of the tab bar to the tab drawn there.
func (m model) tabAt(x int) (tab, bool) {
	pos := lipgloss.Width(logo())
	for t := range tabNames {
		w := lipgloss.Width(tabSty.Render(m.tabLabel(tab(t))))
		if x >= pos && x < pos+w {
			return tab(t), true
		}
		pos += w
	}
	return 0, false
}

// typing reports whether keys go to a text field or a confirmation on the
// current tab, so digits must not switch tabs.
func (m model) typing() bool {
	if d := m.feedOf(m.feedForTab()).detail; d != nil && d.declining {
		return true
	}
	if m.tab == tabMessages {
		return m.chat.composing || m.chat.picker != nil || m.chat.react != nil
	}
	if m.tab != tabGroups {
		return false
	}
	return m.searching || m.invite != nil ||
		m.groups.FilterState() == list.Filtering ||
		m.subgroups.FilterState() == list.Filtering ||
		(m.memberModal != nil && m.memberModal.mode != modeView) ||
		(m.requestModal != nil && m.requestModal.mode != requestView)
}

// feedForTab is the arrangement feed whose modal can be open on this tab.
func (m model) feedForTab() feedKind {
	switch m.tab {
	case tabHome:
		return feedHome
	case tabAlerts:
		return feedAlerts
	}
	return feedGroup
}

// switchTab handles the digits 1–4 and clicks on the tab bar.
func (m model) switchTab(msg tea.Msg) (tea.Model, tea.Cmd, bool) {
	target := m.tab
	switch msg := msg.(type) {
	case tea.KeyMsg:
		k := msg.String()
		if len(k) != 1 || k[0] < '1' || k[0] > '0'+byte(len(tabNames)) || m.typing() {
			return m, nil, false
		}
		target = tab(k[0] - '1')
	case tea.MouseMsg:
		if msg.Action != tea.MouseActionPress || msg.Button != tea.MouseButtonLeft || msg.Y >= tabBarHeight {
			return m, nil, false
		}
		t, ok := m.tabAt(msg.X)
		if !ok {
			return m, nil, true
		}
		target = t
	default:
		return m, nil, false
	}
	if target == m.tab {
		return m, nil, true
	}
	m.tab = target
	m.err, m.status = nil, ""
	m.resize()
	switch {
	case target == tabHome && !m.home.loaded && m.busy == "":
		return m, m.openEvents(feedHome, false), true
	case target == tabAlerts && !m.alerts.loaded && !m.alerts.loading:
		return m, m.loadAlerts(), true
	case target == tabMessages && !m.chat.loaded && !m.chat.loading:
		return m, m.loadChats(""), true
	}
	return m, nil, true
}

func (m model) updateHome(msg tea.Msg) (tea.Model, tea.Cmd) {
	mm, cmd, handled := m.updateEvents(feedHome, msg)
	if handled {
		return mm, cmd
	}
	m = mm.(model)
	switch msg.(tea.KeyMsg).String() {
	case "q":
		return m, tea.Quit
	case "r":
		if m.busy == "" {
			return m, m.openEvents(feedHome, m.home.past)
		}
	}
	return m, cmd
}

func (m model) viewHome() (crumb, body, help string) {
	when := "Upcoming"
	if m.home.past {
		when = "Past"
	}
	crumb = "All groups › " + when + " arrangements"
	body, help = m.feedView(feedHome, " in your groups")
	if m.home.detail == nil {
		help += " · r refresh · 1-4 tabs · q quit"
	}
	return crumb, body, help
}

// groupByID finds a group, or a group containing the subgroup, by id.
func (m model) groupByID(id string) (spondapi.Group, bool) {
	for _, g := range m.allGroups {
		if g.ID == id {
			return g, true
		}
		for _, sg := range g.SubGroups {
			if sg.ID == id {
				return g, true
			}
		}
	}
	return spondapi.Group{}, false
}

// profileNames maps every profile id known from the groups (members and
// guardians) and the signed-in user to a display name.
func profileNames(groups []spondapi.Group, me *spondapi.MyProfile) map[string]string {
	names := map[string]string{}
	for _, g := range groups {
		for _, mem := range g.Members {
			if mem.Profile != nil && mem.Name() != "" {
				names[mem.Profile.ID] = mem.Name()
			}
			for _, gd := range mem.Guardians {
				if gd.Profile != nil && gd.Name() != "" {
					names[gd.Profile.ID] = gd.Name()
				}
			}
		}
	}
	if me != nil {
		names[me.ID] = strings.TrimSpace(me.FirstName + " " + me.LastName)
	}
	return names
}
