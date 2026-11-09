package tui

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/asbjorn/spondcli/internal/spondapi"
)

var (
	modalSty   = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(accent).Padding(0, 1)
	sectionSty = lipgloss.NewStyle().Bold(true).Foreground(accent)
	labelSty   = lipgloss.NewStyle().Foreground(subtle)
	keySty     = lipgloss.NewStyle().Bold(true).Foreground(accent)
)

const labelWidth = 24

// detailModal shows every field of one arrangement and lets the user answer
// for each recipient they may answer for (spond.behalfOfIds).
type detailModal struct {
	s         spondapi.Spond
	group     spondapi.Group
	vp        viewport.Model
	cursor    int // index into s.BehalfOfIDs
	declining bool
	input     textinput.Model
	width     int // outer modal width
	height    int // outer modal height
}

type respondMsg struct {
	kind    feedKind
	gen     int
	spondID string
	who     string // display name of the recipient answered for
	accept  bool
	bundle  *spondapi.ResponseBundle
	fresh   *spondapi.Spond // re-fetched arrangement, nil if the refresh failed
	err     error
}

func newDetailModal(s spondapi.Spond, g spondapi.Group, width, height int) *detailModal {
	in := textinput.New()
	in.Prompt = "Message (optional): "
	in.PromptStyle = lipgloss.NewStyle().Foreground(accent)
	in.CharLimit = 500
	d := &detailModal{s: s, group: g, vp: viewport.New(0, 0), input: in}
	d.resize(width, height)
	return d
}

// resize fits the modal into the body area and re-renders its content.
func (d *detailModal) resize(width, height int) {
	d.width = min(width, 110)
	d.height = height
	inner := max(d.width-4, 20) // border + padding
	d.input.Width = max(inner-len(d.input.Prompt)-2, 10)
	d.vp.Width = inner
	d.vp.Height = max(d.height-2-lipgloss.Height(d.header(inner)), 3)
	d.vp.SetContent(d.body(inner))
}

// update replaces the arrangement after an answer or refresh.
func (d *detailModal) update(s spondapi.Spond) {
	d.s = s
	d.cursor = min(d.cursor, max(len(s.BehalfOfIDs)-1, 0))
	d.resize(d.width, d.height)
}

func (d *detailModal) recipient() (string, bool) {
	if len(d.s.BehalfOfIDs) == 0 {
		return "", false
	}
	return d.s.BehalfOfIDs[d.cursor], true
}

func (d *detailModal) view(width, height int) string {
	inner := max(d.width-4, 20)
	content := lipgloss.JoinVertical(lipgloss.Left, d.header(inner), d.vp.View())
	box := modalSty.Width(d.width - 2).Height(d.height - 2).Render(content)
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Top, box)
}

func (d *detailModal) help() string {
	if d.declining {
		return "enter send · esc cancel"
	}
	keys := "y attend · n can't attend · ↑/↓ scroll · g/G top/bottom · r refresh · esc close"
	if len(d.s.BehalfOfIDs) > 1 {
		keys = "tab person · " + keys
	}
	return keys
}

