package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/asbjorn/spondcli/internal/display"
	"github.com/asbjorn/spondcli/internal/roster"
	"github.com/asbjorn/spondcli/internal/spondapi"
	"github.com/asbjorn/spondcli/internal/store"
)

var (
	// matchSty marks search matches; it wins over the surrounding style.
	matchSty = lipgloss.NewStyle().Bold(true).
			Foreground(lipgloss.Color("#000000")).Background(lipgloss.Color("#FFD43B"))
	// commentSty and selectedCommentSty keep the same width so content
	// offsets do not move when the selection changes.
	commentSty         = lipgloss.NewStyle().Border(lipgloss.NormalBorder(), false, false, false, true).BorderForeground(subtle).PaddingLeft(1)
	selectedCommentSty = commentSty.BorderStyle(lipgloss.ThickBorder()).BorderForeground(accent)
	plainBlockSty      = commentSty.BorderStyle(lipgloss.HiddenBorder())
)

// maxCardComments is how many matching comments a result card shows.
const maxCardComments = 3

// shownHits is what the posts screen lists: search results, or the feed.
func (m model) shownHits() []store.SearchHit {
	if m.query != "" {
		return m.hits
	}
	hits := make([]store.SearchHit, len(m.posts))
	for i, p := range m.posts {
		hits[i] = store.PlainHit(p)
	}
	return hits
}

// refreshPosts re-renders the posts screen and remembers card offsets.
func (m *model) refreshPosts() {
	content, offsets := m.renderPosts()
	m.postOffsets = offsets
	m.feed.SetContent(content)
}

// selectPost moves the selection, clamped, and scrolls it into view.
func (m *model) selectPost(i int) {
	n := len(m.shownHits())
	if n == 0 {
		return
	}
	m.postCursor = min(max(i, 0), n-1)
	m.refreshPosts()
	scrollIntoView(&m.feed, m.postOffsets, m.postCursor)
}

// maybeLoadMorePosts fetches the next feed page once the selection or
// scroll position reaches the end of what is loaded. Search results are
// complete, so they never page.
func (m *model) maybeLoadMorePosts() tea.Cmd {
	nearEnd := m.postCursor >= len(m.posts)-1 || m.feed.AtBottom()
	if m.query != "" || m.busy != "" || m.postsDone || m.err != nil || !nearEnd {
		return nil
	}
	return m.startBusy("Loading more posts…", m.loadPostsCmd())
}

// openPost opens the modal for the selected post.
func (m *model) openPost() tea.Cmd {
	hits := m.shownHits()
	if len(hits) == 0 {
		return nil
	}
	m.err, m.status = nil, ""
	m.postModal = newPostModal(hits[m.postCursor], m.group, m.width, max(m.height-chromeHeight, 3))
	return nil
}

// renderPosts draws the feed, or the search results when a search is
// shown, and returns the first line of each card.
func (m model) renderPosts() (string, []int) {
	hits := m.shownHits()
	if len(hits) == 0 {
		return "", nil
	}
	names := spondapi.MemberNames(m.group)
	cardWidth := max(m.width-2, 20)
	textWidth := max(cardWidth-4, 10) // border + padding
	cards := make([]string, 0, len(hits)+1)
	offsets := make([]int, 0, len(hits))
	line := 0
	for i, h := range hits {
		parts := postHeader(h, m.group, names, textWidth)
		if body := strings.TrimSpace(h.Body); body != "" {
			parts = append(parts, lipgloss.NewStyle().Width(textWidth).Render(renderMarked(body, lipgloss.NewStyle())))
		}
		flat := store.FlattenComments(h.Post.Comments)
		switch matches := h.CommentMatches(); {
		case m.query == "" && len(flat) > 0:
			parts = append(parts, dimSty.Render(display.Plural(len(flat), "comment")))
		case len(matches) > 0:
			for _, ci := range matches[:min(len(matches), maxCardComments)] {
				parts = append(parts, renderComment(flat[ci], h.Comments[ci], names, textWidth, commentSty.BorderForeground(accent), 3))
			}
			if more := len(matches) - maxCardComments; more > 0 {
				parts = append(parts, dimSty.Render(fmt.Sprintf("+%d more matching comments · enter to see all", more)))
			}
		}

		style := cardSty
		if i == m.postCursor {
			style = selectedCardSty
		}
		card := style.Width(cardWidth).Render(strings.Join(parts, "\n"))
		offsets = append(offsets, line)
		line += lipgloss.Height(card)
		cards = append(cards, card)
	}
	if m.query == "" && m.postsDone {
		cards = append(cards, dimSty.Padding(0, 2).Render("— end of posts —"))
	}
	return strings.Join(cards, "\n"), offsets
}

