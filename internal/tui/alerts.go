package tui

import (
	"fmt"
	"html"
	"regexp"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/asbjorn/spondcli/internal/display"
	"github.com/asbjorn/spondcli/internal/spondapi"
	"github.com/asbjorn/spondcli/internal/store"
)

// alertsView is the Alerts tab: the app's notifications list.
type alertsView struct {
	items   []spondapi.Activity
	loaded  bool
	loading bool
	err     error
	gen     int
	cursor  int
	offsets []int
	vp      viewport.Model
	post    *postModal // post opened from an alert, or nil
	feed    eventFeed  // holds the modal of an arrangement opened from an alert
}

func newAlertsView() alertsView {
	return alertsView{vp: viewport.New(0, 0), feed: newEventFeed()}
}

func (a alertsView) unread() int {
	n := 0
	for _, it := range a.items {
		if !it.Read {
			n++
		}
	}
	return n
}

type (
	alertsMsg struct {
		gen   int
		items []spondapi.Activity
		err   error
	}
	// alertOpenMsg carries what an alert points at, fetched for its modal.
	alertOpenMsg struct {
		spond *spondapi.Spond
		post  *spondapi.Post
		group string // group id of the alert, for names in a post
		err   error
	}
)

// loadAlerts fetches the list in the background; it does not use the busy
// line so it can run alongside other loads.
func (m *model) loadAlerts() tea.Cmd {
	m.alerts.gen++
	m.alerts.loading = true
	ctx, c, gen := m.ctx, m.client, m.alerts.gen
	return func() tea.Msg {
		items, err := c.Activities(ctx)
		return alertsMsg{gen: gen, items: items, err: err}
	}
}

func (m model) applyAlerts(msg alertsMsg) (tea.Model, tea.Cmd) {
	if msg.gen != m.alerts.gen {
		return m, nil
	}
	a := &m.alerts
	a.loading = false
	a.err = msg.err
	if msg.err == nil {
		a.items, a.loaded = msg.items, true
		a.cursor = min(a.cursor, max(len(a.items)-1, 0))
	}
	m.refreshAlerts()
	return m, nil
}

// openAlert fetches the arrangement or post behind the selected alert.
func (m *model) openAlert() tea.Cmd {
	if len(m.alerts.items) == 0 || m.busy != "" {
		return nil
	}
	it := m.alerts.items[m.alerts.cursor]
	ctx, c := m.ctx, m.client
	switch {
	case it.SpondID != "":
		return m.startBusy("Opening arrangement…", func() tea.Msg {
			s, err := c.Spond(ctx, it.SpondID)
			return alertOpenMsg{spond: s, err: err}
		})
	case it.PostID != "":
		return m.startBusy("Opening post…", func() tea.Msg {
			p, err := c.Post(ctx, it.PostID)
			return alertOpenMsg{post: p, group: it.GroupID, err: err}
		})
	}
	m.status = "This alert has nothing to open here"
	return nil
}

func (m model) applyAlertOpen(msg alertOpenMsg) (tea.Model, tea.Cmd) {
	m.busy = ""
	switch {
	case msg.err != nil:
		m.err = fmt.Errorf("opening alert: %w", msg.err)
	case msg.spond != nil:
		m.showDetail(feedAlerts, *msg.spond)
	case msg.post != nil:
		id := msg.post.GroupID
		if id == "" {
			id = msg.group
		}
		g, _ := m.groupByID(id)
		m.alerts.post = newPostModal(store.PlainHit(*msg.post), g, m.width, m.bodyHeight())
	}
	return m, nil
}

