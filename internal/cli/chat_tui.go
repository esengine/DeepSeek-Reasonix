package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"image/color"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"reasonix/internal/agent"
	"reasonix/internal/command"
	"reasonix/internal/control"
	"reasonix/internal/event"
	"reasonix/internal/i18n"
	"reasonix/internal/memory"
	"reasonix/internal/outputstyle"
	"reasonix/internal/plugin"
	"reasonix/internal/provider"
	"reasonix/internal/skill"
)

// chatTUI is a bubbletea Model that runs a chat session in the terminal's
// normal buffer (no alt-screen). Finalized output — user bubbles, tool dispatch
// lines, usage lines, reasoning, and the rendered assistant answer — is
// committed to the native scrollback via tea.Println, so the wheel, scrollbar,
// and copy all work like any CLI. The bubbletea-managed region is only the
// bottom — input box, status line, an optional approval/plan banner, and the
// autocomplete menu — and it is kept a stable height (it changes only on
// discrete user actions, never per streamed token) so the renderer commits
// scrollback cleanly without stranding the input box's border lines. This
// mirrors how Claude Code uses Ink's <Static> to freeze finished output into
// scrollback while re-rendering just the active prompt.
type chatTUI struct {
	ctrl    *control.Controller
	label   string
	missing string // missing-key warning surfaced once in the banner, "" when ready

	width  int
	height int
	// repaintToggle flips on each resize-triggered forceRepaintMsg so the View
	// differs byte-for-byte, defeating the renderer's "unchanged view → skip
	// render" optimization and forcing the one extra repaint that clears the
	// resize ghost (the same repaint a keystroke would have triggered).
	repaintToggle bool

	input   textarea.Model
	spinner spinner.Model

	state    tuiState
	runStart time.Time
	elapsed  int
	// turnTokens accumulates this turn's output tokens (summed from per-step Usage
	// events) for the live "↓N" readout in the running status line.
	turnTokens int

	// balance is the last-fetched wallet-balance readout (e.g. "¥110.00"), "" when
	// the provider declares no balance_url or a fetch failed. Refreshed async on
	// startup and after each turn so the status line stays roughly current without
	// blocking the event loop.
	balance string

	// todoArgs is the latest todo_write call's raw args; it drives the task list
	// pinned just above the input (see renderTodoPanel). "" when there's no list.
	// Persists across turns until the work completes or a new session starts.
	todoArgs string

	// planMode mirrors the agent's read-only gate (Tab toggles it). The marker
	// rides in outgoing user messages so the cache-stable prompt prefix is left
	// untouched.
	planMode bool

	// history is a resumed session's messages, committed to scrollback once on
	// the first WindowSizeMsg so a reopened chat shows its prior transcript.
	history []provider.Message

	// reasoning accumulates the in-progress thinking stream (dim); pending
	// accumulates the in-progress answer (raw markdown). They are committed to
	// scrollback (reasoning verbatim, answer markdown-rendered) when they
	// finalize — at a tool/usage boundary or turn end — not previewed live, so
	// the bottom region stays a stable height. pendingCommit queues finalized
	// lines so a single Update emits exactly one ordered tea.Println.
	reasoning     *strings.Builder
	pending       *strings.Builder
	pendingCommit *[]string
	renderer      *mdRenderer
	eventCh       chan event.Event
	started       bool // banner + resumed history committed once

	// The user bubble for an in-flight turn is deferred, not echoed on Enter: it's
	// held in pendingBubble and committed to scrollback only when the first
	// response packet arrives (commitPendingBubble). Pressing Esc/Ctrl+C before
	// then "un-sends" the message — its text returns to the input box and nothing
	// reaches scrollback. bubblePending is true from startTurn until the bubble
	// commits or is un-sent; turnDiscarded then swallows the turn's already-buffered
	// events until its TurnDone settles.
	pendingBubble string
	bubblePending bool
	turnDiscarded bool

	// pendingApproval holds the tool-call approval currently shown in the banner
	// (nil when none). While set, the controller's run goroutine is blocked
	// awaiting ctrl.Approve and key input is captured to answer it.
	pendingApproval *event.Approval

	// chooser holds the `ask` tool's question card (nil when none). While set, the
	// run goroutine is blocked awaiting ctrl.AnswerQuestion and keys drive the card.
	chooser *chooser

	// rewind holds the Esc-Esc / "/rewind" picker (nil when closed); while set,
	// keys drive it and it renders as an overlay. lastEsc times the double-Esc
	// gesture that opens it on an empty composer.
	rewind  *rewindPicker
	lastEsc time.Time

	// host is the running MCP servers (nil when no plugins). The TUI reads
	// prompts (slash commands), resources (@-references), and server status
	// (/mcp) from it.
	host *plugin.Host

	// commands are custom slash commands loaded from .reasonix/commands; each renders
	// its template with the typed args and sends the result as a turn.
	commands []command.Command

	// skills are the discoverable skills (built-in + user/project); each is offered
	// in the slash menu as "/<name>" and managed via /skill.
	skills []skill.Skill

	// buildController builds a fresh controller on a model ref, carrying prior
	// history across (set by chatREPL; it must NOT touch this model — the swap
	// happens in runModelSubcommand on the running copy). nil disables /model.
	// modelRef is the active "provider/model" ref, marked current in the picker.
	buildController func(ref string, carry []provider.Message) (*control.Controller, error)
	modelRef        string

	// outputStyle is the active output-style name (config agent.output_style),
	// shown as the current entry in the /output-style listing. "" = default.
	outputStyle string

	// modelSwitchPending is true while an async /model build is in flight.
	modelSwitchPending bool
	// pendingModelSwitch holds the tea.Cmd that triggers the async build.
	pendingModelSwitch tea.Cmd

	// completion is the live autocomplete menu (slash commands; @-refs later).
	completion completion
}

type tuiState int

const (
	tuiIdle tuiState = iota
	tuiRunning
)

// agentEventMsg is one typed event from the agent's run loop.
type agentEventMsg event.Event

// compactDoneMsg reports that an async /compact pass returned. The card was
// already drawn from the CompactionDone event; this only surfaces a failure and
// snapshots on success.
type compactDoneMsg struct{ err error }

// elapsedTickMsg fires once a second while a turn runs, driving the "thinking
// Ns" counter in the status line.
type elapsedTickMsg struct{}

// forceRepaintMsg is queued after a real terminal resize to trigger one extra
// render. In inline mode bubbletea's resize redraw can leave the prior frame's
// border on screen until the next normal render (typing clears it); this fires
// that render automatically so the user doesn't have to.
type forceRepaintMsg struct{}

// balanceMsg carries the result of an async wallet-balance fetch; text is the
// formatted readout ("" when none/failed).
type balanceMsg struct{ text string }

// modelSwitchMsg carries the result of an async /model switch. A nil err means
// the new controller is ready in ctrl; label/commands/skills/host mirror the
// fields that runModelSubcommand used to set synchronously.
type modelSwitchMsg struct {
	ref      string
	ctrl     *control.Controller
	label    string
	commands []command.Command
	skills   []skill.Skill
	host     *plugin.Host
	err      error
}

// fetchBalance queries the provider's wallet balance off the event loop. It's a
// no-op readout ("") when the provider declares no balance_url or the fetch
// fails, so the status line stays quiet rather than surfacing an error.
func fetchBalance(ctrl *control.Controller) tea.Cmd {
	return func() tea.Msg {
		b, err := ctrl.Balance(context.Background())
		if err != nil || b == nil {
			return balanceMsg{}
		}
		return balanceMsg{text: b.Display()}
	}
}

