package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/asbjorn/spondcli/internal/spondapi"
)

// pendingRequest is a join request together with its group.
type pendingRequest struct {
	group spondapi.Group
	req   spondapi.JoinRequest
}

// pendingRequests lists the join requests the user may handle: managing
// requests needs the members permission (GroupMembersDataState.canManageRequests).
func pendingRequests(groups []spondapi.Group, me *spondapi.MyProfile) []pendingRequest {
	out := []pendingRequest{}
	for _, g := range groups {
		if !myPermissions(g, me).manageMembers {
			continue
		}
		for _, r := range g.MembershipRequests {
			out = append(out, pendingRequest{group: g, req: r})
		}
	}
	return out
}

func (p pendingRequest) kind() string {
	if p.req.GroupMembershipID != "" {
		return "Existing member"
	}
	return "New member"
}

// existingName is the name of the membership an "existing member" request
// claims, if loaded.
func (p pendingRequest) existingName() string {
	for _, m := range p.group.Members {
		if m.ID == p.req.GroupMembershipID {
			return m.Name()
		}
	}
	return p.req.GroupMembershipID
}

// requestList is the pending requests pane on the main screen.
type requestList struct {
	items  []pendingRequest
	cursor int
	offset int
	width  int
	height int // total lines: title, header, separator, rows
}

const requestListHeader = 3

func (l *requestList) setItems(items []pendingRequest) {
	l.items = items
	l.cursor = min(l.cursor, max(len(items)-1, 0))
	l.ensureVisible()
}

func (l *requestList) visibleRows() int { return max(l.height-requestListHeader, 1) }

func (l *requestList) move(n int) {
	if len(l.items) == 0 {
		return
	}
	l.cursor = min(max(l.cursor+n, 0), len(l.items)-1)
	l.ensureVisible()
}

func (l *requestList) ensureVisible() {
	v := l.visibleRows()
	switch {
	case l.cursor < l.offset:
		l.offset = l.cursor
	case l.cursor >= l.offset+v:
		l.offset = l.cursor - v + 1
	}
	l.offset = min(max(l.offset, 0), max(len(l.items)-v, 0))
}

func (l requestList) rowAt(line int) (int, bool) {
	i := l.offset + line - requestListHeader
	if line < requestListHeader || i >= len(l.items) || i >= l.offset+l.visibleRows() {
		return 0, false
	}
	return i, true
}

func (l requestList) view(focused bool) string {
	fracs := []float64{0.20, 0.20, 0.22, 0.13, 0.10, 0.15}
	usable := max(l.width-len(fracs)-1, len(fracs)*6)
	widths := make([]int, len(fracs))
	for i, f := range fracs {
		widths[i] = max(int(float64(usable)*f), 6)
	}
	line := func(cells ...string) string {
		parts := make([]string, len(cells))
		for i, c := range cells {
			parts[i] = fit(c, widths[i])
		}
		return fit(" "+strings.Join(parts, " "), l.width)
	}

	title := headerSty.Render(fmt.Sprintf("Pending join requests (%d)", len(l.items)))
	if !focused {
		title += dimSty.Render("  tab to select")
	}
	lines := []string{
		title,
		listHeaderSty.Render(line("Group", "Name", "Email", "Phone", "Born", "Type")),
		dimSty.Render(strings.Repeat("─", max(l.width, 1))),
	}
	end := min(l.offset+l.visibleRows(), len(l.items))
	for i := l.offset; i < end; i++ {
		p := l.items[i]
		pr := p.req.Profile
		if pr == nil {
			pr = &spondapi.RequestPerson{}
		}
		row := line(p.group.Name, pr.Name(), pr.Email, pr.PhoneNumber, p.req.DateOfBirth, p.kind())
		if focused && i == l.cursor {
			row = listSelectedSty.Render(row)
		}
		lines = append(lines, row)
	}
	return strings.Join(lines, "\n")
}

type requestMode int

const (
	requestView requestMode = iota
	requestApprove
	requestConfirmReject
)

// requestModal shows one join request and lets the user approve or reject it.
type requestModal struct {
	p      pendingRequest
	vp     viewport.Model
	mode   requestMode
	check  checklist
	width  int
	height int
}

type requestOpMsg struct {
	groupID   string
	requestID string
	approved  *spondapi.Membership
	done      string
	err       error
}

func newRequestModal(p pendingRequest, width, height int) *requestModal {
	d := &requestModal{p: p, vp: viewport.New(0, 0)}
	d.resize(width, height)
	return d
}

func (d *requestModal) resize(width, height int) {
	d.width = min(width, 110)
	d.height = height
	inner := max(d.width-4, 20)
	d.vp.Width = inner
	d.vp.Height = max(d.height-2-lipgloss.Height(d.header(inner)), 3)
	d.vp.SetContent(d.body(inner))
}