// postHeader is the author line, subgroup tags and title of a post.
func postHeader(h store.SearchHit, g spondapi.Group, names map[string]string, width int) []string {
	p := h.Post
	parts := []string{authorSty.Render(nameOr(names, p.OwnerID)) + "  " + dimSty.Render(display.Time(p.Timestamp))}
	if len(p.SubGroupIDs) > 0 {
		subgroups := make(map[string]string, len(g.SubGroups))
		for _, sg := range g.SubGroups {
			subgroups[sg.ID] = sg.Name
		}
		tags := make([]string, 0, len(p.SubGroupIDs))
		for _, name := range roster.Lookup(p.SubGroupIDs, subgroups) {
			tags = append(tags, tagSty.Render(name))
		}
		parts = append(parts, strings.Join(tags, " "))
	}
	if p.Title != "" {
		parts = append(parts, lipgloss.NewStyle().Width(width).Render(renderMarked(h.Title, titleSty)))
	}
	return parts
}

// renderComment draws one comment or reply in style, its text limited to
// maxLines (0 for no limit).
func renderComment(c store.ThreadComment, text string, names map[string]string, width int, style lipgloss.Style, maxLines int) string {
	head := authorSty.Render(nameOr(names, c.FromProfileID)) + "  " + dimSty.Render(display.Time(c.Timestamp))
	if c.Edited {
		head += dimSty.Render("  (edited)")
	}
	if c.Reply {
		style = style.MarginLeft(2)
		width -= 2
	}
	textWidth := max(width-2, 10) // border + padding
	body := lipgloss.NewStyle().Width(textWidth).Render(renderMarked(strings.TrimSpace(text), lipgloss.NewStyle()))
	if maxLines > 0 {
		body = clampLines(body, maxLines)
	}
	return style.Render(head + "\n" + body)
}

func nameOr(names map[string]string, id string) string {
	if name, ok := names[id]; ok {
		return name
	}
	return id
}

// renderMarked renders text in base style, with search matches (between
// markStart and markEnd) highlighted. Each piece is styled on its own so a
// highlight does not reset the base style of the text after it.
func renderMarked(text string, base lipgloss.Style) string {
	var b strings.Builder
	for i, part := range strings.Split(text, store.MarkStart) {
		if i == 0 {
			b.WriteString(base.Render(part))
			continue
		}
		match, rest, _ := strings.Cut(part, store.MarkEnd)
		b.WriteString(matchSty.Inherit(base).Render(match))
		b.WriteString(base.Render(rest))
	}
	return b.String()
}

// postModal shows one post with all its comments. When opened from a
// search, n and N step through the matches: the post itself and each
// matching comment.
type postModal struct {
	hit     store.SearchHit
	group   spondapi.Group
	vp      viewport.Model
	targets []int // matches in display order: -1 is the post, otherwise a comment index
	cur     int   // selected target
	lines   []int // first content line of each target
	width   int   // outer modal width
	height  int   // outer modal height
}

func newPostModal(h store.SearchHit, g spondapi.Group, width, height int) *postModal {
	pm := &postModal{hit: h, group: g, vp: viewport.New(0, 0)}
	if h.PostMatches() {
		pm.targets = append(pm.targets, -1)
	}
	pm.targets = append(pm.targets, h.CommentMatches()...)
	pm.resize(width, height)
	pm.jump(0)
	return pm
}

func (pm *postModal) resize(width, height int) {
	pm.width = min(width, 110)
	pm.height = height
	pm.vp.Width = max(pm.width-4, 20)    // border + padding
	pm.vp.Height = max(pm.height-2-2, 3) // border, header line and rule
	pm.render()
}

// jump selects match i, wrapping around, and scrolls to it.
func (pm *postModal) jump(i int) {
	n := len(pm.targets)
	if n == 0 {
		return
	}
	pm.cur = (i%n + n) % n
	pm.render()
	pm.vp.SetYOffset(max(pm.lines[pm.cur]-1, 0))
}

func (pm *postModal) selected(target int) bool {
	return len(pm.targets) > 0 && pm.targets[pm.cur] == target
}