// promptResolvedMsg carries the result of fetching an MCP prompt (an async
// prompts/get). display is the command line echoed as the user bubble; sent is
// the rendered prompt text that becomes the model turn.
type promptResolvedMsg struct {
	display string
	sent    string
	err     error
}

// refsResolvedMsg carries the result of resolving the @references in a
// submitted line (async file reads / MCP resources/read).
type refsResolvedMsg struct {
	line  string
	block string
	errs  []string
}

// newChatTUI assembles the initial model. The controller has already been wired
// with an event sink that feeds eventCh; the TUI issues commands to it and
// renders the events it emits. Label, history, host, and commands are read from
// the controller, so a resumed session pre-populates scrollback.
func newChatTUI(ctrl *control.Controller, missing string, eventCh chan event.Event, termW int) chatTUI {
	ti := textarea.New()
	ti.Prompt = ""
	ti.CharLimit = 16384
	ti.SetHeight(1)
	ti.ShowLineNumbers = false
	// Use the real terminal cursor (not a styled virtual one) so View can place
	// it at the insertion point and IME candidate windows anchor to the input.
	ti.SetVirtualCursor(false)
	// Plain Enter submits (the chatTUI handler intercepts it), so the textarea's
	// own InsertNewline binding moves to Alt+Enter / Ctrl+J / Shift+Enter.
	ti.KeyMap.InsertNewline = key.NewBinding(key.WithKeys("alt+enter", "ctrl+j", "shift+enter"))
	ti.Focus()

	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("173"))

	commitBuf := []string{}
	return chatTUI{
		ctrl:          ctrl,
		label:         ctrl.Label(),
		missing:       missing,
		input:         ti,
		spinner:       sp,
		reasoning:     &strings.Builder{},
		pending:       &strings.Builder{},
		pendingCommit: &commitBuf,
		renderer:      newMarkdownRenderer(termW),
		eventCh:       eventCh,
		history:       ctrl.History(),
		host:          ctrl.Host(),
		commands:      ctrl.Commands(),
		skills:        ctrl.Skills(),
	}
}

// prompts returns the MCP prompts discovered at startup (nil when no plugins).
func (m *chatTUI) prompts() []plugin.Prompt {
	if m.host == nil {
		return nil
	}
	return m.host.Prompts()
}

func (m chatTUI) Init() tea.Cmd {
	return tea.Batch(
		textarea.Blink,
		waitForAgentEvent(m.eventCh),
		fetchBalance(m.ctrl),
	)
}

