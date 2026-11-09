package tui

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/asbjorn/spondcli/internal/spondapi"
)

func TestEncodeMentions(t *testing.T) {
	kari := spondapi.Mention{ID: "Pkari", Name: "Kari Berg", Type: "PROFILE"}
	kar := spondapi.Mention{ID: "Pkar", Name: "Kari", Type: "PROFILE"}
	ola := spondapi.Mention{ID: "Pola", Name: "Ola Lie", Type: "PROFILE"}
	picked := []spondapi.Mention{kar, kari, ola}
	for _, tc := range []struct {
		text, want string
		mentions   []spondapi.Mention
	}{
		{"no tags", "no tags", nil},
		{"@Kari Berg can you?", mentionMark + " can you?", []spondapi.Mention{kari}}, // the longest name wins
		{"@Kari, @Ola Lie and @Kari", mentionMark + ", " + mentionMark + " and " + mentionMark, []spondapi.Mention{kar, ola, kar}},
		{"@Kari Bergersen", mentionMark + " Bergersen", []spondapi.Mention{kar}}, // "Kari Berg" does not end at a word boundary
		{"@Ola Lies", "@Ola Lies", nil},                                     // a longer word is not a tag
		{"@Per Lie", "@Per Lie", nil},                                       // never picked
		{"mail a@Ola Lie", "mail a" + mentionMark, []spondapi.Mention{ola}}, // a picked tag stays a tag
		{"stray " + mentionMark + "mark", "stray mark", nil},                // typed marks are removed
		{"@Øyvind", "@Øyvind", nil},
	} {
		got, mentions := encodeMentions(tc.text, picked)
		if got != tc.want || !slices.Equal(mentions, tc.mentions) {
			t.Errorf("encodeMentions(%q) = %q, %v; want %q, %v", tc.text, got, mentions, tc.want, tc.mentions)
		}
	}
	øy := spondapi.Mention{ID: "Poy", Name: "Øyvind Å", Type: "PROFILE"}
	if got, m := encodeMentions("hei @Øyvind Å!", []spondapi.Mention{øy}); got != "hei "+mentionMark+"!" || len(m) != 1 {
		t.Errorf("non-ASCII name: %q %v", got, m)
	}
}

func TestDecodeMentions(t *testing.T) {
	plain := lipgloss.NewStyle()
	names := map[string]string{"Pola": "Ola Lie"}
	for _, tc := range []struct {
		text     string
		mentions []spondapi.Mention
		want     string
	}{
		{"plain", nil, "plain"},
		{mentionMark + " fix it", []spondapi.Mention{{ID: "Pkari", Name: "Kari Berg"}}, "@Kari Berg fix it"},
		{"hi " + mentionMark + " and " + mentionMark, []spondapi.Mention{{ID: "Pkari", Name: "Kari Berg"}, {ID: "Pola"}}, "hi @Kari Berg and @Ola Lie"},
		{mentionMark + mentionMark + "!", []spondapi.Mention{{ID: "Pkari", Name: "Kari"}}, "@Kari!"}, // a mark without a person is dropped
	} {
		if got := decodeMentions(tc.text, tc.mentions, names, plain); got != tc.want {
			t.Errorf("decodeMentions(%q) = %q, want %q", tc.text, got, tc.want)
		}
	}
	// Round trip through encode.
	picked := []spondapi.Mention{{ID: "Pkari", Name: "Kari Berg", Type: "PROFILE"}}
	text, mentions := encodeMentions("@Kari Berg hello", picked)
	if got := decodeMentions(text, mentions, nil, plain); got != "@Kari Berg hello" {
		t.Errorf("round trip = %q", got)
	}
}

func TestMentionQuery(t *testing.T) {
	for _, tc := range []struct {
		before string
		at     int
		query  string
		ok     bool
	}{
		{"@", 0, "", true},
		{"hi @ka", 3, "ka", true},
		{"hi\t@ka", 3, "ka", true},
		{"mail@ka", 0, "", false}, // not after a space
		{"@kari berg", 0, "", false},
		{"@kari ", 0, "", false},
		{"no tag", 0, "", false},
		{"@a @b", 3, "b", true},
		{"æ @ø", 2, "ø", true}, // positions are in runes
	} {
		at, query, ok := mentionQuery([]rune(tc.before))
		if at != tc.at || query != tc.query || ok != tc.ok {
			t.Errorf("mentionQuery(%q) = %d, %q, %v; want %d, %q, %v", tc.before, at, query, ok, tc.at, tc.query, tc.ok)
		}
	}
}

// The text before the cursor must be right also when the line wraps.
func TestComposerBefore(t *testing.T) {
	c := newChatView()
	c.compose.SetWidth(12)
	c.compose.Focus()
	c.compose.InsertString("first line\nsome words that wrap @ka")
	row, before := c.composerBefore()
	if row != 1 || string(before) != "some words that wrap @ka" {
		t.Fatalf("at the end: row %d, before %q", row, string(before))
	}
	c.compose = deleteBack(c.compose, 3)
	c.compose.CursorStart()
	c.compose.SetCursor(9) // inside the second soft line
	if _, before := c.composerBefore(); string(before) != "some word" {
		t.Errorf("mid-line: before %q", string(before))
	}
}

var backspace = tea.KeyMsg{Type: tea.KeyBackspace}

