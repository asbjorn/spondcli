package tui

import (
	"fmt"
	"slices"
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/asbjorn/spondcli/internal/spondapi"
)

// composerLines is the height of the message box, inside its border.
const composerLines = 3

// composerHeight is the message box with its border.
const composerHeight = composerLines + 2

var (
	composerSty       = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(subtle)
	activeComposerSty = composerSty.BorderForeground(accent)
)

func newComposer() textarea.Model {
	ta := textarea.New()
	ta.Placeholder = "Write a message…"
	ta.ShowLineNumbers = false
	ta.Prompt = ""
	ta.CharLimit = 4000
	ta.SetHeight(composerLines)
	// enter sends; these add a line break instead.
	ta.KeyMap.InsertNewline.SetKeys("alt+enter", "ctrl+j")
	ta.FocusedStyle.CursorLine = lipgloss.NewStyle()
	return ta
}

// chatDraft is a conversation that does not exist yet: the first message
// creates it in a group with these people.
type chatDraft struct {
	groupID    string
	recipients []string // profile ids, not including you
}

type (
	// chatFoundMsg answers whether a conversation with the picked people exists.
	chatFoundMsg struct {
		draft  chatDraft
		thread *spondapi.ChatThread // nil if there is none yet
		err    error
	}
	chatSentMsg struct {
		msg *spondapi.ChatMessage
		err error
	}
)

func (m *model) focusComposer() tea.Cmd {
	m.chat.composing = true
	return m.chat.compose.Focus()
}

func (m model) updateComposer(msg tea.Msg) (tea.Model, tea.Cmd) {
	ch := &m.chat
	if k, ok := msg.(tea.KeyMsg); ok {
		if ch.suggest != nil && m.updateSuggest(k) {
			return m, nil
		}
		switch k.String() {
		case "esc":
			ch.composing, ch.suggest = false, nil
			ch.compose.Blur()
			return m, nil
		case "enter":
			return m, m.sendMessage()
		case "backspace":
			if m.unpickMention() {
				m.updateMentions()
				return m, nil
			}
		}
	}
	var cmd tea.Cmd
	ch.compose, cmd = ch.compose.Update(msg)
	m.updateMentions()
	return m, cmd
}

// sendMessage posts the composed text to the open conversation, creating it
// first for a draft.
func (m *model) sendMessage() tea.Cmd {
	ch := &m.chat
	text, mentions := encodeMentions(strings.TrimSpace(ch.compose.Value()), ch.mentions)
	if text == "" || ch.sending || ch.open == nil {
		return nil
	}
	// As the app does, refuse to tag someone who has left the conversation.
	for _, p := range mentions {
		if (m.me == nil || p.ID != m.me.ID) && !slices.Contains(ch.open.Participants, p.ID) {
			m.err = fmt.Errorf("%s is no longer in this conversation", p.Name)
			return nil
		}
	}
	ch.sending = true
	ctx, c, chatID, draft := m.ctx, m.client, ch.open.ID, ch.draft
	return m.startBusy("Sending…", func() tea.Msg {
		var sent *spondapi.ChatMessage
		var err error
		if draft != nil {
			sent, err = c.StartChat(ctx, draft.groupID, draft.recipients, text, mentions)
		} else {
			sent, err = c.SendChat(ctx, chatID, text, mentions)
		}
		return chatSentMsg{msg: sent, err: err}
	})
}

func (m model) applySent(msg chatSentMsg) (tea.Model, tea.Cmd) {
	ch := &m.chat
	m.busy = ""
	ch.sending = false
	if msg.err != nil {
		m.err = fmt.Errorf("sending: %w", msg.err) // the text stays in the box
		return m, nil
	}
	ch.clearComposer()
	m.status = "Sent"
	if ch.open == nil {
		return m, m.loadChats("")
	}
	if ch.draft != nil {
		// The conversation now exists: show it as the server has it.
		t := *ch.open
		t.ID = msg.msg.ChatID
		cmd := m.showThread(t)
		m.status = "Sent"
		return m, tea.Batch(cmd, m.focusComposer(), m.loadChats(""))
	}
	if msg.msg.ChatID == ch.open.ID {
		ch.msgs = append(ch.msgs, *msg.msg)
		ch.msgCursor = len(ch.msgs) - 1
		m.refreshMessages()
		ch.msgVP.GotoBottom()
	}
	return m, m.loadChats("") // newest message and order in the list
}

