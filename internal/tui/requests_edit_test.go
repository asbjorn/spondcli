package tui

import (
	"context"
	"net/http"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/asbjorn/spondcli/internal/spondapi"
)

func requestGroup() spondapi.Group {
	g := adminGroup()
	g.MembershipRequests = []spondapi.JoinRequest{
		{ID: "R1", Profile: &spondapi.RequestPerson{FirstName: "Nina", LastName: "Berg", Email: "nina@x.no", Creator: true}, DateOfBirth: "2015-03-02"},
		{ID: "R2", GroupMembershipID: "Mkid", Profile: &spondapi.RequestPerson{FirstName: "Per", LastName: "Lie"}},
	}
	return g
}

func mainModel(t *testing.T, c *spondapi.Client, g spondapi.Group, me *spondapi.MyProfile) model {
	t.Helper()
	m := newModel(context.Background(), c)
	m.tab = tabGroups // these tests drive the Groups tab
	m.busy = ""
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = updated.(model)
	m.screen = screenGroups // not a first login: no Home, Alerts or Messages loads
	return sendAll(m, groupsMsg{me: me, groups: []spondapi.Group{g}})
}

func TestPendingRequestsNeedMembersPermission(t *testing.T) {
	g := requestGroup()
	if got := pendingRequests([]spondapi.Group{g}, &spondapi.MyProfile{ID: "Pme"}); len(got) != 2 {
		t.Errorf("coach with members permission: %d requests, want 2", len(got))
	}
	g.Members[0].Roles = []string{"r2"} // treasurer: no members permission
	if got := pendingRequests([]spondapi.Group{g}, &spondapi.MyProfile{ID: "Pme"}); len(got) != 0 {
		t.Errorf("treasurer: %d requests, want 0", len(got))
	}
}