func (m chatTUI) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		// bubbletea already resizes+redraws the renderer on a size change, but in
		// inline mode that single redraw can strand the prior frame's border until
		// the next normal render (a keystroke clears it). We must NOT use
		// tea.ClearScreen: inline-mode clearScreen does MoveTo(0,0) into the
		// scrollback region and repaints there, doubling the frame. Instead, after
		// an actual size change we queue one forceRepaintMsg — an automatic "nudge"
		// that fires exactly that extra render and wipes the ghost.
		resized := m.started && (m.width != msg.Width || m.height != msg.Height)
		m.width = msg.Width
		m.height = msg.Height
		m.input.SetWidth(msg.Width - 4)
		m.renderer = newMarkdownRenderer(msg.Width)
		// Commit the banner — and a resumed session's transcript — to scrollback
		// once, now that the width is known.
		if !m.started {
			m.started = true
			var b strings.Builder
			b.WriteString(renderTUIBanner(m.label, m.missing, msg.Width))
			if len(m.history) > 0 {
				r := newMarkdownRenderer(msg.Width)
				for _, sec := range replaySectionsFor(m.history, msg.Width, r) {
					b.WriteString(sec)
				}
				m.history = nil
			}
			m.commitLine(strings.TrimRight(b.String(), "\n"))
		}
		if resized {
			cmds = append(cmds, func() tea.Msg { return forceRepaintMsg{} })
		}

	case forceRepaintMsg:
		// Flip the toggle so the next View differs (a zero-width char in the
		// status line) and the renderer can't skip it — repainting over the ghost.
		m.repaintToggle = !m.repaintToggle

	case tea.KeyPressMsg:
		// A question card is modal: keys drive it. In its free-text ("Type
		// something") mode, the keystroke goes to the textarea — Enter confirms the
		// custom answer, Esc backs out of typing — so input/IME work as usual.
		if m.chooser != nil {
			if m.chooser.typing {
				switch msg.String() {
				case "enter":
					val := strings.TrimSpace(m.input.Value())
					m.input.Reset()
					m.input.SetHeight(1)
					m.chooser.typing = false
					if val == "" {
						return m, finalize(m, cmds)
					}
					m.chooser.custom[m.chooser.tab] = val
					m.chooser.sel[m.chooser.tab] = map[int]bool{}
					return m.chooserAdvance()
				case "esc":
					m.chooser.typing = false
					m.input.Reset()
					m.input.SetHeight(1)
					return m, finalize(m, cmds)
				}
				var ic tea.Cmd
				m.input, ic = m.input.Update(msg)
				cmds = append(cmds, ic)
				m.growInputToFit()
				return m, finalize(m, cmds)
			}
			return m.handleChooserKey(msg)
		}
		// The rewind picker is modal while open: keys navigate it.
		if m.rewind != nil {
			return m.handleRewindKey(msg)
		}
		// A pending tool approval is modal: keystrokes answer it (y/a/n, Enter,
		// Esc) rather than reaching the input.
		if m.pendingApproval != nil {
			return m.handleApprovalKey(msg)
		}
		// While the autocomplete menu is open it captures navigation/accept keys
		// (↑/↓ move, Tab/Enter accept, Esc close); everything else falls through
		// to the textarea and re-filters the menu at the end of Update.
		if m.completion.active {
			switch msg.String() {
			case "up":
				m.moveCompletion(-1)
				return m, nil
			case "down":
				m.moveCompletion(1)
				return m, nil
			case "tab", "enter":
				m.acceptCompletion()
				return m, nil
			case "esc":
				m.completion = completion{}
				return m, nil
			}
		}
		switch msg.String() {
		case "esc":
			// "Back out" of the most specific in-progress state: un-send a just-sent
			// turn (server not yet replied), cancel a streaming turn, turn plan mode
			// off, or clear typed-but-unsent input. Scrollback is the terminal's now,
			// so there's no viewport to dismiss.
			switch {
			case m.state == tuiRunning && m.bubblePending:
				m.unsendPending()
			case m.state == tuiRunning:
				m.ctrl.Cancel()
			case m.ctrl.Bypass():
				m.ctrl.SetBypass(false) // back out of YOLO
			case m.planMode:
				m.planMode = false
				m.ctrl.SetPlanMode(false)
			default:
				// Idle with nothing to back out: a double-Esc on an empty composer
				// opens the rewind picker (Claude Code's gesture); a first Esc just
				// arms it. Non-empty input clears as before.
				if strings.TrimSpace(m.input.Value()) == "" {
					if !m.lastEsc.IsZero() && time.Since(m.lastEsc) < 600*time.Millisecond {
						m.lastEsc = time.Time{}
						m.openRewind()
					} else {
						m.lastEsc = time.Now()
					}
				} else {
					m.input.Reset()
				}
			}
			return m, nil
		case "ctrl+c":
			if m.state == tuiRunning {
				if m.bubblePending {
					m.unsendPending() // server not yet replied — restore text, leave no trace
				} else {
					m.ctrl.Cancel()
				}
				return m, nil
			}
			return m, tea.Quit
		case "ctrl+d":
			return m, tea.Quit
		case "tab":
			if m.state == tuiRunning {
				break
			}
			m.cycleMode()
			return m, nil
		case "enter":
			if m.state == tuiRunning {
				return m, nil // ignore Enter while a turn is in flight
			}
			if m.modelSwitchPending {
				return m, nil // ignore Enter while /model switch is building
			}
			line := strings.TrimSpace(m.input.Value())

			if line == "" {
				return m, nil
			}
			if line == "exit" || line == "quit" || line == ":q" {
				return m, tea.Quit
			}

			// "#<note>" quick-adds a memory line locally, no model turn —
			// mirroring Claude Code's "#" memory shortcut.
			if strings.HasPrefix(line, "#") {
				m.input.Reset()
				m.input.SetHeight(1)
				note := strings.TrimSpace(strings.TrimPrefix(line, "#"))
				if note == "" {
					m.notice("nothing to remember")
				} else if path, err := m.ctrl.QuickAdd(memory.ScopeProject, note); err != nil {
					m.notice("memory: " + err.Error())
				} else {
					m.notice("remembered → " + path)
				}
				return m, finalize(m, cmds)
			}

			// Slash commands run locally without going through the model.
			if strings.HasPrefix(line, "/") {
				m.input.Reset()
				m.input.SetHeight(1)
				cmds = append(cmds, m.runSlashCommand(line))
				return m, finalize(m, cmds)
			}

			m.input.Reset()
			m.input.SetHeight(1)

			// @references (local files / MCP resources) are resolved off the event
			// loop by the controller; the turn starts when they resolve
			// (refsResolvedMsg).
			if m.ctrl.HasRefs(line) {
				cmds = append(cmds, m.resolveRefs(line))
				return m, finalize(m, cmds)
			}

			cmds = append(cmds, m.startTurn(m.ctrl.Compose(line), line))
			return m, finalize(m, cmds)
		}

	case agentEventMsg:
		m.ingestEvent(event.Event(msg))
		cmds = append(cmds, waitForAgentEvent(m.eventCh))
		// A turn just spent tokens (and money) — refresh the balance readout.
		if event.Event(msg).Kind == event.TurnDone {
			cmds = append(cmds, fetchBalance(m.ctrl))
		}

	case balanceMsg:
		m.balance = msg.text

	case compactDoneMsg:
		if msg.err != nil {
			m.notice(fmt.Sprintf("%s: %v", i18n.M.SlashCompactFailed, msg.err))
		} else {
			_ = m.ctrl.Snapshot()
		}

	case modelSwitchMsg:
		m.modelSwitchPending = false
		m.pendingModelSwitch = nil
		if msg.err != nil {
			m.notice("model: " + msg.err.Error())
		} else {
			m.ctrl = msg.ctrl
			m.label = msg.label
			m.commands = msg.commands
			m.skills = msg.skills
			m.host = msg.host
			m.modelRef = msg.ref
			m.notice(fmt.Sprintf("switched to %s (conversation carried over; prompt cache resets)", m.label))
			cmds = append(cmds, fetchBalance(m.ctrl))
			// Re-issue waitForAgentEvent to keep the event loop alive.
			cmds = append(cmds, waitForAgentEvent(m.eventCh))
		}

	case promptResolvedMsg:
		switch {
		case msg.err != nil:
			m.commitLine(wrapForViewport(i18n.M.ErrorPrefix+" "+msg.err.Error(), m.width, lipgloss.Color("3")))
		case strings.TrimSpace(msg.sent) == "":
			m.notice(i18n.M.SlashPromptEmpty)
		default:
			cmds = append(cmds, m.startTurn(m.ctrl.Compose(msg.sent), msg.display))
		}

	case refsResolvedMsg:
		for _, e := range msg.errs {
			m.notice(e) // surface a fetch failure but still send the turn
		}
		sent := msg.line
		if msg.block != "" {
			sent = "Referenced context:\n\n" + msg.block + "\n\n" + msg.line
		}
		cmds = append(cmds, m.startTurn(m.ctrl.Compose(sent), msg.line))

	case elapsedTickMsg:
		if m.state == tuiRunning {
			m.elapsed = int(time.Since(m.runStart).Seconds())
			cmds = append(cmds, elapsedTick())
		}

	case spinner.TickMsg:
		if m.state == tuiRunning {
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(msg)
			cmds = append(cmds, cmd)
		}
	}

	var ic tea.Cmd
	m.input, ic = m.input.Update(msg)
	cmds = append(cmds, ic)
	m.growInputToFit()
	// Re-filter the autocomplete menu against the freshly-edited input.
	if _, ok := msg.(tea.KeyPressMsg); ok {
		m.updateCompletion()
	}

	return m, finalize(m, cmds)
}

// finalize flushes any queued scrollback lines into a single ordered tea.Println
// (Batch doesn't preserve order across multiple Println cmds, so we coalesce per
// Update) and batches it with the turn's other commands.
func finalize(m chatTUI, cmds []tea.Cmd) tea.Cmd {
	if len(*m.pendingCommit) > 0 {
		out := strings.TrimRight(clampWidth(strings.Join(*m.pendingCommit, "\n"), m.width), "\n")
		*m.pendingCommit = (*m.pendingCommit)[:0]
		// Commit in screen-bounded chunks. v2's inline renderer commits scrollback
		// via insertAbove, which scrolls the screen and InsertLine()s by the
		// block's line count; a single block taller than the screen makes its
		// CursorUp clamp at the top and the inserts misalign — the whole frame
		// (input box, banner) corrupts. Splitting so each Println is at most a
		// screenful keeps insertAbove within bounds. Sequence preserves order
		// (Batch does not across multiple Printlns).
		var prints []tea.Cmd
		for _, chunk := range chunkLines(out, m.scrollChunkHeight()) {
			prints = append(prints, tea.Println(chunk))
		}
		cmds = append(cmds, tea.Sequence(prints...))
	}
	return tea.Batch(cmds...)
}

// scrollChunkHeight is the largest block (in lines) finalize prints at once so
// v2's insertAbove stays within the screen. It leaves room for the pinned
// bottom frame (input box + status). Falls back to a generous default before
// the first WindowSizeMsg sets the height.
func (m chatTUI) scrollChunkHeight() int {
	if m.height <= 0 {
		return 100
	}
	if n := m.height - 5; n > 1 {
		return n
	}
	return 1
}

// chunkLines splits s into blocks of at most n lines each, preserving order and
// line content. A single block is returned when it already fits.
func chunkLines(s string, n int) []string {
	if n < 1 {
		n = 1
	}
	lines := strings.Split(s, "\n")
	if len(lines) <= n {
		return []string{s}
	}
	var out []string
	for i := 0; i < len(lines); i += n {
		end := i + n
		if end > len(lines) {
			end = len(lines)
		}
		out = append(out, strings.Join(lines[i:end], "\n"))
	}
	return out
}

