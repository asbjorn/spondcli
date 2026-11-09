package tui

import (
	"slices"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/asbjorn/spondcli/internal/spondapi"
)

type memberMode int

const (
	modeView memberMode = iota
	modeSubgroups
	modeRoles
	modeConfirmDelete
	modeConfirmMember // toggle member status
	modeGuardian
	modeEdit
)

// memberModal shows everything about one member and, with admin rights, the
// actions the app offers for it.
type memberModal struct {
	m      spondapi.Membership
	actor  actor
	vp     viewport.Model
	mode   memberMode
	check  checklist
	inputs []textinput.Model // guardian form: first, last, email, phone
	edit   *memberEditForm   // details form in modeEdit
	focus  int
	width  int
	height int
}

type memberOpMsg struct {
	gen          int
	membershipID string
	updated      *spondapi.Membership // nil when deleted
	deleted      bool
	done         string // status text on success
	err          error
}

func newMemberModal(m spondapi.Membership, a actor, width, height int) *memberModal {
	d := &memberModal{m: m, actor: a, vp: viewport.New(0, 0)}
	d.resize(width, height)
	return d
}

func roleLabel(g spondapi.Group, id string) string {
	for i, r := range g.Roles {
		if r.ID != id {
			continue
		}
		name := r.Name
		if name == "" {
			name = "Admin"
		}
		if i == 0 {
			name += " (main admin role)"
		}
		return name
	}
	return id
}

func (d *memberModal) resize(width, height int) {
	d.width = min(width, 110)
	d.height = height
	inner := max(d.width-4, 20)
	for i := range d.inputs {
		d.inputs[i].Width = max(inner-24, 10)
	}
	d.vp.Width = inner
	d.vp.Height = max(d.height-2-lipgloss.Height(d.header(inner)), 3)
	d.vp.SetContent(d.body(inner))
	if d.edit != nil {
		d.edit.resize(inner, d.vp.Height)
	}
}

func (d *memberModal) update(m spondapi.Membership) {
	d.m = m
	d.mode = modeView
	d.edit = nil
	d.resize(d.width, d.height)
}

func (d *memberModal) view(width, height int) string {
	inner := max(d.width-4, 20)
	body := d.vp.View()
	if d.mode == modeEdit && d.edit != nil {
		body = d.edit.vp.View()
	}
	content := lipgloss.JoinVertical(lipgloss.Left, d.header(inner), body)
	box := modalSty.Width(d.width - 2).Height(d.height - 2).Render(content)
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Top, box)
}

func (d *memberModal) help() string {
	switch d.mode {
	case modeEdit:
		return "↑/↓ field · ←/→ choose · enter next/save · esc cancel"
	case modeSubgroups, modeRoles:
		return "↑/↓ move · space toggle · enter save · esc cancel"
	case modeConfirmDelete, modeConfirmMember:
		return "y confirm · n/esc cancel"
	case modeGuardian:
		return "tab next field · enter save · esc cancel"
	}
	return "↑/↓ scroll · esc close"
}

// actions lists the admin actions available for this member, as key/label.
func (d *memberModal) actions() [][2]string {
	a, m := d.actor, d.m
	out := [][2]string{}
	if a.checkEdit(m) == nil {
		out = append(out, [2]string{"e", "Edit details"})
	}
	if a.checkSubgroups(m) == nil {
		out = append(out, [2]string{"s", "Subgroups"})
	}
	if a.perms.manageAdmins && len(a.group.Roles) > 0 && !a.isSelf(m) {
		out = append(out, [2]string{"r", "Admin roles"})
	}
	if a.checkRespondent(m, true) == nil {
		out = append(out, [2]string{"m", "Make member"})
	}
	if a.checkRespondent(m, false) == nil {
		out = append(out, [2]string{"m", "Remove member status (keep admin)"})
	}
	if a.checkGuardian(m) == nil {
		out = append(out, [2]string{"g", "Add guardian"})
	}
	if a.perms.manageMembers && !a.isSelf(m) {
		out = append(out, [2]string{"d", "Remove from group"})
	}
	return out
}

