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

	tea "github.com/charmbracelet/bubbletea"

	"github.com/asbjorn/spondcli/internal/spondapi"
)

type fakeGroupServer struct {
	mu      sync.Mutex
	members map[string]spondapi.Membership
	order   []string
	calls   []recorded
}

func newFakeGroupServer(t *testing.T, members []spondapi.Membership) (*spondapi.Client, *fakeGroupServer) {
	t.Helper()
	f := &fakeGroupServer{members: map[string]spondapi.Membership{}}
	for _, m := range members {
		f.members[m.ID] = m
		f.order = append(f.order, m.ID)
	}
	srv := httptest.NewServer(http.HandlerFunc(f.serve(t)))
	t.Cleanup(srv.Close)
	c := spondapi.NewClient()
	c.BaseURL = srv.URL + "/"
	return c, f
}

func (f *fakeGroupServer) serve(t *testing.T) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		rec := recorded{method: r.Method, path: r.URL.Path}
		data, _ := io.ReadAll(r.Body)
		if len(data) > 0 {
			json.Unmarshal(data, &rec.body)
		}
		f.calls = append(f.calls, rec)

		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/group/G/members":
			// Two pages: first member, then the rest.
			ids := f.order
			page := spondapi.MembersPage{}
			if r.URL.Query().Get("cursor") == "" {
				page.Members = []spondapi.Membership{f.members[ids[0]]}
				page.NextCursor = "c2"
			} else {
				for _, id := range ids[1:] {
					page.Members = append(page.Members, f.members[id])
				}
			}
			json.NewEncoder(w).Encode(page)
		case r.Method == http.MethodPost && r.URL.Path == "/group/G/member":
			id, _ := rec.body["id"].(string)
			m, ok := f.members[id]
			if !ok {
				id = "Mnew"
				m = spondapi.Membership{ID: id}
				m.FirstName, _ = rec.body["firstName"].(string)
				m.LastName, _ = rec.body["lastName"].(string)
				f.order = append(f.order, id)
			}
			if sg, ok := rec.body["subGroups"].([]any); ok {
				m.SubGroups = nil
				for _, x := range sg {
					m.SubGroups = append(m.SubGroups, x.(string))
				}
			}
			if v, ok := rec.body["respondent"].(bool); ok {
				m.Respondent = v
			}
			if v, ok := rec.body["firstName"].(string); ok {
				m.FirstName = v
			}
			if v, ok := rec.body["email"].(string); ok {
				m.Email = v
			}
			if v, ok := rec.body["fields"].(map[string]any); ok {
				m.Fields = v
			}
			f.members[id] = m
			json.NewEncoder(w).Encode(m)
		case r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/group/G/membershipRequest/"):
			if strings.HasSuffix(r.URL.Path, "/approve") {
				m := spondapi.Membership{ID: "Mapproved", FirstName: "Nina", Respondent: true}
				f.members[m.ID] = m
				json.NewEncoder(w).Encode(m)
			}
		case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/group/G/members/"):
			delete(f.members, strings.TrimPrefix(r.URL.Path, "/group/G/members/"))
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/guardian"):
			id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/group/G/member/"), "/guardian")
			m := f.members[id]
			fn, _ := rec.body["firstName"].(string)
			m.Guardians = append(m.Guardians, spondapi.Guardian{FirstName: fn})
			f.members[id] = m
			json.NewEncoder(w).Encode(m)
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}
}

func (f *fakeGroupServer) writes() []recorded {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := []recorded{}
	for _, c := range f.calls {
		if c.method != http.MethodGet {
			out = append(out, c)
		}
	}
	return out
}

// sendAll feeds msg and every app message its commands produce, recursively,
// skipping spinner ticks.
func sendAll(m model, msg tea.Msg) model {
	updated, cmd := m.Update(msg)
	m = updated.(model)
	for _, out := range runCmd(cmd) {
		switch out.(type) {
		case membersMsg, memberOpMsg, inviteMsg, respondMsg, requestOpMsg:
			m = sendAll(m, out)
		}
	}
	return m
}

func membersModel(t *testing.T, c *spondapi.Client, g spondapi.Group, me *spondapi.MyProfile) model {
	t.Helper()
	m := newModel(context.Background(), c)
	m.tab = tabGroups // these tests drive the Groups tab
	m.busy = ""
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = updated.(model)
	m.me = me
	m.allGroups = []spondapi.Group{g}
	m.group = g
	m.screen = screenGroupMenu
	return sendAll(m, key("m"))
}

