package tui

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/asbjorn/spondcli/internal/spondapi"
)

func adminGroup() spondapi.Group {
	return spondapi.Group{
		ID:                  "G",
		Name:                "Team A",
		AdminsCanAddMembers: true,
		SubGroups:           []spondapi.Subgroup{{ID: "sg1", Name: "U12"}, {ID: "sg2", Name: "U14"}},
		Roles: []spondapi.Role{
			{ID: "r0", Name: "Admin", Permissions: []string{"members", "admins", "settings"}},
			{ID: "r1", Name: "Coach", Permissions: []string{"events", "members"}},
			{ID: "r2", Name: "Treasurer", Permissions: []string{"payments"}},
		},
		ContactPerson: &spondapi.Profile{ID: "Pcontact"},
		Members: []spondapi.Membership{
			{ID: "Mme", Profile: &spondapi.Profile{ID: "Pme"}, Roles: []string{"r1"}, Respondent: true},
			{ID: "Mcontact", Profile: &spondapi.Profile{ID: "Pcontact"}, Roles: []string{"r0"}},
			{ID: "Mkid", Respondent: true},
		},
	}
}

func TestMyPermissions(t *testing.T) {
	me := &spondapi.MyProfile{ID: "Pme"}
	g := adminGroup()
	if p := myPermissions(g, me); !p.manageMembers || p.manageAdmins {
		t.Errorf("coach role: %+v, want members only", p)
	}

	g.Members[0].Roles = []string{"r0"}
	if p := myPermissions(g, me); !p.manageMembers || !p.manageAdmins {
		t.Errorf("main admin role: %+v, want both", p)
	}

	g.Members[0].PendingAdmin = true
	if p := myPermissions(g, me); p.manageMembers || p.manageAdmins {
		t.Errorf("pending admin must grant nothing: %+v", p)
	}

	g.Members[0].PendingAdmin = false
	g.Members[0].Roles = []string{"r2"}
	if p := myPermissions(g, me); p.manageMembers || p.manageAdmins {
		t.Errorf("treasurer role: %+v, want none", p)
	}

	if p := myPermissions(adminGroup(), &spondapi.MyProfile{ID: "someone-else"}); p != (permissions{}) {
		t.Errorf("other profile: %+v", p)
	}
	if p := myPermissions(adminGroup(), nil); p != (permissions{}) {
		t.Errorf("no profile: %+v", p)
	}

	// myMembership alone (members not included) is enough.
	g = adminGroup()
	g.MyMembership = &g.Members[0]
	g.Members = nil
	if p := myPermissions(g, me); !p.manageMembers {
		t.Errorf("myMembership ignored: %+v", p)
	}
}

func TestCanAdd(t *testing.T) {
	g := adminGroup()
	p := permissions{manageMembers: true}
	if !p.canAdd(g, false) || p.canAdd(g, true) {
		t.Errorf("members permission: member=%v admin=%v", p.canAdd(g, false), p.canAdd(g, true))
	}
	g.AdminsCanAddMembers = false
	if p.canAdd(g, false) {
		t.Errorf("adminsCanAddMembers=false must block adding")
	}
}

func asJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestChangeBodies(t *testing.T) {
	cases := map[string]struct {
		got  any
		want string
	}{
		"subgroups":    {subgroupsChange("M1", []string{"sg2", "sg1"}), `{"id":"M1","subGroups":["sg1","sg2"]}`},
		"no subgroups": {subgroupsChange("M1", nil), `{"id":"M1","subGroups":[]}`},
		"roles":        {rolesChange("M1", []string{"r1"}), `{"id":"M1","roles":["r1"]}`},
		"no roles":     {rolesChange("M1", nil), `{"id":"M1","roles":[]}`},
		"make member":  {respondentChange("M1", true), `{"id":"M1","respondent":true}`},
		"unmember":     {respondentChange("M1", false), `{"id":"M1","respondent":false}`},
		"guardian":     {spondapi.GuardianInput{FirstName: "Ola", LastName: "N", Email: "o@x.no"}, `{"firstName":"Ola","lastName":"N","email":"o@x.no"}`},
	}
	for name, tc := range cases {
		if got := asJSON(t, tc.got); got != tc.want {
			t.Errorf("%s = %s\nwant %s", name, got, tc.want)
		}
	}
}

func TestNewMembershipBody(t *testing.T) {
	p := person{FirstName: "Kari", LastName: "Nordmann", Email: "kari@x.no"}
	member := newMembershipBody(p, false, []string{"sg2", "sg1"}, []string{"r0"}, []spondapi.GuardianInput{{FirstName: "Ola", LastName: "Nordmann", PhoneNumber: "+4712345678"}})
	want := `{"email":"kari@x.no","fields":{},"firstName":"Kari","guardians":[{"firstName":"Ola","lastName":"Nordmann","phoneNumber":"+4712345678"}],"lastName":"Nordmann","phoneNumber":null,"respondent":true,"subGroups":["sg1","sg2"]}`
	if got := asJSON(t, member); got != want {
		t.Errorf("member body = %s\nwant %s", got, want)
	}

	p.DateOfBirth = "2014-05-01"
	admin := newMembershipBody(p, true, nil, []string{"r1"}, nil)
	want = `{"dateOfBirth":"2014-05-01","email":"kari@x.no","fields":{},"firstName":"Kari","lastName":"Nordmann","phoneNumber":null,"roles":["r1"]}`
	if got := asJSON(t, admin); got != want {
		t.Errorf("admin body = %s\nwant %s", got, want)
	}
}

