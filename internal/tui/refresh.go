package tui

import (
	"context"
	"slices"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/asbjorn/spondcli/internal/spondapi"
	"github.com/asbjorn/spondcli/internal/store"
)

// Keeping the TUI up to date works like the app (see docs/API.md, "How
// the app stays up to date"): every pollInterval it reads the activity
// stream from a cursor and re-fetches only the arrangements and posts the
// new activities name. Chat messages are not in the stream (the app learns
// of them by push), so the first page of threads is polled as well.

// pollInterval is the app's PollingConfig interval for SyncNewActivities.
const pollInterval = time.Minute

type (
	pollTickMsg struct{}
	// streamMsg carries the activities since the cursor; init is set for
	// the first call, which only places the cursor.
	streamMsg struct {
		init       bool
		activities []spondapi.StreamActivity
		cursor     spondapi.StreamCursor
		err        error
	}
	spondFetchedMsg struct {
		id    string
		spond *spondapi.Spond // nil when it is gone
		err   error
	}
	postFetchedMsg struct {
		id   string
		post *spondapi.Post // nil when it is gone
		err  error
	}
	chatsPolledMsg struct {
		threads []spondapi.ChatThread
		err     error
	}
)

func pollTick() tea.Cmd {
	return tea.Tick(pollInterval, func(time.Time) tea.Msg { return pollTickMsg{} })
}

// startPolling places the stream cursor at the newest activity and starts
// the poll timer. Called once, after the first login.
func (m *model) startPolling() tea.Cmd {
	m.polling = true
	ctx, c := m.ctx, m.client
	return tea.Batch(pollTick(), func() tea.Msg {
		a, err := c.NewestActivity(ctx)
		msg := streamMsg{init: true, err: err}
		if a != nil {
			msg.cursor = spondapi.StreamCursor{Timestamp: a.Timestamp, ID: a.ID}
		} else if err == nil {
			msg.cursor = spondapi.StreamCursor{Timestamp: time.Now().UTC().Format(time.RFC3339Nano)}
		}
		return msg
	})
}

// poll runs one round. A round still running (a slow network) is not
// overlapped; the next tick tries again.
func (m *model) poll() tea.Cmd {
	cmds := []tea.Cmd{pollTick()}
	if m.chat.loaded && !m.chat.loading {
		ctx, c := m.ctx, m.client
		cmds = append(cmds, func() tea.Msg {
			threads, err := c.Chats(ctx, chatPageSize, "")
			return chatsPolledMsg{threads: threads, err: err}
		})
	}
	if m.polling {
		return tea.Batch(cmds...)
	}
	if m.streamCursor.Timestamp == "" { // placing the cursor failed; try again
		return tea.Batch(append(cmds, m.startPolling())...)
	}
	m.polling = true
	ctx, c, cur := m.ctx, m.client, m.streamCursor
	return tea.Batch(append(cmds, func() tea.Msg {
		items, next, err := c.ActivitiesSince(ctx, cur)
		return streamMsg{activities: items, cursor: next, err: err}
	})...)
}

// groupActivities change a group, its members or join requests, so the
// group list is reloaded.
var groupActivities = []string{
	"update_group", "rename_group", "join_group", "leave_group", "delete_group",
	"become_admin", "become_parent", "update_profile",
	"new_member_request", "member_requests_updated", "member_request_approved",
	"new_group_member_from_club", "new_group_members_from_club",
}

// applyStream re-fetches what the new activities name. Errors are not
// shown: the cursor stays, so the next poll retries.
func (m model) applyStream(msg streamMsg) (tea.Model, tea.Cmd) {
	m.polling = false
	if msg.cursor.Timestamp != "" {
		m.streamCursor = msg.cursor
	}
	if len(msg.activities) == 0 {
		return m, nil
	}

	var cmds []tea.Cmd
	if !m.alerts.loading {
		cmds = append(cmds, m.loadAlerts())
	}
	seen := map[string]bool{}
	reloadGroups := false
	ctx, c, st := m.ctx, m.client, m.store
	for _, a := range msg.activities {
		if slices.Contains(groupActivities, a.Type) {
			reloadGroups = true
		}
		if a.Spond != nil && !seen[a.Spond.ID] {
			seen[a.Spond.ID] = true
			id := a.Spond.ID
			cmds = append(cmds, func() tea.Msg {
				s, err := c.Spond(ctx, id)
				if spondapi.IsNotFound(err) {
					s, err = nil, nil
				}
				return spondFetchedMsg{id: id, spond: s, err: err}
			})
		}
		if a.Post != nil && !seen[a.Post.ID] {
			seen[a.Post.ID] = true
			id, groupID := a.Post.ID, ""
			if a.Group != nil {
				if g, ok := m.groupByID(a.Group.ID); ok {
					groupID = g.ID // the activity may name a subgroup
				}
			}
			cmds = append(cmds, func() tea.Msg { return fetchPost(ctx, c, st, id, groupID) })
		}
	}
	if reloadGroups {
		cmds = append(cmds, m.reloadGroupsCmd())
	}
	return m, tea.Batch(cmds...)
}