func (d *memberModal) header(width int) string {
	m := d.m
	lines := []string{
		lipgloss.NewStyle().Bold(true).Width(width).Render(m.Name()),
		authorSty.Render(memberKind(m)) + dimSty.Render("  ·  "+d.actor.group.Name),
		"",
	}

	switch d.mode {
	case modeSubgroups, modeRoles:
		lines = append(lines, d.check.view())
	case modeConfirmDelete:
		lines = append(lines,
			declinedSty.Bold(true).Render("Remove "+m.Name()+" from "+d.actor.group.Name+"?"),
			dimSty.Render("This deletes the membership (member and admin) and cannot be undone."),
			keySty.Render("[y]")+" Remove   "+keySty.Render("[n]")+" Cancel")
	case modeConfirmMember:
		q := "Make " + m.Name() + " a member of " + d.actor.group.Name + "?"
		if m.Respondent {
			q = "Remove member status from " + m.Name() + "? They stay an admin."
		}
		lines = append(lines, lipgloss.NewStyle().Bold(true).Render(q),
			keySty.Render("[y]")+" Confirm   "+keySty.Render("[n]")+" Cancel")
	case modeEdit:
		lines = append(lines, dimSty.Render("Editing details"))
	case modeGuardian:
		lines = append(lines, sectionSty.Render("Add guardian for "+m.Name()))
		for _, in := range d.inputs {
			lines = append(lines, in.View())
		}
		lines = append(lines, dimSty.Render("Phone in international format, e.g. +4712345678."))
	default:
		acts := d.actions()
		if len(acts) == 0 {
			lines = append(lines, dimSty.Render("You have no admin rights for this member in this group."))
		} else {
			parts := make([]string, 0, len(acts))
			for _, a := range acts {
				parts = append(parts, keySty.Render("["+a[0]+"]")+" "+a[1])
			}
			lines = append(lines, sectionSty.Render("Admin actions"), lipgloss.NewStyle().Width(width).Render(strings.Join(parts, "   ")))
		}
	}
	lines = append(lines, dimSty.Render(strings.Repeat("─", width)))
	return strings.Join(lines, "\n")
}

