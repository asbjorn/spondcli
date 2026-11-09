package tui

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/asbjorn/spondcli/internal/spondapi"
)

func TestAnswerForPrecedence(t *testing.T) {
	// Mirrors SpondMapperKt.calcResponseType: unconfirmed wins over accepted.
	r := &spondapi.ResponseBundle{
		AcceptedIDs:    []string{"a", "u"},
		UnconfirmedIDs: []string{"u"},
		DeclinedIDs:    []string{"d"},
		UnansweredIDs:  []string{"x"},
		WaitinglistIDs: []string{"w"},
	}
	cases := map[string]spondapi.Answer{
		"a": spondapi.AnswerAccepted, "u": spondapi.AnswerUnconfirmed, "d": spondapi.AnswerDeclined,
		"x": spondapi.AnswerUnanswered, "w": spondapi.AnswerWaiting, "zz": spondapi.AnswerNone,
	}
	for id, want := range cases {
		if got := r.AnswerFor(id); got != want {
			t.Errorf("answerFor(%q) = %v, want %v", id, got, want)
		}
	}
	if got := (*spondapi.ResponseBundle)(nil).AnswerFor("a"); got != spondapi.AnswerNone {
		t.Errorf("nil bundle = %v", got)
	}
}

type recorded struct {
	method, path string
	body         map[string]any
}

