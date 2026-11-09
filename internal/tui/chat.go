package tui

import (
	"fmt"
	"slices"
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/asbjorn/spondcli/internal/display"
	"github.com/asbjorn/spondcli/internal/spondapi"
)

const (
	chatPageSize    = 30 // threads per page
	messagePageSize = 50 // messages per page
)

// chatView is the Messages tab: the thread list, and one open thread.
type chatView struct {
	threads []spondapi.ChatThread
	loaded  bool
	done    bool // no older threads
	loading bool
	err     error
	gen     int
	cursor  int
	offsets []int
	vp      viewport.Model

	open     *spondapi.ChatThread   // open thread, or nil
	draft    *chatDraft             // set while open is a conversation not created yet
	msgs     []spondapi.ChatMessage // of the open thread, oldest first
	msgsDone bool                   // the first message is loaded
	msgsBusy bool
	msgGen   int // bumped when a thread opens; older message loads are dropped
	msgVP    viewport.Model
	// msgCursor is the selected message in msgs; msgOffsets is the first
	// line of each message in msgVP.
	msgCursor  int
	msgOffsets []int
	react      *reactPicker // choosing a reaction to a message, or nil

	compose   textarea.Model // message being written in the open thread
	composing bool           // keys go to compose
	sending   bool
	mentions  []spondapi.Mention // people tagged in compose, as picked
	suggest   *mentionSuggest    // people matching the tag being typed, or nil
	shut      *mentionSpot       // tag whose suggestions were closed with esc

	picker *recipientPicker // choosing people for a new conversation, or nil
}

func newChatView() chatView {
	return chatView{vp: viewport.New(0, 0), msgVP: viewport.New(0, 0), compose: newComposer()}
}

// selected is the selected message of the open thread.
func (c chatView) selected() (spondapi.ChatMessage, bool) {
	if c.msgCursor < 0 || c.msgCursor >= len(c.msgs) {
		return spondapi.ChatMessage{}, false
	}
	return c.msgs[c.msgCursor], true
}

func (c chatView) unread() int {
	n := 0
	for _, t := range c.threads {
		if t.Unread {
			n++
		}
	}
	return n
}

type (
	chatsMsg struct {
		gen     int
		before  string // cursor the page was requested with
		threads []spondapi.ChatThread
		err     error
	}
	chatMessagesMsg struct {
		gen    int
		chatID string
		older  bool // a page before the loaded messages
		newer  bool // the newest page, to add messages after the loaded ones
		msgs   []spondapi.ChatMessage
		err    error
	}
)

// loadChats fetches a page of threads in the background; before is "" for
// the first page.
func (m *model) loadChats(before string) tea.Cmd {
	if before == "" {
		m.chat.gen++
	}
	m.chat.loading = true
	ctx, c, gen := m.ctx, m.client, m.chat.gen
	return func() tea.Msg {
		threads, err := c.Chats(ctx, chatPageSize, before)
		return chatsMsg{gen: gen, before: before, threads: threads, err: err}
	}
}

func (m model) applyChats(msg chatsMsg) (tea.Model, tea.Cmd) {
	ch := &m.chat
	if msg.gen != ch.gen {
		return m, nil
	}
	ch.loading = false
	ch.err = msg.err
	if msg.err != nil {
		return m, nil
	}
	if msg.before == "" {
		ch.threads = nil
	}
	added := 0
	for _, t := range msg.threads {
		if !slices.ContainsFunc(ch.threads, func(o spondapi.ChatThread) bool { return o.ID == t.ID }) {
			ch.threads = append(ch.threads, t)
			added++
		}
	}
	ch.loaded = true
	ch.done = len(msg.threads) < chatPageSize || added == 0
	ch.cursor = min(ch.cursor, max(len(ch.threads)-1, 0))
	m.refreshChats()
	return m, nil
}

// maybeLoadMoreChats pages once the selection reaches the last thread.
func (m *model) maybeLoadMoreChats() tea.Cmd {
	ch := &m.chat
	if ch.loading || ch.done || ch.err != nil || ch.cursor < len(ch.threads)-1 {
		return nil
	}
	return m.loadChats(ch.threads[len(ch.threads)-1].NewestTimestamp)
}

