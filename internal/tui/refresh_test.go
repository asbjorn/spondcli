package tui

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/asbjorn/spondcli/internal/spondapi"
	"github.com/asbjorn/spondcli/internal/store"
)

// TestActivitiesSince checks the stream query and that the inclusive bound
// does not report the cursor's activity again.
func TestActivitiesSince(t *testing.T) {
	stream := []spondapi.StreamActivity{
		{ID: "a1", Type: "new_post", Timestamp: "2026-10-27T06:00:00Z", Post: &spondapi.IDRef{ID: "P1"}},
		{ID: "a2", Type: "update_spond", Timestamp: "2026-10-27T06:30:00.5Z", Spond: &spondapi.IDRef{ID: "S1"}},
		{ID: "a3", Type: "post_comment", Timestamp: "2026-10-27T07:00:00Z", Post: &spondapi.IDRef{ID: "P1"}},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if r.URL.Path != "/activities" || q.Get("order") != "asc" || q.Get("max") != "100" {
			t.Errorf("unexpected request %s", r.URL)
		}
		since := parseInstant(q.Get("since"))
		out := []spondapi.StreamActivity{}
		for _, a := range stream {
			if !parseInstant(a.Timestamp).Before(since) {
				out = append(out, a)
			}
		}
		json.NewEncoder(w).Encode(out)
	}))
	defer srv.Close()
	c := spondapi.NewClient()
	c.BaseURL = srv.URL + "/"

	got, cur, err := c.ActivitiesSince(context.Background(), spondapi.StreamCursor{Timestamp: "2026-10-27T06:00:00Z", ID: "a1"})
	if err != nil {
		t.Fatal(err)
	}
	if ids := []string{got[0].ID, got[1].ID}; len(got) != 2 || !slices.Equal(ids, []string{"a2", "a3"}) {
		t.Errorf("got %+v, want a2 and a3", got)
	}
	if cur != (spondapi.StreamCursor{Timestamp: "2026-10-27T07:00:00Z", ID: "a3"}) {
		t.Errorf("cursor = %+v", cur)
	}

	got, cur2, err := c.ActivitiesSince(context.Background(), cur)
	if err != nil || len(got) != 0 || cur2 != cur {
		t.Errorf("nothing new: got %+v, cursor %+v, err %v", got, cur2, err)
	}
}

func upcoming(id string, start time.Time) spondapi.Spond {
	return spondapi.Spond{
		ID:             id,
		Heading:        id,
		StartTimestamp: start.UTC().Format(time.RFC3339),
		EndTimestamp:   start.Add(time.Hour).UTC().Format(time.RFC3339),
		Recipients:     &spondapi.SpondRecipients{Group: &spondapi.RecipientGroup{ID: "G"}},
	}
}

func TestMergeSpond(t *testing.T) {
	m := newModel(context.Background(), spondapi.NewClient())
	m.width, m.height = 100, 40
	base := time.Now().Add(48 * time.Hour)
	f := &m.home
	f.loaded = true
	f.events = []spondapi.Spond{upcoming("A", base), upcoming("C", base.Add(2*time.Hour))}
	f.cursor = 1 // C

	m.mergeSpond(feedHome, upcoming("B", base.Add(time.Hour)))
	if ids := eventIDs(f.events); !slices.Equal(ids, []string{"A", "B", "C"}) {
		t.Fatalf("after insert: %v", ids)
	}
	if f.events[f.cursor].ID != "C" {
		t.Errorf("selection moved off C to %s", f.events[f.cursor].ID)
	}

	// Beyond the last loaded page: paging will bring it.
	m.mergeSpond(feedHome, upcoming("D", base.Add(5*time.Hour)))
	if len(f.events) != 3 {
		t.Errorf("arrangement after the loaded pages was added")
	}

	changed := upcoming("A", base)
	changed.Heading = "A renamed"
	f.detail = newDetailModal(f.events[0], spondapi.Group{}, 100, 30)
	m.mergeSpond(feedHome, changed)
	if f.events[0].Heading != "A renamed" || f.detail.s.Heading != "A renamed" {
		t.Errorf("update not applied to feed and modal")
	}

	// The group feed only takes arrangements of its group.
	m.group = spondapi.Group{ID: "OTHER"}
	m.groupFeed.loaded, m.groupFeed.done = true, true
	m.mergeSpond(feedGroup, upcoming("E", base))
	if len(m.groupFeed.events) != 0 {
		t.Errorf("arrangement of another group added to the group feed")
	}

	m.removeSpond(feedHome, "A")
	if ids := eventIDs(f.events); !slices.Equal(ids, []string{"B", "C"}) || f.detail != nil {
		t.Errorf("after delete: %v, modal open %v", ids, f.detail != nil)
	}
	if f.events[f.cursor].ID != "C" {
		t.Errorf("selection moved off C after delete")
	}
}