// clampWidth hard-breaks any line wider than width so no scrollback line wraps
// in the terminal. bubbletea's inline renderer estimates how far to scroll for
// each printed block from each line's width (insertAbove: offset += width/w); an
// over-wide line that the terminal wraps throws that estimate off and drifts the
// pinned input box off-screen. Lines already within width are left byte-for-byte
// untouched (chunkByWidth preserves content and ANSI), so rendered tables and the
// wrapped answer — which the markdown renderer already fit to width — are safe;
// only stray long lines (tool-dispatch args, unwrapped code) get broken.
func clampWidth(s string, width int) string {
	if width <= 0 {
		return s
	}
	// ansi.Hardwrap breaks any line over `width` visible cols on grapheme
	// boundaries, preserving ANSI and counting wide chars — exactly what we want,
	// and lines already within width pass through unchanged.
	return ansi.Hardwrap(s, width, false)
}

// commitLine queues one finalized block for the next scrollback flush.
func (m *chatTUI) commitLine(s string) {
	*m.pendingCommit = append(*m.pendingCommit, s)
}

// commitReasoning freezes the accumulated thinking stream (verbatim, already
// dim) into scrollback and clears the live buffer.
func (m *chatTUI) commitReasoning() {
	if m.reasoning.Len() == 0 {
		return
	}
	// Wrap to the viewport width before committing. bubbletea's non-alt-screen
	// Println adds an erase-to-end only for message lines *narrower* than the
	// terminal and never truncates them, so an over-wide reasoning line wraps
	// and its short final row leaves the old input-box border (the live region
	// it printed over) bleeding through on the right — the "ghost ────". The
	// rendered answer is already wrapped, which is why only reasoning stranded.
	// Wrap each over-long line; keep short ones (the "▎ thinking" header)
	// verbatim so their indent survives.
	raw := strings.TrimRight(m.reasoning.String(), "\n")
	var b strings.Builder
	for i, line := range strings.Split(raw, "\n") {
		if i > 0 {
			b.WriteByte('\n')
		}
		if m.width > 0 && visibleWidth(line) > m.width {
			b.WriteString(wrapAnsi(line, m.width))
		} else {
			b.WriteString(line) // width unknown (pre-sizing) or already fits: verbatim
		}
	}
	m.commitLine(b.String())
	m.reasoning.Reset()
}

// commitPending renders the accumulated answer as markdown and freezes it into
// scrollback. Joining commitReasoning then commitPending puts the answer on its
// own line, restoring the thinking→answer break the renderer strips.
func (m *chatTUI) commitPending() {
	if m.pending.Len() == 0 {
		return
	}
	raw := m.pending.String()
	rendered := m.renderer.Render(raw)
	if rendered == "" {
		rendered = raw
	}
	m.commitLine(strings.TrimRight(rendered, "\n"))
	m.pending.Reset()
}

// planApprovalTool is the Tool name the controller puts on the ApprovalRequest it
// emits to gate a plan (mirrors control's constant). The banner, status line, and
// approval handler key on it to render the plan-specific prompt and to keep the
// [plan] tag in sync when the plan is approved.
const planApprovalTool = "exit_plan_mode"

// handleApprovalKey resolves a pending approval from a keystroke and re-arms the
// listener. y/Enter allows once, a allows for the rest of the session, n/Esc
// denies. Ctrl-C cancels the whole turn via the run context. For a plan approval
// (planApprovalTool), allowing also drops the local [plan] tag — the controller
// turns plan mode off on its side.
func (m chatTUI) handleApprovalKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	answer := func(allow, session bool) (tea.Model, tea.Cmd) {
		if allow && m.pendingApproval.Tool == planApprovalTool {
			m.planMode = false
		}
		m.ctrl.Approve(m.pendingApproval.ID, allow, session)
		m.pendingApproval = nil
		return m, nil // the next ApprovalRequest / event arrives on eventCh
	}
	switch msg.String() {
	case "ctrl+c":
		m.ctrl.Cancel() // cancels the run; the approver unblocks via ctx.Done()
		return answer(false, false)
	case "enter":
		return answer(true, false)
	case "esc":
		return answer(false, false)
	}
	switch strings.ToLower(msg.String()) {
	case "y":
		return answer(true, false)
	case "a":
		return answer(true, true)
	case "n":
		return answer(false, false)
	}
	return m, nil // ignore anything else while awaiting a decision
}

var (
	// Input box: only top + bottom borders, no sides.
	inputBoxStyle = lipgloss.NewStyle().
			Border(lipgloss.NormalBorder(), true, false, true, false).
			BorderForeground(lipgloss.Color("173")).
			PaddingLeft(1)

	// Approval banner: same frame as the input box, recoloured yellow.
	approvalBannerStyle = lipgloss.NewStyle().
				Border(lipgloss.NormalBorder(), true, false, true, false).
				BorderForeground(lipgloss.Color("220")).
				Foreground(lipgloss.Color("220")).
				Bold(true).
				PaddingLeft(1)

	// Task panel: a top-bordered block pinned above the input.
	todoPanelStyle = lipgloss.NewStyle().
			Border(lipgloss.NormalBorder(), true, false, false, false).
			BorderForeground(lipgloss.Color("240")).
			PaddingLeft(1)

	statusStyle = lipgloss.NewStyle().Faint(true)
)

func (m chatTUI) View() tea.View {
	boxW := m.width
	if boxW < 10 {
		boxW = 10
	}
	box := inputBoxStyle.Width(boxW).Render(m.input.View())

	var modeTag string
	switch {
	case m.ctrl.Bypass():
		modeTag = lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Bold(true).Render("[YOLO]")
	case m.planMode:
		modeTag = yellow("[plan]")
	default:
		modeTag = dim("[auto]")
	}

	ctxTag := m.contextTag()
	var status string
	switch {
	case m.rewind != nil:
		status = "  " + modeTag + " · ⟲ rewind"
	case m.chooser != nil:
		status = "  " + modeTag + " · " + i18n.M.ChatStatusQuestion
	case m.pendingApproval != nil && m.pendingApproval.Tool == planApprovalTool:
		status = "  " + modeTag + " · " + i18n.M.ChatStatusPlanApproval
	case m.pendingApproval != nil:
		status = "  " + modeTag + " · " + i18n.M.ChatStatusToolApproval
	case m.state == tuiRunning:
		status = fmt.Sprintf("  %s · "+i18n.M.ChatStatusThinkingFmt, modeTag, m.spinner.View(), m.elapsed)
		if m.turnTokens > 0 {
			status += " · ↓" + shortTokens(m.turnTokens)
		}
	default:
		status = "  " + modeTag + " · " + i18n.M.ChatStatusIdle
	}
	// Second status row: the live data (context gauge, cache rates, jobs,
	// balance). It lives on its own fixed row so it's always shown in full rather
	// than being truncated off the end of the keybinding hints on line 1. Two
	// rows is a fixed height, so unlike a wrap-when-long status it doesn't
	// reintroduce resize ghosting.
	var data []string
	if ctxTag != "" {
		data = append(data, ctxTag)
	}
	if cache := m.cacheTag(); cache != "" {
		data = append(data, cache)
	}
	if jt := m.jobsTag(); jt != "" {
		data = append(data, jt)
	}
	if m.balance != "" {
		data = append(data, dim(m.balance))
	}
	dataLine := "  " + strings.Join(data, " · ")

	// The bottom region must stay a stable height: bubbletea's non-alt-screen
	// renderer commits scrollback via tea.Println by clearing the previous
	// frame's lines, so a frame whose height changed every streamed token (a
	// growing live preview) drifts and strands input-box border lines in the
	// history. So we don't preview the streaming text here — it lands in
	// scrollback at boundaries (tool lines stream live; reasoning and the
	// rendered answer commit at their edges). The menu/banner change height only
	// on discrete user actions, never mid-stream.
	var parts []string
	rowsAboveBox := 0 // terminal rows occupied by todo/banner/menu before the input box
	// The task list is pinned above the input, updating in place. Its height
	// changes only on a todo_write event (a handful per turn),
	// not per streamed token, so it doesn't thrash the scrollback the way a live
	// text preview would.
	if todo := m.renderTodoPanel(); todo != "" {
		parts = append(parts, todo)
		rowsAboveBox += strings.Count(todo, "\n") + 1
	}
	if banner := m.renderApprovalBanner(); banner != "" {
		parts = append(parts, banner)
		rowsAboveBox += strings.Count(banner, "\n") + 1
	}
	if card := m.renderChooser(); card != "" {
		parts = append(parts, card)
		rowsAboveBox += strings.Count(card, "\n") + 1
	}
	if card := m.renderRewind(); card != "" {
		parts = append(parts, card)
		rowsAboveBox += strings.Count(card, "\n") + 1
	}
	if menu := m.renderCompletion(); menu != "" {
		parts = append(parts, menu)
		rowsAboveBox += strings.Count(menu, "\n") + 1
	}
	if m.repaintToggle {
		// Zero-width space at the front (survives clampStatusLine, which only trims
		// the tail) so a post-resize repaint produces a byte-different View and
		// isn't skipped by the renderer. Invisible: width 0, no glyph.
		dataLine = "​" + dataLine
	}
	// Fixed two-row status: line 1 = mode + keybinding/state hints, line 2 = live
	// data. Each row is clamped to width independently so neither wraps.
	statusBlock := clampStatusLine(status, boxW) + "\n" + clampStatusLine(dataLine, boxW)
	parts = append(parts, box, statusStyle.Render(statusBlock))

	v := tea.NewView(strings.Join(parts, "\n"))
	// Anchor the real terminal cursor at the textarea's insertion point so IME
	// candidate windows appear in the input box, not at the bottom of the frame.
	// input.Cursor() is relative to the textarea; offset it by the box's screen
	// position (rows above + the box's top border row; +1 column for PaddingLeft).
	if cur := m.input.Cursor(); cur != nil {
		cur.X += 1
		cur.Y += rowsAboveBox + 1
		v.Cursor = cur
	}
	return v
}