func TestRequestsPaneApproveAndReject(t *testing.T) {
	g := requestGroup()
	c, srv := newFakeGroupServer(t, g.Members)
	m := mainModel(t, c, g, &spondapi.MyProfile{ID: "Pme"})

	view := m.View()
	for _, want := range []string{"Pending join requests (2)", "Nina Berg", "New member", "Per Lie", "Existing member"} {
		if !strings.Contains(view, want) {
			t.Errorf("main screen lacks %q", want)
		}
	}

	m = sendAll(m, key("tab"))
	m = sendAll(m, key("enter"))
	if m.requestModal == nil || m.requestModal.p.req.ID != "R1" {
		t.Fatalf("enter did not open R1")
	}
	if !strings.Contains(m.View(), "nina@x.no") || !strings.Contains(m.View(), "2015-03-02") {
		t.Errorf("request modal lacks details")
	}
	m = sendAll(m, key("a"))
	m = sendAll(m, key("down")) // U14
	m = sendAll(m, key(" "))
	m = sendAll(m, key("enter"))
	if m.requestModal != nil || len(m.requestList.items) != 1 || !strings.Contains(m.status, "Approved Nina Berg") {
		t.Fatalf("after approve: modal=%v items=%d status=%q err=%v", m.requestModal != nil, len(m.requestList.items), m.status, m.err)
	}
	if !strings.Contains(asJSON(t, m.allGroups[0].Members), "Mapproved") {
		t.Errorf("approved member not added to the group")
	}

	// Click the remaining request (first row of the pane) and reject it.
	m = sendAll(m, tea.MouseMsg{X: 3, Y: m.requestsTop() + requestListHeader, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	if m.requestModal == nil || m.requestModal.p.req.ID != "R2" {
		t.Fatalf("click did not open R2")
	}
	if !strings.Contains(m.View(), "Claims membership") {
		t.Errorf("existing-member request should show the claimed membership")
	}
	m = sendAll(m, key("x"))
	m = sendAll(m, key("y"))
	if len(m.requestList.items) != 0 || strings.Contains(m.View(), "Pending join requests") {
		t.Errorf("requests pane should be gone after the last request")
	}

	writes := srv.writes()
	want := []struct{ path, body string }{
		{"/group/G/membershipRequest/R1/approve", `{"subGroups":["sg2"]}`},
		{"/group/G/membershipRequest/R2/reject", `null`},
	}
	if len(writes) != len(want) {
		t.Fatalf("writes = %+v", writes)
	}
	for i, w := range want {
		if writes[i].method != http.MethodPost || writes[i].path != w.path || asJSON(t, writes[i].body) != w.body {
			t.Errorf("write %d = %s %s %s, want POST %s %s", i, writes[i].method, writes[i].path, asJSON(t, writes[i].body), w.path, w.body)
		}
	}
}

func editGroup() spondapi.Group {
	g := adminGroup()
	g.FieldDefs = []spondapi.FieldDef{
		{ID: "f1", Name: "Shirt", Type: "TEXT"},
		{ID: "f2", Name: "Number", Type: "NUMBER"},
		{ID: "f3", Name: "Level", Type: "SINGLE_OPTION", Options: []string{"Beginner", "Advanced"}},
		{ID: "f4", Name: "Allergies", Type: "MULTIPLE_OPTION"},
		{ID: "f5", Name: "Club id", Type: "TEXT", Permission: "CLUB_ADMIN_ONLY"},
	}
	g.Members[2].FirstName, g.Members[2].LastName = "Kari", "Nordmann"
	g.Members[2].PhoneNumber = "+47 123 45 678"
	g.Members[2].Fields = map[string]any{"f1": "M", "f2": float64(7), "f4": "x", "f5": "C-1"}
	return g
}

func TestFieldEditable(t *testing.T) {
	cases := map[string]bool{"TEXT": true, "MULTIPLE_OPTION": false}
	for typ, want := range cases {
		if got, _ := fieldEditable(spondapi.FieldDef{Type: typ}); got != want {
			t.Errorf("%s editable = %v", typ, got)
		}
	}
	if ok, _ := fieldEditable(spondapi.FieldDef{Type: "TEXT", Locked: true}); ok {
		t.Errorf("locked field editable")
	}
	if ok, _ := fieldEditable(spondapi.FieldDef{Type: "TEXT", Locked: true, Required: true}); !ok {
		t.Errorf("required field must stay editable (app shows required fields first)")
	}
	if ok, _ := fieldEditable(spondapi.FieldDef{Type: "TEXT", Permission: "CLUB_ADMIN_ONLY"}); ok {
		t.Errorf("club-admin-only field editable by group admin")
	}
}

func TestEditBody(t *testing.T) {
	g := editGroup()
	f := newMemberEditForm(g.Members[2], g, 80, 30)

	// Untouched: contact details are sent as stored, nothing else.
	body, err := f.body()
	if err != nil {
		t.Fatal(err)
	}
	if got := asJSON(t, body); got != `{"email":null,"id":"Mkid","phoneNumber":"+47 123 45 678"}` {
		t.Errorf("untouched body = %s", got)
	}
	if f.changed(body) {
		t.Errorf("untouched form reported as changed")
	}

	f.inputs[editFirst].SetValue("Karianne")
	f.inputs[editEmail].SetValue(" Kari@X.no ")
	f.inputs[f.rows[editFixed+1].input].SetValue("") // Number cleared
	f.rows[editFixed+2].option = 2                   // Level: Advanced
	body, err = f.body()
	if err != nil {
		t.Fatal(err)
	}
	want := `{"email":"kari@x.no","fields":{"f1":"M","f3":"Advanced","f4":"x","f5":"C-1"},"firstName":"Karianne","id":"Mkid","phoneNumber":"+47 123 45 678"}`
	if got := asJSON(t, body); got != want {
		t.Errorf("edited body = %s\nwant %s", got, want)
	}

	f.inputs[f.rows[editFixed+1].input].SetValue("seven")
	if _, err := f.body(); err == nil || !strings.Contains(err.Error(), "whole number") {
		t.Errorf("non-number accepted: %v", err)
	}
}

func TestEditInModal(t *testing.T) {
	g := editGroup()
	c, srv := newFakeGroupServer(t, g.Members)
	m := membersModel(t, c, g, &spondapi.MyProfile{ID: "Pme"})
	m.group = g
	m = sendAll(m, key("G"))
	m = sendAll(m, key("enter"))
	if !strings.Contains(m.View(), "[e] Edit details") {
		t.Fatalf("edit action missing")
	}
	m = sendAll(m, key("e"))
	view := m.View()
	for _, want := range []string{"Edit Kari Nordmann", "Custom fields", "‹ (none) ›", "multiple choice: edit in the Spond app", "club admins only"} {
		if !strings.Contains(view, want) {
			t.Errorf("edit form lacks %q", want)
		}
	}
	m = sendAll(m, tea.KeyMsg{Type: tea.KeyBackspace})
	m = typeText(m, "!")
	for m.memberModal.edit.rows[m.memberModal.edit.cursor].submit == false {
		m = sendAll(m, key("down"))
	}
	m = sendAll(m, key("enter"))
	if m.err != nil {
		t.Fatalf("save: %v", m.err)
	}
	if m.memberModal.mode != modeView || m.memberModal.m.FirstName != "Kar!" {
		t.Errorf("after save: mode=%v name=%q", m.memberModal.mode, m.memberModal.m.FirstName)
	}
	writes := srv.writes()
	if len(writes) != 1 || asJSON(t, writes[0].body) != `{"email":null,"firstName":"Kar!","id":"Mkid","phoneNumber":"+47 123 45 678"}` {
		t.Errorf("writes = %+v", writes)
	}
}

func TestEditKeepsOddStoredContact(t *testing.T) {
	g := editGroup()
	g.Members[2].PhoneNumber = "12345678" // no country code, as stored by an older client
	f := newMemberEditForm(g.Members[2], g, 80, 30)
	f.inputs[editLast].SetValue("Hansen")
	body, err := f.body()
	if err != nil {
		t.Fatalf("unchanged odd phone blocked the save: %v", err)
	}
	if body["phoneNumber"] != "12345678" || body["lastName"] != "Hansen" {
		t.Errorf("body = %v", body)
	}
	f.inputs[editPhone].SetValue("999")
	if _, err := f.body(); err == nil {
		t.Errorf("a changed invalid phone must be rejected")
	}
}
