// Package tui is the interactive terminal UI, built with Bubble Tea.
package tui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/asbjorn/spondcli/internal/display"
	"github.com/asbjorn/spondcli/internal/roster"
	"github.com/asbjorn/spondcli/internal/spondapi"
	"github.com/asbjorn/spondcli/internal/store"
)

const pageSize = 10

// Run runs the interactive UI until the user quits or ctx is cancelled.
// st may be nil when the post cache could not be opened; storeErr says why.
func Run(ctx context.Context, c *spondapi.Client, st *store.Store, storeErr error) error {
	m := newModel(ctx, c)
	m.store, m.storeErr = st, storeErr
	_, err := tea.NewProgram(
		m,
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(), // click arrangements, wheel-scroll feeds
		tea.WithContext(ctx),
	).Run()
	if errors.Is(err, tea.ErrProgramKilled) {
		return nil
	}
	return err
}

type screen int

const (
	screenLogin screen = iota
	screenGroups
	screenGroupMenu
	screenPosts
	screenEvents
	screenMembers
	screenSubgroups
)

type menuItem struct{ key, label, hint string }

// menuItems is what the selected group offers; key is the shortcut. The
// subgroup entry only appears for groups that have subgroups.
func (m model) menuItems() []menuItem {
	items := []menuItem{
		{"p", "Posts", "Messages posted to " + m.scopeLabel()},
		{"a", "Arrangements", "Events, upcoming and past"},
		{"m", "Members", "Member list and CSV export"},
	}
	if len(m.group.SubGroups) > 0 {
		items = append(items, menuItem{"s", "Subgroup", "Showing: " + m.scopeLabel()})
	}
	if len(eligibleInviteGroups(m.allGroups, m.me)) > 0 {
		items = append(items, menuItem{"i", "Invite people", "Add a member or admin to one of your groups"})
	}
	return items
}

// subgroupItem is a row in the subgroup picker; a nil sg means the whole group.
type subgroupItem struct {
	sg      *spondapi.Subgroup
	members int
}

func (i subgroupItem) Title() string {
	if i.sg == nil {
		return "Whole group"
	}
	return i.sg.Name
}
func (i subgroupItem) FilterValue() string { return i.Title() }
func (i subgroupItem) Description() string { return display.Plural(i.members, "member") }

const (
	fieldEmail = iota
	fieldPassword
	fieldCode
)

var (
	accent    = lipgloss.AdaptiveColor{Light: "#5A3FD6", Dark: "#9B87F5"}
	subtle    = lipgloss.AdaptiveColor{Light: "#6B6B6B", Dark: "#8A8A8A"}
	errColor  = lipgloss.AdaptiveColor{Light: "#C62828", Dark: "#FF6B6B"}
	okColor   = lipgloss.AdaptiveColor{Light: "#2E7D32", Dark: "#7BD88F"}
	tagBg     = lipgloss.AdaptiveColor{Light: "#ECE8FD", Dark: "#3A3354"}
	headerSty = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#FFFFFF")).Background(accent).Padding(0, 1)
	crumbSty  = lipgloss.NewStyle().Foreground(subtle).Padding(0, 1)
	helpSty   = lipgloss.NewStyle().Foreground(subtle).Padding(0, 1)
	errSty    = lipgloss.NewStyle().Foreground(errColor).Padding(0, 1)
	okSty     = lipgloss.NewStyle().Foreground(okColor).Padding(0, 1)
	authorSty = lipgloss.NewStyle().Bold(true).Foreground(accent)
	dimSty    = lipgloss.NewStyle().Foreground(subtle)
	titleSty  = lipgloss.NewStyle().Bold(true)
	tagSty    = lipgloss.NewStyle().Background(tagBg).Padding(0, 1)
	cardSty   = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(subtle).Padding(0, 1)
	// selectedCardSty must keep cardSty's size so card offsets stay stable.
	selectedCardSty = cardSty.BorderForeground(accent).BorderStyle(lipgloss.ThickBorder())
	boxSty          = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(accent).Padding(1, 3)
)

type (
	loginMsg struct {
		resp *spondapi.LoginResponse
		err  error
	}
	tokenMsg struct {
		token *spondapi.ExpiableToken
		err   error
	}
	groupsMsg struct {
		me     *spondapi.MyProfile
		groups []spondapi.Group
		reload bool // a background refresh: keep the screen
		err    error
	}
	postsMsg struct {
		gen   int
		posts []spondapi.Post
		err   error
	}
	searchMsg struct {
		gen     int
		query   string
		hits    []store.SearchHit
		stats   *store.SyncStats // nil when the cache was recent enough
		syncErr error            // sync failed; hits come from the older cache
		err     error
	}
	eventsMsg struct {
		kind   feedKind
		gen    int
		sponds []spondapi.Spond
		err    error
	}
	membersMsg struct {
		gen    int
		cursor string // the cursor this page was requested with
		page   *spondapi.MembersPage
		err    error
	}
	exportMsg struct {
		path  string
		count int
		err   error
	}
)

type groupItem struct{ g spondapi.Group }

func (i groupItem) Title() string       { return i.g.Name }
func (i groupItem) FilterValue() string { return i.g.Name }
func (i groupItem) Description() string {
	parts := []string{}
	if i.g.Activity != "" {
		parts = append(parts, i.g.Activity)
	}
	parts = append(parts, display.Plural(len(i.g.Members), "member"), display.Plural(len(i.g.SubGroups), "subgroup"))
	return strings.Join(parts, " · ")
}