// loadMessages fetches the newest page of the open thread, or with older the
// page before the first loaded message.
func (m *model) loadMessages(older bool) tea.Cmd {
	ch := &m.chat
	if ch.open == nil || ch.open.ID == "" || ch.msgsBusy || (older && (ch.msgsDone || len(ch.msgs) == 0)) {
		return nil
	}
	ch.msgsBusy = true
	maxNum := -1
	if older {
		maxNum = ch.msgs[0].MsgNum - 1
	}
	ctx, c, gen, id := m.ctx, m.client, ch.msgGen, ch.open.ID
	return func() tea.Msg {
		msgs, err := c.ChatMessages(ctx, id, messagePageSize, maxNum)
		return chatMessagesMsg{gen: gen, chatID: id, older: older, msgs: msgs, err: err}
	}
}

// loadNewMessages fetches the newest page of the open thread to add what
// arrived after the loaded messages.
func (m *model) loadNewMessages() tea.Cmd {
	ch := &m.chat
	if len(ch.msgs) == 0 {
		return m.loadMessages(false)
	}
	if ch.open == nil || ch.open.ID == "" || ch.msgsBusy {
		return nil
	}
	ch.msgsBusy = true
	ctx, c, gen, id := m.ctx, m.client, ch.msgGen, ch.open.ID
	return func() tea.Msg {
		msgs, err := c.ChatMessages(ctx, id, messagePageSize, -1)
		return chatMessagesMsg{gen: gen, chatID: id, newer: true, msgs: msgs, err: err}
	}
}

func (m model) applyMessages(msg chatMessagesMsg) (tea.Model, tea.Cmd) {
	ch := &m.chat
	if msg.gen != ch.msgGen || ch.open == nil || ch.open.ID != msg.chatID {
		return m, nil
	}
	ch.msgsBusy = false
	if msg.err != nil && msg.newer {
		return m, nil // a background refresh; the next poll tries again
	}
	if msg.err != nil {
		m.err = fmt.Errorf("loading messages: %w", msg.err)
		return m, nil
	}
	page := slices.Clone(msg.msgs)
	slices.Reverse(page) // the API returns newest first
	if msg.newer {
		return m.appendMessages(page)
	}
	ch.msgsDone = len(page) < messagePageSize || (len(page) > 0 && page[0].MsgNum == 0)
	if !msg.older {
		// A reload keeps the selected message when it is still there.
		sel, had := ch.selected()
		ch.msgs = page
		ch.msgCursor = len(page) - 1
		if i := slices.IndexFunc(page, func(o spondapi.ChatMessage) bool { return o.MsgNum == sel.MsgNum }); had && i >= 0 {
			ch.msgCursor = i
		}
		m.refreshMessages()
		ch.msgVP.GotoBottom()
		scrollIntoView(&ch.msgVP, ch.msgOffsets, ch.msgCursor)
		return m, nil
	}
	// Keep the same messages in view after adding older ones above.
	before := ch.msgVP.TotalLineCount()
	ch.msgs = append(page, ch.msgs...)
	ch.msgCursor += len(page)
	m.refreshMessages()
	ch.msgVP.SetYOffset(ch.msgVP.YOffset + ch.msgVP.TotalLineCount() - before)
	return m, nil
}

// appendMessages adds the messages of page (oldest first) that come after
// the loaded ones and updates those already loaded (edits, deletions). It
// follows the conversation only when it was scrolled to the bottom.
func (m model) appendMessages(page []spondapi.ChatMessage) (tea.Model, tea.Cmd) {
	ch := &m.chat
	if len(page) == 0 {
		return m, nil
	}
	last := ch.msgs[len(ch.msgs)-1].MsgNum
	if page[0].MsgNum > last+1 {
		// More arrived than one page holds: start again from the newest.
		ch.msgs, ch.msgsDone = page, page[0].MsgNum == 0
		ch.msgCursor = len(page) - 1
		m.refreshMessages()
		ch.msgVP.GotoBottom()
		return m, nil
	}
	atBottom := ch.msgVP.AtBottom()
	onLast := ch.msgCursor == len(ch.msgs)-1
	for _, msg := range page {
		if msg.MsgNum > last {
			ch.msgs = append(ch.msgs, msg)
		} else if i := slices.IndexFunc(ch.msgs, func(o spondapi.ChatMessage) bool { return o.MsgNum == msg.MsgNum }); i >= 0 {
			ch.msgs[i] = msg
		}
	}
	if atBottom && onLast {
		ch.msgCursor = len(ch.msgs) - 1 // follow the conversation
	}
	m.refreshMessages()
	if atBottom {
		ch.msgVP.GotoBottom()
	}
	return m, nil
}

