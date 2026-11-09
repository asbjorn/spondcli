package roster

import (
	"bytes"
	"encoding/csv"
	"testing"

	"github.com/asbjorn/spondcli/internal/spondapi"
)

func TestWriteMembersCSV(t *testing.T) {
	g := spondapi.Group{
		SubGroups: []spondapi.Subgroup{{ID: "sg1", Name: "U12, Girls"}},
		Roles:     []spondapi.Role{{ID: "r1", Name: "Coach"}},
	}
	members := []spondapi.Membership{
		{
			ID:        "m1",
			FirstName: "Kari",
			LastName:  "Nordmann",
			Email:     "kari@example.com",
			SubGroups: []string{"sg1", "unknown"},
			Roles:     []string{"r1"},
			Guardians: []spondapi.Guardian{{FirstName: "Ola", LastName: "Nordmann", PhoneNumber: "+4712345678"}},
			Profile:   &spondapi.Profile{ID: "p1"},
		},
		{ID: "m2", FirstName: "Per"},
	}

	var buf bytes.Buffer
	if err := WriteCSV(&buf, Rows(g, members)); err != nil {
		t.Fatal(err)
	}

	records, err := csv.NewReader(&buf).ReadAll()
	if err != nil {
		t.Fatalf("output is not valid CSV: %v", err)
	}
	if len(records) != 3 {
		t.Fatalf("got %d records, want header + 2", len(records))
	}

	col := map[string]int{}
	for i, h := range records[0] {
		col[h] = i
	}
	first := records[1]
	checks := map[string]string{
		"member_id":       "m1",
		"email":           "kari@example.com",
		"subgroups":       "U12, Girls; unknown",
		"roles":           "Coach",
		"guardians":       "Ola Nordmann (+4712345678)",
		"profile_id":      "p1",
		"has_app_account": "true",
	}
	for name, want := range checks {
		if got := first[col[name]]; got != want {
			t.Errorf("%s = %q, want %q", name, got, want)
		}
	}
	if got := records[2][col["has_app_account"]]; got != "false" {
		t.Errorf("member without profile: has_app_account = %q, want %q", got, "false")
	}
}

func TestInSubgroup(t *testing.T) {
	members := []spondapi.Membership{
		{ID: "a", SubGroups: []string{"sg1", "sg2"}},
		{ID: "b", SubGroups: []string{"sg2"}},
		{ID: "c"},
	}
	got := InSubgroup(members, "sg1")
	if len(got) != 1 || got[0].ID != "a" {
		t.Errorf("inSubgroup(sg1) = %v, want [a]", got)
	}
	if got := InSubgroup(members, "none"); len(got) != 0 {
		t.Errorf("inSubgroup(none) = %v, want empty", got)
	}
}

func TestDefaultCSVName(t *testing.T) {
	cases := map[string]string{
		"Moen Marin IL / G12": "moen-marin-il-g12-members.csv",
		"   ":                 "group-members.csv",
		"Ålesund":             "ålesund-members.csv",
	}
	for in, want := range cases {
		if got := DefaultCSVName(in); got != want {
			t.Errorf("defaultCSVName(%q) = %q, want %q", in, got, want)
		}
	}
}
