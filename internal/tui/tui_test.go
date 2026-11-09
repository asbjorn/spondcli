package tui

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/asbjorn/spondcli/internal/spondapi"
	"github.com/asbjorn/spondcli/internal/store"
)

func TestGroupMenuAndEventsView(t *testing.T) {
	m := newModel(context.Background(), spondapi.NewClient())
	m.tab = tabGroups // these tests drive the Groups tab
	m.busy = ""       // ignore any SPOND_* credentials in the environment
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	m = updated.(model)

	m.group = spondapi.Group{ID: "G", Name: "Team A", SubGroups: []spondapi.Subgroup{{ID: "sg1", Name: "Juniors"}}}
	m.screen = screenGroupMenu
	menu := m.View()
	for _, want := range []string{"Team A", "Posts", "Arrangements", "Members"} {
		if !strings.Contains(menu, want) {
			t.Errorf("menu view lacks %q", want)
		}
	}

	m.screen = screenEvents
	m.groupFeed.events = []spondapi.Spond{{
		ID:              "S1",
		Type:            "RECURRING",
		Heading:         "Training",
		StartTimestamp:  "2026-10-25T17:00:00Z",
		EndTimestamp:    "2026-10-25T18:30:00Z",
		Cancelled:       true,
		CancelledReason: "Rain",
		Location:        &spondapi.SpondLocation{Feature: "Main field", Address: "Main field"},
		ResponseCounts:  &spondapi.ResponseCounts{AcceptedCount: 7, DeclinedCount: 2, UnansweredCount: 4},
		Recipients:      &spondapi.SpondRecipients{Group: &spondapi.RecipientGroup{ID: "G", SubGroups: []spondapi.Subgroup{{ID: "sg1"}}}},
	}}
	m.groupFeed.done = true
	m.refreshEvents(feedGroup)
	view := m.View()
	for _, want := range []string{
		"Arrangements › Upcoming", "Training", "Cancelled", "Recurring",
		"@ Main field", "Juniors", "7 accepted", "2 declined", "4 unanswered",
		"Reason: Rain", "end of arrangements",
	} {
		if !strings.Contains(view, want) {
			t.Errorf("events view lacks %q", want)
		}
	}
	if strings.Count(view, "Main field") != 1 {
		t.Errorf("duplicate location parts should collapse")
	}
}

func TestStaleResultsAreDropped(t *testing.T) {
	m := newModel(context.Background(), spondapi.NewClient())
	m.tab = tabGroups // these tests drive the Groups tab
	m.busy = ""
	m.group = spondapi.Group{ID: "G"}
	m.open("p")
	old := m.gen
	m.open("a") // user switched screens before posts arrived

	updated, _ := m.Update(postsMsg{gen: old, posts: []spondapi.Post{{ID: "x"}}})
	m = updated.(model)
	if len(m.posts) != 0 {
		t.Errorf("stale posts were applied")
	}
	if m.busy == "" {
		t.Errorf("stale result cleared the spinner of the running load")
	}
}

