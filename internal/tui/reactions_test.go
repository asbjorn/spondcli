package tui

import (
	"maps"
	"slices"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestReactionCounts(t *testing.T) {
	r := map[string]map[string]int64{
		"🎉": {"Pkari": 5},
		"👍": {"Pola": 9, "Pme": 3},
		"🔥": {"Pper": 1},
		"👎": {},
	}
	got := reactionCounts(r)
	var order []string
	for _, c := range got {
		order = append(order, c.emoji+strings.Join(c.who, ","))
	}
	// Most used first, then the earliest; people earliest first.
	if want := []string{"👍Pme,Pola", "🔥Pper", "🎉Pkari"}; !slices.Equal(order, want) {
		t.Errorf("reactionCounts = %v, want %v", order, want)
	}
	if myReaction(r, "Pola") != "👍" || myReaction(r, "Pnone") != "" {
		t.Errorf("myReaction wrong")
	}
}

func TestWithReaction(t *testing.T) {
	r := map[string]map[string]int64{"👍": {"Pme": 1, "Pola": 2}}
	changed := withReaction(r, "Pme", "🎉", 7)
	if want := (map[string]map[string]int64{"👍": {"Pola": 2}, "🎉": {"Pme": 7}}); !equalReactions(changed, want) {
		t.Errorf("change = %v", changed)
	}
	if r["👍"]["Pme"] != 1 {
		t.Errorf("the original was modified: %v", r)
	}
	if removed := withReaction(changed, "Pme", "", 0); !equalReactions(removed, map[string]map[string]int64{"👍": {"Pola": 2}}) {
		t.Errorf("remove = %v", removed)
	}
	if got := withReaction(nil, "Pme", "👍", 4); !equalReactions(got, map[string]map[string]int64{"👍": {"Pme": 4}}) {
		t.Errorf("first reaction = %v", got)
	}
}

func equalReactions(a, b map[string]map[string]int64) bool {
	return maps.EqualFunc(a, b, func(x, y map[string]int64) bool { return maps.Equal(x, y) })
}

func TestChatReactions(t *testing.T) {
	c, f := newFakeInbox(t)
	m := inboxModel(t, c)
	m = inboxSend(m, key("4"))
	m = inboxSend(m, key("enter")) // T1: "Are you coming?" (yours), "See you there"

	// The newest message is selected; its reactions show with names.
	view := plainView(m)
	if m.chat.msgCursor != 1 || !strings.Contains(view, "▶ ┃ Kari Berg") || !strings.Contains(view, "👍 2") ||
		!strings.Contains(view, "👍 Ola Lie, You") || !strings.Contains(view, "👤  Kari Berg") || !strings.Contains(view, "Direct message · via Team A") {
		t.Fatalf("thread on open:\n%s", view)
	}

	// up selects the older message; digits do not switch tabs in the picker.
	m = inboxSend(m, tea.KeyMsg{Type: tea.KeyUp})
	if m.chat.msgCursor != 0 || !strings.Contains(plainView(m), "▶ ┃ You") {
		t.Fatalf("up did not select the first message:\n%s", plainView(m))
	}
	m = inboxSend(m, key("e"))
	if m.chat.react == nil || !strings.Contains(plainView(m), "React to your message") {
		t.Fatalf("e did not open the picker:\n%s", plainView(m))
	}
	m = inboxSend(m, key("8")) // 🎉
	if m.tab != tabMessages || m.chat.react != nil || len(f.reacted) != 1 || f.reacted[0] != "PUT /chat/v1/chats/T1/0/reaction 🎉" {
		t.Fatalf("pick 8: tab %d, reacted %v", m.tab, f.reacted)
	}
	if msg := m.chat.msgs[0]; msg.Reactions["🎉"]["Pme"] != 1793094311164 || !strings.Contains(plainView(m), "🎉 1") {
		t.Fatalf("reaction not shown: %v\n%s", msg.Reactions, plainView(m))
	}

	// Picking your reaction again removes it; + toggles 👍.
	m = inboxSend(m, key("e"))
	if !strings.Contains(plainView(m), "yours: 🎉") {
		t.Errorf("picker does not show your reaction:\n%s", plainView(m))
	}
	m = inboxSend(m, key("enter")) // the picker starts on your reaction
	if f.reacted[1] != "DELETE /chat/v1/chats/T1/0/reaction" || len(m.chat.msgs[0].Reactions) != 0 {
		t.Fatalf("second pick: %v, reactions %v", f.reacted, m.chat.msgs[0].Reactions)
	}
	m = inboxSend(m, key("down"))
	m = inboxSend(m, key("+"))
	if f.reacted[2] != "DELETE /chat/v1/chats/T1/1/reaction" || m.chat.msgs[1].Reactions["👍"]["Pme"] != 0 ||
		m.chat.msgs[1].Reactions["👍"]["Pola"] != 2 {
		t.Fatalf("+ on your 👍: %v, reactions %v", f.reacted, m.chat.msgs[1].Reactions)
	}

	// esc closes the picker but stays in the thread; arrows move in it.
	m = inboxSend(m, key("e"))
	m = inboxSend(m, tea.KeyMsg{Type: tea.KeyRight})
	if m.chat.react.cursor != slices.Index(reactionEmojis, thumbsUp)+1 {
		t.Errorf("right: cursor %d", m.chat.react.cursor)
	}
	m = inboxSend(m, key("esc"))
	if m.chat.react != nil || m.chat.open == nil || len(f.reacted) != 3 {
		t.Fatalf("esc in the picker: react %v, open %v, reacted %v", m.chat.react, m.chat.open, f.reacted)
	}

	// A reload keeps the selection.
	m = inboxSend(m, tea.KeyMsg{Type: tea.KeyUp})
	m = inboxSend(m, key("r"))
	if m.chat.msgCursor != 0 {
		t.Errorf("reload moved the selection to %d", m.chat.msgCursor)
	}

	// A click selects the message under it.
	lines := strings.Split(plainView(m), "\n")
	y := slices.IndexFunc(lines, func(l string) bool { return strings.Contains(l, "See you there") })
	m = inboxSend(m, tea.MouseMsg{X: 5, Y: y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	if m.chat.msgCursor != 1 {
		t.Errorf("click on line %d selected %d", y, m.chat.msgCursor)
	}
	if h := len(strings.Split(m.View(), "\n")); h != 30 {
		t.Errorf("view is %d lines, want 30", h)
	}
}