// fakeSpond serves the respond and get-arrangement endpoints, tracking state.
func fakeSpond(t *testing.T, s spondapi.Spond) (*spondapi.Client, func() []recorded) {
	t.Helper()
	var mu sync.Mutex
	var calls []recorded
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		rec := recorded{method: r.Method, path: r.URL.Path}
		if data, _ := io.ReadAll(r.Body); len(data) > 0 {
			json.Unmarshal(data, &rec.body)
		}
		calls = append(calls, rec)

		switch {
		case r.Method == http.MethodPut && strings.HasPrefix(r.URL.Path, "/sponds/S1/responses/"):
			id := strings.TrimPrefix(r.URL.Path, "/sponds/S1/responses/")
			s.Responses = &spondapi.ResponseBundle{UnansweredIDs: []string{}}
			if rec.body["accepted"] == true {
				s.Responses.AcceptedIDs = []string{id}
			} else {
				s.Responses.DeclinedIDs = []string{id}
			}
			s.ResponseCounts = nil
			json.NewEncoder(w).Encode(s.Responses)
		case r.Method == http.MethodGet && r.URL.Path == "/sponds/S1":
			if r.URL.Query().Get("includeComments") != "true" {
				t.Errorf("get spond without includeComments=true")
			}
			json.NewEncoder(w).Encode(s)
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	c := spondapi.NewClient()
	c.BaseURL = srv.URL + "/"
	return c, func() []recorded { mu.Lock(); defer mu.Unlock(); return append([]recorded(nil), calls...) }
}

func TestRespondRequest(t *testing.T) {
	c, calls := fakeSpond(t, spondapi.Spond{ID: "S1"})
	ctx := context.Background()

	if _, err := c.Respond(ctx, "S1", "M1", true, "ignored when accepting"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Respond(ctx, "S1", "M1", false, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Respond(ctx, "S1", "M1", false, "Sick"); err != nil {
		t.Fatal(err)
	}

	got := calls()
	want := []map[string]any{
		{"accepted": true},
		{"accepted": false},
		{"accepted": false, "declineMessage": "Sick"},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d calls, want %d", len(got), len(want))
	}
	for i, rec := range got {
		if rec.method != http.MethodPut || rec.path != "/sponds/S1/responses/M1" {
			t.Errorf("call %d: %s %s", i, rec.method, rec.path)
		}
		if len(rec.body) != len(want[i]) {
			t.Errorf("call %d body = %v, want %v", i, rec.body, want[i])
		}
		for k, v := range want[i] {
			if rec.body[k] != v {
				t.Errorf("call %d body[%s] = %v, want %v", i, k, rec.body[k], v)
			}
		}
	}
}

// runCmd executes a command tree and returns every message it produces.
// Timer commands (cursor blink, spinner ticks) sleep; any command that does
// not finish quickly is skipped. The fake servers answer well within that.
func runCmd(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	done := make(chan tea.Msg, 1)
	go func() { done <- cmd() }()
	var msg tea.Msg
	select {
	case msg = <-done:
	case <-time.After(50 * time.Millisecond):
		return nil
	}
	if batch, ok := msg.(tea.BatchMsg); ok {
		var out []tea.Msg
		for _, c := range batch {
			out = append(out, runCmd(c)...)
		}
		return out
	}
	return []tea.Msg{msg}
}

func key(k string) tea.KeyMsg {
	switch k {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
}

func eventsModel(t *testing.T, c *spondapi.Client, events ...spondapi.Spond) model {
	t.Helper()
	m := newModel(context.Background(), c)
	m.tab = tabGroups // these tests drive the Groups tab
	m.busy = ""
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	m = updated.(model)
	m.group = spondapi.Group{ID: "G", Name: "Team A", Members: []spondapi.Membership{
		{ID: "M1", FirstName: "Kari", LastName: "Nordmann"},
		{ID: "M2", FirstName: "Ola", LastName: "Nordmann"},
	}}
	m.screen = screenEvents
	m.groupFeed.events = events
	m.groupFeed.done = true
	m.refreshEvents(feedGroup)
	return m
}

// send feeds msg to the model and then every message its commands produce,
// except spinner ticks.
func send(m model, msg tea.Msg) model {
	updated, cmd := m.Update(msg)
	m = updated.(model)
	for _, out := range runCmd(cmd) {
		if _, ok := out.(respondMsg); ok {
			m = send(m, out)
		}
	}
	return m
}

func TestDetailModalAnswering(t *testing.T) {
	s := spondapi.Spond{
		ID:             "S1",
		Heading:        "Training",
		StartTimestamp: "2026-10-25T17:00:00Z",
		EndTimestamp:   "2026-10-25T18:30:00Z",
		MeetupPrior:    15,
		BehalfOfIDs:    []string{"M1", "M2"},
		Responses:      &spondapi.ResponseBundle{UnansweredIDs: []string{"M1", "M2"}},
		Payment:        &spondapi.SpondPayment{},
	}
	c, calls := fakeSpond(t, s)
	m := eventsModel(t, c, spondapi.Spond{ID: "S0", Heading: "Other"}, s)

	m = send(m, key("down"))
	if m.groupFeed.cursor != 1 {
		t.Fatalf("cursor = %d, want 1", m.groupFeed.cursor)
	}
	m = send(m, key("enter"))
	if m.groupFeed.detail == nil || m.groupFeed.detail.s.ID != "S1" {
		t.Fatalf("enter did not open the modal for S1")
	}
	view := m.View()
	for _, want := range []string{
		"Your answer", "Kari Nordmann", "Ola Nordmann", "Not answered", "[y] Attend",
		"Meet-up", "15 minutes before start", "Overview",
	} {
		if !strings.Contains(view, want) {
			t.Errorf("modal lacks %q", want)
		}
	}
	m.groupFeed.detail.vp.GotoBottom()
	if bottom := m.View(); !strings.Contains(bottom, "App update needed") {
		t.Errorf("scrolling to the bottom does not reach the last field")
	}
	body := m.groupFeed.detail.body(80)
	for _, want := range []string{"Participation", "Price", "Free", "Automatic reminder", "Public event"} {
		if !strings.Contains(body, want) {
			t.Errorf("modal body lacks %q", want)
		}
	}
	m.groupFeed.detail.vp.GotoTop()

	m = send(m, key("y"))
	if a := m.groupFeed.events[1].Responses.AnswerFor("M1"); a != spondapi.AnswerAccepted {
		t.Errorf("after y: M1 = %v, want attending", a)
	}
	if !strings.Contains(m.status, "Kari Nordmann: attending") {
		t.Errorf("status = %q", m.status)
	}

	m = send(m, key("tab")) // switch to Ola
	m = send(m, key("n"))
	if !m.groupFeed.detail.declining {
		t.Fatalf("n did not open the decline message input")
	}
	for _, r := range "Away" {
		m = send(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	m = send(m, key("enter"))
	if a := m.groupFeed.detail.s.Responses.AnswerFor("M2"); a != spondapi.AnswerDeclined {
		t.Errorf("after n: M2 = %v, want not attending", a)
	}

	var puts []recorded
	for _, rec := range calls() {
		if rec.method == http.MethodPut {
			puts = append(puts, rec)
		}
	}
	if len(puts) != 2 || puts[0].path != "/sponds/S1/responses/M1" || puts[1].path != "/sponds/S1/responses/M2" {
		t.Fatalf("PUTs = %+v", puts)
	}
	if puts[1].body["declineMessage"] != "Away" {
		t.Errorf("decline message = %v", puts[1].body["declineMessage"])
	}

	m = send(m, key("esc"))
	if m.groupFeed.detail != nil {
		t.Errorf("esc did not close the modal")
	}
}

func TestDetailModalBlocksPaidAndCancelled(t *testing.T) {
	c, calls := fakeSpond(t, spondapi.Spond{ID: "S1"})
	paid := spondapi.Spond{ID: "S1", BehalfOfIDs: []string{"M1"}, Payment: &spondapi.SpondPayment{Total: 15000, Currency: "NOK"}}
	m := eventsModel(t, c, paid)
	m = send(m, key("enter"))
	m = send(m, key("y"))
	if m.err == nil || !strings.Contains(m.err.Error(), "150.00 NOK") {
		t.Errorf("paid accept: err = %v", m.err)
	}
	if len(calls()) != 0 {
		t.Errorf("paid accept must not call the API")
	}

	cancelled := spondapi.Spond{ID: "S1", BehalfOfIDs: []string{"M1"}, Cancelled: true}
	m = eventsModel(t, c, cancelled)
	m = send(m, key("enter"))
	m = send(m, key("n"))
	if m.err == nil || m.groupFeed.detail.declining {
		t.Errorf("cancelled decline should be refused, err = %v", m.err)
	}
}

func TestClickOpensArrangement(t *testing.T) {
	m := eventsModel(t, spondapi.NewClient(), spondapi.Spond{ID: "S0", Heading: "First"}, spondapi.Spond{ID: "S1", Heading: "Second"})
	line := m.groupFeed.offsets[1] + 1 // inside the second card
	m = send(m, tea.MouseMsg{X: 5, Y: line + headerLines, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	if m.groupFeed.detail == nil || m.groupFeed.detail.s.ID != "S1" {
		t.Fatalf("click did not open the second arrangement")
	}
}

// TestDecodeSpondWireTypes guards the JSON types the app's serializers use:
// recipients.group.type is GroupType's int code (GroupTypeSerializer), and
// visibility is a case-insensitive string (EventVisibilitySerializer).
func TestDecodeSpondWireTypes(t *testing.T) {
	raw := `[{
		"id": "S1", "type": "EVENT", "visibility": "all",
		"startTimestamp": "2026-10-25T17:00:00Z", "updated": 1761400000000, "meetupPrior": 15,
		"location": {"feature": "Main field", "latitude": 62.47, "longitude": 6.15},
		"payment": {"total": 15000, "currency": "NOK"},
		"owners": [{"id": "P1", "response": "ACCEPTED"}],
		"recipients": {"group": {"id": "G", "name": "Team A", "type": 2, "subGroups": [{"id": "sg1", "name": "U12", "color": "#ff0000"}],
			"members": [{"id": "M1", "firstName": "Kari", "lastName": "N", "profile": {"id": "P1"}}]}},
		"responses": {"acceptedIds": ["M1"], "declineMessages": {"M2": {"profileId": "P2", "message": "Sick"}},
			"registeredAttendance": {"M1": "ATTENDED"}},
		"matchInfo": {"type": "HOME", "teamScore": 2, "opponentScore": 1, "scoresSet": true},
		"tasks": {"openTasks": [{"id": "t"}], "assignedTasks": []},
		"children": {"events": ["E1"]}
	}]`
	var sponds []spondapi.Spond
	if err := json.Unmarshal([]byte(raw), &sponds); err != nil {
		t.Fatalf("decode: %v", err)
	}
	s := sponds[0]
	if s.Recipients.Group.Type != 2 {
		t.Errorf("group type = %d, want 2", s.Recipients.Group.Type)
	}

	got := map[string]string{}
	for _, sec := range detailSections(s, spondapi.Group{}) {
		for _, f := range sec.fields {
			got[sec.title+"/"+f.label] = f.value
		}
	}
	want := map[string]string{
		"Organisation/Group type":      "Children",
		"Organisation/Hosts":           "Kari N (accepted)",
		"Participation/Visible to":     "All group members",
		"Payment/Price":                "150.00 NOK",
		"People/Attending":             "Kari N",
		"People/Decline messages":      `M2: "Sick"`,
		"People/Registered attendance": "Kari N: Attended",
		"Match/Score":                  "2 – 1",
		"Match/Match type":             "Home",
		"Other/Open tasks":             "1",
		"Series/Linked events":         "1",
		"Location/Coordinates":         "62.470000, 6.150000",
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %q, want %q", k, got[k], v)
		}
	}
}