// fetchPost re-fetches a post and brings the search cache in line. The cache
// is best effort: if writing fails, the group's next full sync fixes it.
func fetchPost(ctx context.Context, c *spondapi.Client, st *store.Store, id, groupID string) tea.Msg {
	p, err := c.Post(ctx, id)
	switch {
	case spondapi.IsNotFound(err):
		if st != nil {
			_ = st.RemovePost(ctx, id)
		}
		return postFetchedMsg{id: id}
	case err != nil:
		return postFetchedMsg{id: id, err: err}
	}
	if p.GroupID == "" {
		p.GroupID = groupID
	}
	if st != nil && p.GroupID != "" {
		_ = st.PutPost(ctx, *p)
	}
	return postFetchedMsg{id: id, post: p}
}

// reloadGroupsCmd fetches the groups again without leaving the screen.
func (m model) reloadGroupsCmd() tea.Cmd {
	load := m.loadGroupsCmd()
	return func() tea.Msg {
		msg := load().(groupsMsg)
		msg.reload = true
		return msg
	}
}

// applyGroupsReload swaps in fresh groups, keeping the open group and
// subgroup.
func (m model) applyGroupsReload(msg groupsMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		return m, nil
	}
	if msg.me != nil {
		m.me = msg.me
	}
	m.allGroups = msg.groups
	m.profiles = profileNames(msg.groups, m.me)
	for _, g := range msg.groups {
		if g.ID != m.group.ID {
			continue
		}
		m.group = g
		if m.subgroup != nil {
			id := m.subgroup.ID
			m.subgroup = nil
			for i := range m.group.SubGroups {
				if m.group.SubGroups[i].ID == id {
					m.subgroup = &m.group.SubGroups[i]
				}
			}
		}
	}
	m.refreshRequests()
	m.resize()
	return m, m.syncGroupItems()
}

func (m model) applySpondFetched(msg spondFetchedMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		return m, nil
	}
	for _, k := range []feedKind{feedHome, feedGroup, feedAlerts} {
		if msg.spond == nil {
			m.removeSpond(k, msg.id)
		} else {
			m.mergeSpond(k, *msg.spond)
		}
	}
	return m, nil
}

// mergeSpond replaces s in feed k, or adds it when it is new and belongs
// in the part of the feed that is loaded.
func (m *model) mergeSpond(k feedKind, s spondapi.Spond) {
	f := m.feedOf(k)
	if f.detail != nil && f.detail.s.ID == s.ID {
		f.detail.update(s)
	}
	if i := slices.IndexFunc(f.events, func(e spondapi.Spond) bool { return e.ID == s.ID }); i >= 0 {
		f.events[i] = s
	} else if m.belongsInFeed(k, s) {
		start := parseInstant(s.StartTimestamp)
		i := slices.IndexFunc(f.events, func(e spondapi.Spond) bool { return parseInstant(e.StartTimestamp).After(start) })
		if i < 0 {
			i = len(f.events)
		}
		f.events = slices.Insert(f.events, i, s)
		if i <= f.cursor && len(f.events) > 1 {
			f.cursor++ // keep the same arrangement selected
		}
	} else {
		return
	}
	m.refreshEvents(k)
}

// belongsInFeed reports whether a new arrangement goes in the upcoming list
// of feed k: in its scope, not over, and not past the last loaded page
// (paging fetches those).
func (m *model) belongsInFeed(k feedKind, s spondapi.Spond) bool {
	f := m.feedOf(k)
	if k == feedAlerts || !f.loaded || f.past {
		return false
	}
	now := time.Now()
	startOfToday := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	if parseInstant(s.EndTimestamp).Before(startOfToday) {
		return false
	}
	if k == feedGroup {
		rg := s.Recipients
		if rg == nil || rg.Group == nil || rg.Group.ID != m.group.ID {
			return false
		}
		if m.subgroup != nil && !slices.ContainsFunc(rg.Group.SubGroups, func(sg spondapi.Subgroup) bool { return sg.ID == m.subgroup.ID }) {
			return false
		}
	}
	if f.done || len(f.events) == 0 {
		return true
	}
	return !parseInstant(s.StartTimestamp).After(parseInstant(f.events[len(f.events)-1].StartTimestamp))
}