// compactionCardLines renders a finished compaction as a titled card: a header
// with the message count and trigger, then the structured summary under a dim
// gutter so it reads as one block in scrollback. The summary is also the new
// context base, so this card is the user's window into exactly what was kept.
func compactionCardLines(c event.Compaction) []string {
	trigger := c.Trigger
	switch c.Trigger {
	case "auto":
		trigger = i18n.M.CompactionAuto
	case "manual":
		trigger = i18n.M.CompactionManual
	}
	header := fmt.Sprintf("%s · %d %s · %s", i18n.M.CompactionTitle, c.Messages, i18n.M.CompactionUnit, trigger)
	lines := []string{accent("◆ " + header)}
	for _, ln := range strings.Split(strings.TrimRight(c.Summary, "\n"), "\n") {
		lines = append(lines, dim("  │ "+ln))
	}
	if c.Archive != "" {
		lines = append(lines, dim("  │ archived "+c.Archive))
	}
	return lines
}

// contextTag renders the prompt-vs-context-window gauge for the status line,
// framed around the auto-compaction threshold: it shows how much headroom is
// left until the next compaction, and colours by proximity to that point rather
// than the raw window. Falls back to a plain percentage when compaction is disabled.
func (m chatTUI) contextTag() string {
	used, window := m.ctrl.ContextSnapshot()
	if used == 0 || window == 0 {
		return ""
	}
	pct := used * 100 / window
	ratio := m.ctrl.CompactRatio()
	if ratio <= 0 || ratio >= 1 {
		// Compaction disabled: just the raw gauge, coloured on window fill.
		body := fmt.Sprintf("%s / %s ctx (%d%%)", shortTokens(used), shortTokens(window), pct)
		switch {
		case pct >= 85:
			return lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Render(body)
		case pct >= 60:
			return lipgloss.NewStyle().Foreground(lipgloss.Color("220")).Render(body)
		default:
			return dim(body)
		}
	}
	threshold := int(ratio * 100)
	// Headroom to the compaction point, as a percentage of the window (clamped at 0).
	left := threshold - pct
	if left < 0 {
		left = 0
	}
	body := fmt.Sprintf("%s ctx (%d%%) · %d%% to compact", shortTokens(used), pct, left)
	switch {
	case pct >= threshold:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Render(fmt.Sprintf("%s ctx (%d%%) · compacting soon", shortTokens(used), pct))
	case left <= 10:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("220")).Render(body)
	default:
		return dim(body)
	}
}

// cacheTag renders both prompt cache-hit rates for the status line —
// "cache 88% · avg 78%": the single-turn rate (latest turn, the higher/steeper
// number on a non-compacting DeepSeek session) and the session-aggregate rate
// Σhit/Σ(hit+miss) (the steadier, cost-oriented number that matches the legacy
// dashboard). "" before any cache tokens have been reported.
func (m chatTUI) cacheTag() string {
	now := ""
	if u := m.ctrl.LastUsage(); u != nil {
		d := u.CacheHitTokens + u.CacheMissTokens
		if d == 0 {
			d = u.PromptTokens
		}
		if d > 0 {
			now = fmt.Sprintf("cache %d%%", u.CacheHitTokens*100/d)
		}
	}
	avg := ""
	if hit, miss := m.ctrl.SessionCache(); hit+miss > 0 {
		avg = fmt.Sprintf("avg %d%%", hit*100/(hit+miss))
	}
	switch {
	case now != "" && avg != "":
		return dim(now + " · " + avg)
	case now != "":
		return dim(now)
	case avg != "":
		return dim(avg)
	}
	return ""
}

// jobsTag shows the count of running background jobs in the status line. Job
// start/finish emit Notices that arrive on eventCh and re-render the frame, so
// the count stays current without a dedicated tick.
func (m chatTUI) jobsTag() string {
	n := len(m.ctrl.Jobs())
	if n == 0 {
		return ""
	}
	return dim(fmt.Sprintf("⚙ %d", n))
}

// shortTokens prints token counts compactly: 142_000 → "142K", 1_000_000 → "1M".
func shortTokens(n int) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	case n >= 1_000:
		return fmt.Sprintf("%dK", n/1_000)
	default:
		return fmt.Sprintf("%d", n)
	}
}

// renderApprovalBanner is the slim notice shown above the input while a tool
// call (or a plan) awaits the user's decision.
func (m chatTUI) renderApprovalBanner() string {
	w := m.width
	if w < 10 {
		w = 10
	}
	if m.pendingApproval == nil {
		return ""
	}
	// A plan approval shows the gate prompt (the plan itself is already printed as
	// the assistant's reply); a tool approval names the tool + subject.
	if m.pendingApproval.Tool == planApprovalTool {
		return approvalBannerStyle.Width(w).Render("⏸ " + i18n.M.PlanApprovalPrompt)
	}
	subj := strings.TrimSpace(m.pendingApproval.Subject)
	if subj != "" {
		subj = " " + truncateSubject(subj, w)
	}
	text := fmt.Sprintf(i18n.M.ToolApprovalPromptFmt, m.pendingApproval.Tool, subj)
	return approvalBannerStyle.Width(w).Render("⏸ " + text)
}