type model struct {
	ctx    context.Context
	client *spondapi.Client
	screen screen
	width  int
	height int

	inputs    []textinput.Model
	focus     int
	challenge string // 2FA challenge token; non-empty once a code is requested
	autoLogin bool   // started from a cached login or SPOND_* credentials; hides the form until input is needed
	phone     string

	busy    string // non-empty while a request runs
	gen     int    // bumped when a group screen opens or closes; older results are dropped
	spinner spinner.Model
	err     error
	status  string

	me         *spondapi.MyProfile
	groups     list.Model
	group      spondapi.Group
	subgroup   *spondapi.Subgroup // nil means the whole group
	menuCursor int
	subgroups  list.Model

	posts     []spondapi.Post
	postsDone bool
	feed      viewport.Model // shared by the posts and arrangements screens

	store       *store.Store // local post cache for search; nil if it could not be opened
	storeErr    error        // why store is nil
	searchInput textinput.Model
	searching   bool              // the search input has focus
	query       string            // search whose results are shown; "" shows the feed
	hits        []store.SearchHit // results for query
	postCursor  int               // selected card in the feed or results
	postOffsets []int             // first feed line of each post card
	postModal   *postModal        // open post, or nil

	tab       tab       // shown tab once logged in
	home      eventFeed // Home tab: arrangements from every group
	groupFeed eventFeed // a group's (or subgroup's) arrangements
	alerts    alertsView
	chat      chatView

	allGroups   []spondapi.Group      // every group from GET groups
	profiles    map[string]string     // profile id → name, from allGroups
	allMembers  []spondapi.Membership // members of the open group, unfiltered
	memberList  memberList
	memberModal *memberModal // open member modal, or nil
	invite      *inviteForm  // open invite form, or nil

	requestList   requestList   // pending join requests on the main screen
	focusRequests bool          // keys go to the requests pane
	requestModal  *requestModal // open join request, or nil

	rows []roster.Row // member rows shown (and exported)

	progress    float64 // fraction done for the running request; < 0 when unknown
	tick        int     // spinner ticks, drives the indeterminate bar
	expectedMem int     // member count expected while paging, 0 when unknown

	streamCursor spondapi.StreamCursor // newest activity seen; see refresh.go
	polling      bool                  // an activity stream request is running
}

func newModel(ctx context.Context, c *spondapi.Client) model {
	inputs := make([]textinput.Model, 3)
	for i, spec := range []struct{ prompt, placeholder string }{
		{"Email    ", "you@example.com"},
		{"Password ", ""},
		{"2FA code ", "123456"},
	} {
		in := textinput.New()
		in.Prompt = spec.prompt
		in.Placeholder = spec.placeholder
		in.PromptStyle = lipgloss.NewStyle().Foreground(accent)
		in.Width = 32
		inputs[i] = in
	}
	inputs[fieldPassword].EchoMode = textinput.EchoPassword
	inputs[fieldEmail].SetValue(os.Getenv("SPOND_EMAIL"))
	inputs[fieldPassword].SetValue(os.Getenv("SPOND_PASSWORD"))

	focus := fieldEmail
	if inputs[fieldEmail].Value() != "" {
		focus = fieldPassword
	}
	inputs[focus].Focus()

	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = lipgloss.NewStyle().Foreground(accent)

	groups := list.New(nil, list.NewDefaultDelegate(), 0, 0)
	groups.Title = "Your groups"
	groups.Styles.Title = headerSty
	groups.SetShowHelp(false)

	subgroups := list.New(nil, list.NewDefaultDelegate(), 0, 0)
	subgroups.Title = "Choose subgroup"
	subgroups.Styles.Title = headerSty
	subgroups.SetShowHelp(false)

	search := textinput.New()
	search.Prompt = "Search: "
	search.Placeholder = "words in posts and comments"
	search.PromptStyle = lipgloss.NewStyle().Foreground(accent)

	busy := ""
	switch {
	case c.Token != "": // cached login
		busy = "Loading groups…"
	case inputs[fieldEmail].Value() != "" && inputs[fieldPassword].Value() != "":
		busy = "Logging in…"
	}

	return model{
		autoLogin: busy != "",
		ctx:       ctx,
		client:    c,
		inputs:    inputs,
		focus:     focus,
		busy:      busy,
		spinner:   sp,
		groups:    groups,
		subgroups: subgroups,
		feed:      viewport.New(0, 0),
		home:      newEventFeed(),
		groupFeed: newEventFeed(),
		alerts:    newAlertsView(),
		chat:      newChatView(),

		searchInput: search,
	}
}

func (m model) Init() tea.Cmd {
	switch {
	case m.busy == "":
		return textinput.Blink
	case m.client.Token != "": // cached login
		return tea.Batch(textinput.Blink, m.spinner.Tick, m.loadGroupsCmd())
	default: // credentials came from the environment
		return tea.Batch(textinput.Blink, m.spinner.Tick, m.loginCmd())
	}
}

// Commands. Each captures what it needs so it is safe to run off the UI loop.

func (m model) loginCmd() tea.Cmd {
	ctx, c := m.ctx, m.client
	email := strings.TrimSpace(m.inputs[fieldEmail].Value())
	password := m.inputs[fieldPassword].Value()
	return func() tea.Msg {
		resp, err := c.Login(ctx, email, password)
		return loginMsg{resp: resp, err: err}
	}
}

func (m model) verifyCmd() tea.Cmd {
	ctx, c, challenge := m.ctx, m.client, m.challenge
	code := strings.TrimSpace(m.inputs[fieldCode].Value())
	return func() tea.Msg {
		token, err := c.Verify2FA(ctx, challenge, code)
		return tokenMsg{token: token, err: err}
	}
}