// header is the fixed top of the modal: title, time and the answer panel.
func (d *detailModal) header(width int) string {
	s := d.s
	title := s.Heading
	if title == "" {
		title = "(untitled)"
	}
	lines := []string{
		lipgloss.NewStyle().Bold(true).Width(width).Render(title),
		authorSty.Render(formatRange(s)),
		"",
		sectionSty.Render("Your answer"),
	}

	idx := newNameIndex(s, d.group)
	if len(s.BehalfOfIDs) == 0 {
		lines = append(lines, dimSty.Render("You are not among the recipients of this arrangement."))
	}
	for i, id := range s.BehalfOfIDs {
		marker := "  "
		if i == d.cursor && len(s.BehalfOfIDs) > 1 {
			marker = keySty.Render("› ")
		}
		a := s.Responses.AnswerFor(id)
		name := idx.name(id)
		lines = append(lines, marker+lipgloss.NewStyle().Width(labelWidth).Render(name)+answerStyle(a).Render(a.String()))
		if a == spondapi.AnswerDeclined && s.Responses != nil {
			if dm, ok := s.Responses.DeclineMessages[id]; ok && dm.Message != "" {
				lines = append(lines, "  "+dimSty.Render(fmt.Sprintf("%q", dm.Message)))
			}
		}
	}

	switch {
	case d.declining:
		lines = append(lines, "", d.input.View())
	case len(s.BehalfOfIDs) > 0 && !s.InvitationsSent(time.Now()):
		lines = append(lines, "", lipgloss.NewStyle().Width(width).Foreground(accent).Render(s.InvitationsPending()))
	case len(s.BehalfOfIDs) > 0:
		actions := keySty.Render("[y]") + " Attend   " + keySty.Render("[n]") + " Can't attend"
		lines = append(lines, "", actions)
		if s.RequiresPayment() {
			lines = append(lines, dimSty.Render("Attending costs "+paymentLabel(s.Payment)+"; pay and join in the Spond app."))
		}
	}
	lines = append(lines, dimSty.Render(strings.Repeat("─", width)))
	return strings.Join(lines, "\n")
}