// todoPanelMaxRows caps how many task lines the pinned panel shows; a long list
// is truncated with a "+N more" footer so the bottom region stays compact.
const todoPanelMaxRows = 8

// renderTodoPanel renders the task list pinned above the input from the latest
// todo_write call (m.todoArgs): a "Tasks done/total" header, completed items
// dimmed/checked, the in-progress one highlighted (its activeForm if given),
// pending ones muted. It returns "" when there's no list or every item is done,
// so the panel appears while work is outstanding and clears itself when finished.
func (m chatTUI) renderTodoPanel() string {
	var p struct {
		Todos []struct {
			Content    string `json:"content"`
			Status     string `json:"status"`
			ActiveForm string `json:"activeForm"`
		} `json:"todos"`
	}
	if err := json.Unmarshal([]byte(m.todoArgs), &p); err != nil || len(p.Todos) == 0 {
		return ""
	}
	done := 0
	for _, t := range p.Todos {
		if t.Status == "completed" {
			done++
		}
	}
	if done == len(p.Todos) {
		return "" // all finished — clear the panel
	}

	var b strings.Builder
	fmt.Fprintf(&b, "%s %s\n", accent("To-dos"), dim(fmt.Sprintf("%d/%d", done, len(p.Todos))))
	shown := 0
	for _, t := range p.Todos {
		if shown >= todoPanelMaxRows {
			b.WriteString(dim(fmt.Sprintf("  +%d more", len(p.Todos)-shown)) + "\n")
			break
		}
		shown++
		switch t.Status {
		case "completed":
			b.WriteString("  " + green("✔") + " " + dim(t.Content) + "\n")
		case "in_progress":
			label := t.Content
			if t.ActiveForm != "" {
				label = t.ActiveForm
			}
			b.WriteString("  " + yellow("▶ "+label) + "\n")
		default:
			b.WriteString("  " + dim("○ "+t.Content) + "\n")
		}
	}
	return todoPanelStyle.Width(max(m.width, 10)).Render(strings.TrimRight(b.String(), "\n"))
}

// truncateSubject trims a tool subject so the approval banner fits one line.
func truncateSubject(s string, width int) string {
	max := width - 28
	if max < 16 {
		max = 16
	}
	r := []rune(s)
	if len(r) > max {
		return string(r[:max]) + "…"
	}
	return s
}

// clampStatusLine truncates a status line to `width` visible columns, ANSI-aware,
// appending an ellipsis and a reset. The bottom region must stay a fixed height —
// the non-alt-screen renderer commits scrollback by clearing the prior frame's
// lines, so a status that wraps to a second row strands input-box borders in
// history. Truncating (not wrapping) keeps it one row regardless of how many tags
// (ctx · cache · avg · jobs · balance) it carries on a narrow terminal.
func clampStatusLine(s string, width int) string {
	// ansi.Truncate is ANSI-aware, counts wide chars, and appends the tail when
	// it actually clips — one row regardless of how many tags the status carries.
	return ansi.Truncate(s, width, "…")
}

// growInputToFit resizes the textarea to the number of lines its value spans,
// capped at maxInputRows so a long paste doesn't crowd the screen.
const maxInputRows = 5

func (m *chatTUI) growInputToFit() {
	lines := strings.Count(m.input.Value(), "\n") + 1
	if lines < 1 {
		lines = 1
	}
	if lines > maxInputRows {
		lines = maxInputRows
	}
	if lines != m.input.Height() {
		m.input.SetHeight(lines)
	}
}

// cycleMode advances the input mode normal → plan → YOLO → normal (Tab),
// mirroring the desktop composer's Shift+Tab. plan is read-only; YOLO
// auto-approves every tool call for the session (deny rules still apply). The
// status line's mode tag ([auto]/[plan]/[YOLO]) reflects the result.
func (m *chatTUI) cycleMode() {
	switch {
	case m.ctrl.Bypass():
		m.ctrl.SetBypass(false) // YOLO → normal
	case m.planMode:
		m.planMode = false
		m.ctrl.SetPlanMode(false)
		m.ctrl.SetBypass(true) // plan → YOLO
	default:
		m.planMode = true
		m.ctrl.SetPlanMode(true) // normal → plan
	}
}

// startTurn commits the user bubble to scrollback, resets the turn accumulator,
// and kicks off runner.Run. `sent` goes to the model (may carry a plan-mode
// marker); `displayed` is what the transcript shows.
func (m *chatTUI) startTurn(sent, displayed string) tea.Cmd {
	// Flush any half-streamed leftover before the new turn (defensive).
	m.commitReasoning()
	m.commitPending()

	// Defer the user bubble until the first response packet (commitPendingBubble):
	// pressing Esc before the server replies un-sends the message, restoring its
	// text to the input box with nothing stranded in scrollback.
	m.pendingBubble = displayed
	m.bubblePending = true
	m.turnDiscarded = false

	m.state = tuiRunning
	m.runStart = time.Now()
	m.elapsed = 0
	m.turnTokens = 0
	// The controller owns the run goroutine, its context, and cancellation; it
	// streams events to eventCh and emits TurnDone when the turn settles.
	m.ctrl.Send(sent)
	return tea.Batch(m.spinner.Tick, elapsedTick())
}

// commitPendingBubble flushes the deferred user bubble into scrollback — a blank
// separator then the bubble. Called when a turn's first response packet arrives
// (the message is now really sent) and, defensively, at turn end if it wasn't
// un-sent. A no-op once committed.
func (m *chatTUI) commitPendingBubble() {
	if !m.bubblePending {
		return
	}
	m.bubblePending = false
	m.commitLine("") // blank line separating turns
	m.commitLine(renderUserBubble(m.pendingBubble, m.width, m.planMode))
	m.pendingBubble = ""
}

// unsendPending "un-sends" the in-flight turn while the server hasn't replied yet
// (bubblePending): it restores the just-sent text to the input box, drops the
// deferred bubble, and cancels the request — marking the turn discarded so its
// already-buffered events reach nothing. Once a packet has arrived the bubble is
// committed and this path isn't taken (Esc cancels normally instead).
func (m *chatTUI) unsendPending() {
	m.input.SetValue(m.pendingBubble)
	m.growInputToFit()
	m.bubblePending = false
	m.pendingBubble = ""
	m.turnDiscarded = true
	m.ctrl.Cancel()
}

