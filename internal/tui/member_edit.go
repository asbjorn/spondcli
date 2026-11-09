package tui

import (
	"fmt"
	"maps"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/asbjorn/spondcli/internal/spondapi"
)

// fieldEditable mirrors the app's edit form for an admin
// (EditMembershipItemsKt + MemberFieldPermission.isEditable): required fields
// are always shown, locked ones never, and CLUB_ADMIN_ONLY fields are not
// editable by group admins. Multiple-choice fields are left read-only here:
// their value encoding could not be confirmed from the app.
func fieldEditable(d spondapi.FieldDef) (bool, string) {
	switch {
	case d.Type == "MULTIPLE_OPTION":
		return false, "multiple choice: edit in the Spond app"
	case d.Required:
		return true, ""
	case d.Locked:
		return false, "locked"
	case d.Permission == "CLUB_ADMIN_ONLY":
		return false, "club admins only"
	}
	return true, ""
}

// Edit form rows.
const (
	editFirst = iota
	editLast
	editEmail
	editPhone
	editFixed // number of fixed text rows
)

type editRow struct {
	input  int                // index into inputs, -1 for none
	def    *spondapi.FieldDef // custom field, nil for fixed rows
	option int                // SINGLE_OPTION: 0 = none, i = Options[i-1]
	reason string             // why a field is read-only
	submit bool
}

// memberEditForm edits a member's name, contact details and custom fields.
type memberEditForm struct {
	m      spondapi.Membership
	group  spondapi.Group
	inputs []textinput.Model
	rows   []editRow
	cursor int
	vp     viewport.Model
	width  int
	height int
}

func newMemberEditForm(m spondapi.Membership, g spondapi.Group, width, height int) *memberEditForm {
	f := &memberEditForm{m: m, group: g, vp: viewport.New(0, 0)}
	add := func(prompt, value string) int {
		in := textinput.New()
		in.Prompt = prompt
		in.PromptStyle = labelSty
		in.CharLimit = 500
		in.SetValue(value)
		f.inputs = append(f.inputs, in)
		return len(f.inputs) - 1
	}
	for _, spec := range []struct{ prompt, value string }{
		{"First name           ", m.FirstName},
		{"Last name            ", m.LastName},
		{"Email                ", m.Email},
		{"Phone                ", m.PhoneNumber},
	} {
		f.rows = append(f.rows, editRow{input: add(spec.prompt, spec.value)})
	}

	for i := range g.FieldDefs {
		d := &g.FieldDefs[i]
		ok, reason := fieldEditable(*d)
		row := editRow{input: -1, def: d, reason: reason}
		value := fieldValue(m.Fields[d.ID])
		switch {
		case !ok:
		case d.Type == "SINGLE_OPTION":
			for j, o := range d.Options {
				if o == value {
					row.option = j + 1
				}
			}
		default:
			prompt := fmt.Sprintf("%-21s", fit(d.Name, 20))
			row.input = add(prompt, value)
			if d.Type == "DATE" {
				f.inputs[row.input].Placeholder = "YYYY-MM-DD"
			}
		}
		f.rows = append(f.rows, row)
	}
	f.rows = append(f.rows, editRow{input: -1, submit: true})
	f.resize(width, height)
	return f
}

func (f *memberEditForm) resize(width, height int) {
	f.width, f.height = width, height
	for i := range f.inputs {
		f.inputs[i].Width = max(width-28, 10)
	}
	f.vp.Width, f.vp.Height = width, max(height, 3)
	f.refresh()
}

func (f *memberEditForm) refresh() {
	content, line := f.render()
	f.vp.SetContent(content)
	switch {
	case line < f.vp.YOffset:
		f.vp.SetYOffset(line)
	case line >= f.vp.YOffset+f.vp.Height:
		f.vp.SetYOffset(line - f.vp.Height + 1)
	}
}

func (f *memberEditForm) focusCursor() tea.Cmd {
	f.cursor = min(max(f.cursor, 0), len(f.rows)-1)
	for i := range f.inputs {
		f.inputs[i].Blur()
	}
	var cmd tea.Cmd
	if r := f.rows[f.cursor]; r.input >= 0 {
		cmd = f.inputs[r.input].Focus()
	}
	f.refresh()
	return cmd
}

func (f *memberEditForm) render() (string, int) {
	lines := []string{sectionSty.Render("Edit " + f.m.Name()), ""}
	cursorLine := 0
	customHeader := false
	for i, r := range f.rows {
		if r.def != nil && !customHeader {
			lines = append(lines, "", sectionSty.Render("Custom fields"))
			customHeader = true
		}
		if r.submit {
			lines = append(lines, "")
		}
		marker := "  "
		if i == f.cursor {
			marker = keySty.Render("› ")
			cursorLine = len(lines)
		}
		var text string
		switch {
		case r.submit:
			text = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#FFFFFF")).Background(accent).Render(" Save ")
		case r.input >= 0:
			text = f.inputs[r.input].View()
		case r.reason != "":
			text = labelSty.Render(fmt.Sprintf("%-21s", fit(r.def.Name, 20))) + fieldValue(f.m.Fields[r.def.ID]) + dimSty.Render("  ("+r.reason+")")
		default: // single option
			choice := "(none)"
			if r.option > 0 {
				choice = r.def.Options[r.option-1]
			}
			text = labelSty.Render(fmt.Sprintf("%-21s", fit(r.def.Name, 20))) + "‹ " + lipgloss.NewStyle().Bold(true).Render(choice) + " ›" + dimSty.Render("  ←/→ change")
		}
		lines = append(lines, marker+text)
	}
	lines = append(lines, "", dimSty.Render("Phone in international format, e.g. +4712345678."))
	return strings.Join(lines, "\n"), cursorLine
}