func TestSubgroupPicker(t *testing.T) {
	m := newModel(context.Background(), spondapi.NewClient())
	m.tab = tabGroups // these tests drive the Groups tab
	m.busy = ""
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	m = updated.(model)
	m.group = spondapi.Group{
		ID:        "G",
		Name:      "Team A",
		SubGroups: []spondapi.Subgroup{{ID: "sg1", Name: "Juniors"}, {ID: "sg2", Name: "Seniors"}},
		Members:   []spondapi.Membership{{ID: "a", SubGroups: []string{"sg2"}}, {ID: "b"}},
	}
	m.screen = screenGroupMenu

	press := func(keys ...string) {
		for _, k := range keys {
			msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
			switch k {
			case "enter":
				msg = tea.KeyMsg{Type: tea.KeyEnter}
			case "down":
				msg = tea.KeyMsg{Type: tea.KeyDown}
			case "esc":
				msg = tea.KeyMsg{Type: tea.KeyEsc}
			}
			updated, _ := m.Update(msg)
			m = updated.(model)
		}
	}

	if !strings.Contains(m.View(), "Showing: the whole group") {
		t.Fatalf("menu lacks subgroup entry:\n%s", m.View())
	}

	press("s")
	if m.screen != screenSubgroups {
		t.Fatalf("s did not open the picker")
	}
	view := m.View()
	for _, want := range []string{"Whole group", "Juniors", "Seniors", "1 member", "2 members"} {
		if !strings.Contains(view, want) {
			t.Errorf("picker lacks %q", want)
		}
	}

	press("down", "down", "enter") // Whole group → Juniors → Seniors
	if m.screen != screenGroupMenu || m.subgroup == nil || m.subgroup.ID != "sg2" {
		t.Fatalf("picking Seniors: screen=%v subgroup=%v", m.screen, m.subgroup)
	}
	if got := m.scope(); got != (spondapi.Scope{GroupID: "G", SubgroupID: "sg2"}) {
		t.Errorf("scope = %+v", got)
	}
	if !strings.Contains(m.View(), "Team A › Seniors") {
		t.Errorf("crumb does not show the subgroup")
	}

	press("s")
	if item, ok := m.subgroups.SelectedItem().(subgroupItem); !ok || item.sg == nil || item.sg.ID != "sg2" {
		t.Errorf("picker should reopen on the current subgroup")
	}
	press("esc")
	if m.subgroup == nil || m.subgroup.ID != "sg2" {
		t.Errorf("esc must keep the current subgroup")
	}

	press("m")
	if m.screen != screenMembers || m.scope().SubgroupID != "sg2" {
		t.Errorf("members screen lost the subgroup scope")
	}

	// Back out to the group list the way a user does; picking the group
	// again resets the scope.
	m.groups.SetItems([]list.Item{groupItem{g: m.group}})
	press("esc", "esc")
	if m.screen != screenGroups {
		t.Fatalf("esc esc should reach the group list, got screen %v", m.screen)
	}
	press("enter")
	if m.subgroup != nil {
		t.Errorf("entering a group should reset to the whole group")
	}
}