// body lists every field, grouped in sections; empty values show a dash.
func (d *detailModal) body(width int) string {
	valueWidth := max(width-labelWidth-1, 10)
	valueSty := lipgloss.NewStyle().Width(valueWidth)
	var b strings.Builder
	for i, sec := range detailSections(d.s, d.group) {
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString(sectionSty.Render(sec.title) + "\n")
		for _, f := range sec.fields {
			value := dimSty.Render("—")
			if f.value != "" {
				value = valueSty.Render(f.value)
			}
			label := labelSty.Width(labelWidth).Render(f.label)
			b.WriteString(lipgloss.JoinHorizontal(lipgloss.Top, label, " ", value) + "\n")
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

// openDetail opens the modal for the selected arrangement of feed k.
func (m *model) openDetail(k feedKind) tea.Cmd {
	f := m.feedOf(k)
	if len(f.events) == 0 {
		return nil
	}
	m.showDetail(k, f.events[f.cursor])
	return nil
}

// showDetail opens the modal for s in feed k.
func (m *model) showDetail(k feedKind, s spondapi.Spond) {
	m.err, m.status = nil, ""
	m.feedOf(k).detail = newDetailModal(s, m.spondGroup(s), m.width, m.bodyHeight())
}

func (m model) updateDetail(k feedKind, msg tea.Msg) (tea.Model, tea.Cmd) {
	d := m.feedOf(k).detail
	key, isKey := msg.(tea.KeyMsg)

	if d.declining && isKey {
		switch key.String() {
		case "esc":
			d.declining = false
			d.input.Blur()
			d.resize(d.width, d.height)
			return m, nil
		case "enter":
			d.declining = false
			d.input.Blur()
			message := strings.TrimSpace(d.input.Value())
			d.resize(d.width, d.height)
			return m, m.answer(k, false, message)
		}
		var cmd tea.Cmd
		d.input, cmd = d.input.Update(msg)
		return m, cmd
	}

	if isKey {
		switch key.String() {
		case "esc", "q", "backspace":
			m.feedOf(k).detail = nil
			m.err, m.status = nil, ""
			return m, nil
		case "tab", "right", "l":
			if n := len(d.s.BehalfOfIDs); n > 1 {
				d.cursor = (d.cursor + 1) % n
				d.resize(d.width, d.height)
			}
			return m, nil
		case "shift+tab", "left", "h":
			if n := len(d.s.BehalfOfIDs); n > 1 {
				d.cursor = (d.cursor + n - 1) % n
				d.resize(d.width, d.height)
			}
			return m, nil
		case "y":
			return m, m.answer(k, true, "")
		case "n":
			if err := m.canAnswer(d, false); err != nil {
				m.err = err
				return m, nil
			}
			d.declining = true
			d.input.SetValue("")
			d.resize(d.width, d.height)
			return m, d.input.Focus()
		case "home", "g":
			d.vp.GotoTop()
			return m, nil
		case "end", "G":
			d.vp.GotoBottom()
			return m, nil
		case "r":
			if m.busy == "" {
				return m, m.startBusy("Refreshing…", m.refreshDetailCmd(k))
			}
			return m, nil
		}
	}

	var cmd tea.Cmd
	d.vp, cmd = d.vp.Update(msg)
	return m, cmd
}

// canAnswer applies the checks the app makes before answering.
func (m model) canAnswer(d *detailModal, accept bool) error {
	switch {
	case m.busy != "":
		return errors.New("please wait for the current request")
	case len(d.s.BehalfOfIDs) == 0:
		return errors.New("you are not among the recipients of this arrangement")
	case d.s.Cancelled:
		return errors.New("this arrangement is cancelled")
	case !d.s.InvitationsSent(time.Now()):
		return errors.New(d.s.InvitationsPending())
	case accept && d.s.RequiresPayment():
		// The app joins priced arrangements through JoinPaymentSpondUseCase.
		return fmt.Errorf("attending costs %s; pay and join in the Spond app", paymentLabel(d.s.Payment))
	}
	return nil
}

// answer sends a response for the selected recipient of feed k's modal.
func (m *model) answer(k feedKind, accept bool, message string) tea.Cmd {
	d := m.feedOf(k).detail
	if err := m.canAnswer(d, accept); err != nil {
		m.err = err
		return nil
	}
	recipientID, _ := d.recipient()
	who := newNameIndex(d.s, d.group).name(recipientID)
	if accept && d.s.Responses.AnswerFor(recipientID) == spondapi.AnswerAccepted {
		m.status = who + " is already attending"
		return nil
	}

	ctx, c, gen, spondID := m.ctx, m.client, m.feedGen(k), d.s.ID
	label := "Sending: can't attend…"
	if accept {
		label = "Sending: attending…"
	}
	return m.startBusy(label, func() tea.Msg {
		bundle, err := c.Respond(ctx, spondID, recipientID, accept, message)
		if spondapi.HasErrorKey(err, "inviteNotSent") {
			// The local clock may disagree with the server's.
			err = errors.New("Invitations for this arrangement haven't been sent yet; you can answer once they go out.")
		}
		if err != nil {
			return respondMsg{kind: k, gen: gen, spondID: spondID, err: err}
		}
		// Counts and other fields are not in the reply; re-read the arrangement.
		fresh, _ := c.Spond(ctx, spondID)
		return respondMsg{kind: k, gen: gen, spondID: spondID, who: who, accept: accept, bundle: bundle, fresh: fresh}
	})
}

func (m *model) refreshDetailCmd(k feedKind) tea.Cmd {
	ctx, c, gen, spondID := m.ctx, m.client, m.feedGen(k), m.feedOf(k).detail.s.ID
	return func() tea.Msg {
		fresh, err := c.Spond(ctx, spondID)
		return respondMsg{kind: k, gen: gen, spondID: spondID, fresh: fresh, err: err}
	}
}

// applyRespond stores an answer or refresh result in the list and the modal.
func (m model) applyRespond(msg respondMsg) (tea.Model, tea.Cmd) {
	if msg.gen != m.feedGen(msg.kind) {
		return m, nil
	}
	m.busy = ""
	if msg.err != nil {
		m.err = msg.err
		return m, nil
	}

	f := m.feedOf(msg.kind)
	apply := func(s *spondapi.Spond) {
		switch {
		case msg.fresh != nil:
			*s = *msg.fresh
		case msg.bundle != nil:
			s.Responses = msg.bundle
			s.ResponseCounts = nil // recount from the new lists
		}
	}
	for i := range f.events {
		if f.events[i].ID == msg.spondID {
			apply(&f.events[i])
		}
	}
	if f.detail != nil && f.detail.s.ID == msg.spondID {
		s := f.detail.s
		apply(&s)
		f.detail.update(s)
	}
	m.refreshEvents(msg.kind)

	switch {
	case msg.bundle == nil:
		m.status = "Refreshed"
	case msg.accept:
		m.status = "Answered for " + msg.who + ": attending"
	default:
		m.status = "Answered for " + msg.who + ": can't attend"
	}
	return m, nil
}