func (m *model) openThread() tea.Cmd {
	ch := &m.chat
	if len(ch.threads) == 0 {
		return nil
	}
	return m.showThread(ch.threads[ch.cursor])
}

// showThread opens t and loads its newest messages.
func (m *model) showThread(t spondapi.ChatThread) tea.Cmd {
	ch := &m.chat
	ch.open, ch.draft = &t, nil
	ch.msgGen++
	ch.msgs, ch.msgsDone, ch.msgsBusy = nil, false, false
	ch.msgCursor, ch.react = 0, nil
	ch.composing = false
	ch.compose.Blur()
	ch.clearComposer()
	ch.msgVP.SetContent("")
	m.err, m.status = nil, ""
	return m.loadMessages(false)
}

func (m model) updateChat(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.chat.picker != nil {
		return m.updatePicker(msg)
	}
	if m.chat.open != nil {
		return m.updateThread(msg)
	}
	ch := &m.chat
	switch msg := msg.(type) {
	case tea.MouseMsg:
		if msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft {
			if i, ok := cardAtLine(ch.offsets, ch.vp.TotalLineCount(), msg.Y-headerLines+ch.vp.YOffset); ok {
				m.selectThread(i)
				return m, m.openThread()
			}
			return m, nil
		}
	case tea.KeyMsg:
		switch msg.String() {
		case "q":
			return m, tea.Quit
		case "up", "k":
			m.selectThread(ch.cursor - 1)
			return m, nil
		case "down", "j":
			m.selectThread(ch.cursor + 1)
			return m, m.maybeLoadMoreChats()
		case "home", "g":
			m.selectThread(0)
			return m, nil
		case "end", "G":
			m.selectThread(len(ch.threads) - 1)
			return m, m.maybeLoadMoreChats()
		case "enter", " ":
			return m, m.openThread()
		case "n":
			m.openPicker()
			return m, nil
		case "r":
			if !ch.loading {
				return m, m.loadChats("")
			}
			return m, nil
		}
	}
	var cmd tea.Cmd
	ch.vp, cmd = ch.vp.Update(msg)
	return m, cmd
}

func (m model) updateThread(msg tea.Msg) (tea.Model, tea.Cmd) {
	ch := &m.chat
	if ch.composing {
		return m.updateComposer(msg)
	}
	if key, ok := msg.(tea.KeyMsg); ok && ch.react != nil {
		return m.updateReact(key)
	}
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "esc", "backspace", "q":
			ch.open, ch.draft, ch.msgs = nil, nil, nil
			m.err, m.status = nil, ""
			m.refreshChats()
			return m, nil
		case "i", "enter", "tab":
			return m, m.focusComposer()
		case "up", "k":
			return m, m.selectMessage(ch.msgCursor - 1)
		case "down", "j":
			return m, m.selectMessage(ch.msgCursor + 1)
		case "home", "g":
			ch.msgVP.GotoTop()
			return m, tea.Batch(m.selectMessage(0), m.loadMessages(true))
		case "end", "G":
			cmd := m.selectMessage(len(ch.msgs) - 1)
			ch.msgVP.GotoBottom()
			return m, cmd
		case "e":
			m.openReactions()
			return m, nil
		case "+":
			if sel, ok := ch.selected(); ok {
				return m, m.toggleReaction(sel.MsgNum, thumbsUp)
			}
			return m, nil
		case "r":
			return m, m.loadMessages(false)
		}
	case tea.MouseMsg:
		if msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft {
			line := msg.Y - headerLines - threadBannerHeight + ch.msgVP.YOffset
			if i, ok := cardAtLine(ch.msgOffsets, ch.msgVP.TotalLineCount(), line); ok {
				return m, m.selectMessage(i)
			}
			return m, nil
		}
	}
	var cmd tea.Cmd
	ch.msgVP, cmd = ch.msgVP.Update(msg)
	if ch.msgVP.AtTop() {
		cmd = tea.Batch(cmd, m.loadMessages(true))
	}
	return m, cmd
}

