package tui

import (
	"context"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"

	"reasonix/internal/base/i18n"
	"reasonix/internal/frontend/termrender"
)

// Options is what a TUI session starts with.
type Options struct {
	Client *Client
	// Prompt, when set, is sent as the first message.
	Prompt string
	// Restore reads the session back from /history before the first frame:
	// the session was resumed, and its conversation belongs on screen.
	Restore bool
	// PickSession opens the saved-session picker on the first frame.
	PickSession bool
	// Inline writes the conversation into the terminal's own scrollback
	// instead of taking the full screen.
	Inline bool
}

// Run drives the terminal until the user quits or ctx ends.
func Run(ctx context.Context, opts Options) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	m := newModel(ctx, opts)
	_, err := tea.NewProgram(m, tea.WithContext(ctx)).Run()
	return err
}

const (
	statusEvery    = time.Second
	quitArmWindow  = time.Second
	composerMaxRow = 8
)

type model struct {
	ctx     context.Context
	client  *Client
	opts    Options
	updates <-chan Update

	tr        Transcript
	committed map[int]bool
	// sayShown is how much of a streaming answer's text is already in the
	// terminal's scrollback.
	sayShown map[int]int

	width, height int
	composer      textarea.Model
	shell         bool
	history       []string
	histAt        int
	draft         string // what the composer held when a history walk began
	pastes        pasteStore
	status        Status
	quitArmedAt   time.Time
	ask           *askState
	menu          *menu
	todos         []TodoItem
	runSince      time.Time
	spinning      bool
	cancelling    bool
	apSel         approvalSel
	balance       string
	compaction    Compaction
	scr           *screen
	picker        *sessionPicker
	// frameRows is how tall the last inline frame was: a print has only the
	// rows above it to land in.
	frameRows int
}

type (
	updateMsg struct {
		u  Update
		ok bool
	}
	actionMsg struct {
		what string
		err  error
	}
	queuedMsg struct {
		row    int
		itemID string
		err    error
	}
	statusMsg struct {
		s   Status
		err error
	}
	historyMsg struct {
		msgs []HistoryMessage
		err  error
		// reprint is false after a gap: what was already printed stays, and
		// only the record behind it is reloaded.
		reprint bool
	}
	statusTickMsg struct{}
)

func newModel(ctx context.Context, opts Options) *model {
	ta := textarea.New()
	ta.Prompt = ""
	ta.ShowLineNumbers = false
	ta.DynamicHeight = true
	ta.MinHeight = 1
	ta.MaxHeight = composerMaxRow
	ta.SetVirtualCursor(false)
	ta.KeyMap.InsertNewline = key.NewBinding(key.WithKeys("ctrl+j", "shift+enter", "alt+enter"))
	termrender.ApplyTextareaTheme(&ta)
	ta.Focus()
	m := &model{
		ctx: ctx, client: opts.Client, opts: opts,
		committed: map[int]bool{}, sayShown: map[int]int{},
		composer: ta, width: 80, height: 24,
	}
	if !opts.Inline {
		m.scr = &screen{follow: true}
	}
	return m
}

func (m *model) Init() tea.Cmd {
	m.updates = m.client.Subscribe(m.ctx)
	cmds := []tea.Cmd{m.waitUpdate(), m.fetchStatus(), tickStatus(), m.fetchMeters()}
	if m.opts.Restore {
		cmds = append(cmds, m.fetchHistory(true))
	}
	if m.opts.PickSession {
		cmds = append(cmds, m.openPicker())
	}
	if p := strings.TrimSpace(m.opts.Prompt); p != "" {
		m.tr.AddUser(p)
		cmds = append(cmds, m.commit(), m.call("submit", func(ctx context.Context) error { return m.client.Submit(ctx, p) }))
	}
	return tea.Sequence(m.greet(), tea.Batch(cmds...))
}

func (m *model) waitUpdate() tea.Cmd {
	return func() tea.Msg {
		u, ok := <-m.updates
		return updateMsg{u: u, ok: ok}
	}
}

func (m *model) call(what string, fn func(context.Context) error) tea.Cmd {
	return func() tea.Msg { return actionMsg{what: what, err: fn(m.ctx)} }
}

func (m *model) fetchStatus() tea.Cmd {
	return func() tea.Msg {
		s, err := m.client.Status(m.ctx)
		return statusMsg{s: s, err: err}
	}
}

func (m *model) fetchHistory(reprint bool) tea.Cmd {
	return func() tea.Msg {
		msgs, err := m.client.History(m.ctx)
		return historyMsg{msgs: msgs, err: err, reprint: reprint}
	}
}

type metersMsg struct {
	balance    string
	compaction *Compaction
}

// fetchMeters reads what the footer shows that changes only between turns:
// the wallet and where the session folds.
func (m *model) fetchMeters() tea.Cmd {
	return func() tea.Msg {
		var out metersMsg
		out.balance, _, _ = m.client.Balance(m.ctx)
		if c, err := m.client.Compaction(m.ctx); err == nil {
			out.compaction = &c
		}
		return out
	}
}