func (m model) loadGroupsCmd() tea.Cmd {
	ctx, c := m.ctx, m.client
	return func() tea.Msg {
		groups, err := c.Groups(ctx)
		if err != nil {
			return groupsMsg{err: err}
		}
		// The profile only decorates the header, so its failure is not fatal.
		me, _ := c.Me(ctx)
		return groupsMsg{me: me, groups: groups}
	}
}

func (m model) loadPostsCmd() tea.Cmd {
	ctx, c, sc, gen := m.ctx, m.client, m.scope(), m.gen
	var cursor *spondapi.Post
	if len(m.posts) > 0 {
		last := m.posts[len(m.posts)-1]
		cursor = &last
	}
	return func() tea.Msg {
		posts, err := c.Posts(ctx, sc, pageSize, cursor)
		return postsMsg{gen: gen, posts: posts, err: err}
	}
}

// searchCmd syncs the open group's posts if the cache is stale, then
// searches it within the current scope.
func (m model) searchCmd(query string) tea.Cmd {
	ctx, c, st, storeErr, g, sc, gen := m.ctx, m.client, m.store, m.storeErr, m.group, m.scope(), m.gen
	return func() tea.Msg {
		msg := searchMsg{gen: gen, query: query}
		if st == nil {
			msg.err = fmt.Errorf("post cache unavailable: %w", storeErr)
			return msg
		}
		stats, usable, err := st.Freshen(ctx, c, g, false)
		if err != nil && !usable {
			msg.err = err
			return msg
		}
		msg.stats, msg.syncErr = stats, err
		msg.hits, msg.err = st.Search(ctx, sc.GroupID, sc.SubgroupID, query)
		return msg
	}
}

// loadMembersCmd fetches one page of members; cursor is "" for the first.
func (m model) loadMembersCmd(cursor string) tea.Cmd {
	ctx, c, groupID, gen := m.ctx, m.client, m.group.ID, m.gen
	return func() tea.Msg {
		page, err := c.MembersPage(ctx, groupID, cursor)
		return membersMsg{gen: gen, cursor: cursor, page: page, err: err}
	}
}

// rebuildMemberRows applies the subgroup scope to allMembers.
func (m *model) rebuildMemberRows() {
	members := m.allMembers
	if m.subgroup != nil {
		members = roster.InSubgroup(members, m.subgroup.ID)
	}
	m.rows = roster.Rows(m.group, members)
	m.memberList.setRows(m.rows)
}

func (m model) exportCmd() tea.Cmd {
	rows := m.rows
	name := m.group.Name
	if m.subgroup != nil {
		name += " " + m.subgroup.Name
	}
	path := roster.DefaultCSVName(name)
	return func() tea.Msg {
		return exportMsg{path: path, count: len(rows), err: roster.ExportCSV(path, rows)}
	}
}