func TestPostSearchView(t *testing.T) {
	m := newModel(context.Background(), spondapi.NewClient())
	m.tab = tabGroups // these tests drive the Groups tab
	m.busy = ""
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	m = updated.(model)
	m.group = spondapi.Group{ID: "G", Name: "Team A"}
	m.open("p")
	m.busy = ""
	m.posts, m.postsDone = []spondapi.Post{{ID: "feed", Body: "Feed post"}}, true

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	m = updated.(model)
	if !m.searching {
		t.Fatal("/ did not open the search input")
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("kafe")})
	m = updated.(model)
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(model)
	if m.searching || cmd == nil || m.busy == "" {
		t.Fatalf("enter did not start a search (searching=%v busy=%q)", m.searching, m.busy)
	}

	reply := spondapi.Comment{ID: "C2", FromProfileID: "U2", Timestamp: "2026-10-02T12:00:00Z", Text: "Vi tar kafé etterpå"}
	hitPost := spondapi.Post{ID: "P2", Title: "Kamp", Body: "Bortekamp", Comments: []spondapi.Comment{
		{ID: "C1", Text: "Hvem kjører?", Children: []spondapi.Comment{reply}},
	}}
	updated, _ = m.Update(searchMsg{gen: m.gen, query: "kafe", hits: []store.SearchHit{
		{Post: spondapi.Post{ID: "P1", Title: "Kafé"}, Title: store.MarkStart + "Kafé" + store.MarkEnd},
		{
			Post: hitPost, Title: "Kamp", Body: "Bortekamp",
			Comments: []string{"Hvem kjører?", "Vi tar " + store.MarkStart + "kafé" + store.MarkEnd + " etterpå"},
		},
	}})
	m = updated.(model)
	view := m.View()
	for _, want := range []string{"“kafe” (2)", "Kamp", "Vi tar", "enter/click open", "esc back to posts"} {
		if !strings.Contains(view, want) {
			t.Errorf("search view lacks %q", want)
		}
	}
	if strings.Contains(view, "Feed post") || strings.Contains(view, store.MarkStart) || strings.Contains(view, "Hvem kjører") {
		t.Errorf("search view shows the feed, raw markers or a comment that does not match")
	}

	// Select the second result and open it: every comment shows, and the
	// match counter covers only the matching reply.
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = updated.(model)
	if m.postCursor != 1 {
		t.Fatalf("down: cursor = %d, want 1", m.postCursor)
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("g")})
	m = updated.(model)
	if m.postCursor != 0 || m.feed.YOffset != 0 {
		t.Fatalf("g: cursor = %d offset = %d, want top", m.postCursor, m.feed.YOffset)
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnd})
	m = updated.(model)
	if m.postCursor != 1 {
		t.Fatalf("end: cursor = %d, want 1", m.postCursor)
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(model)
	if m.postModal == nil || m.postModal.hit.Post.ID != "P2" {
		t.Fatal("enter did not open the selected post")
	}
	view = m.View()
	for _, want := range []string{"1 match", "2 comments", "Hvem kjører?", "Vi tar", "esc close"} {
		if !strings.Contains(view, want) {
			t.Errorf("post view lacks %q", want)
		}
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(model)
	if m.postModal != nil || m.query != "kafe" {
		t.Fatalf("esc should close the post and keep the results")
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(model)
	if m.screen != screenPosts || m.query != "" || !strings.Contains(m.View(), "Feed post") {
		t.Errorf("esc did not return to the feed")
	}
}

func TestPostModalMatches(t *testing.T) {
	h := store.SearchHit{
		Post:     spondapi.Post{ID: "P", Title: "Dugnad", Comments: []spondapi.Comment{{ID: "A", Text: "ja"}, {ID: "B", Text: "dugnad"}, {ID: "C", Text: "dugnad"}}},
		Title:    store.MarkStart + "Dugnad" + store.MarkEnd,
		Comments: []string{"ja", store.MarkStart + "dugnad" + store.MarkEnd, store.MarkStart + "dugnad" + store.MarkEnd},
	}
	pm := newPostModal(h, spondapi.Group{}, 80, 8)
	if want := []int{-1, 1, 2}; !slices.Equal(pm.targets, want) {
		t.Fatalf("targets = %v, want %v", pm.targets, want)
	}
	pm.jump(2)
	if pm.vp.YOffset == 0 || !strings.Contains(pm.header(), "Match 3 of 3") {
		t.Errorf("jump to last match: offset %d, header %q", pm.vp.YOffset, pm.header())
	}
	pm.jump(3) // wraps to the post
	if pm.cur != 0 || pm.vp.YOffset != 0 {
		t.Errorf("wrap: cur %d offset %d", pm.cur, pm.vp.YOffset)
	}
}

func TestAutoLoginHidesForm(t *testing.T) {
	c := spondapi.NewClient()
	c.Token = "cached"
	m := newModel(context.Background(), c)
	m.tab = tabGroups // these tests drive the Groups tab
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = updated.(model)
	view := m.View()
	if strings.Contains(view, "Password") || !strings.Contains(view, "Loading groups") {
		t.Errorf("cached login shows the form:\n%s", view)
	}

	// The cached token was rejected: the form must appear.
	updated, _ = m.Update(groupsMsg{err: &spondapi.APIError{Status: 401}})
	m = updated.(model)
	if view := m.View(); !strings.Contains(view, "Password") || !strings.Contains(view, "Session expired") {
		t.Errorf("expired login hides the form:\n%s", view)
	}
}
