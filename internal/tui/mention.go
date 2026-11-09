package tui

import (
	"slices"
	"strings"
	"unicode"

	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/asbjorn/spondcli/internal/display"
	"github.com/asbjorn/spondcli/internal/spondapi"
)

// mentionMark stands for one mention in a message's text; the message's
// mentions name the people in the same order (MentionTextProcessor).
const mentionMark = "\ue001"

// mentionRows is how many suggestions show at once above the message box.
const mentionRows = 5

var mentionSty = lipgloss.NewStyle().Bold(true).Foreground(accent)

// decodeMentions replaces the n-th mark in text with "@" and the n-th
// person's name, styled by sty. Marks without a person are dropped, as in
// the app. A mention without a name is looked up in names.
func decodeMentions(text string, mentions []spondapi.Mention, names map[string]string, sty lipgloss.Style) string {
	if !strings.Contains(text, mentionMark) {
		return text
	}
	parts := strings.Split(text, mentionMark)
	var b strings.Builder
	for i, part := range parts {
		b.WriteString(part)
		if i < len(parts)-1 && i < len(mentions) {
			name := mentions[i].Name
			if name == "" {
				name = nameOr(names, mentions[i].ID)
			}
			b.WriteString(sty.Render("@" + name))
		}
	}
	return b.String()
}

// encodeMentions turns each "@Name" in text of a person in picked into a
// mark and lists those people in order. A tag that was edited afterwards no
// longer matches and stays plain text. Marks typed or pasted into text are
// removed, so they cannot shift the list.
func encodeMentions(text string, picked []spondapi.Mention) (string, []spondapi.Mention) {
	runes := []rune(strings.ReplaceAll(text, mentionMark, ""))
	var b strings.Builder
	var out []spondapi.Mention
	for i := 0; i < len(runes); i++ {
		if runes[i] == '@' {
			if p, n := matchMention(runes[i+1:], picked); n > 0 {
				b.WriteString(mentionMark)
				out = append(out, p)
				i += n
				continue
			}
		}
		b.WriteRune(runes[i])
	}
	return b.String(), out
}

// matchMention finds the person in picked with the longest name that rest
// starts with, followed by the end or a character that cannot continue a
// name. It returns the person and the name's length in runes, or 0.
func matchMention(rest []rune, picked []spondapi.Mention) (spondapi.Mention, int) {
	var best spondapi.Mention
	n := 0
	for _, p := range picked {
		name := []rune(p.Name)
		if len(name) <= n || len(name) > len(rest) || string(rest[:len(name)]) != p.Name {
			continue
		}
		if len(rest) > len(name) && (unicode.IsLetter(rest[len(name)]) || unicode.IsDigit(rest[len(name)])) {
			continue
		}
		best, n = p, len(name)
	}
	return best, n
}

// mentionQuery finds the tag being typed: an "@" at the start of the line
// or after a space, followed by the text up to the cursor with no space in
// it. before is the line up to the cursor. It returns the "@"'s position in
// runes and the query.
func mentionQuery(before []rune) (int, string, bool) {
	at := len(before) - 1
	for at >= 0 && before[at] != '@' {
		at--
	}
	if at < 0 {
		return 0, "", false
	}
	query := before[at+1:]
	if slices.ContainsFunc(query, unicode.IsSpace) || (at > 0 && !unicode.IsSpace(before[at-1])) {
		return 0, "", false
	}
	return at, string(query), true
}

// mentionSpot is where a tag's "@" sits in the message box: line and rune
// column.
type mentionSpot struct{ row, at int }

// mentionSuggest lists the people matching the tag being typed.
type mentionSuggest struct {
	spot   mentionSpot
	query  string
	people []pickPerson
	cursor int
}

// clearComposer empties the message box and forgets its tags.
func (c *chatView) clearComposer() {
	c.compose.Reset()
	c.mentions, c.suggest, c.shut = nil, nil, nil
}

// composerBefore returns the cursor's line in the message box and the text
// on that line before the cursor.
func (c *chatView) composerBefore() (int, []rune) {
	row := c.compose.Line()
	lines := strings.Split(c.compose.Value(), "\n")
	if row >= len(lines) {
		return row, nil
	}
	li := c.compose.LineInfo()
	line := []rune(lines[row])
	return row, line[:min(max(li.StartColumn+li.ColumnOffset, 0), len(line))]
}

// mentionCandidates lists who can be tagged in the open conversation: its
// participants apart from you, and only in a group conversation, as in the
// app.
func (m model) mentionCandidates() []pickPerson {
	t := m.chat.open
	if t == nil || t.Type != "GROUP" {
		return nil
	}
	left := map[string]bool{}
	for _, id := range t.Participants {
		if m.me == nil || id != m.me.ID {
			left[id] = true
		}
	}
	var out []pickPerson
	if g, ok := m.groupByID(t.GroupID); ok {
		for _, p := range m.people(g) {
			if left[p.id] {
				out = append(out, p)
				delete(left, p.id)
			}
		}
	}
	// Participants not in the group's member list, if their name is known.
	for _, id := range t.Participants {
		if name, ok := m.profiles[id]; ok && left[id] && name != "" {
			out = append(out, pickPerson{id: id, name: name})
			delete(left, id)
		}
	}
	slices.SortFunc(out, func(a, b pickPerson) int { return strings.Compare(strings.ToLower(a.name), strings.ToLower(b.name)) })
	return out
}