func (m model) updateAlerts(msg tea.Msg) (tea.Model, tea.Cmd) {
	a := &m.alerts
	if a.post != nil {
		closed, cmd := a.post.update(msg)
		if closed {
			a.post = nil
		}
		return m, cmd
	}
	if a.feed.detail != nil {
		return m.updateDetail(feedAlerts, msg)
	}

	switch msg := msg.(type) {
	case tea.MouseMsg:
		if msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft {
			if i, ok := cardAtLine(a.offsets, a.vp.TotalLineCount(), msg.Y-headerLines+a.vp.YOffset); ok {
				m.selectAlert(i)
				return m, m.openAlert()
			}
			return m, nil
		}
	case tea.KeyMsg:
		switch msg.String() {
		case "q":
			return m, tea.Quit
		case "up", "k":
			m.selectAlert(a.cursor - 1)
			return m, nil
		case "down", "j":
			m.selectAlert(a.cursor + 1)
			return m, nil
		case "home", "g":
			m.selectAlert(0)
			return m, nil
		case "end", "G":
			m.selectAlert(len(a.items) - 1)
			return m, nil
		case "enter", " ":
			return m, m.openAlert()
		case "r":
			if !a.loading {
				return m, m.loadAlerts()
			}
			return m, nil
		}
	}
	var cmd tea.Cmd
	a.vp, cmd = a.vp.Update(msg)
	return m, cmd
}

func (m *model) selectAlert(i int) {
	a := &m.alerts
	if len(a.items) == 0 {
		return
	}
	a.cursor = min(max(i, 0), len(a.items)-1)
	m.refreshAlerts()
	scrollIntoView(&a.vp, a.offsets, a.cursor)
}

var unreadSty = lipgloss.NewStyle().Bold(true).Foreground(accent)

// refreshAlerts renders the list: a bar marks the selection, a dot unread
// alerts.
func (m *model) refreshAlerts() {
	a := &m.alerts
	width := max(m.width-4, 20)
	blocks := make([]string, 0, len(a.items))
	a.offsets = a.offsets[:0]
	line := 0
	for i, it := range a.items {
		dot := "  "
		if !it.Read {
			dot = unreadSty.Render("● ")
		}
		meta := display.Time(it.Timestamp)
		if g, ok := m.groupByID(it.GroupID); ok {
			meta += " · " + g.Name
		}
		text := lipgloss.NewStyle().Width(width - 2).Render(renderBoldHTML(it.Text))
		block := lipgloss.JoinHorizontal(lipgloss.Top, dot, lipgloss.JoinVertical(lipgloss.Left, text, dimSty.Render(meta)))
		style := plainBlockSty
		if i == a.cursor {
			style = selectedCommentSty
		}
		block = style.Render(block)
		a.offsets = append(a.offsets, line)
		line += lipgloss.Height(block) + 1 // blank line between alerts
		blocks = append(blocks, block)
	}
	a.vp.SetContent(strings.Join(blocks, "\n\n"))
}

func (m model) viewAlerts() (crumb, body, help string) {
	a := m.alerts
	crumb = "Alerts"
	if a.loaded {
		crumb = fmt.Sprintf("Alerts (%d, %d unread)", len(a.items), a.unread())
	}
	help = "↑/↓ select · g/G top/bottom · enter/click open · r refresh · 1-4 tabs · q quit"
	switch {
	case a.post != nil:
		return crumb, a.post.view(m.width, m.bodyHeight()), a.post.help()
	case a.feed.detail != nil:
		return crumb, a.feed.detail.view(m.width, m.bodyHeight()), a.feed.detail.help()
	case a.err != nil:
		body = errSty.Render("Loading alerts failed: " + a.err.Error())
	case a.loading && !a.loaded:
		body = dimSty.Padding(1, 2).Render("Loading alerts…")
	case len(a.items) == 0:
		body = dimSty.Padding(1, 2).Render("No alerts.")
	default:
		body = a.vp.View()
	}
	return crumb, body, help
}

var (
	boldTag = regexp.MustCompile(`(?i)<b>(.*?)</b>`)
	anyTag  = regexp.MustCompile(`<[^>]*>`)
)

// renderBoldHTML renders the alert markup: <b> becomes bold, other tags go.
func renderBoldHTML(s string) string {
	plain := func(t string) string { return html.UnescapeString(anyTag.ReplaceAllString(t, "")) }
	var b strings.Builder
	last := 0
	for _, loc := range boldTag.FindAllStringSubmatchIndex(s, -1) {
		b.WriteString(plain(s[last:loc[0]]))
		b.WriteString(titleSty.Render(plain(s[loc[2]:loc[3]])))
		last = loc[1]
	}
	b.WriteString(plain(s[last:]))
	return b.String()
}