// ingestEvent routes one typed event from the agent. Reasoning (dim) and answer
// free-text accumulate in their live buffers; every other event first finalizes
// the reasoning and answer streamed so far, then commits its own line —
// preserving order. Switching on the event Kind replaces the old prefix-sniffing
// of a flattened byte stream: the structure is now explicit.
func (m *chatTUI) ingestEvent(e event.Event) {
	if m.turnDiscarded {
		// The turn was un-sent (Esc before any packet); swallow whatever was already
		// buffered for it until it settles, so nothing lands in scrollback.
		if e.Kind == event.TurnDone {
			m.turnDiscarded = false
			m.state = tuiIdle
		}
		return
	}
	// The first packet of any kind means the server replied — commit the deferred
	// user bubble before rendering it. TurnStarted is local (emitted before the
	// request) and TurnDone is handled in its own case, so neither triggers it.
	if e.Kind != event.TurnStarted && e.Kind != event.TurnDone {
		m.commitPendingBubble()
	}
	switch e.Kind {
	case event.Reasoning:
		if m.reasoning.Len() == 0 {
			m.reasoning.WriteString(dim("  ▎ thinking") + "\n")
		}
		m.reasoning.WriteString(dim(e.Text))

	case event.Text:
		m.commitReasoning() // reasoning ends as the answer begins
		m.pending.WriteString(e.Text)

	case event.Message:
		// The answer stream is complete — freeze reasoning + the markdown answer.
		m.commitReasoning()
		m.commitPending()

	case event.ToolDispatch:
		// The early (partial) dispatch only carries the name — the full dispatch
		// with args prints the line. The running spinner covers the gap meanwhile.
		if e.Tool.Partial {
			break
		}
		m.finalizeStreamed()
		switch e.Tool.Name {
		case "todo_write":
			// Drive the pinned task list above the input (renderTodoPanel) rather
			// than printing a tool line; it updates in place as the list evolves.
			m.todoArgs = e.Tool.Args
		case planApprovalTool:
			// No longer a tool, but guard anyway: the plan is the assistant's reply.
		default:
			m.commitLine(fmt.Sprintf("  -> %s %s", e.Tool.Name, compactArgs(e.Tool.Args)))
		}

	case event.ToolResult:
		// A successful result is silent (it only feeds the model); a blocked call
		// surfaces a "⊘ name <reason>" line.
		if e.Tool.Err != "" {
			m.finalizeStreamed()
			m.commitLine(fmt.Sprintf("  ⊘ %s %s", e.Tool.Name, e.Tool.Err))
		}

	case event.Usage:
		if e.Usage != nil {
			m.turnTokens += e.Usage.CompletionTokens
		}
		if line := agent.FormatUsageLine(e.Usage, e.Pricing); line != "" {
			m.finalizeStreamed()
			m.commitLine(line)
		}

	case event.Notice:
		glyph := "·"
		if e.Level == event.LevelWarn {
			glyph = "!"
		}
		m.finalizeStreamed()
		m.commitLine(fmt.Sprintf("  %s %s", glyph, e.Text))

	case event.CompactionStarted:
		m.finalizeStreamed()
		m.commitLine(dim("  ⋯ " + i18n.M.CompactionWorking))

	case event.CompactionDone:
		// An aborted pass carries no summary; the accompanying Notice (auto) or
		// compactDoneMsg error (manual) explains why, so don't draw an empty card.
		if e.Compaction.Summary == "" {
			break
		}
		m.finalizeStreamed()
		for _, ln := range compactionCardLines(e.Compaction) {
			m.commitLine(ln)
		}

	case event.Phase:
		m.finalizeStreamed()
		m.commitLine(fmt.Sprintf("[%s]", e.Text))

	case event.ApprovalRequest:
		// The controller's run goroutine is now blocked inside the gate awaiting
		// this decision; the banner shows it in View and key input answers it via
		// ctrl.Approve. At most one prompt is outstanding (the controller
		// serialises them), so a plain field holds the current one.
		a := e.Approval
		m.pendingApproval = &a

	case event.AskRequest:
		// The `ask` tool raised a question card; the run goroutine blocks until
		// ctrl.AnswerQuestion resolves it. Keys drive the card while it's set.
		m.finalizeStreamed()
		m.chooser = newChooser(e.Ask)

	case event.TurnDone:
		// The turn settled — freeze anything still streaming, autosave, surface a
		// real error, and gate a plan-mode proposal on the user's approval.
		m.commitReasoning()
		m.commitPending()
		// If the bubble is still deferred at turn end, the message was sent but
		// produced nothing visible (an error before any reply, or an empty turn):
		// commit it so the user sees what they sent — unless this is a user cancel,
		// where it was already un-sent (handled above) or should leave no trace.
		if e.Err == nil || !strings.Contains(e.Err.Error(), "context canceled") {
			m.commitPendingBubble()
		} else {
			m.bubblePending = false
			m.pendingBubble = ""
		}
		m.state = tuiIdle
		_ = m.ctrl.Snapshot() // best-effort; never the user's problem mid-chat
		if e.Err != nil && e.Err.Error() != "" && !strings.Contains(e.Err.Error(), "context canceled") {
			m.commitLine(wrapForViewport(i18n.M.ErrorPrefix+" "+e.Err.Error(), m.width, lipgloss.Color("3")))
		}
		// Plan-mode approval is now driven by the controller (it emits an
		// ApprovalRequest when a plan-mode turn produces a proposal), so there's
		// nothing to detect here.
	}
}

// finalizeStreamed freezes any in-progress reasoning + answer into scrollback so
// a following event line lands after them, preserving chronological order.
func (m *chatTUI) finalizeStreamed() {
	m.commitReasoning()
	m.commitPending()
}

func waitForAgentEvent(ch chan event.Event) tea.Cmd {
	return func() tea.Msg { return agentEventMsg(<-ch) }
}

func elapsedTick() tea.Cmd {
	return tea.Tick(time.Second, func(_ time.Time) tea.Msg { return elapsedTickMsg{} })
}

// runSlashCommand handles "/<cmd> <args>" input. Local commands queue their
// output to scrollback; MCP prompt / custom commands resolve to a model turn.
func (m *chatTUI) runSlashCommand(input string) tea.Cmd {
	cmd := strings.TrimSpace(strings.SplitN(input, " ", 2)[0])

	if strings.HasPrefix(cmd, "/mcp__") {
		return m.runMCPPrompt(input)
	}

	switch cmd {
	case "/compact":
		// Compaction makes a (network) summarizer call; run it off the Update loop
		// so the TUI doesn't freeze. The CompactionStarted/Done events render the
		// card as they arrive; compactDoneMsg only handles the terminal error /
		// snapshot once the pass returns. Any text after "/compact" is focus
		// guidance steering what the summary keeps.
		focus := strings.TrimSpace(strings.TrimPrefix(input, cmd))
		return func() tea.Msg { return compactDoneMsg{err: m.ctrl.Compact(context.Background(), focus)} }
	case "/new":
		if err := m.ctrl.NewSession(); err != nil {
			m.notice(fmt.Sprintf("%s: %v", i18n.M.SlashNewFailed, err))
			return nil
		}
		// Native scrollback keeps the old transcript; mark the fork with a fresh
		// banner and reset live state.
		m.pending.Reset()
		m.reasoning.Reset()
		m.todoArgs = ""
		m.chooser = nil
		m.commitLine("")
		m.commitLine(strings.TrimRight(renderTUIBanner(m.label, "", m.width), "\n"))
		m.notice(i18n.M.SlashNewDone)
	case "/todo":
		// Dismiss the pinned task list; a later todo_write brings it back.
		m.todoArgs = ""
		m.notice(i18n.M.SlashTodoCleared)
	case "/rewind":
		m.openRewind()
	case "/mcp":
		m.runMCPSubcommand(input)
	case "/model":
		m.runModelSubcommand(input)
		if m.pendingModelSwitch != nil {
			return m.pendingModelSwitch
		}
	case "/skill", "/skills":
		m.runSkillSubcommand(input)
	case "/hooks":
		m.runHooksSubcommand(input)
	case "/output-style", "/output-styles":
		styles := outputstyle.List(outputstyle.Dirs())
		if len(styles) == 0 {
			m.notice(i18n.M.OutputStyleNone)
		} else {
			m.notice(i18n.M.OutputStyleHeader + "\n" + outputstyle.DescribeList(styles, m.outputStyle) + "\n" + i18n.M.OutputStyleHint)
		}
	case "/help":
		m.notice(i18n.M.SlashHelp)
		if names := m.commandNames(); names != "" {
			m.notice("custom: " + names)
		}
	case "/memory":
		m.showMemory()
	default:
		// A custom command wins over a skill of the same name; both resolve to a turn.
		if sent, ok := m.ctrl.CustomCommand(input); ok {
			return m.startTurn(m.ctrl.Compose(sent), input)
		}
		if sent, ok := m.ctrl.RunSkill(input); ok {
			return m.startTurn(m.ctrl.Compose(sent), input)
		}
		m.notice(fmt.Sprintf("%s: %s", i18n.M.SlashUnknown, cmd))
	}
	return nil
}

