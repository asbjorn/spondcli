package tui

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/asbjorn/spondcli/internal/spondapi"
)

// fakeInbox serves the Home, Alerts and Messages endpoints. The chat server
// rejects the first chat token to exercise the re-login.
type fakeInbox struct {
	mu         sync.Mutex
	chatLogins int
	queries    map[string]string // path → last raw query
	posted     []map[string]any  // bodies of POST messages
	found      []string          // bodies of POST …/find
	reacted    []string          // "METHOD path body" of reaction requests
}

func newFakeInbox(t *testing.T) (*spondapi.Client, *fakeInbox) {
	t.Helper()
	f := &fakeInbox{queries: map[string]string{}}
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.queries[r.URL.Path] = r.URL.RawQuery
		reply := func(v any) { json.NewEncoder(w).Encode(v) }
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/core/sponds":
			if q := r.URL.Query(); q.Has("groupId") || q.Has("includeHidden") {
				t.Errorf("home feed query %q should not name a group", r.URL.RawQuery)
			}
			reply([]spondapi.Spond{{ID: "S1", Heading: "Cup final", StartTimestamp: "2026-10-25T17:00:00Z",
				Recipients: &spondapi.SpondRecipients{Group: &spondapi.RecipientGroup{ID: "G", Name: "Team A"}}}})
		case r.Method == http.MethodGet && r.URL.Path == "/core/activities/summary":
			reply([]spondapi.Activity{
				{ID: "A1", Type: "post_comment", GroupID: "G", PostID: "P1", Text: "<b>Kari</b> commented on <b>Kit &amp; boots</b>", Timestamp: "2026-10-23T10:00:00Z"},
				{ID: "A2", Type: "update_spond", GroupID: "G", SpondID: "S1", Text: "Ola updated <b>Cup final</b>", Timestamp: "2026-10-22T10:00:00Z", Read: true},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/core/activities": // the poll's stream cursor
			reply([]spondapi.StreamActivity{})
		case r.Method == http.MethodGet && r.URL.Path == "/core/posts/P1":
			reply(spondapi.Post{ID: "P1", GroupID: "G", Title: "Kit & boots", Body: "Bring boots", Timestamp: "2026-10-20T10:00:00Z"})
		case r.Method == http.MethodGet && r.URL.Path == "/core/sponds/S1":
			reply(spondapi.Spond{ID: "S1", Heading: "Cup final", StartTimestamp: "2026-10-25T17:00:00Z"})
		case r.Method == http.MethodPost && r.URL.Path == "/core/chat/":
			f.chatLogins++
			reply(spondapi.ChatAuth{URL: srv.URL + "/chat/v1", Auth: "token" + string(rune('0'+f.chatLogins))})
		case strings.HasPrefix(r.URL.Path, "/chat/v1/"):
			if r.Header.Get("auth") != "token2" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			if strings.HasSuffix(r.URL.Path, "/reaction") {
				var body map[string]string
				json.NewDecoder(r.Body).Decode(&body)
				f.reacted = append(f.reacted, strings.TrimSpace(r.Method+" "+r.URL.Path+" "+body["value"]))
				if r.Method == http.MethodPut {
					reply(map[string]int64{"value": 1793094311164})
				}
				return
			}
			switch r.URL.Path {
			case "/chat/v1/messages":
				var body map[string]any
				json.NewDecoder(r.Body).Decode(&body)
				f.posted = append(f.posted, body)
				id, _ := body["chatId"].(string)
				if id == "" {
					id = "T9" // a new conversation
				}
				// The stored message, with its mentions but without names.
				var stored struct {
					Mentions []spondapi.Mention `json:"mentions"`
				}
				raw, _ := json.Marshal(body)
				json.Unmarshal(raw, &stored)
				reply(spondapi.ChatMessage{ChatID: id, MsgNum: 2, User: "Pme", Text: body["text"].(string), Type: "TEXT", Mentions: stored.Mentions})
			case "/chat/v1/chats/community/G/type/GROUP/find":
				var ids []string
				json.NewDecoder(r.Body).Decode(&ids)
				f.found = append(f.found, strings.Join(ids, ","))
				reply(map[string]any{}) // no conversation yet
			case "/chat/v1/chats/T9/messages":
				reply([]spondapi.ChatMessage{{ChatID: "T9", MsgNum: 0, User: "Pme", Text: "Hi Ola", Type: "TEXT"}})
			case "/chat/v1/chats":
				reply([]spondapi.ChatThread{
					{ID: "T1", Type: "DM", GroupID: "G", Participants: []string{"Pme", "Pkari"}, Unread: true, NewestTimestamp: "2026-10-23T10:00:00Z",
						Message: &spondapi.ChatMessage{MsgNum: 1, User: "Pkari", Text: "See you there", Type: "TEXT"}},
					{ID: "T2", Type: "GROUP", GroupID: "G", Participants: []string{"Pme", "Pkari", "Pola"}, NewestTimestamp: "2026-10-22T10:00:00Z"},
				})
			case "/chat/v1/chats/T2/messages":
				reply([]spondapi.ChatMessage{{ChatID: "T2", MsgNum: 0, User: "Pkari", Type: "TEXT", Timestamp: "2026-10-22T10:00:00Z",
					Text: mentionMark + " can you fix the light?", Mentions: []spondapi.Mention{{ID: "Pme", Name: "Me Myself", Type: "PROFILE"}}}})
			case "/chat/v1/chats/T1/messages":
				reply([]spondapi.ChatMessage{
					{MsgNum: 1, User: "Pkari", Text: "See you there", Type: "TEXT", Timestamp: "2026-10-23T10:00:00Z",
						Reactions: map[string]map[string]int64{"👍": {"Pola": 2, "Pme": 3}, "🎉": {"Pkari": 1}}},
					{MsgNum: 0, User: "Pme", Text: "Are you coming?", Type: "TEXT", Timestamp: "2026-10-23T09:00:00Z"},
				})
			default:
				t.Errorf("unexpected chat request %s", r.URL.Path)
			}
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	c := spondapi.NewClient()
	c.BaseURL = srv.URL + "/core/"
	c.Token = "core-token"
	return c, f
}

// inboxSend applies msg and every message its commands produce.
func inboxSend(m model, msg tea.Msg) model {
	updated, cmd := m.Update(msg)
	m = updated.(model)
	for _, out := range runCmd(cmd) {
		switch out.(type) {
		case eventsMsg, alertsMsg, alertOpenMsg, chatsMsg, chatMessagesMsg, chatSentMsg, chatFoundMsg, chatReactedMsg:
			m = inboxSend(m, out)
		}
	}
	return m
}

func plainView(m model) string { return ansi.Strip(m.View()) }

// inboxModel is logged in to the fake inbox, on the Home tab.
func inboxModel(t *testing.T, c *spondapi.Client) model {
	t.Helper()
	m := newModel(context.Background(), c)
	m.busy = ""
	m = inboxSend(m, tea.WindowSizeMsg{Width: 100, Height: 30})
	g := spondapi.Group{ID: "G", Name: "Team A", Members: []spondapi.Membership{
		{ID: "M1", FirstName: "Kari", LastName: "Berg", Profile: &spondapi.Profile{ID: "Pkari"}},
		{ID: "M2", FirstName: "Per", LastName: "Lie", Guardians: []spondapi.Guardian{{FirstName: "Ola", LastName: "Lie", Profile: &spondapi.Profile{ID: "Pola"}}}},
		{ID: "M3", FirstName: "Me", Profile: &spondapi.Profile{ID: "Pme"}},
	}}
	return inboxSend(m, groupsMsg{me: &spondapi.MyProfile{ID: "Pme", FirstName: "Me"}, groups: []spondapi.Group{g}})
}

func TestTabs(t *testing.T) {
	c, _ := newFakeInbox(t)
	m := inboxModel(t, c)

	// The first login lands on Home, with badges from the background loads.
	view := plainView(m)
	for _, want := range []string{"1 Home", "2 Alerts  1 ", "3 Groups", "4 Messages  1 ", "All groups › Upcoming", "Cup final", "Team A"} {
		if !strings.Contains(view, want) {
			t.Errorf("home view lacks %q:\n%s", want, view)
		}
	}

	m = inboxSend(m, key("2"))
	view = plainView(m)
	if m.tab != tabAlerts || !strings.Contains(view, "Kari commented on Kit & boots") || strings.Contains(view, "<b>") {
		t.Fatalf("alerts view:\n%s", view)
	}
	m = inboxSend(m, key("enter"))
	if m.alerts.post == nil || !strings.Contains(plainView(m), "Bring boots") {
		t.Fatalf("enter did not open the post:\n%s", plainView(m))
	}
	m = inboxSend(m, key("esc"))
	m = inboxSend(m, key("down"))
	m = inboxSend(m, key("enter"))
	if d := m.alerts.feed.detail; d == nil || d.s.ID != "S1" {
		t.Fatalf("enter did not open the arrangement")
	}
	m = inboxSend(m, key("esc"))

	m = inboxSend(m, key("4"))
	view = plainView(m)
	// A direct message is named after the other person, not its group.
	if m.tab != tabMessages || !strings.Contains(view, "Kari Berg") || !strings.Contains(view, "Kari Berg: See you there") {
		t.Fatalf("messages view:\n%s", view)
	}
	m = inboxSend(m, key("enter"))
	view = plainView(m)
	if !strings.Contains(view, "Messages › Kari Berg") || strings.Index(view, "Are you coming?") > strings.Index(view, "See you there") ||
		!strings.Contains(view, "You") || !strings.Contains(view, "start of conversation") {
		t.Fatalf("thread view (oldest first, own messages as You):\n%s", view)
	}
	m = inboxSend(m, key("esc"))
	if m.chat.open != nil {
		t.Errorf("esc did not close the thread")
	}

	// Digits switch tabs, but not while typing in the group filter.
	m = inboxSend(m, key("3"))
	if m.tab != tabGroups || !strings.Contains(plainView(m), "Your groups") {
		t.Fatalf("3 did not open Groups:\n%s", plainView(m))
	}
	m = inboxSend(m, key("/"))
	m = inboxSend(m, key("1"))
	if m.tab != tabGroups {
		t.Errorf("1 switched tabs while filtering groups")
	}
	m = inboxSend(m, key("esc"))

	// Clicking a tab label switches to it.
	lines := strings.Split(plainView(m), "\n")
	x := strings.Index(lines[1], "4 Messages") // labels sit on the tab bar's middle row
	m = inboxSend(m, tea.MouseMsg{X: ansi.StringWidth(lines[1][:x]) + 1, Y: 1, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	if m.tab != tabMessages {
		t.Errorf("click on the tab bar: tab = %d, want Messages", m.tab)
	}
}

func TestChatCompose(t *testing.T) {
	c, f := newFakeInbox(t)
	m := inboxModel(t, c)
	m = inboxSend(m, key("4"))

	// Reply in an existing conversation.
	m = inboxSend(m, key("enter"))
	m = inboxSend(m, key("i"))
	m = inboxSend(m, key("4")) // typed, not a tab switch
	if m.tab != tabMessages || m.chat.compose.Value() != "4" {
		t.Fatalf("digit while writing: tab %d, text %q", m.tab, m.chat.compose.Value())
	}
	m.chat.compose.Reset()
	m = inboxSend(m, key("On my way"))
	m = inboxSend(m, key("enter"))
	if len(f.posted) != 1 || f.posted[0]["chatId"] != "T1" || f.posted[0]["text"] != "On my way" || f.posted[0]["type"] != "TEXT" {
		t.Fatalf("posted %v, want a TEXT message to T1", f.posted)
	}
	view := plainView(m)
	if m.chat.compose.Value() != "" || !strings.Contains(view, "On my way") {
		t.Errorf("after sending: box %q, view:\n%s", m.chat.compose.Value(), view)
	}
	m = inboxSend(m, key("esc")) // stop writing
	m = inboxSend(m, key("esc")) // back to the list

	// Start a new conversation with a guardian, found by filtering.
	m = inboxSend(m, key("n"))
	m = inboxSend(m, key("enter")) // the only group
	m = inboxSend(m, key("ola"))
	if view := plainView(m); !strings.Contains(view, "Ola Lie") || strings.Contains(view, "Kari Berg") || strings.Contains(view, "[ ] Me") {
		t.Fatalf("picker filtered on ola:\n%s", view)
	}
	m = inboxSend(m, key("enter"))
	if len(f.found) != 1 || f.found[0] != "Pola" || m.chat.draft == nil || !m.chat.composing {
		t.Fatalf("find bodies %v, draft %v, composing %v", f.found, m.chat.draft, m.chat.composing)
	}
	m = inboxSend(m, key("Hi Ola"))
	m = inboxSend(m, key("enter"))
	got := f.posted[len(f.posted)-1]
	community, _ := got["community"].(map[string]any)
	if got["recipient"] != "Pola" || community["type"] != "GROUP" || community["id"] != "G" || got["chatId"] != nil {
		t.Fatalf("first message body = %v", got)
	}
	if m.chat.draft != nil || m.chat.open == nil || m.chat.open.ID != "T9" || !strings.Contains(plainView(m), "Hi Ola") {
		t.Errorf("after starting: draft %v, open %+v\n%s", m.chat.draft, m.chat.open, plainView(m))
	}
}

func TestChatTokenRefresh(t *testing.T) {
	c, _ := newFakeInbox(t)
	threads, err := c.Chats(context.Background(), 10, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(threads) != 2 || c.Chat.Auth != "token2" {
		t.Errorf("got %d threads with token %q; want 2 after logging in to chat again", len(threads), c.Chat.Auth)
	}
}

func TestRenderBoldHTML(t *testing.T) {
	got := ansi.Strip(renderBoldHTML("<b>Kari &amp; Ola</b> liked <i>x</i> &lt;3"))
	if got != "Kari & Ola liked x <3" {
		t.Errorf("renderBoldHTML = %q", got)
	}
}