func tickStatus() tea.Cmd {
	return tea.Tick(statusEvery, func(time.Time) tea.Msg { return statusTickMsg{} })
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.composer.SetWidth(max(msg.Width-4, 10))
		return m, nil
	case updateMsg:
		if !msg.ok {
			return m, nil
		}
		if msg.u.Gap {
			return m, tea.Batch(m.fetchHistory(false), m.waitUpdate())
		}
		was := m.tr.Running
		m.tr.Apply(msg.u.Event)
		cmds := []tea.Cmd{m.commit(), m.waitUpdate(), m.noteRunning(was)}
		if msg.u.Event.Kind == "turn_done" {
			m.noteTurnEnd()
			cmds = append(cmds, m.commit(), m.fetchMeters())
		}
		if m.tr.TodosMoved {
			m.tr.TodosMoved = false
			cmds = append(cmds, m.fetchTodos())
		}
		return m, tea.Batch(cmds...)
	case historyMsg:
		return m, m.restore(msg)
	case statusMsg:
		if msg.err == nil {
			m.status = msg.s
		}
		return m, nil
	case metersMsg:
		m.balance = msg.balance
		if msg.compaction != nil {
			m.compaction = *msg.compaction
		}
		return m, nil
	case spinMsg:
		return m, m.onSpin()
	case statusTickMsg:
		return m, tea.Batch(m.fetchStatus(), tickStatus())
	case actionMsg:
		if msg.err != nil {
			m.tr.AddNotice("error", msg.what+": "+msg.err.Error())
			return m, m.commit()
		}
		return m, nil
	case queuedMsg:
		m.tr.SetQueueID(msg.row, msg.itemID)
		if msg.err != nil {
			m.tr.Drop(msg.row)
			m.tr.AddNotice("error", "queue: "+msg.err.Error())
		}
		return m, m.commit()
	case todosMsg:
		if msg.err == nil {
			m.todos = msg.items
		}
		return m, nil
	case completionMsg:
		m.onCompletion(msg)
		return m, nil
	case tea.PasteMsg:
		m.composer.InsertString(m.pastes.fold(msg.Content))
		return m, nil
	case tea.KeyPressMsg:
		return m.onKey(msg)
	}
	if cmd, ok := m.onScreenMsg(msg); ok {
		return m, cmd
	}
	var cmd tea.Cmd
	m.composer, cmd = m.composer.Update(msg)
	return m, cmd
}

// onScreenMsg takes the answers to what this screen asked for itself: the
// clipboard, the session list, the mouse and its timers.
func (m *model) onScreenMsg(msg tea.Msg) (tea.Cmd, bool) {
	switch msg := msg.(type) {
	case clipImageMsg:
		return m.onClipImage(msg), true
	case clipTextMsg:
		return m.onClipText(msg), true
	case sessionsMsg:
		return m.onSessions(msg), true
	case resumedMsg:
		return m.onResumed(msg), true
	case bannerMsg:
		return m.emit(func(int) string { return banner(msg.s) }), true
	case tea.MouseMsg:
		return m.onMouse(msg), true
	case termrender.ClipboardCopyMsg:
		return m.onCopied(msg), true
	case flashDoneMsg:
		return nil, true
	case edgeMsg:
		return m.onEdge(), true
	}
	return nil, false
}

// noteTurnEnd says how a turn that did not finish ended; a finished one says
// so through its answer and receipt.
func (m *model) noteTurnEnd() {
	switch m.tr.Terminal {
	case TurnCancelled:
		m.tr.AddNotice("warn", i18n.M.TurnCancelled)
	case TurnFailed:
		m.tr.AddNotice("error", m.tr.EndReason)
	}
}

// restore reloads the record. After a gap the lines already printed stay
// where they are: the terminal owns them now, so the reload marks the record
// as shown rather than printing it twice.
func (m *model) restore(msg historyMsg) tea.Cmd {
	if msg.err != nil {
		m.tr.AddNotice("error", "history: "+msg.err.Error())
		return m.commit()
	}
	m.tr.Restore(msg.msgs)
	m.sayShown = map[int]int{}
	if !msg.reprint {
		m.committed = map[int]bool{}
		for _, it := range m.tr.Items {
			m.committed[it.ID] = true
		}
		m.tr.AddNotice("warn", i18n.M.StreamReloaded)
		return m.commit()
	}
	return tea.Sequence(m.fillScreen(), m.commit())
}

// commit prints, in order, every row from the top that has settled. It stops
// at the first row still changing so the scrollback keeps the order the
// conversation happened in; input still waiting in the queue does not hold the
// rows after it back.
func (m *model) commit() tea.Cmd {
	var out []settledPrint
	for i := range m.tr.Items {
		it := &m.tr.Items[i]
		if m.committed[it.ID] {
			continue
		}
		if it.Kind == ItemUser && it.Pending {
			continue
		}
		if it.Kind == ItemSay && !it.Done {
			if chunk := m.settledChunk(it); chunk != nil {
				out = append(out, settledPrint{render: chunk})
			}
			break
		}
		if !settled(it) {
			break
		}
		m.committed[it.ID] = true
		out = append(out, m.settledRow(*it, m.sayShown[it.ID]))
	}
	return m.publish(out)
}

// settledChunk draws the part of a streaming answer that has become final
// since it was last printed, or nil when nothing new has.
func (m *model) settledChunk(it *Item) func(int) string {
	end := settledPrefix(it.Text)
	shown := m.sayShown[it.ID]
	if end <= shown {
		return nil
	}
	m.sayShown[it.ID] = end
	row := *it
	return func(w int) string { return withThought(&row, shown, renderSayPart(row.Text[shown:end], shown == 0, w)) }
}

func settled(it *Item) bool {
	switch it.Kind {
	case ItemSay, ItemCompaction:
		return it.Done
	case ItemTool:
		return !it.Running
	case ItemApproval, ItemAsk:
		return it.Verdict != ""
	}
	return true
}