// commandNames renders the custom command list for /help, "" when there are none.
func (m *chatTUI) commandNames() string {
	if len(m.commands) == 0 {
		return ""
	}
	names := make([]string, len(m.commands))
	for i, c := range m.commands {
		names[i] = "/" + c.Name
	}
	return strings.Join(names, " · ")
}

// runMCPSubcommand handles "/mcp" (status), "/mcp add …" (connect a server live
// and persist it), and "/mcp remove <name>" (disconnect + drop from config). Add
// connects synchronously — like /compact, an explicit command may briefly block
// the UI while the handshake runs.
func (m *chatTUI) runMCPSubcommand(input string) {
	args := tokenizeArgs(input) // args[0] == "/mcp"
	if len(args) < 2 {
		m.showMCPStatus()
		return
	}
	switch args[1] {
	case "list", "ls":
		// The completion menu offers "list"; treat it as the status view (same as
		// a bare /mcp) rather than an unknown subcommand.
		m.showMCPStatus()
	case "add":
		entry, err := parseMCPAdd(args[2:])
		if err != nil {
			m.notice(err.Error())
			return
		}
		n, err := m.ctrl.AddMCPServer(entry)
		if err != nil {
			m.notice("mcp add: " + err.Error())
			return
		}
		m.notice(fmt.Sprintf("connected %s — %d tools, saved to config (available next message)", entry.Name, n))
	case "remove", "rm":
		if len(args) < 3 {
			m.notice("usage: /mcp remove <name>")
			return
		}
		name := args[2]
		disconnected, err := m.ctrl.RemoveMCPServer(name)
		if err != nil {
			m.notice("mcp remove: " + err.Error())
			return
		}
		if disconnected {
			m.notice("disconnected " + name + " and removed it from config")
		} else {
			m.notice("removed " + name + " from config")
		}
	default:
		m.notice("unknown /mcp subcommand " + args[1] + " — try: /mcp, /mcp add, /mcp remove")
	}
}

// showMCPStatus queues the connected MCP servers, their counts, and the prompt
// commands / resource refs they expose — the discovery surface for /mcp.
func (m *chatTUI) showMCPStatus() {
	if m.host == nil || len(m.host.Servers()) == 0 {
		m.notice(i18n.M.SlashMCPNone)
		return
	}
	servers := m.host.Servers()
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n", dim(fmt.Sprintf("  · MCP servers (%d)", len(servers))))
	for _, s := range servers {
		fmt.Fprintf(&b, "    %s %s %s\n", accent("✓"), bold(s.Name),
			dim(fmt.Sprintf("(%s) — %d tools · %d prompts · %d resources", s.Transport, s.Tools, s.Prompts, s.Resources)))
	}
	for _, p := range m.host.Prompts() {
		fmt.Fprintf(&b, "      %s  %s\n", "/"+p.Name, dim(p.Description))
	}
	for _, r := range m.host.Resources() {
		label := r.Name
		if label == "" {
			label = r.Description
		}
		fmt.Fprintf(&b, "      %s  %s\n", "@"+r.Server+":"+r.URI, dim(label))
	}
	m.commitLine(strings.TrimRight(b.String(), "\n"))
}

// notice queues a dim informational line to scrollback.
func (m *chatTUI) notice(note string) {
	m.commitLine(dim("  · " + note))
}

// resolveRefs resolves a line's @references off the event loop via the
// controller, delivering a refsResolvedMsg with the tagged context block.
func (m *chatTUI) resolveRefs(line string) tea.Cmd {
	return func() tea.Msg {
		block, errs := m.ctrl.ResolveRefs(context.Background(), line)
		return refsResolvedMsg{line: line, block: block, errs: errs}
	}
}

// runMCPPrompt resolves a /mcp__server__prompt command off the event loop via
// the controller, delivering a promptResolvedMsg with the rendered prompt.
func (m *chatTUI) runMCPPrompt(input string) tea.Cmd {
	return func() tea.Msg {
		sent, found, err := m.ctrl.MCPPrompt(context.Background(), input)
		if !found {
			name := strings.TrimPrefix(strings.Fields(input)[0], "/")
			return promptResolvedMsg{display: input, err: fmt.Errorf("%s: /%s", i18n.M.SlashUnknown, name)}
		}
		return promptResolvedMsg{display: input, sent: sent, err: err}
	}
}

// replaySectionsFor turns a loaded session into scrollback blocks: user bubbles
// and assistant markdown. Tool messages are dropped — needed in session state
// but noise in the visible transcript on resume.
func replaySectionsFor(history []provider.Message, width int, renderer *mdRenderer) []string {
	var out []string
	for _, m := range history {
		switch m.Role {
		case provider.RoleUser:
			content := strings.TrimPrefix(m.Content, control.PlanModeMarker+"\n\n")
			out = append(out, renderUserBubble(content, width, false)+"\n\n")
		case provider.RoleAssistant:
			body := strings.TrimSpace(m.Content)
			if body == "" {
				continue
			}
			rendered := renderer.Render(body)
			if rendered == "" {
				rendered = body
			}
			out = append(out, rendered+"\n")
		}
	}
	return out
}

// renderTUIBanner is the title + tip + optional missing-key warning printed once
// at the top of the session.
func renderTUIBanner(label, missing string, width int) string {
	var b strings.Builder
	b.WriteString(accent("◆") + " " + bold("reasonix chat") + "  " + dim("· "+label) + "\n")
	b.WriteString(dim("  "+i18n.M.ChatTip) + "\n")
	if missing != "" {
		b.WriteString(wrapForViewport("  ! "+missing, width, lipgloss.Color("3")) + "\n")
	}
	return b.String()
}

// wrapForViewport hard-wraps text to fit width columns and colours every line.
func wrapForViewport(text string, width int, fg color.Color) string {
	if width <= 0 {
		width = 80
	}
	return lipgloss.NewStyle().
		Foreground(fg).
		Width(width).
		Render(text)
}

// renderUserBubble styles the just-submitted line with a filled dim background.
func renderUserBubble(line string, width int, planMode bool) string {
	prefix := "› "
	if planMode {
		prefix = "› [plan] "
	}
	if !colorEnabled {
		return "│ " + prefix + line
	}
	w := width - 4
	if w < 10 {
		w = 10
	}
	bubble := lipgloss.NewStyle().
		Background(lipgloss.Color("236")).
		Width(w).
		Padding(0, 1)
	return bubble.Render(prefix + line)
}

// eventSink is the event.Sink the agent emits to in TUI mode. Each event
// becomes an agentEventMsg. The channel is generously buffered so streaming
// bursts don't back-pressure the agent goroutine.
type eventSink struct {
	ch chan<- event.Event
}

func (s *eventSink) Emit(e event.Event) { s.ch <- e }

// compactArgs delegates to agent.CompactArgs so the CLI and headless rendering
// stay identical.
func compactArgs(s string) string { return agent.CompactArgs(s) }
