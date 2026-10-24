package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"reasonix/internal/base/i18n"
	"reasonix/internal/frontend/termrender"
)

// queueRowLimit is how many queued messages are drawn under the working line;
// the rest are named by a count and listed by /queue.
const queueRowLimit = 3

// queueView is the durable queue as the screen holds it: the kernel's snapshot,
// and the one message the arrow keys have selected into the composer.
type queueView struct {
	items    []InboxItem
	paused   bool
	readonly bool
	// selID names the selected message; empty when none is.
	selID string
	// draft is what the composer held when the selection began, set aside and
	// put back when it ends.
	draft string
	// loaded is the selected message's full text as put in the composer, to tell a
	// key that acts on the message from one that edits it. ready says it has been
	// read: nothing is saved over a message whose text is not in the composer.
	loaded string
	ready  bool
	// confirm is a first d waiting for its second.
	confirm bool
}

type queueSnapMsg struct {
	snap InboxSnapshot
	err  error
}

type queueBodyMsg struct {
	id   string
	body string
	err  error
}

// queueDoneMsg settles a key's change to the queue: a note, and a fresh read.
type queueDoneMsg struct {
	level, text string
	err         error
	// deleted is the id of a message this key removed from the queue.
	deleted string
}

func (m *model) fetchQueue() tea.Cmd {
	ctx, c := m.ctx, m.client
	return func() tea.Msg {
		snap, err := c.Inbox(ctx)
		return queueSnapMsg{snap: snap, err: err}
	}
}

func (m *model) onQueueRowsMsg(msg tea.Msg) (tea.Cmd, bool) {
	switch msg := msg.(type) {
	case queueSnapMsg:
		if msg.err != nil {
			return nil, true
		}
		m.queue.items, m.queue.paused, m.queue.readonly = msg.snap.Items, msg.snap.Paused, msg.snap.Readonly
		if m.queue.selID != "" && m.queueIndex() < 0 {
			// The message ran or was removed elsewhere: the edit ends and the
			// draft comes back, so nothing is saved over or sent again.
			id := m.queue.selID
			m.queueEnd(true)
			m.tr.AddNotice("warn", fmt.Sprintf(i18n.M.QueueGoneFmt, shortID(id)))
			return m.commit(), true
		}
		return nil, true
	case queueBodyMsg:
		if msg.id != m.queue.selID {
			return nil, true
		}
		if msg.err != nil {
			m.queueEnd(true)
			m.tr.AddNotice("warn", fmt.Sprintf(i18n.M.QueueActionFailedFmt, msg.err))
			return m.commit(), true
		}
		m.queue.loaded, m.queue.ready = msg.body, true
		m.composer.SetValue(msg.body)
		return nil, true
	case queueDoneMsg:
		level := msg.level
		text := msg.text
		if msg.err != nil {
			level, text = "warn", fmt.Sprintf(i18n.M.QueueActionFailedFmt, msg.err)
		}
		m.tr.DropQueued(msg.deleted)
		m.tr.AddNotice(level, text)
		return tea.Batch(m.commit(), m.fetchQueue()), true
	}
	return nil, false
}

// queuePending is the lines the person still owes action on: the kernel's
// snapshot minus what it has taken and minus a host continuation that will be
// delivered on its own. The kernel words that line's display for a queue row;
// whether the pending strip is that row is the surface's call, and this one
// follows the desktop: the person's outbox, not a line they never wrote.
func (m *model) queuePending() []InboxItem {
	out := make([]InboxItem, 0, len(m.queue.items))
	for _, it := range m.queue.items {
		if it.pending() {
			out = append(out, it)
		}
	}
	return out
}

// queueIndex is where the selected message sits in the pending rows, -1 when
// none is selected or it has left them.
func (m *model) queueIndex() int {
	for i, it := range m.queuePending() {
		if it.ID == m.queue.selID {
			return i
		}
	}
	return -1
}

// queueStoreIndex is where an entry sits in the kernel's own list, which holds
// rows this surface does not draw; a move names a position there.
func (m *model) queueStoreIndex(id string) int {
	for i, it := range m.queue.items {
		if it.ID == id {
			return i
		}
	}
	return -1
}

// queueTag is what the working line says about the queue.
func (m *model) queueTag() string {
	pending := m.queuePending()
	if len(pending) == 0 {
		return ""
	}
	return " · " + fmt.Sprintf(i18n.M.QueueTagFmt, len(pending))
}

