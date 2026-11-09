package tui

import (
	"cmp"
	"fmt"
	"slices"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/asbjorn/spondcli/internal/display"
	"github.com/asbjorn/spondcli/internal/spondapi"
)

// reactionEmojis are offered in the reaction picker: the app's six
// (Reactions.DEFAULT_EMOJIS) and four more that people use. All are a single
// code point, so terminals agree on their width.
var reactionEmojis = []string{"🤩", "😃", "🤣", "🙁", "👍", "👎", "👏", "🎉", "💪", "🔥"}

// thumbsUp is the quick reaction on +.
const thumbsUp = "👍"

var (
	// pickColor marks the selected message and the picker's choice.
	pickColor   = lipgloss.AdaptiveColor{Light: "#B45309", Dark: "#FFC857"}
	pickSty     = lipgloss.NewStyle().Bold(true).Foreground(pickColor)
	chipSty     = lipgloss.NewStyle().Background(tagBg).Padding(0, 1)
	myChipSty   = chipSty.Bold(true).Foreground(lipgloss.Color("#FFFFFF")).Background(accent)
	pickChipSty = chipSty.Bold(true).Foreground(lipgloss.Color("#000000")).Background(pickColor)
)

// reactPicker chooses a reaction to one message of the open thread.
type reactPicker struct {
	msgNum int
	cursor int // index in reactionEmojis
}

type chatReactedMsg struct {
	chatID string
	msgNum int
	emoji  string // "" when the reaction was removed
	at     int64
	err    error
}

// reactionCount is one emoji's tally on a message.
type reactionCount struct {
	emoji string
	who   []string // profile ids, earliest first
	first int64
}

// reactionCounts tallies a message's reactions, most used first, then the
// earliest.
func reactionCounts(r map[string]map[string]int64) []reactionCount {
	var out []reactionCount
	for emoji, who := range r {
		if len(who) == 0 {
			continue
		}
		c := reactionCount{emoji: emoji}
		for id := range who {
			c.who = append(c.who, id)
		}
		slices.SortFunc(c.who, func(a, b string) int { return cmp.Or(cmp.Compare(who[a], who[b]), strings.Compare(a, b)) })
		c.first = who[c.who[0]]
		out = append(out, c)
	}
	slices.SortFunc(out, func(a, b reactionCount) int {
		return cmp.Or(cmp.Compare(len(b.who), len(a.who)), cmp.Compare(a.first, b.first), strings.Compare(a.emoji, b.emoji))
	})
	return out
}

// myReaction is the emoji profile reacted with, or "".
func myReaction(r map[string]map[string]int64, profile string) string {
	for emoji, who := range r {
		if _, ok := who[profile]; ok {
			return emoji
		}
	}
	return ""
}

// withReaction returns r with profile's reaction replaced by emoji, or
// removed when emoji is "", as Reactions.change does. r is not modified.
func withReaction(r map[string]map[string]int64, profile, emoji string, at int64) map[string]map[string]int64 {
	out := map[string]map[string]int64{}
	for e, who := range r {
		for id, t := range who {
			if id == profile {
				continue
			}
			if out[e] == nil {
				out[e] = map[string]int64{}
			}
			out[e][id] = t
		}
	}
	if emoji != "" {
		if out[emoji] == nil {
			out[emoji] = map[string]int64{}
		}
		out[emoji][profile] = at
	}
	return out
}

// reactionChips renders a message's reactions; yours stands out.
func (m model) reactionChips(r map[string]map[string]int64) string {
	var chips []string
	for _, c := range reactionCounts(r) {
		sty := chipSty
		if m.me != nil && slices.Contains(c.who, m.me.ID) {
			sty = myChipSty
		}
		chips = append(chips, sty.Render(fmt.Sprintf("%s %d", c.emoji, len(c.who))))
	}
	return strings.Join(chips, " ")
}

// reactionNames lists who reacted with what, one emoji per line.
func (m model) reactionNames(r map[string]map[string]int64) string {
	var lines []string
	for _, c := range reactionCounts(r) {
		names := make([]string, len(c.who))
		for i, id := range c.who {
			names[i] = nameOr(m.profiles, id)
			if m.me != nil && id == m.me.ID {
				names[i] = "You"
			}
		}
		lines = append(lines, c.emoji+" "+strings.Join(names, ", "))
	}
	return dimSty.Render(strings.Join(lines, "\n"))
}

// canReact reports whether msg takes reactions: any message not deleted.
func canReact(msg spondapi.ChatMessage) bool {
	return !msg.Deleted
}