// selectMessage selects message i of the open thread and scrolls it into
// view. Reaching the first loaded message loads older ones.
func (m *model) selectMessage(i int) tea.Cmd {
	ch := &m.chat
	if len(ch.msgs) == 0 {
		return nil
	}
	ch.msgCursor = min(max(i, 0), len(ch.msgs)-1)
	m.refreshMessages()
	scrollIntoView(&ch.msgVP, ch.msgOffsets, ch.msgCursor)
	if ch.msgCursor == 0 {
		if ch.msgsDone {
			ch.msgVP.GotoTop() // show the start of the conversation too
		}
		return m.loadMessages(true)
	}
	return nil
}

func (m *model) selectThread(i int) {
	ch := &m.chat
	if len(ch.threads) == 0 {
		return
	}
	ch.cursor = min(max(i, 0), len(ch.threads)-1)
	m.refreshChats()
	scrollIntoView(&ch.vp, ch.offsets, ch.cursor)
}

// threadName is the thread's own name, the other person in a direct
// message, or the group's name.
func (m model) threadName(t spondapi.ChatThread) string {
	if t.Name != "" {
		return t.Name
	}
	// A direct message carries the group it started in; name the person.
	if t.GroupID != "" && t.Type != "DM" {
		if g, ok := m.groupByID(t.GroupID); ok {
			for _, sg := range g.SubGroups {
				if sg.ID == t.GroupID {
					return g.Name + " › " + sg.Name
				}
			}
			return g.Name
		}
	}
	var names []string
	for _, id := range t.Participants {
		if m.me != nil && id == m.me.ID {
			continue
		}
		names = append(names, nameOr(m.profiles, id))
	}
	if len(names) == 0 {
		return "(no name)"
	}
	return strings.Join(names, ", ")
}

// sender is who wrote a message, as shown above it.
func (m model) sender(msg spondapi.ChatMessage) string {
	switch {
	case msg.ClubMessage != nil:
		s := msg.ClubMessage.SenderName
		if msg.ClubMessage.SenderTitle != "" {
			s += " (" + msg.ClubMessage.SenderTitle + ")"
		}
		return strings.TrimSpace(s + ", " + msg.ClubMessage.ClubName)
	case msg.User == "":
		return "Spond"
	case m.me != nil && msg.User == m.me.ID:
		return "You"
	}
	return nameOr(m.profiles, msg.User)
}

// messageText is a message's text, with tags as "@Name" styled by tagSty,
// and a note for content shown only in the app. names names tagged people
// whose name the message lacks.
func messageText(msg spondapi.ChatMessage, names map[string]string, tagSty lipgloss.Style) string {
	if msg.Deleted {
		return "(deleted)"
	}
	var parts []string
	if msg.ClubMessage != nil && msg.ClubMessage.Subject != "" && msg.ClubMessage.Subject != msg.Text {
		parts = append(parts, titleSty.Render(msg.ClubMessage.Subject))
	}
	if msg.Text != "" {
		parts = append(parts, decodeMentions(msg.Text, msg.Mentions, names, tagSty))
	}
	switch {
	case len(msg.Images) > 0:
		parts = append(parts, dimSty.Render(fmt.Sprintf("[%s]", display.Plural(len(msg.Images), "image"))))
	case msg.File != nil:
		parts = append(parts, dimSty.Render("[file: "+msg.File.Name+"]"))
	case msg.Campaign != nil:
		parts = append(parts, dimSty.Render("[campaign: "+msg.Campaign.CampaignName+"]"))
	case len(parts) == 0:
		parts = append(parts, dimSty.Render("["+strings.ToLower(msg.Type)+"]"))
	}
	return strings.Join(parts, "\n")
}