func typeText(m model, s string) model {
	for _, r := range s {
		m = sendAll(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	return m
}

func TestMembersAdminFlow(t *testing.T) {
	g := adminGroup()
	c, srv := newFakeGroupServer(t, g.Members)
	m := membersModel(t, c, g, &spondapi.MyProfile{ID: "Pme"})

	if len(m.rows) != 3 || m.busy != "" {
		t.Fatalf("after paging: %d rows, busy %q", len(m.rows), m.busy)
	}

	// Select the child member (third row) and open it.
	m = sendAll(m, key("down"))
	m = sendAll(m, key("down"))
	m = sendAll(m, key("enter"))
	if m.memberModal == nil || m.memberModal.m.ID != "Mkid" {
		t.Fatalf("enter did not open Mkid")
	}
	view := m.View()
	for _, want := range []string{"Admin actions", "[s]", "Subgroups", "[d]", "Remove from group", "[g]", "Add guardian"} {
		if !strings.Contains(view, want) {
			t.Errorf("member modal lacks %q", want)
		}
	}
	if strings.Contains(view, "[r]") {
		t.Errorf("coach (members permission only) must not see admin roles action")
	}

	// Add to subgroup U14 (second item).
	m = sendAll(m, key("s"))
	m = sendAll(m, key("down"))
	m = sendAll(m, key(" "))
	m = sendAll(m, key("enter"))
	if got := m.memberModal.m.SubGroups; len(got) != 1 || got[0] != "sg2" {
		t.Errorf("subgroups after save = %v", got)
	}

	// Add a guardian.
	m = sendAll(m, key("g"))
	m = typeText(m, "Ola")
	m = sendAll(m, key("tab"))
	m = typeText(m, "Nordmann")
	m = sendAll(m, key("tab"))
	m = sendAll(m, key("tab"))
	m = typeText(m, "+47 123 45 678")
	m = sendAll(m, key("enter"))
	if m.err != nil {
		t.Fatalf("guardian: %v", m.err)
	}

	// Remove from group.
	m = sendAll(m, key("d"))
	m = sendAll(m, key("y"))
	if m.memberModal != nil || len(m.rows) != 2 {
		t.Errorf("after delete: modal open=%v rows=%d", m.memberModal != nil, len(m.rows))
	}

	writes := srv.writes()
	want := []struct{ method, path, body string }{
		{"POST", "/group/G/member", `{"id":"Mkid","subGroups":["sg2"]}`},
		{"POST", "/group/G/member/Mkid/guardian", `{"firstName":"Ola","lastName":"Nordmann","phoneNumber":"+4712345678"}`},
		{"DELETE", "/group/G/members/Mkid", `null`},
	}
	if len(writes) != len(want) {
		t.Fatalf("writes = %+v", writes)
	}
	for i, w := range want {
		if writes[i].method != w.method || writes[i].path != w.path || asJSON(t, writes[i].body) != w.body {
			t.Errorf("write %d = %s %s %s\nwant %s %s %s", i, writes[i].method, writes[i].path, asJSON(t, writes[i].body), w.method, w.path, w.body)
		}
	}
}

func TestMemberGuardsInUI(t *testing.T) {
	g := adminGroup()
	g.Members[0].Roles = []string{"r0"} // main admin: members and admins permissions
	c, srv := newFakeGroupServer(t, g.Members)
	m := membersModel(t, c, g, &spondapi.MyProfile{ID: "Pme"})

	m = sendAll(m, key("down")) // contact person
	m = sendAll(m, key("enter"))
	m = sendAll(m, key("d"))
	if m.err == nil || !strings.Contains(m.err.Error(), "contact person") || m.memberModal.mode != modeView {
		t.Errorf("deleting the contact person must be refused, err=%v", m.err)
	}
	if len(srv.writes()) != 0 {
		t.Errorf("refused action reached the server")
	}
}

func TestMemberModalReadOnly(t *testing.T) {
	g := adminGroup()
	c, _ := newFakeGroupServer(t, g.Members)
	m := membersModel(t, c, g, &spondapi.MyProfile{ID: "not-in-group"})
	m = sendAll(m, key("enter"))
	if !strings.Contains(m.View(), "no admin rights") {
		t.Errorf("read-only modal should say there are no admin rights")
	}
	m = sendAll(m, key("d"))
	if m.err != errNoMemberRights {
		t.Errorf("d without rights: %v", m.err)
	}
}

func TestClickOpensMember(t *testing.T) {
	g := adminGroup()
	c, _ := newFakeGroupServer(t, g.Members)
	m := membersModel(t, c, g, &spondapi.MyProfile{ID: "Pme"})
	// Row 2 (Mkid) is drawn after the header and separator.
	y := headerLines + memberListHeader + 2
	m = sendAll(m, tea.MouseMsg{X: 3, Y: y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	if m.memberModal == nil || m.memberModal.m.ID != "Mkid" {
		t.Fatalf("click did not open the third member")
	}
}

func TestInviteFlow(t *testing.T) {
	g := adminGroup()
	c, srv := newFakeGroupServer(t, g.Members)
	m := membersModel(t, c, g, &spondapi.MyProfile{ID: "Pme"})

	m = sendAll(m, key("i"))
	if m.invite == nil {
		t.Fatalf("i did not open the invite form: %v", m.err)
	}
	if strings.Contains(m.View(), "Admin") && strings.Contains(m.View(), "( ) Admin") {
		t.Errorf("coach without admins permission must not be offered Admin")
	}

	m = sendAll(m, key("down")) // type
	m = sendAll(m, key("down")) // first name
	m = typeText(m, "Kari")
	m = sendAll(m, key("enter"))
	m = typeText(m, "Nordmann")
	m = sendAll(m, key("enter"))
	m = typeText(m, "Kari@Example.com")
	m = sendAll(m, key("enter")) // phone (empty)
	m = sendAll(m, key("enter")) // dob (empty)
	m = sendAll(m, key("enter")) // subgroup U12
	m = sendAll(m, key(" "))
	for m.invite != nil && m.invite.rows()[m.invite.cursor].kind != rowSubmit {
		m = sendAll(m, key("down"))
	}
	m = sendAll(m, key("enter"))
	if m.err != nil {
		t.Fatalf("invite: %v", m.err)
	}
	if m.invite != nil || !strings.Contains(m.status, "Added Kari Nordmann to Team A as member") {
		t.Errorf("after invite: form open=%v status=%q", m.invite != nil, m.status)
	}
	if len(m.rows) != 4 {
		t.Errorf("new member not added to the list: %d rows", len(m.rows))
	}

	writes := srv.writes()
	wantBody := `{"email":"kari@example.com","fields":{},"firstName":"Kari","lastName":"Nordmann","phoneNumber":null,"respondent":true,"subGroups":["sg1"]}`
	if len(writes) != 1 || writes[0].path != "/group/G/member" || asJSON(t, writes[0].body) != wantBody {
		t.Fatalf("invite writes = %+v\nwant body %s", writes, wantBody)
	}
}

func TestInviteAdminNeedsRole(t *testing.T) {
	g := adminGroup()
	g.Members[0].Roles = []string{"r0"} // main admin: may add admins
	c, srv := newFakeGroupServer(t, g.Members)
	m := membersModel(t, c, g, &spondapi.MyProfile{ID: "Pme"})
	m = sendAll(m, key("i"))
	m = sendAll(m, key("down"))
	m = sendAll(m, key("right")) // switch to Admin
	if !m.invite.asAdmin {
		t.Fatalf("could not switch to Admin")
	}
	m = sendAll(m, key("down"))
	m = typeText(m, "Per")
	m = sendAll(m, key("down"))
	m = typeText(m, "Hansen")
	for m.invite.rows()[m.invite.cursor].kind != rowSubmit {
		m = sendAll(m, key("down"))
	}
	m = sendAll(m, key("enter"))
	if m.err == nil || !strings.Contains(m.err.Error(), "admin role") {
		t.Fatalf("admin without role: err = %v", m.err)
	}
	if len(srv.writes()) != 0 {
		t.Errorf("invalid invite reached the server")
	}
}

func TestRolesAndMemberStatus(t *testing.T) {
	g := adminGroup()
	g.Members[0].Roles = []string{"r0"} // main admin
	c, srv := newFakeGroupServer(t, g.Members)
	m := membersModel(t, c, g, &spondapi.MyProfile{ID: "Pme"})

	m = sendAll(m, key("G")) // last row: Mkid
	m = sendAll(m, key("enter"))
	if !strings.Contains(m.View(), "[r]") {
		t.Fatalf("main admin should see the admin roles action")
	}
	m = sendAll(m, key("r"))
	m = sendAll(m, key("down")) // Coach
	m = sendAll(m, key(" "))
	m = sendAll(m, key("enter"))

	m = sendAll(m, key("esc"))
	m = sendAll(m, key("up")) // contact person: admin only
	m = sendAll(m, key("enter"))
	m = sendAll(m, key("m"))
	m = sendAll(m, key("y"))

	writes := srv.writes()
	want := []string{`{"id":"Mkid","roles":["r1"]}`, `{"id":"Mcontact","respondent":true}`}
	if len(writes) != len(want) {
		t.Fatalf("writes = %+v", writes)
	}
	for i, w := range want {
		if writes[i].method != http.MethodPost || writes[i].path != "/group/G/member" || asJSON(t, writes[i].body) != w {
			t.Errorf("write %d = %s %s %s, want POST /group/G/member %s", i, writes[i].method, writes[i].path, asJSON(t, writes[i].body), w)
		}
	}
}
