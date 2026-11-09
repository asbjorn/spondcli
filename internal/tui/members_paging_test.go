package tui

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/asbjorn/spondcli/internal/spondapi"
)

// pagedServer serves pages keyed by cursor and records the limits asked for.
func pagedServer(t *testing.T, pages map[string]spondapi.MembersPage, rejectAbove int) (*spondapi.Client, func() []string) {
	t.Helper()
	var mu sync.Mutex
	var limits []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		q := r.URL.Query()
		limits = append(limits, q.Get("limit"))
		if rejectAbove > 0 && q.Get("limit") == "2000" {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(`{"message":"limit too large"}`))
			return
		}
		json.NewEncoder(w).Encode(pages[q.Get("cursor")])
	}))
	t.Cleanup(srv.Close)
	c := spondapi.NewClient()
	c.BaseURL = srv.URL + "/"
	return c, func() []string { mu.Lock(); defer mu.Unlock(); return append([]string(nil), limits...) }
}

func ids(ms []spondapi.Membership) []string {
	out := []string{}
	for _, m := range ms {
		out = append(out, m.ID)
	}
	return out
}

func TestMembersPagingLikeApp(t *testing.T) {
	pages := map[string]spondapi.MembersPage{
		"":   {Members: []spondapi.Membership{{ID: "a"}, {ID: "b"}}, NextCursor: "c1"},
		"c1": {Members: nil, NextCursor: "c2"}, // empty page that still has a cursor
		"c2": {Members: []spondapi.Membership{{ID: "b", FirstName: "B2"}, {ID: "c"}}},
	}
	c, limits := pagedServer(t, pages, 0)
	got, err := c.Members(context.Background(), "G")
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"a", "b", "c"}; asJSON(t, ids(got)) != asJSON(t, want) {
		t.Errorf("members = %v, want %v (empty pages must not stop paging; duplicates merge)", ids(got), want)
	}
	if got[1].FirstName != "B2" {
		t.Errorf("a repeated member should be replaced by the later copy")
	}
	for _, l := range limits() {
		if l != "2000" {
			t.Errorf("limit = %s, want 2000 like the app", l)
		}
	}
}

func TestMembersPageFallsBackTo500(t *testing.T) {
	pages := map[string]spondapi.MembersPage{"": {Members: []spondapi.Membership{{ID: "a"}}, NextCursor: "c1"}, "c1": {Members: []spondapi.Membership{{ID: "b"}}}}
	c, limits := pagedServer(t, pages, 2000)
	got, err := c.Members(context.Background(), "G")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Errorf("members = %v", ids(got))
	}
	if want := `["2000","500","500"]`; asJSON(t, limits()) != want {
		t.Errorf("limits = %v, want %s (retry with 500, then keep 500)", limits(), want)
	}
}

func TestMembersRepeatingCursorStops(t *testing.T) {
	pages := map[string]spondapi.MembersPage{"": {Members: []spondapi.Membership{{ID: "a"}}, NextCursor: "c1"}, "c1": {Members: []spondapi.Membership{{ID: "b"}}, NextCursor: "c1"}}
	c, _ := pagedServer(t, pages, 0)
	got, err := c.Members(context.Background(), "G")
	if err != nil || len(got) != 2 {
		t.Errorf("members = %v, err = %v", ids(got), err)
	}
}