// openReactions opens the reaction picker on the selected message, starting
// on your current reaction.
func (m *model) openReactions() {
	ch := &m.chat
	msg, ok := ch.selected()
	if !ok || !canReact(msg) {
		return
	}
	cursor := slices.Index(reactionEmojis, thumbsUp)
	if m.me != nil {
		if i := slices.Index(reactionEmojis, myReaction(msg.Reactions, m.me.ID)); i >= 0 {
			cursor = i
		}
	}
	ch.react = &reactPicker{msgNum: msg.MsgNum, cursor: cursor}
	m.err, m.status = nil, ""
}

// toggleReaction reacts with emoji to the message msgNum, or removes your
// reaction when it already is emoji, as tapping a checked emoji does in the
// app.
func (m *model) toggleReaction(msgNum int, emoji string) tea.Cmd {
	ch := &m.chat
	i := slices.IndexFunc(ch.msgs, func(o spondapi.ChatMessage) bool { return o.MsgNum == msgNum })
	if i < 0 || ch.open == nil || ch.open.ID == "" || m.me == nil || !canReact(ch.msgs[i]) {
		return nil
	}
	if myReaction(ch.msgs[i].Reactions, m.me.ID) == emoji {
		emoji = ""
	}
	ctx, c, chatID := m.ctx, m.client, ch.open.ID
	return m.startBusy("Reacting…", func() tea.Msg {
		if emoji == "" {
			return chatReactedMsg{chatID: chatID, msgNum: msgNum, err: c.RemoveReaction(ctx, chatID, msgNum)}
		}
		at, err := c.SetReaction(ctx, chatID, msgNum, emoji)
		return chatReactedMsg{chatID: chatID, msgNum: msgNum, emoji: emoji, at: at, err: err}
	})
}

func (m model) applyReacted(msg chatReactedMsg) (tea.Model, tea.Cmd) {
	ch := &m.chat
	m.busy = ""
	if msg.err != nil {
		m.err = fmt.Errorf("reacting: %w", msg.err)
		return m, nil
	}
	if ch.open == nil || ch.open.ID != msg.chatID || m.me == nil {
		return m, nil
	}
	if i := slices.IndexFunc(ch.msgs, func(o spondapi.ChatMessage) bool { return o.MsgNum == msg.msgNum }); i >= 0 {
		ch.msgs[i].Reactions = withReaction(ch.msgs[i].Reactions, m.me.ID, msg.emoji, msg.at)
		m.refreshMessages()
	}
	return m, nil
}

// updateReact handles a key while the reaction picker is open.
func (m model) updateReact(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	ch := &m.chat
	p := ch.react
	n := len(reactionEmojis)
	switch s := k.String(); s {
	case "esc", "q":
		ch.react = nil
	case "left", "h", "shift+tab":
		p.cursor = (p.cursor + n - 1) % n
	case "right", "l", "tab":
		p.cursor = (p.cursor + 1) % n
	case "enter", " ":
		ch.react = nil
		return m, m.toggleReaction(p.msgNum, reactionEmojis[p.cursor])
	default:
		if i := strings.Index("1234567890", s); len(s) == 1 && i >= 0 && i < n {
			ch.react = nil
			return m, m.toggleReaction(p.msgNum, reactionEmojis[i])
		}
	}
	return m, nil
}

// reactView is the reaction picker shown above the message box.
func (m model) reactView() string {
	ch := m.chat
	p := ch.react
	whose := "the message"
	mine := ""
	if i := slices.IndexFunc(ch.msgs, func(o spondapi.ChatMessage) bool { return o.MsgNum == p.msgNum }); i >= 0 {
		if who := m.sender(ch.msgs[i]); who == "You" {
			whose = "your message"
		} else {
			whose = who + "’s message"
		}
		if m.me != nil {
			mine = myReaction(ch.msgs[i].Reactions, m.me.ID)
		}
	}
	width := max(m.width-6, 10)
	var lines []string
	line := ""
	for i, e := range reactionEmojis {
		sty := chipSty
		switch {
		case i == p.cursor:
			sty = pickChipSty
		case e == mine:
			sty = myChipSty
		}
		chip := sty.Render(fmt.Sprintf("%d %s", (i+1)%10, e))
		if line != "" && lipgloss.Width(line)+1+lipgloss.Width(chip) > width {
			lines = append(lines, line)
			line = ""
		}
		if line != "" {
			line += " "
		}
		line += chip
	}
	lines = append(lines, line)
	title := sectionSty.Render(display.FirstLine("React to "+whose, width))
	if hint := "  yours: " + mine + ", pick it again to remove it"; mine != "" && lipgloss.Width(title+hint) <= width {
		title += dimSty.Render(hint)
	}
	return activeComposerSty.BorderForeground(pickColor).Width(max(m.width-2, 10)).
		Render(title + "\n" + strings.Join(lines, "\n"))
}