func (d *requestModal) header(width int) string {
	pr := d.p.req.Profile
	lines := []string{
		lipgloss.NewStyle().Bold(true).Width(width).Render(pr.Name()),
		authorSty.Render("Wants to join "+d.p.group.Name) + dimSty.Render("  ·  "+d.p.kind()),
		"",
	}
	switch d.mode {
	case requestApprove:
		lines = append(lines, d.check.view(), "", dimSty.Render("enter approves into the ticked subgroups (none is fine)"))
	case requestConfirmReject:
		lines = append(lines,
			declinedSty.Bold(true).Render("Reject the request from "+pr.Name()+"?"),
			keySty.Render("[y]")+" Reject   "+keySty.Render("[n]")+" Cancel")
	default:
		lines = append(lines, keySty.Render("[a]")+" Approve   "+keySty.Render("[x]")+" Reject")
	}
	lines = append(lines, dimSty.Render(strings.Repeat("─", width)))
	return strings.Join(lines, "\n")
}

func (d *requestModal) body(width int) string {
	valueSty := lipgloss.NewStyle().Width(max(width-labelWidth-1, 10))
	var b strings.Builder
	for i, sec := range requestSections(d.p) {
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString(sectionSty.Render(sec.title) + "\n")
		for _, f := range sec.fields {
			value := dimSty.Render("—")
			if f.value != "" {
				value = valueSty.Render(f.value)
			}
			b.WriteString(lipgloss.JoinHorizontal(lipgloss.Top, labelSty.Width(labelWidth).Render(f.label), " ", value) + "\n")
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

func requestSections(p pendingRequest) []section {
	r := p.req
	pr := r.Profile
	if pr == nil {
		pr = &spondapi.RequestPerson{}
	}
	existing := ""
	if r.GroupMembershipID != "" {
		existing = p.existingName()
	}
	sections := []section{
		{"Request", []field{
			{"Group", p.group.Name},
			{"Type", p.kind()},
			{"Claims membership", existing},
		}},
		{"Person", []field{
			{"Name", pr.Name()},
			{"Email", pr.Email},
			{"Phone", pr.PhoneNumber},
			{"Date of birth", r.DateOfBirth},
			{"Gender", genderLabel(r.Gender)},
			{"Country", r.Country},
			{"Nationality", r.Nationality},
			{"Address", strings.Join(r.Address, "\n")},
			{"Sent the request", yesNo(pr.Creator)},
		}},
	}
	guardians := []field{}
	for i, g := range r.Guardians {
		contact := []string{}
		for _, c := range []string{g.Email, g.PhoneNumber} {
			if c != "" {
				contact = append(contact, c)
			}
		}
		value := g.Name()
		if len(contact) > 0 {
			value += " (" + strings.Join(contact, ", ") + ")"
		}
		if g.Creator {
			value += " · sent the request"
		}
		guardians = append(guardians, field{fmt.Sprintf("Guardian %d", i+1), value})
	}
	if len(guardians) == 0 {
		guardians = []field{{"Guardians", ""}}
	}
	sections = append(sections, section{"Guardians", guardians})

	if len(r.FieldValues) > 0 {
		names := map[string]string{}
		for _, d := range p.group.FieldDefs {
			names[d.ID] = d.Name
		}
		custom := []field{}
		for _, d := range p.group.FieldDefs {
			if v, ok := r.FieldValues[d.ID]; ok {
				custom = append(custom, field{d.Name, fieldValue(v)})
			}
		}
		for id, v := range r.FieldValues {
			if _, ok := names[id]; !ok {
				custom = append(custom, field{id, fieldValue(v)})
			}
		}
		sections = append(sections, section{"Custom fields", custom})
	}
	sections = append(sections, section{"Ids", []field{{"Request id", r.ID}, {"Profile id", pr.ID}}})
	return sections
}

func (d *requestModal) view(width, height int) string {
	inner := max(d.width-4, 20)
	content := lipgloss.JoinVertical(lipgloss.Left, d.header(inner), d.vp.View())
	box := modalSty.Width(d.width - 2).Height(d.height - 2).Render(content)
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Top, box)
}

func (d *requestModal) help() string {
	switch d.mode {
	case requestApprove:
		return "↑/↓ move · space toggle · enter approve · esc cancel"
	case requestConfirmReject:
		return "y reject · n/esc cancel"
	}
	return "a approve · x reject · ↑/↓ scroll · esc close"
}

// Main-screen integration.

func (m *model) refreshRequests() {
	m.requestList.setItems(pendingRequests(m.allGroups, m.me))
	if len(m.requestList.items) == 0 {
		m.focusRequests = false
	}
	m.resize()
}

func (m *model) openRequest() {
	if len(m.requestList.items) == 0 {
		return
	}
	m.err, m.status = nil, ""
	m.requestModal = newRequestModal(m.requestList.items[m.requestList.cursor], m.width, max(m.height-chromeHeight, 3))
}

func (m model) updateRequestsPane(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "tab", "esc":
			m.focusRequests = false
		case "up", "k":
			m.requestList.move(-1)
		case "down", "j":
			m.requestList.move(1)
		case "enter":
			m.openRequest()
		case "q":
			return m, tea.Quit
		}
	}
	return m, nil
}