// queueRows draws the queued messages above the composer: up to queueRowLimit
// numbered lines with the selected one marked, then a count of the rest.
func (m *model) queueRows() []string {
	pending := m.queuePending()
	if len(pending) == 0 {
		return nil
	}
	width := m.contentWidth()
	sel := m.queueIndex()
	start := 0
	if sel >= queueRowLimit {
		start = sel - queueRowLimit + 1
	}
	var out []string
	for i := start; i < len(pending) && i < start+queueRowLimit; i++ {
		it := pending[i]
		room := width - 10
		tag := ""
		if it.Origin == "host" {
			// It is here because it stopped: say whose it is, so the person is
			// deciding about the runtime's line and not their own.
			tag = "[" + i18n.M.QueueHostTag + "] "
			room -= len(tag)
		}
		line := fmt.Sprintf("[%d] %s%s", i+1, tag, oneLine(it.Preview, room))
		if i == sel {
			out = append(out, "  "+termrender.Accent("▸ "+line))
			continue
		}
		out = append(out, termrender.Dim("    "+line))
	}
	if more := len(pending) - start - queueRowLimit; more > 0 {
		out = append(out, termrender.Dim("    "+fmt.Sprintf(i18n.M.QueueRowsMoreFmt, more)))
	}
	switch {
	case m.queue.readonly:
		// The paused line offers space to resume; a newer schema would refuse it.
		out = append(out, termrender.Dim("  "+i18n.M.QueueRowsReadonly))
	case m.queue.paused:
		out = append(out, termrender.Dim("  "+i18n.M.QueueRowsPaused))
	}
	return out
}

// queued reports whether a pending row of the transcript is one the queue rows
// already draw, so a message is not drawn twice.
func (m *model) queued(it *Item) bool {
	if it.QueueID == "" {
		return false
	}
	for _, q := range m.queuePending() {
		if q.ID == it.QueueID {
			return true
		}
	}
	return false
}

// queueKey takes the keys that browse and manage the queue: up and down select
// a queued message into the composer, alt+up and alt+down move it, d (twice)
// deletes it, r retries one that cannot have run and says why when it may have,
// enter saves what was edited, esc lets go, and space pauses or resumes the
// queue when the composer is empty.
func (m *model) queueKey(k string) (tea.Cmd, bool) {
	selected := m.queue.selID != ""
	acting := selected && m.queue.ready && m.composer.Value() == m.queue.loaded
	if cmd, answered := m.queueReadonlyKey(k, selected, acting); answered {
		return cmd, true
	}
	switch {
	case k == "up" && m.queueCanBrowseUp(selected):
		return m.queueSelect(-1), true
	case k == "down" && selected:
		return m.queueSelect(1), true
	case (k == "alt+up" || k == "alt+down") && selected:
		return m.queueMove(k == "alt+up"), true
	case k == "esc" && selected:
		m.queueEnd(true)
		return nil, true
	case k == "enter" && selected:
		if !m.queue.ready {
			return nil, true
		}
		return m.queueSave(), true
	case k == "d" && acting:
		return m.queueDelete(), true
	case k == "r" && acting:
		return m.queueRetry(), true
	case (k == "space" || k == " ") && len(m.queue.items) > 0 && m.composer.Value() == "":
		return m.queuePause(), true
	}
	if selected && k != "alt+up" && k != "alt+down" {
		m.queue.confirm = false
	}
	return nil, false
}

// queueCanBrowseUp reports whether up enters the pending rows: something is
// there to select, a turn is running or the queue is held, and the person is not
// typing over it.
func (m *model) queueCanBrowseUp(selected bool) bool {
	return len(m.queuePending()) > 0 && (m.tr.Running || m.queue.paused) &&
		(selected || m.composer.Line() == 0)
}

// queueReadonlyKey answers the keys whose mutation a newer schema refuses with
// 409 inbox.schema_readonly: they change nothing and say why, instead of asking
// the kernel for a refusal it already owns.
func (m *model) queueReadonlyKey(k string, selected, acting bool) (tea.Cmd, bool) {
	if !m.queue.readonly {
		return nil, false
	}
	switch {
	case k == "esc" && selected:
		m.queueEnd(true)
		return nil, true
	case k == "enter" && selected, k == "d" && acting, k == "r" && acting,
		(k == "alt+up" || k == "alt+down") && selected,
		(k == "space" || k == " ") && len(m.queue.items) > 0 && m.composer.Value() == "":
		return m.queueRefuseReadonly(), true
	}
	return nil, false
}