func (m model) composerView() string {
	ch := m.chat
	sty := composerSty
	if ch.composing {
		sty = activeComposerSty
	}
	return sty.Width(max(m.width-2, 10)).Render(ch.compose.View())
}

// recipientPicker chooses a group, then people in it, for a new conversation.
type recipientPicker struct {
	group  *spondapi.Group // nil while choosing the group
	cursor int
	filter textinput.Model
	people []pickPerson
	chosen []string // profile ids, in the order picked
}

type pickPerson struct {
	id, name, note string
}

func (m *model) openPicker() {
	f := textinput.New()
	f.Prompt = "To: "
	f.Placeholder = "type to filter"
	f.PromptStyle = lipgloss.NewStyle().Foreground(accent)
	m.chat.picker = &recipientPicker{filter: f}
	m.err, m.status = nil, ""
}

// people lists who in g you can write to: members and guardians with a
// Spond profile, apart from you.
func (m model) people(g spondapi.Group) []pickPerson {
	seen := map[string]bool{}
	if m.me != nil {
		seen[m.me.ID] = true
	}
	var out []pickPerson
	add := func(id, name, note string) {
		if id != "" && name != "" && !seen[id] {
			seen[id] = true
			out = append(out, pickPerson{id: id, name: name, note: note})
		}
	}
	for _, mem := range g.Members {
		if mem.Profile != nil {
			add(mem.Profile.ID, mem.Name(), "")
		}
		for _, gd := range mem.Guardians {
			if gd.Profile != nil {
				add(gd.Profile.ID, gd.Name(), "guardian of "+mem.Name())
			}
		}
	}
	slices.SortFunc(out, func(a, b pickPerson) int { return strings.Compare(strings.ToLower(a.name), strings.ToLower(b.name)) })
	return out
}

// shown is the list under the cursor: groups, or people matching the filter.
func (p *recipientPicker) shown(groups []spondapi.Group) []pickPerson {
	if p.group == nil {
		out := make([]pickPerson, len(groups))
		for i, g := range groups {
			out[i] = pickPerson{id: g.ID, name: g.Name}
		}
		return out
	}
	q := strings.ToLower(strings.TrimSpace(p.filter.Value()))
	var out []pickPerson
	for _, pp := range p.people {
		if q == "" || strings.Contains(strings.ToLower(pp.name+" "+pp.note), q) {
			out = append(out, pp)
		}
	}
	return out
}

func (m model) updatePicker(msg tea.Msg) (tea.Model, tea.Cmd) {
	p := m.chat.picker
	k, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	shown := p.shown(m.allGroups)
	switch k.String() {
	case "esc":
		if p.group != nil {
			p.group, p.cursor, p.chosen = nil, 0, nil
			p.filter.Reset()
			p.filter.Blur()
		} else {
			m.chat.picker = nil
		}
		return m, nil
	case "up":
		p.cursor = max(p.cursor-1, 0)
		return m, nil
	case "down":
		p.cursor = min(p.cursor+1, max(len(shown)-1, 0))
		return m, nil
	case "tab":
		if p.group != nil && len(shown) > 0 {
			p.toggle(shown[p.cursor].id)
		}
		return m, nil
	case "enter":
		if len(shown) == 0 && (p.group == nil || len(p.chosen) == 0) {
			return m, nil
		}
		if p.group == nil {
			g, _ := m.groupByID(shown[p.cursor].id)
			p.group, p.cursor = &g, 0
			p.people = m.people(g)
			return m, p.filter.Focus()
		}
		if len(p.chosen) == 0 {
			p.toggle(shown[p.cursor].id)
		}
		return m, m.findConversation()
	}
	if p.group == nil {
		return m, nil
	}
	var cmd tea.Cmd
	p.filter, cmd = p.filter.Update(msg)
	p.cursor = min(p.cursor, max(len(p.shown(m.allGroups))-1, 0))
	return m, cmd
}

