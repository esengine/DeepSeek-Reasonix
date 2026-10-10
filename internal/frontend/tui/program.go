package tui

import (
	"context"
	"os"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"reasonix/internal/base/i18n"
	"reasonix/internal/frontend/termrender"
)

// Options is what a TUI session starts with.
type Options struct {
	Client *Client
	// Version is the build version passed to the CLI launcher.
	Version string
	// Prompt, when set, is sent as the first message.
	Prompt string
	// Restore reads the session back from /history before the first frame:
	// the session was resumed, and its conversation belongs on screen.
	Restore bool
	// PickSession opens the saved-session picker on the first frame.
	PickSession bool
	// PickAmong, when set, limits that first picker to these session paths.
	PickAmong []string
	// Inline writes the conversation into the terminal's own scrollback
	// instead of taking the full screen.
	Inline bool
	// HideTurnUsage keeps each request's token and cost receipt off the transcript.
	HideTurnUsage bool
	// AutoSubmit commits a multi-question ask once its last question is
	// answered, instead of showing the Submit tab.
	AutoSubmit bool
	// CommandMode gives the composer a vi command mode: Esc enters command
	// mode, a running turn or not, and only Ctrl+C interrupts.
	CommandMode bool
	// QuitCommands are the slash names that end the session, from the
	// command catalogue the completion menu offers.
	QuitCommands []string
	// Statusline, when set, turns the footer's context JSON into one line
	// that replaces the telemetry row; "" keeps the built-in row.
	Statusline func(ctx context.Context, stdin string) string
}

// Run drives the terminal until the user quits or ctx ends.
func Run(ctx context.Context, opts Options) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	m := newModel(ctx, opts)
	p := tea.NewProgram(m, tea.WithContext(ctx))
	if clusters, ok := clustersEmoji(); ok && clusters {
		go p.Send(clusterReport)
	}
	// Full screen repaints every frame, so the diff formatter runs off the render
	// path: placeholder rows now, formatted ones when the run lands. Inline writes
	// to the scrollback, where a row cannot be repainted, so it stays inline.
	if !opts.Inline {
		termrender.SetDiffFormatNotify(func(k termrender.DiffKey) { p.Send(diffFormattedMsg{key: k}) })
		defer termrender.SetDiffFormatNotify(nil)
	}
	_, err := p.Run()
	return err
}

const (
	statusEvery    = time.Second
	quitArmWindow  = time.Second
	escArmWindow   = 600 * time.Millisecond
	composerMaxRow = 8
)

type model struct {
	ctx     context.Context
	client  *Client
	opts    Options
	updates <-chan Update
	focus   focusState

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
	viCmd         bool   // idle composer is in vi command (normal) mode, not insert
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
	statusline    string
	compaction    Compaction
	git           GitInfo
	scr           *screen
	picker        *sessionPicker
	rewind        *rewindPicker
	copying       *copyPicker
	clearing      *clearConfirm
	setup         *connectionSetup
	skills        *skillPicker
	quick         *quickPicker
	mcp           *mcpPanel
	lastEsc       time.Time // an idle Esc on an empty composer, arming the second
	// frameRows is how tall the last inline frame was: a print has only the
	// rows above it to land in.
	frameRows int
	glyphs    *glyphFit // console-measured stand-ins for runes drawn wider than counted
	// yoloRestore is the posture Ctrl+Y leaves YOLO for.
	yoloRestore string
	// verbose keeps an answer's thinking open as it settles; /verbose toggles it.
	verbose bool
}

type (
	updateMsg struct {
		us []Update
		ok bool
	}
	actionMsg struct {
		what string
		err  error
	}
	sentMsg struct {
		display string
		err     error
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
	// diffFormattedMsg is the diff formatter's background run reporting a
	// result; the model repaints the rows that asked for that key so the
	// formatted rows replace the placeholder drawn while the run was in flight.
	diffFormattedMsg struct {
		key termrender.DiffKey
	}
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
		glyphs: newConsoleGlyphFit(os.Stdout),
	}
	termrender.SetCells(ansi.WcWidth)
	m.verbose = storedVerbose()
	if !opts.Inline {
		m.scr = &screen{follow: true, mouseOff: mouseCaptureOffByDefault()}
	}
	return m
}