func (d *memberModal) body(width int) string {
	valueWidth := max(width-labelWidth-1, 10)
	valueSty := lipgloss.NewStyle().Width(valueWidth)
	var b strings.Builder
	for i, sec := range memberSections(d.m, d.actor.group) {
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

// openMember opens the modal for the selected member row.
func (m *model) openMember() {
	r, ok := m.memberList.selected()
	if !ok {
		return
	}
	m.err, m.status = nil, ""
	m.memberModal = newMemberModal(r.Member, m.actor(), m.width, max(m.height-chromeHeight, 3))
}

func (m model) actor() actor {
	return actor{group: m.group, groups: m.allGroups, me: m.me, perms: myPermissions(m.group, m.me)}
}

func (m model) updateMemberModal(msg tea.Msg) (tea.Model, tea.Cmd) {
	d := m.memberModal
	key, isKey := msg.(tea.KeyMsg)
	if !isKey {
		var cmd tea.Cmd
		d.vp, cmd = d.vp.Update(msg)
		return m, cmd
	}
	if m.busy != "" && d.mode != modeView {
		return m, nil
	}

	switch d.mode {
	case modeEdit:
		if key.String() == "esc" {
			d.mode, d.edit = modeView, nil
			d.resize(d.width, d.height)
			return m, nil
		}
		cmd, save := d.edit.update(key)
		if !save {
			return m, cmd
		}
		body, err := d.edit.body()
		if err != nil {
			m.err = err
			return m, nil
		}
		if !d.edit.changed(body) {
			m.status = "Nothing changed"
			return m, nil
		}
		return m, m.saveMemberCmd(body, "Saved details for "+d.m.Name())
	case modeSubgroups, modeRoles:
		switch key.String() {
		case "esc":
			d.mode = modeView
			d.resize(d.width, d.height)
		case "enter":
			return m, m.saveChecklist()
		default:
			if d.check.update(key) {
				d.resize(d.width, d.height)
			}
		}
		return m, nil

	case modeConfirmDelete, modeConfirmMember:
		switch key.String() {
		case "y":
			mode := d.mode
			d.mode = modeView
			d.resize(d.width, d.height)
			if mode == modeConfirmDelete {
				return m, m.deleteMemberCmd()
			}
			return m, m.saveMemberCmd(respondentChange(d.m.ID, !d.m.Respondent), memberStatusDone(d.m))
		case "n", "esc":
			d.mode = modeView
			d.resize(d.width, d.height)
		}
		return m, nil

	case modeGuardian:
		switch key.String() {
		case "esc":
			d.mode = modeView
			d.resize(d.width, d.height)
			return m, nil
		case "tab", "down":
			d.setFocus((d.focus + 1) % len(d.inputs))
			return m, nil
		case "shift+tab", "up":
			d.setFocus((d.focus + len(d.inputs) - 1) % len(d.inputs))
			return m, nil
		case "enter":
			if d.focus < len(d.inputs)-1 {
				d.setFocus(d.focus + 1)
				return m, nil
			}
			return m, m.addGuardianCmd()
		}
		var cmd tea.Cmd
		d.inputs[d.focus], cmd = d.inputs[d.focus].Update(msg)
		return m, cmd
	}

	a := d.actor
	switch key.String() {
	case "esc", "q", "backspace":
		m.memberModal = nil
		m.err, m.status = nil, ""
		return m, nil
	case "e":
		if err := a.checkEdit(d.m); err != nil {
			m.err = err
			return m, nil
		}
		d.mode = modeEdit
		d.edit = newMemberEditForm(d.m, a.group, max(d.width-4, 20), d.vp.Height)
		d.resize(d.width, d.height)
		return m, d.edit.focusCursor()
	case "s":
		if err := a.checkSubgroups(d.m); err != nil {
			m.err = err
			return m, nil
		}
		items := make([]checkItem, 0, len(a.group.SubGroups))
		for _, sg := range a.group.SubGroups {
			items = append(items, checkItem{id: sg.ID, label: sg.Name, on: slices.Contains(d.m.SubGroups, sg.ID)})
		}
		d.check = newChecklist("Subgroups for "+d.m.Name(), items)
		d.mode = modeSubgroups
		d.resize(d.width, d.height)
		return m, nil
	case "r":
		if !a.perms.manageAdmins {
			m.err = errNoAdminRights
			return m, nil
		}
		if a.isSelf(d.m) {
			m.err = errSelfRoles
			return m, nil
		}
		items := make([]checkItem, 0, len(a.group.Roles))
		for _, r := range a.group.Roles {
			items = append(items, checkItem{id: r.ID, label: roleLabel(a.group, r.ID), on: slices.Contains(d.m.Roles, r.ID)})
		}
		d.check = newChecklist("Admin roles for "+d.m.Name(), items)
		d.mode = modeRoles
		d.resize(d.width, d.height)
		return m, nil
	case "m":
		if err := a.checkRespondent(d.m, !d.m.Respondent); err != nil {
			m.err = err
			return m, nil
		}
		d.mode = modeConfirmMember
		d.resize(d.width, d.height)
		return m, nil
	case "g":
		if err := a.checkGuardian(d.m); err != nil {
			m.err = err
			return m, nil
		}
		d.inputs = guardianInputs()
		d.focus = 0
		d.inputs[0].Focus()
		d.mode = modeGuardian
		d.resize(d.width, d.height)
		return m, textinput.Blink
	case "d":
		if err := a.checkDelete(d.m); err != nil {
			m.err = err
			return m, nil
		}
		d.mode = modeConfirmDelete
		d.resize(d.width, d.height)
		return m, nil
	}

	var cmd tea.Cmd
	d.vp, cmd = d.vp.Update(msg)
	return m, cmd
}

func (d *memberModal) setFocus(i int) {
	d.inputs[d.focus].Blur()
	d.focus = i
	d.inputs[i].Focus()
}

func guardianInputs() []textinput.Model {
	specs := []struct{ prompt, placeholder string }{
		{"First name  ", ""},
		{"Last name   ", ""},
		{"Email       ", "parent@example.com"},
		{"Phone       ", "+4712345678"},
	}
	inputs := make([]textinput.Model, len(specs))
	for i, sp := range specs {
		in := textinput.New()
		in.Prompt = sp.prompt
		in.Placeholder = sp.placeholder
		in.PromptStyle = labelSty
		in.CharLimit = 200
		inputs[i] = in
	}
	return inputs
}

func memberStatusDone(m spondapi.Membership) string {
	if m.Respondent {
		return m.Name() + " is no longer a member (still admin)"
	}
	return m.Name() + " is now a member"
}

// saveChecklist saves the subgroup or role selection.
func (m *model) saveChecklist() tea.Cmd {
	d := m.memberModal
	ids := d.check.selected()
	if d.mode == modeSubgroups {
		if slices.Equal(sortedCopy(ids), sortedCopy(d.m.SubGroups)) {
			d.mode = modeView
			d.resize(d.width, d.height)
			return nil
		}
		return m.saveMemberCmd(subgroupsChange(d.m.ID, ids), "Saved subgroups for "+d.m.Name())
	}
	if err := d.actor.checkRoles(d.m, ids); err != nil {
		m.err = err
		return nil
	}
	if slices.Equal(sortedCopy(ids), sortedCopy(d.m.Roles)) {
		d.mode = modeView
		d.resize(d.width, d.height)
		return nil
	}
	// Keep the group's role order, as the app does.
	ordered := []string{}
	for _, r := range d.actor.group.Roles {
		if slices.Contains(ids, r.ID) {
			ordered = append(ordered, r.ID)
		}
	}
	return m.saveMemberCmd(rolesChange(d.m.ID, ordered), "Saved admin roles for "+d.m.Name())
}

func sortedCopy(ids []string) []string {
	out := slices.Clone(ids)
	slices.Sort(out)
	return out
}

func (m *model) saveMemberCmd(body map[string]any, done string) tea.Cmd {
	ctx, c, gen, groupID, id := m.ctx, m.client, m.gen, m.group.ID, m.memberModal.m.ID
	return m.startBusy("Saving…", func() tea.Msg {
		updated, err := c.SaveMembership(ctx, groupID, body)
		return memberOpMsg{gen: gen, membershipID: id, updated: updated, done: done, err: err}
	})
}

func (m *model) deleteMemberCmd() tea.Cmd {
	ctx, c, gen, groupID, mem := m.ctx, m.client, m.gen, m.group.ID, m.memberModal.m
	group := m.group.Name
	return m.startBusy("Removing…", func() tea.Msg {
		err := c.DeleteMembership(ctx, groupID, mem.ID)
		return memberOpMsg{gen: gen, membershipID: mem.ID, deleted: err == nil, done: "Removed " + mem.Name() + " from " + group, err: err}
	})
}

func (m *model) addGuardianCmd() tea.Cmd {
	d := m.memberModal
	p, err := person{
		FirstName:   d.inputs[0].Value(),
		LastName:    d.inputs[1].Value(),
		Email:       d.inputs[2].Value(),
		PhoneNumber: d.inputs[3].Value(),
	}.normalize(true)
	if err != nil {
		m.err = err
		return nil
	}
	ctx, c, gen, groupID, id := m.ctx, m.client, m.gen, m.group.ID, d.m.ID
	name := d.m.Name()
	return m.startBusy("Adding guardian…", func() tea.Msg {
		updated, err := c.AddGuardian(ctx, groupID, id, p.guardian())
		return memberOpMsg{gen: gen, membershipID: id, updated: updated, done: "Added " + p.FirstName + " " + p.LastName + " as guardian of " + name, err: err}
	})
}

// applyMemberOp stores the result of an admin action.
func (m model) applyMemberOp(msg memberOpMsg) (tea.Model, tea.Cmd) {
	if msg.gen != m.gen {
		return m, nil
	}
	m.busy = ""
	if msg.err != nil {
		m.err = msg.err
		return m, nil
	}

	replace := func(list []spondapi.Membership) []spondapi.Membership {
		out := make([]spondapi.Membership, 0, len(list))
		for _, x := range list {
			switch {
			case x.ID != msg.membershipID:
				out = append(out, x)
			case !msg.deleted && msg.updated != nil:
				out = append(out, *msg.updated)
			}
		}
		return out
	}
	m.allMembers = replace(m.allMembers)
	m.group.Members = replace(m.group.Members)
	for i := range m.allGroups {
		if m.allGroups[i].ID == m.group.ID {
			m.allGroups[i].Members = m.group.Members
		}
	}
	m.rebuildMemberRows()

	switch {
	case msg.deleted:
		m.memberModal = nil
	case m.memberModal != nil && msg.updated != nil:
		m.memberModal.actor = m.actor()
		m.memberModal.update(*msg.updated)
	}
	m.status = msg.done
	return m, nil
}