// refreshChats renders the thread list: name, time and the newest message.
func (m *model) refreshChats() {
	ch := &m.chat
	width := max(m.width-4, 20)
	blocks := make([]string, 0, len(ch.threads))
	ch.offsets = ch.offsets[:0]
	line := 0
	for i, t := range ch.threads {
		name := m.threadName(t)
		dot := "  "
		if t.Unread {
			dot = unreadSty.Render("● ")
			name = titleSty.Render(name)
		}
		head := name + dimSty.Render("  "+display.Time(t.NewestTimestamp))
		if t.Muted {
			head += dimSty.Render(" · muted")
		}
		preview := ""
		if t.Message != nil {
			preview = display.FirstLine(m.sender(*t.Message)+": "+anyTag.ReplaceAllString(messageText(*t.Message, m.profiles, lipgloss.NewStyle()), ""), width-2)
		}
		block := lipgloss.JoinHorizontal(lipgloss.Top, dot,
			lipgloss.JoinVertical(lipgloss.Left, display.FirstLine(head, width-2), dimSty.Render(preview)))
		style := plainBlockSty
		if i == ch.cursor {
			style = selectedCommentSty
		}
		block = style.Render(block)
		ch.offsets = append(ch.offsets, line)
		line += lipgloss.Height(block) + 1
		blocks = append(blocks, block)
	}
	ch.vp.SetContent(strings.Join(blocks, "\n\n"))
}

// threadBannerHeight is the coloured bar and the line under it that name
// the open conversation.
const threadBannerHeight = 2

var (
	bannerSty    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#FFFFFF")).Background(accent).Padding(0, 1)
	bannerSubSty = lipgloss.NewStyle().Foreground(accent).Background(tagBg).Padding(0, 1)
	bubbleSty    = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(subtle).Padding(0, 1)
	myBubbleSty  = bubbleSty.BorderForeground(accent)
	// selectedBubbleSty keeps bubbleSty's size, so selecting moves nothing.
	selectedBubbleSty = bubbleSty.BorderStyle(lipgloss.ThickBorder()).BorderForeground(pickColor)
	youSty            = lipgloss.NewStyle().Bold(true).Foreground(okColor)
)

// threadBanner names the open conversation: an icon for its kind, its
// name and who is in it.
func (m model) threadBanner() string {
	ch := m.chat
	t := *ch.open
	width := max(m.width, 20)
	var others []string
	for _, id := range t.Participants {
		if m.me == nil || id != m.me.ID {
			others = append(others, nameOr(m.profiles, id))
		}
	}
	icon, sub := "💬", "with "+strings.Join(others, ", ")
	switch {
	case ch.draft != nil:
		icon, sub = "📝", "New conversation with "+strings.Join(others, ", ")+" · your first message starts it"
	case t.Type == "DM":
		icon, sub = "👤", "Direct message"
		if g, ok := m.groupByID(t.GroupID); ok {
			sub += " · via " + g.Name
		}
	case t.Type == "CLUB_MESSAGE":
		icon, sub = "📣", "Message from the club"
	case t.Type == "CAMPAIGN":
		icon, sub = "💰", "Fundraising campaign"
	case len(others) == 0:
		sub = "Group conversation"
	}
	var info []string
	if n := len(t.Participants); n > 2 {
		info = append(info, fmt.Sprintf("👥 %d people", n))
	}
	if t.Muted {
		info = append(info, "🔇 muted")
	}
	left, right := icon+"  "+m.threadName(t), strings.Join(info, " · ")
	if gap := width - 2 - lipgloss.Width(left) - lipgloss.Width(right); gap >= 2 {
		left += strings.Repeat(" ", gap) + right
	}
	bar := bannerSty.Width(width).MaxHeight(1).Render(left)
	return bar + "\n" + bannerSubSty.Width(width).MaxHeight(1).Render(display.FirstLine(sub, width-3))
}

// refreshMessages renders the open thread, oldest first, as chat bubbles:
// yours on the right, the selected one with a thick border and a pointer.
func (m *model) refreshMessages() {
	ch := &m.chat
	width := max(m.width-2, 20)
	var blocks []string
	ch.msgOffsets = ch.msgOffsets[:0]
	line := 0
	add := func(b string) {
		blocks = append(blocks, b)
		line += lipgloss.Height(b) + 1 // blocks are joined by a blank line
	}
	if ch.msgsDone {
		add(lipgloss.PlaceHorizontal(width, lipgloss.Center, dimSty.Render("— start of conversation —")))
	} else if len(ch.msgs) > 0 {
		add(lipgloss.PlaceHorizontal(width, lipgloss.Center, dimSty.Render("↑ older messages load at the top")))
	}
	for i, msg := range ch.msgs {
		ch.msgOffsets = append(ch.msgOffsets, line)
		add(lipgloss.NewStyle().PaddingLeft(1).Render(m.messageBlock(msg, i == ch.msgCursor, width-1)))
	}
	ch.msgVP.SetContent(strings.Join(blocks, "\n\n"))
}