// queueRefuseReadonly says why a queue key changed nothing: a newer schema owns
// the queue, so the kernel refuses every mutation and offering one only earns a
// 409.
func (m *model) queueRefuseReadonly() tea.Cmd {
	return func() tea.Msg {
		return queueDoneMsg{level: "warn", text: i18n.M.QueueReadonlyNote}
	}
}

// queueSelect moves the selection by delta and puts the message in the
// composer; past the newest it ends the selection.
func (m *model) queueSelect(delta int) tea.Cmd {
	pending := m.queuePending()
	if len(pending) == 0 {
		return nil
	}
	at := m.queueIndex()
	switch m.queue.selID {
	case "":
		m.queue.draft = m.composer.Value()
		at = len(pending) - 1
	default:
		at += delta
	}
	if at < 0 {
		at = 0
	}
	if at >= len(pending) {
		m.queueEnd(true)
		return nil
	}
	m.queue.confirm = false
	m.queue.selID = pending[at].ID
	m.queue.loaded, m.queue.ready = "", false
	id, ctx, c := m.queue.selID, m.ctx, m.client
	return func() tea.Msg {
		body, err := c.InboxBody(ctx, id)
		return queueBodyMsg{id: id, body: body, err: err}
	}
}

// queueEnd lets go of the selection; the draft comes back when asked.
func (m *model) queueEnd(restore bool) {
	if restore {
		m.composer.SetValue(m.queue.draft)
	}
	m.queue.selID, m.queue.loaded, m.queue.draft, m.queue.confirm, m.queue.ready = "", "", "", false, false
}

func (m *model) queueMove(up bool) tea.Cmd {
	pending := m.queuePending()
	at := m.queueIndex()
	to := at + 1
	if up {
		to = at - 1
	}
	if at < 0 || to < 0 || to >= len(pending) {
		return nil
	}
	// The kernel moves within its own list, which also holds rows this surface
	// does not draw; the position it takes is the one the visible neighbour has.
	toIndex := m.queueStoreIndex(pending[to].ID)
	if toIndex < 0 {
		return nil
	}
	id, ctx, c := m.queue.selID, m.ctx, m.client
	return func() tea.Msg {
		if err := c.InboxMove(ctx, id, toIndex); err != nil {
			return queueDoneMsg{err: err}
		}
		snap, err := c.Inbox(ctx)
		return queueSnapMsg{snap: snap, err: err}
	}
}

func (m *model) queueSave() tea.Cmd {
	text, id, ctx, c := strings.TrimSpace(m.composer.Value()), m.queue.selID, m.ctx, m.client
	if text == "" {
		return nil
	}
	m.queueEnd(true)
	return func() tea.Msg {
		if err := c.InboxUpdate(ctx, id, text); err != nil {
			return queueDoneMsg{err: err}
		}
		return queueDoneMsg{level: "info", text: fmt.Sprintf(i18n.M.QueueSavedFmt, shortID(id))}
	}
}

func (m *model) queueDelete() tea.Cmd {
	id, ctx, c := m.queue.selID, m.ctx, m.client
	if !m.queue.confirm {
		m.queue.confirm = true
		m.tr.AddNotice("info", fmt.Sprintf(i18n.M.QueueDeleteAgainFmt, shortID(id)))
		return m.commit()
	}
	m.queueEnd(true)
	return func() tea.Msg {
		if err := c.InboxDelete(ctx, id); err != nil {
			return queueDoneMsg{err: err}
		}
		return queueDoneMsg{level: "info", text: fmt.Sprintf(i18n.M.QueueDeletedFmt, shortID(id)), deleted: id}
	}
}

func (m *model) queueRetry() tea.Cmd {
	if at := m.queueIndex(); at >= 0 && mayHaveRun(m.queuePending()[at]) {
		return m.queueRetryHeld(m.queuePending()[at])
	}
	id, ctx, c := m.queue.selID, m.ctx, m.client
	m.queueEnd(true)
	return func() tea.Msg {
		if err := c.InboxRetry(ctx, id); err != nil {
			return queueDoneMsg{err: err}
		}
		return queueDoneMsg{level: "info", text: fmt.Sprintf(i18n.M.QueueRetriedFmt, shortID(id))}
	}
}

func (m *model) queuePause() tea.Cmd {
	paused, ctx, c := !m.queue.paused, m.ctx, m.client
	return func() tea.Msg {
		if err := c.InboxPause(ctx, paused); err != nil {
			return queueDoneMsg{err: err}
		}
		note := i18n.M.QueueResumedNote
		if paused {
			note = i18n.M.QueuePausedNote
		}
		return queueDoneMsg{level: "info", text: note}
	}
}