// startBusy marks a request as running and keeps the spinner ticking.
func (m *model) startBusy(label string, cmd tea.Cmd) tea.Cmd {
	m.busy = label
	m.progress = -1
	m.err = nil
	m.status = ""
	return tea.Batch(m.spinner.Tick, cmd)
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.resize()
		return m, nil

	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		}

	case spinner.TickMsg:
		if m.busy == "" {
			return m, nil
		}
		m.tick++
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd

	case loginMsg:
		m.busy = ""
		switch {
		case msg.err != nil:
			m.err = fmt.Errorf("login failed: %w", msg.err)
			return m, nil
		case msg.resp.AccessToken != nil:
			m.err = m.client.SetToken(msg.resp.AccessToken) // shown, but the login still works
			return m, m.startBusy("Loading groups…", m.loadGroupsCmd())
		case msg.resp.Token != "":
			m.challenge, m.phone = msg.resp.Token, msg.resp.PhoneNumber
			m.setFocus(fieldCode)
			return m, textinput.Blink
		default:
			m.err = fmt.Errorf("login: response has neither tokens nor a 2FA challenge")
			return m, nil
		}

	case tokenMsg:
		m.busy = ""
		if msg.err != nil {
			m.err = fmt.Errorf("2FA failed: %w", msg.err)
			return m, nil
		}
		m.err = m.client.SetToken(msg.token) // shown, but the login still works
		return m, m.startBusy("Loading groups…", m.loadGroupsCmd())

	case groupsMsg:
		if msg.reload {
			return m.applyGroupsReload(msg)
		}
		m.busy = ""
		if spondapi.IsUnauthorized(msg.err) && m.screen == screenLogin {
			// The cached login was revoked or expired early: ask for the password.
			m.err = m.client.Logout()
			m.status = "Session expired, please log in again"
			return m, textinput.Blink
		}
		if msg.err != nil {
			m.err = fmt.Errorf("loading groups: %w", msg.err)
			return m, nil
		}
		m.me = msg.me
		m.allGroups = msg.groups
		m.profiles = profileNames(msg.groups, msg.me)
		m.refreshRequests()
		items := make([]list.Item, 0, len(msg.groups))
		for _, g := range msg.groups {
			items = append(items, groupItem{g: g})
		}
		firstLoad := m.screen == screenLogin
		m.screen = screenGroups
		m.resize()
		cmd := m.groups.SetItems(items)
		if !firstLoad {
			return m, cmd
		}
		// Start on Home; fetch alerts and messages now for the tab badges.
		m.tab = tabHome
		return m, tea.Batch(cmd, m.openEvents(feedHome, false), m.loadAlerts(), m.loadChats(""), m.startPolling())

	case postsMsg:
		if msg.gen != m.gen {
			return m, nil // the user left this screen while it loaded
		}
		m.busy = ""
		if msg.err != nil {
			m.err = fmt.Errorf("loading posts: %w", msg.err)
			return m, nil
		}
		m.posts = append(m.posts, msg.posts...)
		m.postsDone = len(msg.posts) < pageSize
		m.refreshPosts()
		return m, nil

	case searchMsg:
		if msg.gen != m.gen {
			return m, nil
		}
		m.busy = ""
		if msg.err != nil {
			m.err = fmt.Errorf("search: %w", msg.err)
			return m, nil
		}
		m.query, m.hits = msg.query, msg.hits
		switch {
		case msg.syncErr != nil:
			m.err = fmt.Errorf("showing cached posts: %w", msg.syncErr)
		case msg.stats != nil:
			m.status = fmt.Sprintf("Synced %s", display.Plural(msg.stats.Posts, "post"))
		}
		m.postCursor, m.postModal = 0, nil
		m.refreshPosts()
		m.feed.GotoTop()
		return m, nil

	case eventsMsg:
		return m.applyEvents(msg)

	case pollTickMsg:
		return m, m.poll()

	case streamMsg:
		return m.applyStream(msg)

	case spondFetchedMsg:
		return m.applySpondFetched(msg)

	case postFetchedMsg:
		return m.applyPostFetched(msg)

	case chatsPolledMsg:
		return m.applyChatsPolled(msg)

	case alertsMsg:
		return m.applyAlerts(msg)

	case alertOpenMsg:
		return m.applyAlertOpen(msg)

	case chatsMsg:
		return m.applyChats(msg)

	case chatMessagesMsg:
		return m.applyMessages(msg)

	case chatFoundMsg:
		return m.applyFound(msg)

	case chatSentMsg:
		return m.applySent(msg)

	case chatReactedMsg:
		return m.applyReacted(msg)

	case membersMsg:
		if msg.gen != m.gen {
			return m, nil
		}
		if msg.err != nil {
			m.busy = ""
			m.err = fmt.Errorf("loading members: %w", msg.err)
			return m, nil
		}
		m.allMembers = spondapi.MergeMembers(m.allMembers, msg.page.Members)
		m.rebuildMemberRows()
		if next := spondapi.NextMembersCursor(msg.page, msg.cursor); next != "" {
			if m.expectedMem > 0 {
				m.progress = min(float64(len(m.allMembers))/float64(m.expectedMem), 0.99)
			}
			m.busy = fmt.Sprintf("Loading members… %d loaded", len(m.allMembers))
			return m, m.loadMembersCmd(next)
		}
		m.busy = ""
		return m, nil

	case respondMsg:
		return m.applyRespond(msg)

	case memberOpMsg:
		return m.applyMemberOp(msg)

	case requestOpMsg:
		return m.applyRequestOp(msg)

	case inviteMsg:
		return m.applyInvite(msg)

	case exportMsg:
		m.busy = ""
		if msg.err != nil {
			m.err = fmt.Errorf("export failed: %w", msg.err)
			return m, nil
		}
		m.status = fmt.Sprintf("Exported %s to %s", display.Plural(msg.count, "member"), msg.path)
		return m, nil
	}

	if m.screen == screenLogin {
		return m.updateLogin(msg)
	}
	if mm, cmd, ok := m.switchTab(msg); ok {
		return mm, cmd
	}
	switch m.tab {
	case tabHome:
		return m.updateHome(msg)
	case tabAlerts:
		return m.updateAlerts(msg)
	case tabMessages:
		return m.updateChat(msg)
	}

	if m.invite != nil {
		return m.updateInvite(msg)
	}
	switch m.screen {
	case screenGroups:
		return m.updateGroups(msg)
	case screenGroupMenu:
		return m.updateGroupMenu(msg)
	case screenPosts:
		return m.updatePosts(msg)
	case screenEvents:
		return m.updateGroupEvents(msg)
	case screenSubgroups:
		return m.updateSubgroups(msg)
	default:
		return m.updateMembers(msg)
	}
}

func (m model) updateLogin(msg tea.Msg) (tea.Model, tea.Cmd) {
	if key, ok := msg.(tea.KeyMsg); ok && m.busy == "" {
		m.autoLogin = false // the user is typing
		last := fieldPassword
		if m.challenge != "" {
			last = fieldCode
		}
		switch key.String() {
		case "esc":
			return m, tea.Quit
		case "tab", "down":
			if m.challenge == "" {
				m.setFocus((m.focus + 1) % (last + 1))
			}
			return m, nil
		case "shift+tab", "up":
			if m.challenge == "" {
				m.setFocus((m.focus + last) % (last + 1))
			}
			return m, nil
		case "enter":
			if m.focus < last {
				m.setFocus(m.focus + 1)
				return m, nil
			}
			if m.challenge != "" {
				return m, m.startBusy("Verifying code…", m.verifyCmd())
			}
			return m, m.startBusy("Logging in…", m.loginCmd())
		}
	}

	var cmd tea.Cmd
	m.inputs[m.focus], cmd = m.inputs[m.focus].Update(msg)
	return m, cmd
}

func (m *model) setFocus(i int) {
	m.inputs[m.focus].Blur()
	m.focus = i
	m.inputs[i].Focus()
}