// requestsTop is the first screen line of the requests pane.
func (m model) requestsTop() int {
	return headerLines + m.groupsHeight()
}

// groupsHeight is the height of the group list when the requests pane shows.
func (m model) groupsHeight() int {
	body := max(m.height-chromeHeight, 3)
	if len(m.requestList.items) == 0 {
		return body
	}
	return body - m.requestList.height
}

func (m model) updateRequestModal(msg tea.Msg) (tea.Model, tea.Cmd) {
	d := m.requestModal
	key, isKey := msg.(tea.KeyMsg)
	if !isKey {
		var cmd tea.Cmd
		d.vp, cmd = d.vp.Update(msg)
		return m, cmd
	}
	if m.busy != "" {
		return m, nil
	}

	switch d.mode {
	case requestApprove:
		switch key.String() {
		case "esc":
			d.mode = requestView
			d.resize(d.width, d.height)
		case "enter":
			return m, m.approveCmd(d.check.selected())
		default:
			if d.check.update(key) {
				d.resize(d.width, d.height)
			}
		}
		return m, nil
	case requestConfirmReject:
		switch key.String() {
		case "y":
			d.mode = requestView
			return m, m.rejectCmd()
		case "n", "esc":
			d.mode = requestView
			d.resize(d.width, d.height)
		}
		return m, nil
	}

	switch key.String() {
	case "esc", "q", "backspace":
		m.requestModal = nil
		return m, nil
	case "a":
		items := make([]checkItem, 0, len(d.p.group.SubGroups))
		for _, sg := range d.p.group.SubGroups {
			items = append(items, checkItem{id: sg.ID, label: sg.Name})
		}
		d.check = newChecklist("Approve into subgroups", items)
		d.mode = requestApprove
		d.resize(d.width, d.height)
		return m, nil
	case "x":
		d.mode = requestConfirmReject
		d.resize(d.width, d.height)
		return m, nil
	}
	var cmd tea.Cmd
	d.vp, cmd = d.vp.Update(msg)
	return m, cmd
}

func (m *model) approveCmd(subgroups []string) tea.Cmd {
	ctx, c, p := m.ctx, m.client, m.requestModal.p
	return m.startBusy("Approving…", func() tea.Msg {
		added, err := c.ApproveRequest(ctx, p.group.ID, p.req.ID, subgroups)
		return requestOpMsg{groupID: p.group.ID, requestID: p.req.ID, approved: added,
			done: "Approved " + p.req.Profile.Name() + " into " + p.group.Name, err: err}
	})
}

func (m *model) rejectCmd() tea.Cmd {
	ctx, c, p := m.ctx, m.client, m.requestModal.p
	return m.startBusy("Rejecting…", func() tea.Msg {
		err := c.RejectRequest(ctx, p.group.ID, p.req.ID)
		return requestOpMsg{groupID: p.group.ID, requestID: p.req.ID,
			done: "Rejected the request from " + p.req.Profile.Name(), err: err}
	})
}

// applyRequestOp removes a handled request and adds an approved member.
func (m model) applyRequestOp(msg requestOpMsg) (tea.Model, tea.Cmd) {
	m.busy = ""
	if msg.err != nil {
		m.err = msg.err
		return m, nil
	}
	update := func(g *spondapi.Group) {
		reqs := []spondapi.JoinRequest{}
		for _, r := range g.MembershipRequests {
			if r.ID != msg.requestID {
				reqs = append(reqs, r)
			}
		}
		g.MembershipRequests = reqs
		if msg.approved == nil {
			return
		}
		replaced := false
		for i := range g.Members {
			if g.Members[i].ID == msg.approved.ID {
				g.Members[i], replaced = *msg.approved, true
			}
		}
		if !replaced {
			g.Members = append(g.Members, *msg.approved)
		}
	}
	for i := range m.allGroups {
		if m.allGroups[i].ID == msg.groupID {
			update(&m.allGroups[i])
		}
	}
	if m.group.ID == msg.groupID {
		update(&m.group)
	}
	m.requestModal = nil
	m.status = msg.done
	m.refreshRequests()
	return m, m.syncGroupItems()
}

// syncGroupItems refreshes the group list after local changes.
func (m *model) syncGroupItems() tea.Cmd {
	items := make([]list.Item, 0, len(m.allGroups))
	for _, g := range m.allGroups {
		items = append(items, groupItem{g: g})
	}
	return m.groups.SetItems(items)
}
