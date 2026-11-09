package store

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/asbjorn/spondcli/internal/spondapi"
)

// fakeFeeds serves GET posts from a map of subGroupId ("" for the group
// feed) to posts, newest first, honoring max and the prevId cursor.
func fakeFeeds(t *testing.T) (*spondapi.Client, func(map[string][]spondapi.Post)) {
	t.Helper()
	var mu sync.Mutex
	feeds := map[string][]spondapi.Post{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		q := r.URL.Query()
		if q.Get("includeComments") != "true" {
			t.Errorf("sync must request comments, got %q", q.Get("includeComments"))
		}
		feed := feeds[q.Get("subGroupId")]
		if prev := q.Get("prevId"); prev != "" {
			for i, p := range feed {
				if p.ID == prev {
					feed = feed[i+1:]
					break
				}
			}
		}
		var max int
		json.Unmarshal([]byte(q.Get("max")), &max)
		if len(feed) > max {
			feed = feed[:max]
		}
		json.NewEncoder(w).Encode(nonNilPosts(feed))
	}))
	t.Cleanup(srv.Close)
	c := spondapi.NewClient()
	c.BaseURL = srv.URL + "/"
	return c, func(f map[string][]spondapi.Post) {
		mu.Lock()
		defer mu.Unlock()
		feeds = f
	}
}

func nonNilPosts(p []spondapi.Post) []spondapi.Post {
	if p == nil {
		return []spondapi.Post{}
	}
	return p
}

func TestStoreSyncAndSearch(t *testing.T) {
	ctx := context.Background()
	st, err := Open(filepath.Join(t.TempDir(), "posts.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	c, setFeeds := fakeFeeds(t)
	g := spondapi.Group{ID: "G", SubGroups: []spondapi.Subgroup{{ID: "SG1"}, {ID: "SG2"}}}
	shared := spondapi.Post{ID: "P3", Timestamp: "2026-10-03T10:00:00.000Z", Title: "Dugnad", Body: "Rydding av banen", SubGroupIDs: []string{"SG1", "SG2"}}
	setFeeds(map[string][]spondapi.Post{
		"": {
			{ID: "P2", Timestamp: "2026-10-02T10:00:00.000Z", Title: "Kamp", Body: "Bortekamp på lørdag",
				Comments: []spondapi.Comment{{ID: "C1", Text: "Hvem kjører?", Children: []spondapi.Comment{{ID: "C2", Text: "Vi tar kafé etterpå"}}}}},
			{ID: "P1", Timestamp: "2026-10-01T10:00:00.000Z", Title: "Trening", Body: "Husk drikkeflaske"},
		},
		"SG1": {shared},
		"SG2": {shared, {ID: "P4", Timestamp: "2026-09-30T10:00:00.000Z", Body: "Kun for SG2"}},
	})

	stats, err := st.sync(ctx, c, g)
	if err != nil {
		t.Fatal(err)
	}
	if want := (SyncStats{Posts: 4, Added: 4}); stats != want {
		t.Errorf("first sync = %+v, want %+v", stats, want)
	}

	ids := func(subgroupID, q string) string {
		t.Helper()
		hits, err := st.Search(ctx, "G", subgroupID, q)
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, h := range hits {
			out = append(out, h.Post.ID)
		}
		return strings.Join(out, ",")
	}
	for _, tc := range []struct{ subgroup, query, want string }{
		{"", "bortekamp", "P2"},
		{"", "KAFE", "P2"}, // case and accent insensitive, and searches replies
		{"", "drikke", "P1"},
		{"", "dugnad banen", "P3"}, // all words, across title and body
		{"", "dugnad trening", ""},
		{"", "kun", "P4"}, // subgroup-only post is synced
		{"SG1", "kun", ""},
		{"SG1", "dugnad", "P3"},
		{"", `"AND OR*`, ""}, // FTS syntax in input is literal
	} {
		if got := ids(tc.subgroup, tc.query); got != tc.want {
			t.Errorf("search(%q, %q) = %q, want %q", tc.subgroup, tc.query, got, tc.want)
		}
	}

	hits, err := st.Search(ctx, "G", "", "kafe")
	if err != nil {
		t.Fatal(err)
	}
	h := hits[0]
	if got := h.CommentMatches(); len(h.Comments) != 2 || len(got) != 1 || got[0] != 1 {
		t.Fatalf("comments = %q, matches %v; want the reply (index 1) to match", h.Comments, got)
	}
	if !strings.Contains(h.Comments[1], MarkStart+"kafé"+MarkEnd) || h.PostMatches() {
		t.Errorf("hit = %+v, want the reply marked and the post not", h)
	}

	// Second sync: P1 deleted, P2 edited, the rest unchanged.
	setFeeds(map[string][]spondapi.Post{
		"":    {{ID: "P2", Timestamp: "2026-10-02T10:00:00.000Z", Title: "Kamp", Body: "Hjemmekamp på lørdag"}},
		"SG1": {shared},
		"SG2": {shared, {ID: "P4", Timestamp: "2026-09-30T10:00:00.000Z", Body: "Kun for SG2"}},
	})
	if stats, err = st.sync(ctx, c, g); err != nil {
		t.Fatal(err)
	}
	if want := (SyncStats{Posts: 3, Changed: 1, Removed: 1}); stats != want {
		t.Errorf("second sync = %+v, want %+v", stats, want)
	}
	if got := ids("", "drikke"); got != "" {
		t.Errorf("deleted post still found: %q", got)
	}
	if got := ids("", "bortekamp") + "|" + ids("", "hjemmekamp"); got != "|P2" {
		t.Errorf("edited post: got %q, want |P2", got)
	}
}

func TestStoreSyncPages(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "posts.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	c, setFeeds := fakeFeeds(t)

	var feed []spondapi.Post
	for i := range spondapi.SyncPageSize + 3 {
		feed = append(feed, spondapi.Post{ID: fmt.Sprintf("P%d", i), Body: "x"})
	}
	setFeeds(map[string][]spondapi.Post{"": feed})
	stats, err := st.sync(context.Background(), c, spondapi.Group{ID: "G"})
	if err != nil {
		t.Fatal(err)
	}
	if stats.Posts != spondapi.SyncPageSize+3 {
		t.Errorf("synced %d posts, want %d", stats.Posts, spondapi.SyncPageSize+3)
	}
}