// update handles a key; it reports whether the form wants to be saved.
func (f *memberEditForm) update(key tea.KeyMsg) (tea.Cmd, bool) {
	r := f.rows[f.cursor]
	switch key.String() {
	case "up", "shift+tab":
		f.cursor--
		return f.focusCursor(), false
	case "down", "tab":
		f.cursor++
		return f.focusCursor(), false
	case "enter":
		if r.submit {
			return nil, true
		}
		f.cursor++
		return f.focusCursor(), false
	case "left", "right":
		if r.def != nil && r.input < 0 && r.reason == "" {
			n := len(r.def.Options) + 1
			step := 1
			if key.String() == "left" {
				step = n - 1
			}
			f.rows[f.cursor].option = (r.option + step) % n
			f.refresh()
			return nil, false
		}
	}
	if r.input >= 0 {
		var cmd tea.Cmd
		f.inputs[r.input], cmd = f.inputs[r.input].Update(key)
		f.refresh()
		return cmd, false
	}
	return nil, false
}

// normalizeFields copies custom field values as the app holds them: whole
// numbers as integers (LongMemberField), everything else as strings.
func normalizeFields(in map[string]any) map[string]any {
	out := make(map[string]any, len(in))
	for k, v := range in {
		if n, ok := v.(float64); ok && n == float64(int64(n)) {
			out[k] = int64(n)
			continue
		}
		out[k] = v
	}
	return out
}

// body validates the form and builds the change for POST group/{id}/member.
//
// It mirrors UiMembershipFormKt.toEditableMembership for an existing
// membership: phoneNumber and email are always sent (null when empty), other
// fields only when changed, and "fields" is the complete custom-field map
// (empty number and choice fields are left out, like the app's null values).
func (f *memberEditForm) body() (map[string]any, error) {
	v := func(i int) string { return f.inputs[i].Value() }
	// Untouched contact details are sent exactly as stored and not
	// re-validated, so an old odd format never blocks other edits.
	emailSame := strings.TrimSpace(v(editEmail)) == f.m.Email
	phoneSame := strings.TrimSpace(v(editPhone)) == f.m.PhoneNumber
	in := person{FirstName: v(editFirst), LastName: v(editLast), Email: v(editEmail), PhoneNumber: v(editPhone)}
	if emailSame {
		in.Email = ""
	}
	if phoneSame {
		in.PhoneNumber = ""
	}
	p, err := in.normalize(true)
	if err != nil {
		return nil, err
	}
	if emailSame {
		p.Email = f.m.Email
	}
	if phoneSame {
		p.PhoneNumber = f.m.PhoneNumber
	}

	nullable := func(s string) any {
		if s == "" {
			return nil
		}
		return s
	}
	body := map[string]any{
		"id":          f.m.ID,
		"phoneNumber": nullable(p.PhoneNumber),
		"email":       nullable(p.Email),
	}
	if p.FirstName != f.m.FirstName {
		body["firstName"] = p.FirstName
	}
	if p.LastName != f.m.LastName {
		body["lastName"] = p.LastName
	}

	original := normalizeFields(f.m.Fields)
	fields := maps.Clone(original)
	for _, r := range f.rows {
		if r.def == nil || r.reason != "" {
			continue
		}
		id := r.def.ID
		delete(fields, id)
		switch r.def.Type {
		case "SINGLE_OPTION":
			if r.option > 0 {
				fields[id] = r.def.Options[r.option-1]
			}
		case "NUMBER":
			s := strings.TrimSpace(f.inputs[r.input].Value())
			if s == "" {
				continue
			}
			n, err := strconv.ParseInt(s, 10, 64)
			if err != nil {
				return nil, fmt.Errorf("%s must be a whole number", r.def.Name)
			}
			fields[id] = n
		case "DATE":
			s := strings.TrimSpace(f.inputs[r.input].Value())
			if s == "" {
				continue
			}
			if _, err := time.Parse("2006-01-02", s); err != nil {
				return nil, fmt.Errorf("%s must be a date like 2024-05-31", r.def.Name)
			}
			fields[id] = s
		default: // TEXT: the app keeps an empty string
			fields[id] = f.inputs[r.input].Value()
		}
		if r.def.Required {
			if _, ok := fields[id]; !ok || fields[id] == "" {
				return nil, fmt.Errorf("%s is required", r.def.Name)
			}
		}
	}
	if !reflect.DeepEqual(fields, original) {
		body["fields"] = fields
	}
	return body, nil
}

// changed reports whether body changes anything beyond the always-sent
// contact fields.
func (f *memberEditForm) changed(body map[string]any) bool {
	if len(body) > 3 {
		return true
	}
	return body["email"] != nullableAny(f.m.Email) || body["phoneNumber"] != nullableAny(f.m.PhoneNumber)
}

func nullableAny(s string) any {
	if s == "" {
		return nil
	}
	return s
}
