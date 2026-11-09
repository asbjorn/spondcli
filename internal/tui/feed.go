package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/asbjorn/spondcli/internal/roster"
	"github.com/asbjorn/spondcli/internal/spondapi"
)

// feedKind names one of the arrangement feeds.
type feedKind int

const (
	feedGroup  feedKind = iota // the open group's Arrangements screen
	feedHome                   // the Home tab: arrangements from every group
	feedAlerts                 // an arrangement opened from Alerts; only its modal is used
)

// eventFeed is a paged list of arrangement cards with an optional detail
// modal for the selected one.
type eventFeed struct {
	gen     int // bumped when the feed reloads; the group feed uses model.gen instead
	events  []spondapi.Spond
	done    bool // the last page has arrived
	loaded  bool // the first page has arrived
	past    bool
	cursor  int   // selected arrangement
	offsets []int // first line of each card
	vp      viewport.Model
	detail  *detailModal // open arrangement, or nil
}

func newEventFeed() eventFeed { return eventFeed{vp: viewport.New(0, 0)} }

func (m *model) feedOf(k feedKind) *eventFeed {
	switch k {
	case feedHome:
		return &m.home
	case feedAlerts:
		return &m.alerts.feed
	}
	return &m.groupFeed
}

// feedGen is the generation results for feed k must carry to be applied.
func (m *model) feedGen(k feedKind) int {
	if k == feedGroup {
		return m.gen
	}
	return m.feedOf(k).gen
}

// spondGroup is the group an arrangement was sent in, falling back to the
// open group.
func (m model) spondGroup(s spondapi.Spond) spondapi.Group {
	if s.Recipients != nil && s.Recipients.Group != nil {
		for _, g := range m.allGroups {
			if g.ID == s.Recipients.Group.ID {
				return g
			}
		}
	}
	return m.group
}

func (m *model) loadEventsCmd(k feedKind) tea.Cmd {
	f := m.feedOf(k)
	var sc spondapi.Scope // every group
	if k == feedGroup {
		sc = m.scope()
	}
	ctx, c, past, gen := m.ctx, m.client, f.past, m.feedGen(k)
	var cursor *spondapi.Spond
	if len(f.events) > 0 {
		last := f.events[len(f.events)-1]
		cursor = &last
	}
	return func() tea.Msg {
		sponds, err := c.Sponds(ctx, sc, past, pageSize, cursor)
		return eventsMsg{kind: k, gen: gen, sponds: sponds, err: err}
	}
}

// openEvents (re)loads feed k from its first page.
func (m *model) openEvents(k feedKind, past bool) tea.Cmd {
	if k == feedGroup {
		m.gen++
		m.screen = screenEvents
	} else {
		m.feedOf(k).gen++
	}
	f := m.feedOf(k)
	f.detail, f.cursor = nil, 0
	f.events, f.done, f.loaded, f.past = nil, false, false, past
	f.vp.SetContent("")
	f.vp.GotoTop()
	label := "Loading upcoming arrangements…"
	if past {
		label = "Loading past arrangements…"
	}
	return m.startBusy(label, m.loadEventsCmd(k))
}

// applyEvents stores a page of feed k.
func (m model) applyEvents(msg eventsMsg) (tea.Model, tea.Cmd) {
	if msg.gen != m.feedGen(msg.kind) {
		return m, nil // stale: screen, group or tab changed while loading
	}
	m.busy = ""
	if msg.err != nil {
		m.err = fmt.Errorf("loading arrangements: %w", msg.err)
		return m, nil
	}
	f := m.feedOf(msg.kind)
	f.events = append(f.events, msg.sponds...)
	f.done = len(msg.sponds) < pageSize
	f.loaded = true
	m.refreshEvents(msg.kind)
	return m, nil
}

// updateEvents handles the keys and clicks every arrangement feed shares.
// handled is false for keys the screen itself should handle.
func (m model) updateEvents(k feedKind, msg tea.Msg) (_ tea.Model, _ tea.Cmd, handled bool) {
	f := m.feedOf(k)
	if f.detail != nil {
		mm, cmd := m.updateDetail(k, msg)
		return mm, cmd, true
	}

	switch msg := msg.(type) {
	case tea.MouseMsg:
		if msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft {
			if i, ok := cardAtLine(f.offsets, f.vp.TotalLineCount(), msg.Y-headerLines+f.vp.YOffset); ok {
				m.selectEvent(k, i)
				return m, m.openDetail(k), true
			}
			return m, nil, true
		}
	case tea.KeyMsg:
		switch key := msg.String(); key {
		case "up", "k":
			m.selectEvent(k, f.cursor-1)
			return m, nil, true
		case "down", "j":
			m.selectEvent(k, f.cursor+1)
			return m, m.maybeLoadMoreEvents(k), true
		case "home", "g":
			m.selectEvent(k, 0)
			return m, nil, true
		case "end", "G":
			m.selectEvent(k, len(f.events)-1)
			return m, m.maybeLoadMoreEvents(k), true
		case "enter", " ":
			return m, m.openDetail(k), true
		case "tab", "u", "h":
			past := !f.past
			switch key {
			case "u":
				past = false
			case "h":
				past = true
			}
			if past == f.past {
				return m, nil, true
			}
			return m, m.openEvents(k, past), true
		}
	}

	var cmd tea.Cmd
	f.vp, cmd = f.vp.Update(msg)
	if _, isKey := msg.(tea.KeyMsg); isKey {
		if f.vp.AtBottom() {
			cmd = tea.Batch(cmd, m.maybeLoadMoreEvents(k))
		}
		return m, cmd, false
	}
	return m, cmd, true
}

