package tui

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/asbjorn/spondcli/internal/spondapi"
)

// Text inputs of the invite form.
const (
	inFirst = iota
	inLast
	inEmail
	inPhone
	inDOB
	inGuardianFirst
	inGuardianLast
	inGuardianEmail
	inGuardianPhone
	inCount
)

type rowKind int

const (
	rowGroup rowKind = iota
	rowType
	rowInput
	rowSubgroup
	rowRole
	rowSubmit
)

type formRow struct {
	kind  rowKind
	input int    // for rowInput
	id    string // for rowSubgroup / rowRole
}

// inviteForm adds a new member or admin to a group (the app's
// CreateMemberViewModel / CreateAdminViewModel). Spond contacts the person by
// the email or phone given.
type inviteForm struct {
	groups    []spondapi.Group // groups the user may add people to
	groupIdx  int
	asAdmin   bool
	inputs    []textinput.Model
	subgroups map[string]bool
	roles     map[string]bool
	cursor    int
	vp        viewport.Model
	width     int
	height    int
	me        *spondapi.MyProfile
}

type inviteMsg struct {
	groupID string
	added   *spondapi.Membership
	done    string
	err     error
}

// eligibleInviteGroups are the groups where the user may add a member or an admin.
func eligibleInviteGroups(groups []spondapi.Group, me *spondapi.MyProfile) []spondapi.Group {
	out := []spondapi.Group{}
	for _, g := range groups {
		p := myPermissions(g, me)
		if p.canAdd(g, false) || p.canAdd(g, true) {
			out = append(out, g)
		}
	}
	return out
}

func newInviteForm(groups []spondapi.Group, current string, me *spondapi.MyProfile, width, height int) *inviteForm {
	specs := []struct{ prompt, placeholder string }{
		{"First name     ", ""},
		{"Last name      ", ""},
		{"Email          ", "name@example.com"},
		{"Phone          ", "+4712345678"},
		{"Date of birth  ", "YYYY-MM-DD (optional)"},
		{"First name     ", ""},
		{"Last name      ", ""},
		{"Email          ", ""},
		{"Phone          ", ""},
	}
	inputs := make([]textinput.Model, inCount)
	for i, sp := range specs {
		in := textinput.New()
		in.Prompt = sp.prompt
		in.Placeholder = sp.placeholder
		in.PromptStyle = labelSty
		in.CharLimit = 200
		inputs[i] = in
	}
	f := &inviteForm{groups: groups, inputs: inputs, vp: viewport.New(0, 0), me: me}
	for i, g := range groups {
		if g.ID == current {
			f.groupIdx = i
		}
	}
	f.resetGroup()
	f.resize(width, height)
	return f
}

func (f *inviteForm) group() spondapi.Group { return f.groups[f.groupIdx] }

func (f *inviteForm) perms() permissions { return myPermissions(f.group(), f.me) }

// resetGroup clears group-specific choices and picks an allowed type.
func (f *inviteForm) resetGroup() {
	f.subgroups = map[string]bool{}
	f.roles = map[string]bool{}
	p, g := f.perms(), f.group()
	if f.asAdmin && !p.canAdd(g, true) {
		f.asAdmin = false
	}
	if !f.asAdmin && !p.canAdd(g, false) {
		f.asAdmin = true
	}
}

func (f *inviteForm) rows() []formRow {
	rows := []formRow{{kind: rowGroup}, {kind: rowType}}
	for _, i := range []int{inFirst, inLast, inEmail, inPhone, inDOB} {
		rows = append(rows, formRow{kind: rowInput, input: i})
	}
	for _, sg := range f.group().SubGroups {
		rows = append(rows, formRow{kind: rowSubgroup, id: sg.ID})
	}
	if f.asAdmin {
		for _, r := range f.group().Roles {
			rows = append(rows, formRow{kind: rowRole, id: r.ID})
		}
	} else {
		for _, i := range []int{inGuardianFirst, inGuardianLast, inGuardianEmail, inGuardianPhone} {
			rows = append(rows, formRow{kind: rowInput, input: i})
		}
	}
	return append(rows, formRow{kind: rowSubmit})
}

func (f *inviteForm) resize(width, height int) {
	f.width = min(width, 100)
	f.height = height
	inner := max(f.width-4, 20)
	for i := range f.inputs {
		f.inputs[i].Width = max(inner-22, 10)
	}
	f.vp.Width = inner
	f.vp.Height = max(f.height-2, 3)
	f.refresh()
}

// refresh re-renders the form and keeps the cursor row on screen.
func (f *inviteForm) refresh() {
	content, line := f.render(f.vp.Width)
	f.vp.SetContent(content)
	switch {
	case line < f.vp.YOffset:
		f.vp.SetYOffset(line)
	case line >= f.vp.YOffset+f.vp.Height:
		f.vp.SetYOffset(line - f.vp.Height + 1)
	}
}