// messageBlock renders one message as a bubble at most three quarters of
// width wide, with its reactions under it; for the selected message also
// who reacted.
func (m model) messageBlock(msg spondapi.ChatMessage, selected bool, width int) string {
	mine := m.me != nil && msg.User == m.me.ID
	outer := max(min(width*3/4, width-2), 16)
	inner := outer - 4 // border and padding
	head := authorSty.Render(m.sender(msg))
	sty, align := bubbleSty, lipgloss.Left
	if mine {
		head, sty, align = youSty.Render("You"), myBubbleSty, lipgloss.Right
	}
	if selected {
		sty = selectedBubbleSty
	}
	head += dimSty.Render("  " + display.Time(msg.Timestamp))
	text := messageText(msg, m.profiles, mentionSty)
	w := min(max(lipgloss.Width(text), lipgloss.Width(head)), inner)
	parts := []string{sty.Render(lipgloss.NewStyle().Width(w).Render(head + "\n" + text))}
	if chips := m.reactionChips(msg.Reactions); chips != "" {
		parts = append(parts, chips)
		if selected {
			names := m.reactionNames(msg.Reactions)
			parts = append(parts, lipgloss.NewStyle().Width(min(lipgloss.Width(names), outer)).Render(names))
		}
	}
	block := lipgloss.JoinVertical(align, parts...)
	pointer := "  "
	if selected {
		pointer = "\n" + pickSty.Render("▶ ")
	}
	block = lipgloss.JoinHorizontal(lipgloss.Top, pointer, block)
	if mine {
		return lipgloss.PlaceHorizontal(width, lipgloss.Right, block)
	}
	return block
}

func (m model) viewChat() (crumb, body, help string) {
	ch := m.chat
	if ch.open != nil {
		crumb = "Messages › " + m.threadName(*ch.open)
		body = ch.msgVP.View()
		switch {
		case ch.draft != nil:
			body = dimSty.Padding(1, 2).Render("New conversation. Say hello 👋, your first message starts it.")
		case len(ch.msgs) == 0 && ch.msgsBusy:
			body = dimSty.Padding(1, 2).Render("Loading messages…")
		}
		// The tag suggestions or the reaction picker cover the bottom of the
		// messages.
		parts := []string{m.composerView()}
		bodyHeight := ch.msgVP.Height
		var popup string
		switch {
		case ch.composing && ch.suggest != nil:
			popup = m.mentionView(bodyHeight)
		case ch.react != nil:
			popup = m.reactView()
		}
		if popup != "" {
			parts = append([]string{popup}, parts...)
			bodyHeight -= lipgloss.Height(popup)
		}
		if bodyHeight > 0 {
			body = lipgloss.NewStyle().Height(bodyHeight).MaxHeight(bodyHeight).Render(body)
			parts = append([]string{body}, parts...)
		}
		body = lipgloss.JoinVertical(lipgloss.Left, append([]string{m.threadBanner()}, parts...)...)
		help = "↑/↓ select message · e react · + 👍 · i/enter write · g/G first/last · r refresh · esc back"
		switch {
		case ch.composing && ch.suggest != nil && len(ch.suggest.people) > 0:
			help = "↑/↓ choose · tab/enter tag · esc close the list"
		case ch.composing:
			help = "enter send · alt+enter new line · @ tag someone · esc stop writing"
		case ch.react != nil:
			help = "←/→ choose · enter or 1-9, 0 react · esc close"
		}
		return crumb, body, help
	}
	if ch.picker != nil {
		return "Messages › New conversation", m.pickerView(), m.pickerHelp()
	}
	crumb = "Messages"
	if ch.loaded {
		crumb = fmt.Sprintf("Messages (%s, %d unread)", display.Plural(len(ch.threads), "conversation"), ch.unread())
	}
	help = "↑/↓ select · g/G top/bottom · enter/click open · n new message · r refresh · 1-4 tabs · q quit"
	switch {
	case ch.err != nil:
		body = errSty.Render("Loading messages failed: " + ch.err.Error())
	case ch.loading && !ch.loaded:
		body = dimSty.Padding(1, 2).Render("Loading conversations…")
	case len(ch.threads) == 0:
		body = dimSty.Padding(1, 2).Render("No conversations.")
	default:
		body = ch.vp.View()
	}
	return crumb, body, help
}