// headerLines is how many rows sit above the feed: tab bar and pane title.
const headerLines = tabBarHeight + 1

// maybeLoadMoreEvents fetches the next page once the selection or scroll
// position reaches the end of what is loaded.
func (m *model) maybeLoadMoreEvents(k feedKind) tea.Cmd {
	f := m.feedOf(k)
	nearEnd := f.cursor >= len(f.events)-1 || f.vp.AtBottom()
	if m.busy != "" || f.done || m.err != nil || !nearEnd {
		return nil
	}
	return m.startBusy("Loading more arrangements…", m.loadEventsCmd(k))
}

// selectEvent moves the selection, clamped, and scrolls it into view.
func (m *model) selectEvent(k feedKind, i int) {
	f := m.feedOf(k)
	if len(f.events) == 0 {
		return
	}
	f.cursor = min(max(i, 0), len(f.events)-1)
	m.refreshEvents(k)
	scrollIntoView(&f.vp, f.offsets, f.cursor)
}

// refreshEvents re-renders feed k and remembers card offsets.
func (m *model) refreshEvents(k feedKind) {
	content, offsets := m.renderEvents(k)
	f := m.feedOf(k)
	f.offsets = offsets
	f.vp.SetContent(content)
}

// maxDescriptionLines keeps long arrangement descriptions from flooding the feed.
const maxDescriptionLines = 6

func (m *model) renderEvents(k feedKind) (string, []int) {
	f := m.feedOf(k)
	if len(f.events) == 0 {
		return "", nil
	}

	cardWidth := max(m.width-2, 20)
	textWidth := max(cardWidth-4, 10) // border + padding
	cards := make([]string, 0, len(f.events)+1)
	offsets := make([]int, 0, len(f.events))
	line := 0
	for i, s := range f.events {
		g := m.spondGroup(s)
		head := titleSty.Render(s.Heading)
		if s.Heading == "" {
			head = dimSty.Render("(untitled)")
		}
		tags := []string{}
		if s.Cancelled {
			tags = append(tags, cancelledTag.Render("Cancelled"))
		}
		switch s.Type {
		case "RECURRING":
			tags = append(tags, tagSty.Render("Recurring"))
		case "AVAILABILITY":
			tags = append(tags, tagSty.Render("Availability"))
		}
		if s.Hidden {
			tags = append(tags, tagSty.Render("Hidden"))
		}
		if !s.InvitationsSent(time.Now()) {
			tags = append(tags, tagSty.Render("Invitations scheduled"))
		}
		if len(tags) > 0 {
			head += "  " + strings.Join(tags, " ")
		}

		when := authorSty.Render(formatRange(s))
		if k == feedHome && s.Recipients != nil && s.Recipients.Group != nil {
			when += dimSty.Render(" · " + s.Recipients.Group.Name)
		}
		parts := []string{head, when}
		if loc := formatLocation(s.Location); loc != "" {
			parts = append(parts, dimSty.Render("@ "+loc))
		}
		if s.Recipients != nil && s.Recipients.Group != nil && len(s.Recipients.Group.SubGroups) > 0 {
			subgroups := make(map[string]string, len(g.SubGroups))
			for _, sg := range g.SubGroups {
				subgroups[sg.ID] = sg.Name
			}
			names := make([]string, 0, len(s.Recipients.Group.SubGroups))
			for _, sg := range s.Recipients.Group.SubGroups {
				name := sg.Name
				if name == "" {
					name = roster.Lookup([]string{sg.ID}, subgroups)[0]
				}
				names = append(names, tagSty.Render(name))
			}
			parts = append(parts, strings.Join(names, " "))
		}
		parts = append(parts, formatCounts(s))
		if mine := myAnswers(s, newNameIndex(s, g)); mine != "" {
			parts = append(parts, mine)
		}
		if s.Cancelled && s.CancelledReason != "" {
			parts = append(parts, declinedSty.Render("Reason: "+s.CancelledReason))
		}
		if desc := clampLines(strings.TrimSpace(s.Description), maxDescriptionLines); desc != "" {
			parts = append(parts, lipgloss.NewStyle().Width(textWidth).Render(desc))
		}
		style := cardSty
		if i == f.cursor {
			style = selectedCardSty
		}
		card := style.Width(cardWidth).Render(strings.Join(parts, "\n"))
		offsets = append(offsets, line)
		line += lipgloss.Height(card)
		cards = append(cards, card)
	}
	if f.done {
		cards = append(cards, dimSty.Padding(0, 2).Render("— end of arrangements —"))
	}
	return strings.Join(cards, "\n"), offsets
}

// feedView is the body of an arrangement feed screen, with an empty-state
// message and the detail modal when open.
func (m model) feedView(k feedKind, where string) (body, help string) {
	f := m.feedOf(k)
	if f.detail != nil {
		return f.detail.view(m.width, m.bodyHeight()), f.detail.help()
	}
	body = f.vp.View()
	if len(f.events) == 0 && m.busy == "" && m.err == nil {
		when := "upcoming"
		if f.past {
			when = "past"
		}
		body = dimSty.Padding(1, 2).Render("No " + when + " arrangements" + where + ".")
	}
	return body, "↑/↓ select · g/G top/bottom · enter/click details · tab upcoming/past"
}