func (f *inviteForm) focusCursor() tea.Cmd {
	rows := f.rows()
	f.cursor = min(max(f.cursor, 0), len(rows)-1)
	var cmd tea.Cmd
	for i := range f.inputs {
		f.inputs[i].Blur()
	}
	if r := rows[f.cursor]; r.kind == rowInput {
		cmd = f.inputs[r.input].Focus()
	}
	f.refresh()
	return cmd
}

// render returns the form and the line of the cursor row.
func (f *inviteForm) render(width int) (string, int) {
	g := f.group()
	lines := []string{
		lipgloss.NewStyle().Bold(true).Render("Invite to a group"),
		dimSty.Render("Adds the person to the group; Spond contacts them by the email or phone given."),
		"",
	}
	cursorLine := 0
	section := ""
	for i, r := range f.rows() {
		want := ""
		switch {
		case r.kind == rowInput && r.input >= inGuardianFirst:
			want = "Guardian (optional, e.g. for a child)"
		case r.kind == rowInput:
			want = "Person"
		case r.kind == rowSubgroup:
			want = "Subgroups"
		case r.kind == rowRole:
			want = "Admin roles (choose at least one)"
		}
		if want != "" && want != section {
			lines = append(lines, "", sectionSty.Render(want))
			section = want
		}

		marker := "  "
		if i == f.cursor {
			marker = keySty.Render("› ")
			cursorLine = len(lines)
		}
		var text string
		switch r.kind {
		case rowGroup:
			text = labelSty.Render("Group          ") + "‹ " + lipgloss.NewStyle().Bold(true).Render(g.Name) + " ›"
			if len(f.groups) > 1 {
				text += dimSty.Render("  ←/→ change")
			}
		case rowType:
			kinds := []string{}
			p := f.perms()
			for _, k := range []struct {
				admin bool
				label string
			}{{false, "Member"}, {true, "Admin"}} {
				if !p.canAdd(g, k.admin) {
					continue
				}
				label := k.label
				if k.admin == f.asAdmin {
					label = lipgloss.NewStyle().Bold(true).Foreground(accent).Render("(•) " + label)
				} else {
					label = "( ) " + label
				}
				kinds = append(kinds, label)
			}
			text = labelSty.Render("Invite as      ") + strings.Join(kinds, "   ")
		case rowInput:
			text = f.inputs[r.input].View()
		case rowSubgroup:
			text = checkbox(f.subgroups[r.id]) + " " + subgroupName(g, r.id)
		case rowRole:
			text = checkbox(f.roles[r.id]) + " " + roleLabel(g, r.id)
		case rowSubmit:
			label := " Add member "
			if f.asAdmin {
				label = " Add admin "
			}
			lines = append(lines, "")
			if i == f.cursor {
				cursorLine = len(lines)
			}
			text = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#FFFFFF")).Background(accent).Render(label)
		}
		lines = append(lines, marker+text)
	}
	return lipgloss.NewStyle().Width(width).Render(strings.Join(lines, "\n")), cursorLine
}

func checkbox(on bool) string {
	if on {
		return acceptedSty.Render("[x]")
	}
	return "[ ]"
}

func subgroupName(g spondapi.Group, id string) string {
	for _, sg := range g.SubGroups {
		if sg.ID == id {
			return sg.Name
		}
	}
	return id
}

func (f *inviteForm) view(width, height int) string {
	box := modalSty.Width(f.width - 2).Height(f.height - 2).Render(f.vp.View())
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Top, box)
}

func (f *inviteForm) help() string {
	return "↑/↓ field · ←/→ choose · space toggle · enter next/submit · esc cancel"
}

// request validates the form and builds the request.
func (f *inviteForm) request() (string, map[string]any, string, error) {
	g := f.group()
	if !f.perms().canAdd(g, f.asAdmin) {
		return "", nil, "", errors.New("you may not add this kind of user to this group")
	}
	p, err := person{
		FirstName:   f.inputs[inFirst].Value(),
		LastName:    f.inputs[inLast].Value(),
		Email:       f.inputs[inEmail].Value(),
		PhoneNumber: f.inputs[inPhone].Value(),
		DateOfBirth: f.inputs[inDOB].Value(),
	}.normalize(true)
	if err != nil {
		return "", nil, "", err
	}

	subgroups := []string{}
	for _, sg := range g.SubGroups {
		if f.subgroups[sg.ID] {
			subgroups = append(subgroups, sg.ID)
		}
	}
	roles := []string{}
	for _, r := range g.Roles {
		if f.roles[r.ID] {
			roles = append(roles, r.ID)
		}
	}
	if f.asAdmin && len(roles) == 0 {
		return "", nil, "", errors.New("choose at least one admin role")
	}

	guardians := []spondapi.GuardianInput{}
	if !f.asAdmin {
		gp, err := person{
			FirstName:   f.inputs[inGuardianFirst].Value(),
			LastName:    f.inputs[inGuardianLast].Value(),
			Email:       f.inputs[inGuardianEmail].Value(),
			PhoneNumber: f.inputs[inGuardianPhone].Value(),
		}.normalize(false)
		if err != nil {
			return "", nil, "", fmt.Errorf("guardian: %w", err)
		}
		if gd := gp.guardian(); !gd.Empty() {
			if gp.FirstName == "" || gp.LastName == "" {
				return "", nil, "", errors.New("guardian: first and last name are required")
			}
			guardians = append(guardians, gd)
		}
	}

	kind := "member"
	if f.asAdmin {
		kind = "admin"
	}
	done := fmt.Sprintf("Added %s %s to %s as %s", p.FirstName, p.LastName, g.Name, kind)
	if p.Email == "" && p.PhoneNumber == "" {
		done += " (no email or phone given)"
	}
	return g.ID, newMembershipBody(p, f.asAdmin, subgroups, roles, guardians), done, nil
}