// matchesMention reports whether p matches a tag query: by name, or for a
// guardian by the child's name.
func matchesMention(p pickPerson, query string) bool {
	q := strings.ToLower(query)
	return strings.Contains(strings.ToLower(p.name), q) ||
		strings.Contains(strings.ToLower(strings.TrimPrefix(p.note, "guardian of ")), q)
}

// updateMentions opens, updates or closes the suggestions for the tag
// before the cursor. A list closed with esc stays closed for that tag.
func (m *model) updateMentions() {
	ch := &m.chat
	row, before := ch.composerBefore()
	at, query, ok := mentionQuery(before)
	if !ok {
		ch.suggest, ch.shut = nil, nil
		return
	}
	spot := mentionSpot{row, at}
	if ch.shut != nil && *ch.shut == spot {
		ch.suggest = nil
		return
	}
	ch.shut = nil
	candidates := m.mentionCandidates()
	if len(candidates) == 0 {
		ch.suggest = nil
		return
	}
	var people []pickPerson
	for _, p := range candidates {
		if matchesMention(p, query) {
			people = append(people, p)
		}
	}
	cursor := 0
	if s := ch.suggest; s != nil && s.spot == spot && s.cursor < len(s.people) {
		// Keep the same person selected while the query narrows.
		cursor = max(slices.IndexFunc(people, func(p pickPerson) bool { return p.id == s.people[s.cursor].id }), 0)
	}
	ch.suggest = &mentionSuggest{spot: spot, query: query, people: people, cursor: cursor}
}

// pickMention replaces the tag being typed with the selected person's name
// and a space, as the app does.
func (m *model) pickMention() {
	ch := &m.chat
	s := ch.suggest
	p := s.people[s.cursor]
	ch.compose = deleteBack(ch.compose, len([]rune(s.query))+1)
	ch.compose.InsertString("@" + p.name + " ")
	ch.mentions = append(ch.mentions, spondapi.Mention{ID: p.id, Name: p.name, Type: "PROFILE"})
	ch.suggest = nil
}

// unpickMention handles backspace right after a tag: as in the app, the tag
// turns back into "@" and its suggestions open again. It reports whether
// the cursor was after a tag.
func (m *model) unpickMention() bool {
	ch := &m.chat
	_, before := ch.composerBefore()
	n := 0
	for _, p := range ch.mentions {
		tag := []rune("@" + p.Name)
		if len(tag) > n+1 && len(before) >= len(tag) && string(before[len(before)-len(tag):]) == string(tag) {
			n = len(tag) - 1
		}
	}
	if n == 0 {
		return false
	}
	ch.compose = deleteBack(ch.compose, n)
	return true
}

// deleteBack deletes n characters before the cursor.
func deleteBack(ta textarea.Model, n int) textarea.Model {
	for range n {
		ta, _ = ta.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	}
	return ta
}

// updateSuggest handles a key while the suggestions show. It reports
// whether the key was used.
func (m *model) updateSuggest(k tea.KeyMsg) bool {
	ch := &m.chat
	s := ch.suggest
	switch k.String() {
	case "esc":
		ch.shut, ch.suggest = &s.spot, nil
		return true
	case "up":
		if len(s.people) > 0 {
			s.cursor = max(s.cursor-1, 0)
			return true
		}
	case "down":
		if len(s.people) > 0 {
			s.cursor = min(s.cursor+1, len(s.people)-1)
			return true
		}
	case "tab", "enter":
		if len(s.people) > 0 {
			m.pickMention()
			return true
		}
	}
	return false
}

// mentionView is the suggestion list shown above the message box, at most
// height lines high.
func (m model) mentionView(height int) string {
	s := m.chat.suggest
	width := max(m.width-4, 10)
	rows := max(min(len(s.people), mentionRows, height-2), 1)
	var lines []string
	if len(s.people) == 0 {
		lines = append(lines, dimSty.Render("Nobody in this conversation matches."))
	}
	start := max(0, min(s.cursor-rows/2, len(s.people)-rows))
	for i := start; i < min(len(s.people), start+rows); i++ {
		p := s.people[i]
		marker, nameSty := "  ", lipgloss.NewStyle()
		if i == s.cursor {
			marker, nameSty = keySty.Render("› "), lipgloss.NewStyle().Bold(true)
		}
		label := nameSty.Render(display.FirstLine(p.name, width-2))
		if note := "  " + p.note; p.note != "" && len([]rune(p.name+note)) <= width-2 {
			label += dimSty.Render(note)
		}
		lines = append(lines, marker+label)
	}
	return activeComposerSty.Width(max(m.width-2, 10)).Render(strings.Join(lines, "\n"))
}