func eventIDs(ss []spondapi.Spond) []string {
	var ids []string
	for _, s := range ss {
		ids = append(ids, s.ID)
	}
	return ids
}

func TestPostFetchedAddsNewPostOnTop(t *testing.T) {
	m := newModel(context.Background(), spondapi.NewClient())
	m.width, m.height = 100, 40
	m.screen = screenPosts
	m.group = spondapi.Group{ID: "G"}
	m.posts = []spondapi.Post{{ID: "old", GroupID: "G", Timestamp: "2026-10-20T10:00:00Z"}}

	updated, _ := m.Update(postFetchedMsg{id: "sub", post: &spondapi.Post{ID: "sub", GroupID: "G", Timestamp: "2026-10-27T10:00:00Z", SubGroupIDs: []string{"sg"}}})
	m = updated.(model)
	if len(m.posts) != 1 {
		t.Errorf("subgroup post added to the whole group's feed")
	}

	updated, _ = m.Update(postFetchedMsg{id: "new", post: &spondapi.Post{ID: "new", GroupID: "G", Timestamp: "2026-10-27T10:00:00Z", Title: "Hello"}})
	m = updated.(model)
	if len(m.posts) != 2 || m.posts[0].ID != "new" || m.posts[m.postCursor].ID != "old" {
		t.Errorf("new post not on top with selection kept: %+v cursor %d", m.posts, m.postCursor)
	}

	updated, _ = m.Update(postFetchedMsg{id: "old"}) // deleted
	m = updated.(model)
	if len(m.posts) != 1 || m.posts[0].ID != "new" {
		t.Errorf("deleted post still shown: %+v", m.posts)
	}
}

func TestChatsPolledMergesAndKeepsSelection(t *testing.T) {
	m := newModel(context.Background(), spondapi.NewClient())
	m.width, m.height = 100, 40
	ch := &m.chat
	ch.loaded = true
	ch.threads = []spondapi.ChatThread{
		{ID: "A", NewestTimestamp: "2026-10-27T08:00:00Z"},
		{ID: "B", NewestTimestamp: "2026-10-26T08:00:00Z"},
	}
	ch.cursor = 1 // B

	updated, _ := m.Update(chatsPolledMsg{threads: []spondapi.ChatThread{
		{ID: "B", NewestTimestamp: "2026-10-27T09:00:00.5Z", Unread: true},
		{ID: "C", NewestTimestamp: "2026-10-27T08:30:00Z"},
	}})
	m = updated.(model)
	var ids []string
	for _, th := range m.chat.threads {
		ids = append(ids, th.ID)
	}
	if !slices.Equal(ids, []string{"B", "C", "A"}) {
		t.Errorf("order = %v", ids)
	}
	if m.chat.threads[m.chat.cursor].ID != "B" || !m.chat.threads[0].Unread {
		t.Errorf("selection or update lost: cursor %d, %+v", m.chat.cursor, m.chat.threads[0])
	}
}

func TestAppendMessages(t *testing.T) {
	m := newModel(context.Background(), spondapi.NewClient())
	m.width, m.height = 100, 40
	m.chat.open = &spondapi.ChatThread{ID: "T"}
	m.chat.msgs = []spondapi.ChatMessage{{MsgNum: 4, Text: "a"}, {MsgNum: 5, Text: "b"}}

	updated, _ := m.appendMessages([]spondapi.ChatMessage{{MsgNum: 5, Text: "b", Deleted: true}, {MsgNum: 6, Text: "c"}})
	m = updated.(model)
	if n := len(m.chat.msgs); n != 3 || m.chat.msgs[2].Text != "c" || !m.chat.msgs[1].Deleted {
		t.Errorf("msgs = %+v", m.chat.msgs)
	}

	// A gap: more arrived than one page; start from the newest page.
	updated, _ = m.appendMessages([]spondapi.ChatMessage{{MsgNum: 60}, {MsgNum: 61}})
	m = updated.(model)
	if len(m.chat.msgs) != 2 || m.chat.msgs[0].MsgNum != 60 {
		t.Errorf("gap not handled: %+v", m.chat.msgs)
	}
}

func TestStorePutAndRemovePost(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "posts.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	p := spondapi.Post{ID: "P", GroupID: "G", Timestamp: "2026-10-27T10:00:00Z", Title: "Dugnad", Comments: []spondapi.Comment{{ID: "c", Text: "kaffe"}}}
	if err := st.PutPost(ctx, p); err != nil {
		t.Fatal(err)
	}
	hits, err := st.Search(ctx, "G", "", "kaffe")
	if err != nil || len(hits) != 1 {
		t.Fatalf("search after put: %v, %v", hits, err)
	}
	if err := st.RemovePost(ctx, "P"); err != nil {
		t.Fatal(err)
	}
	if hits, _ := st.Search(ctx, "G", "", "dugnad"); len(hits) != 0 {
		t.Errorf("removed post still found")
	}
}