func (p *recipientPicker) toggle(id string) {
	if i := slices.Index(p.chosen, id); i >= 0 {
		p.chosen = slices.Delete(p.chosen, i, i+1)
	} else {
		p.chosen = append(p.chosen, id)
	}
}

// findConversation looks for an existing conversation with the picked
// people, as the app does before showing an empty one.
func (m *model) findConversation() tea.Cmd {
	p := m.chat.picker
	draft := chatDraft{groupID: p.group.ID, recipients: slices.Clone(p.chosen)}
	ctx, c := m.ctx, m.client
	return m.startBusy("Looking for the conversation…", func() tea.Msg {
		t, err := c.FindChat(ctx, draft.groupID, draft.recipients)
		return chatFoundMsg{draft: draft, thread: t, err: err}
	})
}

func (m model) applyFound(msg chatFoundMsg) (tea.Model, tea.Cmd) {
	m.busy = ""
	if msg.err != nil {
		m.err = fmt.Errorf("finding the conversation: %w", msg.err)
		return m, nil
	}
	m.chat.picker = nil
	if msg.thread != nil {
		return m, tea.Batch(m.showThread(*msg.thread), m.focusComposer())
	}
	typ := "DM"
	if len(msg.draft.recipients) > 1 {
		typ = "GROUP"
	}
	participants := slices.Clone(msg.draft.recipients)
	if m.me != nil {
		participants = append(participants, m.me.ID)
	}
	cmd := m.showThread(spondapi.ChatThread{Type: typ, GroupID: msg.draft.groupID, Participants: participants})
	draft := msg.draft
	m.chat.draft = &draft
	m.chat.msgsDone = true
	return m, tea.Batch(cmd, m.focusComposer())
}

var chosenSty = lipgloss.NewStyle().Bold(true).Foreground(okColor)

func (m model) pickerView() string {
	p := m.chat.picker
	shown := p.shown(m.allGroups)
	var lines []string
	if p.group == nil {
		lines = append(lines, sectionSty.Render("Choose the group to write in"), "")
	} else {
		var names []string
		for _, id := range p.chosen {
			names = append(names, chosenSty.Render(nameOr(m.profiles, id)))
		}
		lines = append(lines, sectionSty.Render("Write to people in "+p.group.Name), p.filter.View())
		if len(names) > 0 {
			lines = append(lines, "Chosen: "+strings.Join(names, ", "))
		}
		lines = append(lines, "")
	}
	// Keep the cursor in view.
	rows := max(m.bodyHeight()-len(lines), 3)
	start := max(0, min(p.cursor-rows/2, len(shown)-rows))
	for i := start; i < min(len(shown), start+rows); i++ {
		pp := shown[i]
		marker := "  "
		if i == p.cursor {
			marker = keySty.Render("› ")
		}
		label := pp.name
		if p.group != nil {
			box := "[ ] "
			if slices.Contains(p.chosen, pp.id) {
				box = acceptedSty.Render("[x] ")
			}
			label = box + label
			if pp.note != "" {
				label += dimSty.Render("  " + pp.note)
			}
		}
		if i == p.cursor {
			label = lipgloss.NewStyle().Bold(true).Render(label)
		}
		lines = append(lines, marker+label)
	}
	if len(shown) == 0 {
		lines = append(lines, dimSty.Render("  Nobody matches."))
	}
	return lipgloss.NewStyle().Padding(0, 1).Render(strings.Join(lines, "\n"))
}

func (m model) pickerHelp() string {
	if m.chat.picker.group == nil {
		return "↑/↓ choose · enter next · esc cancel"
	}
	return "type to filter · ↑/↓ move · tab pick/unpick · enter write · esc back to groups"
}