// openInvite opens the invite form, or explains why it can't.
func (m *model) openInvite() tea.Cmd {
	groups := eligibleInviteGroups(m.allGroups, m.me)
	if len(groups) == 0 {
		m.err = errors.New("you can't add people to any of your groups (needs admin rights, and the group must allow admins to add members)")
		return nil
	}
	m.err, m.status = nil, ""
	m.invite = newInviteForm(groups, m.group.ID, m.me, m.width, max(m.height-chromeHeight, 3))
	return m.invite.focusCursor()
}

func (m model) updateInvite(msg tea.Msg) (tea.Model, tea.Cmd) {
	f := m.invite
	key, isKey := msg.(tea.KeyMsg)
	if !isKey {
		var cmd tea.Cmd
		f.vp, cmd = f.vp.Update(msg)
		return m, cmd
	}
	if m.busy != "" {
		return m, nil
	}
	rows := f.rows()
	r := rows[f.cursor]

	switch key.String() {
	case "esc":
		m.invite = nil
		return m, nil
	case "up", "shift+tab":
		f.cursor--
		return m, f.focusCursor()
	case "down", "tab":
		f.cursor++
		return m, f.focusCursor()
	case "left", "right":
		switch r.kind {
		case rowGroup:
			step := 1
			if key.String() == "left" {
				step = len(f.groups) - 1
			}
			f.groupIdx = (f.groupIdx + step) % len(f.groups)
			f.resetGroup()
			return m, f.focusCursor()
		case rowType:
			if f.perms().canAdd(f.group(), !f.asAdmin) {
				f.asAdmin = !f.asAdmin
			}
			return m, f.focusCursor()
		}
	case " ":
		switch r.kind {
		case rowSubgroup:
			f.subgroups[r.id] = !f.subgroups[r.id]
			f.refresh()
			return m, nil
		case rowRole:
			f.roles[r.id] = !f.roles[r.id]
			f.refresh()
			return m, nil
		case rowType:
			if f.perms().canAdd(f.group(), !f.asAdmin) {
				f.asAdmin = !f.asAdmin
			}
			return m, f.focusCursor()
		}
	case "enter":
		if r.kind != rowSubmit {
			f.cursor++
			return m, f.focusCursor()
		}
		groupID, body, done, err := f.request()
		if err != nil {
			m.err = err
			return m, nil
		}
		ctx, c := m.ctx, m.client
		return m, m.startBusy("Adding…", func() tea.Msg {
			added, err := c.SaveMembership(ctx, groupID, body)
			return inviteMsg{groupID: groupID, added: added, done: done, err: err}
		})
	}

	if r.kind == rowInput {
		var cmd tea.Cmd
		f.inputs[r.input], cmd = f.inputs[r.input].Update(msg)
		f.refresh()
		return m, cmd
	}
	return m, nil
}

func (m model) applyInvite(msg inviteMsg) (tea.Model, tea.Cmd) {
	m.busy = ""
	if msg.err != nil {
		m.err = msg.err
		return m, nil
	}
	m.invite = nil
	m.status = msg.done
	if msg.added == nil {
		return m, nil
	}
	for i := range m.allGroups {
		if m.allGroups[i].ID == msg.groupID {
			m.allGroups[i].Members = append(m.allGroups[i].Members, *msg.added)
		}
	}
	if m.group.ID == msg.groupID {
		m.group.Members = append(m.group.Members, *msg.added)
		if m.screen == screenMembers && !slices.ContainsFunc(m.allMembers, func(x spondapi.Membership) bool { return x.ID == msg.added.ID }) {
			m.allMembers = append(m.allMembers, *msg.added)
			m.rebuildMemberRows()
		}
	}
	return m, nil
}