func (m model) updateGroups(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.requestModal != nil {
		return m.updateRequestModal(msg)
	}
	if mouse, ok := msg.(tea.MouseMsg); ok && mouse.Action == tea.MouseActionPress && mouse.Button == tea.MouseButtonLeft {
		if i, ok := m.requestList.rowAt(mouse.Y - m.requestsTop()); ok {
			m.focusRequests = true
			m.requestList.cursor = i
			m.openRequest()
			return m, nil
		}
	}
	if m.focusRequests {
		return m.updateRequestsPane(msg)
	}
	key, isKey := msg.(tea.KeyMsg)
	filtering := m.groups.FilterState() == list.Filtering
	if isKey && !filtering && key.String() == "tab" && len(m.requestList.items) > 0 {
		m.focusRequests = true
		return m, nil
	}
	if isKey && !filtering && m.busy == "" {
		item, ok := m.groups.SelectedItem().(groupItem)
		switch k := key.String(); k {
		case "q":
			return m, tea.Quit
		case "enter":
			if ok {
				m.group, m.subgroup, m.menuCursor = item.g, nil, 0
				m.screen = screenGroupMenu
				m.err, m.status = nil, ""
				return m, nil
			}
		case "p", "a", "m":
			if ok {
				m.group, m.subgroup = item.g, nil
				return m, m.open(k)
			}
		}
	}

	var cmd tea.Cmd
	m.groups, cmd = m.groups.Update(msg)
	return m, cmd
}

func (m model) updateGroupMenu(msg tea.Msg) (tea.Model, tea.Cmd) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	switch k := key.String(); k {
	case "esc", "backspace", "q":
		m.screen = screenGroups
		return m, nil
	case "up", "k":
		n := len(m.menuItems())
		m.menuCursor = (m.menuCursor + n - 1) % n
	case "down", "j", "tab":
		m.menuCursor = (m.menuCursor + 1) % len(m.menuItems())
	case "enter":
		k = m.menuItems()[m.menuCursor].key
		if k == "s" {
			return m, m.openSubgroups()
		}
		return m, m.open(k)
	case "p", "a", "m":
		return m, m.open(k)
	case "i":
		return m, m.openInvite()
	case "s":
		if len(m.group.SubGroups) > 0 {
			return m, m.openSubgroups()
		}
	}
	return m, nil
}

// openSubgroups shows the subgroup picker with the current scope selected.
func (m *model) openSubgroups() tea.Cmd {
	counts := map[string]int{}
	for _, mem := range m.group.Members {
		for _, id := range mem.SubGroups {
			counts[id]++
		}
	}
	items := make([]list.Item, 0, len(m.group.SubGroups)+1)
	items = append(items, subgroupItem{members: len(m.group.Members)})
	selected := 0
	for i := range m.group.SubGroups {
		sg := &m.group.SubGroups[i]
		items = append(items, subgroupItem{sg: sg, members: counts[sg.ID]})
		if m.subgroup != nil && m.subgroup.ID == sg.ID {
			selected = i + 1
		}
	}
	m.screen = screenSubgroups
	m.subgroups.ResetFilter()
	cmd := m.subgroups.SetItems(items)
	m.subgroups.Select(selected)
	return cmd
}

func (m model) updateSubgroups(msg tea.Msg) (tea.Model, tea.Cmd) {
	key, isKey := msg.(tea.KeyMsg)
	if isKey && m.subgroups.FilterState() != list.Filtering {
		switch key.String() {
		case "esc", "backspace", "q":
			m.screen = screenGroupMenu
			return m, nil
		case "enter":
			if item, ok := m.subgroups.SelectedItem().(subgroupItem); ok {
				m.subgroup = item.sg
			}
			m.screen = screenGroupMenu
			return m, nil
		}
	}

	var cmd tea.Cmd
	m.subgroups, cmd = m.subgroups.Update(msg)
	return m, cmd
}

// scope is what posts, arrangements and members are loaded for.
func (m model) scope() spondapi.Scope {
	sc := spondapi.Scope{GroupID: m.group.ID}
	if m.subgroup != nil {
		sc.SubgroupID = m.subgroup.ID
	}
	return sc
}

func (m model) scopeLabel() string {
	if m.subgroup == nil {
		return "the whole group"
	}
	return m.subgroup.Name
}

// scopeCrumb is the breadcrumb prefix: "Group" or "Group › Subgroup".
func (m model) scopeCrumb() string {
	if m.subgroup == nil {
		return m.group.Name
	}
	return m.group.Name + " › " + m.subgroup.Name
}

// open switches to the screen behind a group menu shortcut and starts
// loading it.
func (m *model) open(key string) tea.Cmd {
	m.gen++
	m.err, m.status = nil, ""
	switch key {
	case "p":
		m.posts, m.postsDone = nil, false
		m.searching, m.query, m.hits = false, "", nil
		m.postCursor, m.postModal = 0, nil
		m.screen = screenPosts
		m.feed.SetContent("")
		m.feed.GotoTop()
		return m.startBusy("Loading posts…", m.loadPostsCmd())
	case "a":
		return m.openEvents(feedGroup, false)
	case "i":
		return m.openInvite()
	default:
		m.rows, m.allMembers, m.memberModal = nil, nil, nil
		m.memberList = memberList{}
		m.screen = screenMembers
		m.resize()
		cmd := m.startBusy("Loading members…", m.loadMembersCmd(""))
		// GET groups (includeMembers=true) gives the size for the progress bar.
		m.expectedMem = len(m.group.Members)
		if m.expectedMem > 0 {
			m.progress = 0
		}
		return cmd
	}
}

// backToMenu returns from a group screen to the group's menu.
func (m model) backToMenu() (tea.Model, tea.Cmd) {
	m.gen++
	m.screen = screenGroupMenu
	m.groupFeed.detail = nil
	m.searching, m.query, m.hits, m.postModal = false, "", nil, nil
	m.busy, m.err, m.status = "", nil, ""
	return m, nil
}