func (m *model) Init() tea.Cmd {
	m.updates = m.client.Subscribe(m.ctx)
	cmds := []tea.Cmd{m.waitUpdate(), m.fetchStatus(), tickStatus(), m.fetchMeters(), m.checkSetup()}
	if m.opts.Restore {
		cmds = append(cmds, m.fetchHistory(true))
	}
	if m.opts.PickSession {
		cmds = append(cmds, m.openPicker())
	}
	cmds = append(cmds, m.viewedAtStart())
	if p := strings.TrimSpace(m.opts.Prompt); p != "" {
		m.tr.AddUser(p)
		cmds = append(cmds, m.commit(), m.call("submit", func(ctx context.Context) error { return m.client.Submit(ctx, p) }))
	}
	return tea.Sequence(m.greet(), tea.Batch(cmds...))
}

// resize records the new terminal size and drops any held viewport position: the
// rows re-wrap, so a held position no longer means what it did.
func (m *model) resize(msg tea.WindowSizeMsg) {
	m.width, m.height = msg.Width, msg.Height
	m.composer.SetWidth(max(msg.Width-4, 10))
	if m.scr != nil && m.scr.follow {
		m.scr.yoff = 0
	}
}

// waitUpdate hands the model the next stream frames. It coalesces everything
// already queued into one message so a burst of deltas costs one render rather
// than one per delta: the view re-parses the whole growing answer each time it
// is drawn, so drawing a token at a time is quadratic in the stream length.
// Order is kept, and a slow stream still delivers each frame as it arrives.
func (m *model) waitUpdate() tea.Cmd {
	return func() tea.Msg {
		u, ok := <-m.updates
		if !ok {
			return updateMsg{ok: false}
		}
		us := []Update{u}
		for {
			select {
			case v, ok := <-m.updates:
				if !ok {
					return updateMsg{us: us, ok: true}
				}
				us = append(us, v)
			default:
				return updateMsg{us: us, ok: true}
			}
		}
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
	statusline *string
	git        *GitInfo
}

// fetchMeters reads what the footer shows that changes only between turns:
// the wallet, where the session folds and the work tree's branch.
func (m *model) fetchMeters() tea.Cmd {
	return func() tea.Msg {
		var out metersMsg
		out.balance, _, _ = m.client.Balance(m.ctx)
		if c, err := m.client.Compaction(m.ctx); err == nil {
			out.compaction = &c
		}
		if g, err := m.client.WorkspaceGit(m.ctx); err == nil {
			out.git = &g
		}
		if run := m.opts.Statusline; run != nil {
			if s, err := m.client.Status(m.ctx); err == nil {
				line := run(m.ctx, statuslinePayload(s))
				out.statusline = &line
			}
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
		m.resize(msg)
		return m, nil
	case tea.FocusMsg, tea.BlurMsg:
		return m, m.focus.report(msg, m.markViewed)
	case tea.ModeReportMsg:
		noteCells(msg)
		return m, nil
	case updateMsg:
		if !msg.ok {
			return m, nil
		}
		was := m.tr.Running
		rearm := m.waitUpdate()
		turnDone := false
		for _, u := range msg.us {
			if u.Gap {
				// Frames between two points are gone: reload the record rather
				// than apply a frame that follows a hole.
				return m, tea.Batch(m.fetchHistory(false), rearm)
			}
			m.dropSpentTodos(u.Event.Kind)
			m.tr.Apply(u.Event)
			if u.Event.Kind == "turn_done" {
				turnDone = true
			}
		}
		cmds := []tea.Cmd{m.commit(), rearm, m.noteRunning(was)}
		if turnDone {
			m.noteTurnEnd()
			cmds = append(cmds, m.commit(), m.fetchMeters(), m.viewedOnTurnEnd())
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
		if msg.statusline != nil {
			m.statusline = *msg.statusline
		}
		if msg.git != nil {
			m.git = *msg.git
		}
		return m, nil
	case spinMsg:
		return m, m.onSpin()
	case statusTickMsg:
		return m, tea.Batch(m.fetchStatus(), tickStatus())
	case diffFormattedMsg:
		m.invalidateDiffKey(msg.key)
		return m, m.commit()
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
		m.onTodos(msg)
		return m, nil
	case completionMsg:
		m.onCompletion(msg)
		return m, nil
	case tea.PasteMsg:
		m.insertPaste(msg.Content)
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
	case sentMsg:
		return m.onSent(msg), true
	case urlAnswerMsg:
		return m.onURLAnswer(msg), true
	case clipImageMsg:
		return m.onClipImage(msg), true
	case clipTextMsg:
		return m.onClipText(msg), true
	case helpMsg:
		return m.onHelp(msg), true
	case setupStateMsg:
		return m.onSetupState(msg), true
	case connectionsMsg:
		return m.onConnections(msg), true
	case connectionTestedMsg:
		return m.onConnectionTested(msg), true
	case connectionSavedMsg:
		return m.onConnectionSaved(msg), true
	case skillsMsg:
		return m.onSkills(msg), true
	case skillsSavedMsg:
		return m.onSkillsSaved(msg), true
	case modelsMsg:
		return m.onModels(msg), true
	case modelSwitchedMsg:
		return m.onModelSwitched(msg), true
	case mcpMsg:
		return m.onMCP(msg), true
	case mcpActionErrMsg:
		return m.onMCPActionErr(msg), true
	case sessionsMsg:
		return m.onSessions(msg), true
	case resumedMsg:
		return m.onResumed(msg), true
	case checkpointsMsg:
		return m.onCheckpoints(msg), true
	case rewindPlanMsg:
		return m.onRewindPlan(msg), true
	case rewoundMsg:
		return m.onRewound(msg), true
	case bannerMsg:
		return m.emit(func(int, bool) string { return banner(msg.s) }), true
	case tea.MouseMsg:
		return m.onMouse(msg), true
	case termrender.ClipboardCopyMsg:
		return m.onCopied(msg), true
	case flashDoneMsg:
		return nil, true
	case edgeMsg:
		return m.onEdge(), true
	}
	return m.onQueueMsg(msg)
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
	m.pastes.seed(msg.msgs)
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
		if m.hidden(it) {
			m.committed[it.ID] = true
			continue
		}
		if it.Kind == ItemSay && !it.Done {
			if chunk, ok := m.settledChunk(it); ok {
				out = append(out, chunk)
			}
			break
		}
		if !settled(it) {
			break
		}
		m.committed[it.ID] = true
		m.settleThought(it)
		out = append(out, m.settledRow(*it, m.sayShown[it.ID]))
	}
	return m.publish(out)
}

// hidden is a row kept off the screen: one the configuration hides, or task
// bookkeeping that settled cleanly. It still folds into the transcript: a later
// frame of the same request restates it in place.
func (m *model) hidden(it *Item) bool {
	return (it.Kind == ItemUsage && m.opts.HideTurnUsage) || (it.bookkeeping() && !it.Running)
}

// settledChunk draws the part of a streaming answer that has become final
// since it was last printed; false when nothing new has. The first part keeps
// its row, so full screen its thinking can open.
func (m *model) settledChunk(it *Item) (settledPrint, bool) {
	end := settledPrefix(it.Text)
	shown := m.sayShown[it.ID]
	if end <= shown {
		return settledPrint{}, false
	}
	m.sayShown[it.ID] = end
	row := *it
	p := settledPrint{render: func(w int, hideRail bool) string {
		return withThought(&row, shown, w, renderSayPart(row.Text[shown:end], shown == 0, w, hideRail))
	}}
	if m.scr != nil && shown == 0 && hasThought(row.Reasoning) {
		row.Fold = foldShut
		p.row = &row
	}
	if m.verbose && shown == 0 && hasThought(row.Reasoning) {
		row.Fold = m.verboseFold()
	}
	return p, true
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