func TestPersonNormalize(t *testing.T) {
	got, err := person{FirstName: " Kari ", LastName: "N", Email: " Kari@X.no ", PhoneNumber: "0047 123 45 678"}.normalize(true)
	if err != nil {
		t.Fatal(err)
	}
	if got.FirstName != "Kari" || got.Email != "kari@x.no" || got.PhoneNumber != "+4712345678" {
		t.Errorf("normalize = %+v", got)
	}

	bad := []person{
		{FirstName: "", LastName: "N"},
		{FirstName: "K", LastName: "N", Email: "not-an-email"},
		{FirstName: "K", LastName: "N", PhoneNumber: "12345678"},
		{FirstName: "K", LastName: "N", DateOfBirth: "01.05.2014"},
		{FirstName: "K", LastName: "N", DateOfBirth: "2999-01-01"},
	}
	for _, p := range bad {
		if _, err := p.normalize(true); err == nil {
			t.Errorf("normalize(%+v) should fail", p)
		}
	}
	if _, err := (person{}).normalize(false); err != nil {
		t.Errorf("empty optional person: %v", err)
	}
}

func TestActionGuards(t *testing.T) {
	g := adminGroup()
	me := &spondapi.MyProfile{ID: "Pme"}
	full := actor{group: g, groups: []spondapi.Group{g}, me: me, perms: permissions{manageMembers: true, manageAdmins: true}}
	membersOnly := full
	membersOnly.perms = permissions{manageMembers: true}

	self, contact, kid := g.Members[0], g.Members[1], g.Members[2]

	mustFail := func(name string, err error, want string) {
		t.Helper()
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: err = %v, want containing %q", name, err, want)
		}
	}
	mustFail("delete self", full.checkDelete(self), "yourself")
	mustFail("delete contact person", full.checkDelete(contact), "contact person")
	mustFail("delete admin without admins permission", membersOnly.checkDelete(contact), "admins permission")
	if err := full.checkDelete(kid); err != nil {
		t.Errorf("delete plain member: %v", err)
	}
	if err := (actor{group: g}).checkDelete(kid); err != errNoMemberRights {
		t.Errorf("delete without rights: %v", err)
	}

	mustFail("remove last role of admin-only", full.checkRoles(contact, nil), "only an admin")
	mustFail("roles without admins permission", membersOnly.checkRoles(kid, []string{"r1"}), "admins permission")
	mustFail("own roles", full.checkRoles(self, nil), "your own")
	if err := full.checkRoles(kid, []string{"r1"}); err != nil {
		t.Errorf("give kid a role: %v", err)
	}

	mustFail("unmember a non-admin", full.checkRespondent(kid, false), "not an admin")
	mustFail("member already", full.checkRespondent(kid, true), "already")
	if err := full.checkRespondent(contact, true); err != nil {
		t.Errorf("make admin-only a member: %v", err)
	}
}

func TestClubContactPerson(t *testing.T) {
	club := spondapi.Group{ID: "C", Name: "Club", OrganizationType: "CLUB"}
	linked := spondapi.Group{ID: "G2", Name: "Seniors", Club: &spondapi.GroupClub{ID: "C"}, ContactPerson: &spondapi.Profile{ID: "P9"}}
	a := actor{group: club, groups: []spondapi.Group{club, linked}, perms: permissions{manageMembers: true, manageAdmins: true}}
	err := a.checkDelete(spondapi.Membership{ID: "M9", Profile: &spondapi.Profile{ID: "P9"}})
	if err == nil || !strings.Contains(err.Error(), "Seniors") {
		t.Errorf("club contact person in linked group: %v", err)
	}
}

func TestMemberSections(t *testing.T) {
	card := true
	g := adminGroup()
	g.FieldDefs = []spondapi.FieldDef{{ID: "f1", Name: "Shirt size"}}
	m := spondapi.Membership{
		ID: "M1", FirstName: "Kari", LastName: "N", Gender: "F", Respondent: true, Roles: []string{"r0"},
		SubGroups: []string{"sg1"}, Fields: map[string]any{"f1": "M", "f2": float64(42)},
		HasClubMembershipCard: &card,
		Guardians:             []spondapi.Guardian{{FirstName: "Ola", LastName: "N", Email: "o@x.no", Profile: &spondapi.Profile{ID: "P2"}}},
	}
	got := map[string]string{}
	for _, sec := range memberSections(m, g) {
		for _, f := range sec.fields {
			got[sec.title+"/"+f.label] = f.value
		}
	}
	want := map[string]string{
		"Membership/Type":           "Member and admin",
		"Membership/Admin roles":    "Admin",
		"Membership/Subgroups":      "U12",
		"Personal/Gender":           "Female",
		"Guardians/Guardian 1":      "Ola N (o@x.no, has the app)",
		"Custom fields/Shirt size":  "M",
		"Custom fields/f2":          "42",
		"Club/Club membership card": "Yes",
		"Membership/Has the app":    "No",
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %q, want %q", k, got[k], v)
		}
	}
}

func TestProgressBar(t *testing.T) {
	if s := progressBar(0.5, 0); !strings.Contains(s, "50%") {
		t.Errorf("determinate bar lacks percentage: %q", s)
	}
	if progressBar(-1, 0) == progressBar(-1, 5) {
		t.Errorf("indeterminate bar should move with the tick")
	}
}