func (m model) updatePosts(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.postModal != nil {
		return m.updatePostModal(msg)
	}
	if m.searching {
		return m.updateSearchInput(msg)
	}

	switch msg := msg.(type) {
	case tea.MouseMsg:
		if msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft {
			if i, ok := cardAtLine(m.postOffsets, m.feed.TotalLineCount(), msg.Y-headerLines+m.feed.YOffset); ok {
				m.selectPost(i)
				return m, m.openPost()
			}
			return m, nil
		}
	case tea.KeyMsg:
		switch msg.String() {
		case "up", "k":
			m.selectPost(m.postCursor - 1)
			return m, nil
		case "down", "j":
			m.selectPost(m.postCursor + 1)
			return m, m.maybeLoadMorePosts()
		case "home", "g":
			m.selectPost(0)
			return m, nil
		case "end", "G":
			m.selectPost(len(m.shownHits()) - 1)
			return m, m.maybeLoadMorePosts()
		case "enter", " ":
			return m, m.openPost()
		}
	}

	var cmd tea.Cmd
	m.feed, cmd = m.feed.Update(msg)

	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, cmd
	}
	switch k := key.String(); k {
	case "/":
		m.searching = true
		m.searchInput.SetValue(m.query)
		m.searchInput.CursorEnd()
		return m, m.searchInput.Focus()
	case "esc", "backspace", "q":
		if m.query != "" { // leave the results for the feed
			m.query, m.hits, m.err, m.status = "", nil, nil, ""
			m.postCursor = 0
			m.refreshPosts()
			m.feed.GotoTop()
			return m, cmd
		}
		return m.backToMenu()
	case "a", "m":
		return m, m.open(k)
	}

	if m.feed.AtBottom() {
		return m, tea.Batch(cmd, m.maybeLoadMorePosts())
	}
	return m, cmd
}

func (m model) updateSearchInput(msg tea.Msg) (tea.Model, tea.Cmd) {
	if key, ok := msg.(tea.KeyMsg); ok {
		switch key.String() {
		case "esc":
			m.searching = false
			m.searchInput.Blur()
			return m, nil
		case "enter":
			query := strings.TrimSpace(m.searchInput.Value())
			if query == "" || m.busy != "" {
				return m, nil
			}
			m.searching = false
			m.searchInput.Blur()
			m.err, m.status = nil, ""
			return m, m.startBusy("Searching posts…", m.searchCmd(query))
		}
	}
	var cmd tea.Cmd
	m.searchInput, cmd = m.searchInput.Update(msg)
	return m, cmd
}

func (m model) updateGroupEvents(msg tea.Msg) (tea.Model, tea.Cmd) {
	mm, cmd, handled := m.updateEvents(feedGroup, msg)
	if handled {
		return mm, cmd
	}
	m = mm.(model)
	switch k := msg.(tea.KeyMsg).String(); k {
	case "esc", "backspace", "q":
		return m.backToMenu()
	case "p", "m":
		return m, m.open(k)
	}
	return m, cmd
}

func (m model) updateMembers(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.memberModal != nil {
		return m.updateMemberModal(msg)
	}

	switch msg := msg.(type) {
	case tea.MouseMsg:
		switch {
		case msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft:
			if i, ok := m.memberList.rowAt(msg.Y - headerLines); ok {
				m.memberList.cursor = i
				m.openMember()
			}
		case msg.Button == tea.MouseButtonWheelUp:
			m.memberList.move(-3)
		case msg.Button == tea.MouseButtonWheelDown:
			m.memberList.move(3)
		}
		return m, nil
	case tea.KeyMsg:
		switch k := msg.String(); k {
		case "esc", "backspace", "q":
			return m.backToMenu()
		case "p", "a":
			return m, m.open(k)
		case "i":
			return m, m.openInvite()
		case "e":
			if m.busy == "" && len(m.rows) > 0 {
				return m, m.startBusy("Exporting…", m.exportCmd())
			}
		case "up", "k":
			m.memberList.move(-1)
		case "down", "j":
			m.memberList.move(1)
		case "pgup":
			m.memberList.move(-m.memberList.visibleRows())
		case "pgdown", " ":
			m.memberList.move(m.memberList.visibleRows())
		case "home", "g":
			m.memberList.move(-len(m.rows))
		case "end", "G":
			m.memberList.move(len(m.rows))
		case "enter":
			m.openMember()
		}
	}
	return m, nil
}

// Layout.

const chromeHeight = tabBarHeight + 3 // tab bar, pane title, status line, help line

// bodyHeight is the height left between the crumb and the status line.
func (m model) bodyHeight() int { return max(m.height-chromeHeight, 3) }

func (m *model) resize() {
	bodyHeight := m.bodyHeight()
	m.requestList.width = m.width
	m.requestList.height = 0
	if n := len(m.requestList.items); n > 0 {
		m.requestList.height = min(n+requestListHeader, max(bodyHeight/2, requestListHeader+1))
	}
	m.requestList.ensureVisible()
	m.groups.SetSize(m.width, m.groupsHeight())
	if m.requestModal != nil {
		m.requestModal.resize(m.width, bodyHeight)
	}
	m.subgroups.SetSize(m.width, bodyHeight)
	m.feed.Width, m.feed.Height = m.width, bodyHeight
	if m.screen == screenPosts {
		m.refreshPosts()
		if m.postModal != nil {
			m.postModal.resize(m.width, bodyHeight)
		}
	}
	for _, k := range []feedKind{feedGroup, feedHome, feedAlerts} {
		f := m.feedOf(k)
		f.vp.Width, f.vp.Height = m.width, bodyHeight
		m.refreshEvents(k)
		if f.detail != nil {
			f.detail.resize(m.width, bodyHeight)
		}
	}
	for _, vp := range []*viewport.Model{&m.alerts.vp, &m.chat.vp, &m.chat.msgVP} {
		vp.Width, vp.Height = m.width, bodyHeight
	}
	m.chat.msgVP.Height = max(bodyHeight-composerHeight-threadBannerHeight, 3)
	m.chat.compose.SetWidth(max(m.width-4, 10))
	m.refreshAlerts()
	m.refreshChats()
	m.refreshMessages()
	if m.alerts.post != nil {
		m.alerts.post.resize(m.width, bodyHeight)
	}

	m.memberList.setSize(m.width, bodyHeight)
	if m.memberModal != nil {
		m.memberModal.resize(m.width, bodyHeight)
	}
	if m.invite != nil {
		m.invite.resize(m.width, bodyHeight)
	}
}