// render lays out the post and its comments and records where each match
// target starts.
func (pm *postModal) render() {
	width := pm.vp.Width
	names := spondapi.MemberNames(pm.group)
	h := pm.hit
	lineOf := map[int]int{}

	blockStyle := func(target int) lipgloss.Style {
		switch {
		case pm.selected(target):
			return selectedCommentSty
		case target == -1:
			return plainBlockSty
		default:
			return commentSty
		}
	}

	parts := postHeader(h, pm.group, names, width-2)
	if body := strings.TrimSpace(h.Body); body != "" {
		parts = append(parts, lipgloss.NewStyle().Width(width-2).Render(renderMarked(body, lipgloss.NewStyle())))
	}
	blocks := []string{blockStyle(-1).Render(strings.Join(parts, "\n"))}
	lineOf[-1] = 0
	line := lipgloss.Height(blocks[0])

	flat := store.FlattenComments(h.Post.Comments)
	section := "\n" + sectionSty.Render(display.Plural(len(flat), "comment"))
	if len(flat) == 0 {
		section = "\n" + dimSty.Render("No comments")
	}
	blocks = append(blocks, section)
	line += lipgloss.Height(section)
	for i, c := range flat {
		block := renderComment(c, h.Comments[i], names, width, blockStyle(i), 0)
		lineOf[i] = line
		blocks = append(blocks, block)
		line += lipgloss.Height(block)
	}

	pm.lines = pm.lines[:0]
	for _, t := range pm.targets {
		pm.lines = append(pm.lines, lineOf[t])
	}
	pm.vp.SetContent(strings.Join(blocks, "\n"))
}

// replace shows a newer version of the post, keeping the scroll position.
// Only for a modal without matches, whose marks would no longer line up.
func (pm *postModal) replace(p spondapi.Post) {
	if len(pm.targets) > 0 {
		return
	}
	pm.hit = store.PlainHit(p)
	pm.render()
}

func (pm *postModal) header() string {
	switch n := len(pm.targets); n {
	case 0:
		return dimSty.Render("Post")
	case 1:
		return keySty.Render("1 match")
	default:
		return keySty.Render(fmt.Sprintf("Match %d of %d", pm.cur+1, n))
	}
}

func (pm *postModal) view(width, height int) string {
	rule := dimSty.Render(strings.Repeat("─", pm.vp.Width))
	content := lipgloss.JoinVertical(lipgloss.Left, pm.header(), rule, pm.vp.View())
	box := modalSty.Width(pm.width - 2).Height(pm.height - 2).Render(content)
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Top, box)
}

func (pm *postModal) help() string {
	if len(pm.targets) > 1 {
		return "n/tab next match · N/shift+tab previous · ↑/↓ scroll · g/G top/bottom · esc close"
	}
	return "↑/↓ scroll · g/G top/bottom · esc close"
}

func (m model) updatePostModal(msg tea.Msg) (tea.Model, tea.Cmd) {
	closed, cmd := m.postModal.update(msg)
	if closed {
		m.postModal = nil
	}
	return m, cmd
}

// update handles keys and scrolling; closed reports that the modal should go.
func (pm *postModal) update(msg tea.Msg) (closed bool, cmd tea.Cmd) {
	if key, ok := msg.(tea.KeyMsg); ok {
		switch key.String() {
		case "esc", "q", "backspace":
			return true, nil
		case "n", "tab":
			pm.jump(pm.cur + 1)
			return false, nil
		case "N", "shift+tab":
			pm.jump(pm.cur - 1)
			return false, nil
		case "home", "g":
			pm.vp.GotoTop()
			return false, nil
		case "end", "G":
			pm.vp.GotoBottom()
			return false, nil
		}
	}
	pm.vp, cmd = pm.vp.Update(msg)
	return false, cmd
}

// scrollIntoView scrolls vp so card i, of cards starting at offsets, is
// visible.
func scrollIntoView(vp *viewport.Model, offsets []int, i int) {
	top := offsets[i]
	bottom := vp.TotalLineCount()
	if i+1 < len(offsets) {
		bottom = offsets[i+1]
	}
	switch {
	case top < vp.YOffset:
		vp.SetYOffset(top)
	case bottom > vp.YOffset+vp.Height:
		vp.SetYOffset(max(bottom-vp.Height, top))
	}
}

// cardAtLine maps a content line to the card drawn there, given the first
// line of each card and the total line count.
func cardAtLine(offsets []int, total, line int) (int, bool) {
	if line < 0 {
		return 0, false
	}
	for i := len(offsets) - 1; i >= 0; i-- {
		if line >= offsets[i] {
			end := total
			if i+1 < len(offsets) {
				end = offsets[i+1]
			}
			return i, line < end
		}
	}
	return 0, false
}