func TestChatMentions(t *testing.T) {
	c, f := newFakeInbox(t)
	m := inboxModel(t, c)
	m = inboxSend(m, key("4"))

	// In a direct message nobody is offered.
	m = inboxSend(m, key("enter"))
	m = inboxSend(m, key("i"))
	m = inboxSend(m, key("@"))
	if m.chat.suggest != nil {
		t.Fatalf("suggestions in a direct message: %+v", m.chat.suggest)
	}
	m = inboxSend(m, key("esc"))
	m = inboxSend(m, key("esc"))

	// A group conversation shows received tags with the name.
	m = inboxSend(m, key("down"))
	m = inboxSend(m, key("enter"))
	if view := plainView(m); !strings.Contains(view, "@Me Myself can you fix the light?") || strings.Contains(view, mentionMark) {
		t.Fatalf("received tag:\n%s", view)
	}
	m = inboxSend(m, key("i"))
	m = inboxSend(m, key("Hi @"))
	view := plainView(m)
	if m.chat.suggest == nil || len(m.chat.suggest.people) != 2 ||
		!strings.Contains(view, "Kari Berg") || !strings.Contains(view, "Ola Lie  guardian of Per Lie") || strings.Contains(view, "› Me") {
		t.Fatalf("suggestions after @ (participants apart from you):\n%s", view)
	}
	// The list covers the bottom of the messages; the screen keeps its height.
	if h := lipgloss.Height(m.View()); h != 30 {
		t.Errorf("view is %d lines with the list open, want 30", h)
	}
	small := inboxSend(m, tea.WindowSizeMsg{Width: 60, Height: 14})
	if h := lipgloss.Height(small.View()); h != 14 || !strings.Contains(plainView(small), "Kari Berg") {
		t.Errorf("small window: %d lines, want 14:\n%s", h, plainView(small))
	}
	// A guardian is found by the child's name too.
	m = inboxSend(m, key("pe"))
	if s := m.chat.suggest; s == nil || len(s.people) != 1 || s.people[0].id != "Pola" {
		t.Fatalf("query pe: %+v", m.chat.suggest)
	}
	m = inboxSend(m, key("tab"))
	if got := m.chat.compose.Value(); got != "Hi @Ola Lie " || m.chat.suggest != nil {
		t.Fatalf("after picking: %q, suggest %+v", got, m.chat.suggest)
	}

	// enter picks while the list shows, and down moves in it.
	m = inboxSend(m, key("and @"))
	m = inboxSend(m, key("down"))
	if m.chat.suggest.cursor != 1 {
		t.Fatalf("down: cursor %d", m.chat.suggest.cursor)
	}
	m = inboxSend(m, tea.KeyMsg{Type: tea.KeyUp})
	m = inboxSend(m, key("k"))
	m = inboxSend(m, key("enter"))
	if got := m.chat.compose.Value(); got != "Hi @Ola Lie and @Kari Berg " || len(f.posted) != 0 {
		t.Fatalf("enter on the list: %q, posted %v", got, f.posted)
	}

	// Backspace right after a tag turns it back into "@" with the list open.
	m = inboxSend(m, backspace)
	m = inboxSend(m, backspace)
	if got := m.chat.compose.Value(); got != "Hi @Ola Lie and @" || m.chat.suggest == nil || len(m.chat.suggest.people) != 2 {
		t.Fatalf("backspace into the tag: %q, suggest %+v", got, m.chat.suggest)
	}
	// esc closes the list, which stays closed for this tag; esc again stops writing.
	m = inboxSend(m, key("esc"))
	m = inboxSend(m, key("ka"))
	if !m.chat.composing || m.chat.suggest != nil {
		t.Fatalf("after esc: composing %v, suggest %+v", m.chat.composing, m.chat.suggest)
	}
	m = inboxSend(m, backspace)
	m = inboxSend(m, backspace)
	m = inboxSend(m, key("Kari Berg!"))
	if got := m.chat.compose.Value(); got != "Hi @Ola Lie and @Kari Berg!" {
		t.Fatalf("typed by hand: %q", got)
	}

	m = inboxSend(m, key("enter"))
	if len(f.posted) != 1 {
		t.Fatalf("posted %v", f.posted)
	}
	got := f.posted[0]
	raw, _ := json.Marshal(got["mentions"])
	if got["text"] != "Hi "+mentionMark+" and "+mentionMark+"!" ||
		string(raw) != `[{"id":"Pola","type":"PROFILE"},{"id":"Pkari","type":"PROFILE"}]` {
		t.Fatalf("sent text %q, mentions %s", got["text"], raw)
	}
	if view := plainView(m); !strings.Contains(view, "Hi @Ola Lie and @Kari Berg!") || m.chat.mentions != nil {
		t.Errorf("after sending: mentions %v\n%s", m.chat.mentions, view)
	}

	// Someone who has left the conversation cannot be tagged.
	m = inboxSend(m, key("@ka"))
	m = inboxSend(m, key("tab"))
	m.chat.open.Participants = []string{"Pme", "Pola"}
	m = inboxSend(m, key("enter"))
	if len(f.posted) != 1 || m.err == nil || !strings.Contains(m.err.Error(), "Kari Berg is no longer") {
		t.Errorf("tag of someone who left: posted %d, err %v", len(f.posted), m.err)
	}
}