// removeSpond drops a deleted arrangement from feed k and closes its modal.
func (m *model) removeSpond(k feedKind, id string) {
	f := m.feedOf(k)
	if f.detail != nil && f.detail.s.ID == id {
		f.detail = nil
		m.status = "That arrangement was deleted"
	}
	i := slices.IndexFunc(f.events, func(e spondapi.Spond) bool { return e.ID == id })
	if i < 0 {
		return
	}
	f.events = slices.Delete(f.events, i, i+1)
	if i < f.cursor {
		f.cursor--
	}
	f.cursor = min(f.cursor, max(len(f.events)-1, 0))
	m.refreshEvents(k)
}

func (m model) applyPostFetched(msg postFetchedMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		return m, nil
	}
	if msg.post == nil {
		m.removePost(msg.id)
		return m, nil
	}
	p := *msg.post
	if m.alerts.post != nil && m.alerts.post.hit.Post.ID == p.ID {
		m.alerts.post.replace(p)
	}
	if m.postModal != nil && m.postModal.hit.Post.ID == p.ID && m.query == "" {
		m.postModal.replace(p)
	}
	if i := slices.IndexFunc(m.posts, func(o spondapi.Post) bool { return o.ID == p.ID }); i >= 0 {
		m.posts[i] = p
	} else if m.belongsInPosts(p) {
		m.posts = slices.Insert(m.posts, 0, p)
		if m.query == "" && len(m.posts) > 1 {
			m.postCursor++ // keep the same post selected
		}
	} else {
		return m, nil
	}
	m.refreshPosts()
	return m, nil
}

// belongsInPosts reports whether a post not in the loaded feed is a new one
// for its top: same scope, and newer than the newest loaded post. A group's
// feed leaves out posts sent to subgroups.
func (m model) belongsInPosts(p spondapi.Post) bool {
	if m.screen != screenPosts || p.GroupID != m.group.ID || (len(m.posts) == 0 && !m.postsDone) {
		return false
	}
	if m.subgroup == nil && len(p.SubGroupIDs) > 0 || m.subgroup != nil && !slices.Contains(p.SubGroupIDs, m.subgroup.ID) {
		return false
	}
	return len(m.posts) == 0 || parseInstant(p.Timestamp).After(parseInstant(m.posts[0].Timestamp))
}

// removePost drops a deleted post from the feed, results and modals.
func (m *model) removePost(id string) {
	gone := func(o spondapi.Post) bool { return o.ID == id }
	if m.alerts.post != nil && gone(m.alerts.post.hit.Post) {
		m.alerts.post = nil
		m.status = "That post was deleted"
	}
	if m.postModal != nil && gone(m.postModal.hit.Post) {
		m.postModal = nil
		m.status = "That post was deleted"
	}
	n := len(m.shownHits())
	m.posts = slices.DeleteFunc(m.posts, gone)
	m.hits = slices.DeleteFunc(m.hits, func(h store.SearchHit) bool { return gone(h.Post) })
	if len(m.shownHits()) == n {
		return
	}
	m.postCursor = min(m.postCursor, max(len(m.shownHits())-1, 0))
	m.refreshPosts()
}

// applyChatsPolled merges the newest threads into the list, newest first,
// keeping the selection, and loads new messages of the open thread.
func (m model) applyChatsPolled(msg chatsPolledMsg) (tea.Model, tea.Cmd) {
	ch := &m.chat
	if msg.err != nil || !ch.loaded || ch.loading {
		return m, nil
	}
	selected := ""
	if len(ch.threads) > 0 {
		selected = ch.threads[ch.cursor].ID
	}
	var cmd tea.Cmd
	for _, t := range msg.threads {
		if i := slices.IndexFunc(ch.threads, func(o spondapi.ChatThread) bool { return o.ID == t.ID }); i >= 0 {
			ch.threads[i] = t
		} else {
			ch.threads = append(ch.threads, t)
		}
		if ch.open != nil && ch.open.ID == t.ID && ch.open.NewestTimestamp != t.NewestTimestamp {
			// Remember the new timestamp only once its messages are on the way,
			// so a busy thread is tried again on the next poll.
			if load := m.loadNewMessages(); load != nil {
				ch.open, cmd = &t, load
			}
		}
	}
	slices.SortStableFunc(ch.threads, func(a, b spondapi.ChatThread) int {
		return parseInstant(b.NewestTimestamp).Compare(parseInstant(a.NewestTimestamp))
	})
	if i := slices.IndexFunc(ch.threads, func(t spondapi.ChatThread) bool { return t.ID == selected }); i >= 0 {
		ch.cursor = i
	}
	m.refreshChats()
	return m, cmd
}

// parseInstant parses an API timestamp; the zero time if it cannot. Compare
// parsed times: the API writes milliseconds only when they are non-zero, so
// the strings do not sort.
func parseInstant(s string) time.Time {
	t, _ := time.Parse(time.RFC3339Nano, s)
	return t
}