func (m model) View() string {
	if m.width == 0 {
		return ""
	}
	if m.screen == screenLogin {
		return m.viewLogin()
	}

	var crumb, body, help string
	switch m.tab {
	case tabHome:
		crumb, body, help = m.viewHome()
	case tabAlerts:
		crumb, body, help = m.viewAlerts()
	case tabMessages:
		crumb, body, help = m.viewChat()
	default:
		crumb, body, help = m.viewGroupsTab()
	}

	bodyHeight := m.bodyHeight()
	body = lipgloss.NewStyle().Height(bodyHeight).MaxHeight(bodyHeight).Render(body)
	return lipgloss.JoinVertical(
		lipgloss.Left,
		m.tabBar(),
		paneTitleSty.Render(display.FirstLine(crumb, m.width-2)),
		body,
		m.statusLine(),
		helpSty.Render(help),
	)
}

// viewGroupsTab renders the Groups tab's current screen.
func (m model) viewGroupsTab() (crumb, body, help string) {
	switch m.screen {
	case screenGroups:
		crumb = display.Plural(len(m.groups.Items()), "group")
		body = m.groups.View()
		help = "enter open · p posts · a arrangements · m members · / filter · 1-4 tabs · q quit"
		if len(m.requestList.items) > 0 {
			groups := lipgloss.NewStyle().Height(m.groupsHeight()).MaxHeight(m.groupsHeight()).Render(body)
			body = lipgloss.JoinVertical(lipgloss.Left, groups, m.requestList.view(m.focusRequests))
			help = "tab switch to requests · " + help
			if m.focusRequests {
				help = "↑/↓ select · enter/click review · tab back to groups · q quit"
			}
		}
		if m.requestModal != nil {
			body = m.requestModal.view(m.width, max(m.height-chromeHeight, 3))
			help = m.requestModal.help()
		}
	case screenGroupMenu:
		crumb = m.scopeCrumb()
		body = m.viewGroupMenu()
		help = "↑/↓ choose · enter open · p/a/m/i shortcut · esc back"
		if len(m.group.SubGroups) > 0 {
			help = "↑/↓ choose · enter open · p/a/m/i shortcut · s subgroup · esc back"
		}
	case screenSubgroups:
		crumb = m.group.Name
		body = m.subgroups.View()
		help = "enter select · / filter · esc back"
	case screenEvents:
		when := "Upcoming"
		if m.groupFeed.past {
			when = "Past"
		}
		crumb = m.scopeCrumb() + " › Arrangements › " + when
		body, help = m.feedView(feedGroup, " in "+m.scopeCrumb())
		if m.groupFeed.detail == nil {
			help += " · p posts · m members · esc back"
		}
	case screenPosts:
		crumb = m.scopeCrumb() + " › Posts"
		body = m.feed.View()
		help = "↑/↓ select · g/G top/bottom · enter/click open · / search · a arrangements · m members · esc back"
		switch {
		case m.query != "":
			crumb += fmt.Sprintf(" › “%s” (%d)", m.query, len(m.hits))
			if len(m.hits) == 0 {
				body = dimSty.Padding(1, 2).Render("No posts in " + m.scopeCrumb() + " match “" + m.query + "”.")
			}
			help = "↑/↓ select · g/G top/bottom · enter/click open · / new search · esc back to posts"
		case len(m.posts) == 0 && m.busy == "" && m.err == nil:
			body = dimSty.Padding(1, 2).Render("No posts in " + m.scopeCrumb() + ".")
		}
		if m.searching {
			help = "enter search · esc cancel"
		}
		if m.postModal != nil {
			body = m.postModal.view(m.width, max(m.height-chromeHeight, 3))
			help = m.postModal.help()
		}
	case screenMembers:
		crumb = fmt.Sprintf("%s › Members (%d)", m.scopeCrumb(), len(m.rows))
		body = m.memberList.view()
		help = "↑/↓ select · g/G top/bottom · enter/click details · i invite · e export CSV · p posts · a arrangements · esc back"
		if m.memberModal != nil {
			body = m.memberModal.view(m.width, max(m.height-chromeHeight, 3))
			help = m.memberModal.help()
		}
	}
	if m.invite != nil {
		body = m.invite.view(m.width, m.bodyHeight())
		help = m.invite.help()
	}
	return crumb, body, help
}

func (m model) statusLine() string {
	switch {
	case m.searching:
		return m.searchInput.View()
	case m.busy != "":
		return crumbSty.Render(m.spinner.View() + " " + progressBar(m.progress, m.tick) + "  " + m.busy)
	case m.err != nil:
		return errSty.Render(display.FirstLine(m.err.Error(), m.width-2))
	case m.status != "":
		return okSty.Render(m.status)
	case m.tab == tabGroups && m.screen == screenPosts && m.query == "" && m.postsDone && len(m.posts) > 0:
		return crumbSty.Render(fmt.Sprintf("All %s loaded", display.Plural(len(m.posts), "post")))
	case m.tab == tabHome || m.tab == tabGroups && m.screen == screenEvents:
		if f := m.feedOf(m.feedForTab()); f.done && len(f.events) > 0 {
			return crumbSty.Render(fmt.Sprintf("All %s loaded", display.Plural(len(f.events), "arrangement")))
		}
	}
	return ""
}

func (m model) viewLogin() string {
	if m.autoLogin && m.busy != "" && m.challenge == "" {
		// Signing in without the user; the form appears only if that fails.
		lines := []string{titleSty.Foreground(accent).Render("Spond"), "", m.statusLine()}
		box := boxSty.Render(lipgloss.JoinVertical(lipgloss.Left, lines...))
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box)
	}
	lines := []string{titleSty.Foreground(accent).Render("Sign in to Spond"), ""}
	lines = append(lines, m.inputs[fieldEmail].View(), m.inputs[fieldPassword].View())
	if m.challenge != "" {
		lines = append(lines, "", dimSty.Render("A code was sent to "+m.phone), m.inputs[fieldCode].View())
	}
	lines = append(lines, "", m.statusLine(), helpSty.Render("tab next field · enter submit · esc quit"))

	box := boxSty.Render(lipgloss.JoinVertical(lipgloss.Left, lines...))
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box)
}

func (m model) viewGroupMenu() string {
	selected := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#FFFFFF")).Background(accent).Padding(0, 1)
	normal := lipgloss.NewStyle().Padding(0, 1)

	lines := []string{""}
	for i, item := range m.menuItems() {
		label := fmt.Sprintf("%s  %s", item.key, item.label)
		row := normal.Render(label)
		if i == m.menuCursor {
			row = selected.Render(label)
		}
		lines = append(lines, "  "+row+"  "+dimSty.Render(item.hint), "")
	}
	return strings.Join(lines, "\n")
}

var (
	cancelledTag = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#FFFFFF")).Background(errColor).Padding(0, 1)
	acceptedSty  = lipgloss.NewStyle().Foreground(okColor)
	declinedSty  = lipgloss.NewStyle().Foreground(errColor)
)

// myAnswers summarises the logged-in user's answers on a card, e.g.
// "You: Attending" or "Ola: Not answered · Kari: Attending".
func myAnswers(s spondapi.Spond, idx nameIndex) string {
	if len(s.BehalfOfIDs) == 0 {
		return ""
	}
	parts := make([]string, 0, len(s.BehalfOfIDs))
	for _, id := range s.BehalfOfIDs {
		a := s.Responses.AnswerFor(id)
		label := answerStyle(a).Render(a.String())
		if len(s.BehalfOfIDs) == 1 {
			return "You: " + label
		}
		parts = append(parts, idx.name(id)+": "+label)
	}
	return strings.Join(parts, dimSty.Render(" · "))
}

func answerStyle(a spondapi.Answer) lipgloss.Style {
	switch a {
	case spondapi.AnswerAccepted, spondapi.AnswerUnconfirmed:
		return acceptedSty.Bold(true)
	case spondapi.AnswerDeclined:
		return declinedSty.Bold(true)
	case spondapi.AnswerUnanswered:
		return lipgloss.NewStyle().Bold(true).Foreground(accent)
	}
	return dimSty
}

// formatRange renders start and end, dropping the end date when it is the
// same day and the end entirely for open-ended arrangements.
func formatRange(s spondapi.Spond) string {
	start, err := time.Parse(time.RFC3339, s.StartTimestamp)
	if err != nil {
		return s.StartTimestamp
	}
	start = start.Local()
	from := start.Format("Mon 2 Jan 2006 15:04")
	if s.OpenEnded || s.EndTimestamp == "" {
		return from
	}
	end, err := time.Parse(time.RFC3339, s.EndTimestamp)
	if err != nil {
		return from
	}
	end = end.Local()
	if end.Year() == start.Year() && end.YearDay() == start.YearDay() {
		return from + "–" + end.Format("15:04")
	}
	return from + " – " + end.Format("Mon 2 Jan 2006 15:04")
}

func formatLocation(l *spondapi.SpondLocation) string {
	if l == nil {
		return ""
	}
	parts := []string{}
	for _, p := range []string{l.Feature, l.Address, strings.TrimSpace(l.PostalCode + " " + l.Locality)} {
		if p != "" && !slices.Contains(parts, p) {
			parts = append(parts, p)
		}
	}
	return strings.Join(parts, ", ")
}

func formatCounts(s spondapi.Spond) string {
	c := s.Counts()
	parts := []string{
		acceptedSty.Render(fmt.Sprintf("✓ %d accepted", c.AcceptedCount)),
		declinedSty.Render(fmt.Sprintf("✗ %d declined", c.DeclinedCount)),
		dimSty.Render(fmt.Sprintf("? %d unanswered", c.UnansweredCount)),
	}
	if c.WaitinglistCount > 0 {
		parts = append(parts, dimSty.Render(fmt.Sprintf("%d on waiting list", c.WaitinglistCount)))
	}
	if s.MaxAccepted > 0 {
		parts = append(parts, dimSty.Render(fmt.Sprintf("max %d", s.MaxAccepted)))
	}
	return strings.Join(parts, dimSty.Render(" · "))
}

func clampLines(s string, n int) string {
	lines := strings.Split(s, "\n")
	if len(lines) <= n {
		return s
	}
	return strings.Join(lines[:n], "\n") + "\n…"
}
