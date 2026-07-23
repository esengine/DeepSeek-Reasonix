package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"reasonix/internal/agent"
	"reasonix/internal/command"
	"reasonix/internal/config"
	"reasonix/internal/control"
	"reasonix/internal/event"
	"reasonix/internal/hook"
	"reasonix/internal/i18n"
	"reasonix/internal/memory"
	"reasonix/internal/migration"
	"reasonix/internal/outputstyle"
	"reasonix/internal/permission"
	"reasonix/internal/plugin"
	"reasonix/internal/provider"
	"reasonix/internal/recovery"
	"reasonix/internal/sandbox"
	"reasonix/internal/skill"
	"reasonix/internal/tool"
)

// chatTUI is a bubbletea Model that normally owns the terminal with an
// alt-screen transcript viewport. Termux is the exception: it stays in the
// normal buffer and commits finalized output to native scrollback via
// tea.Println so taps can still focus the soft keyboard.
type chatTUI struct {
	ctrl    control.SessionAPI
	label   string
	missing string // missing-key warning surfaced once in the banner, "" when ready

	width  int
	height int
	// nativeScrollback keeps Termux out of alt-screen mode so taps still focus
	// the textarea and raise the soft keyboard.
	nativeScrollback bool
	// mouseCaptureOff releases mouse ownership back to the terminal (View() sets
	// tea.MouseModeNone instead of MouseModeCellMotion) so its native
	// click-drag selection and right-click context menu work again. Toggled by
	// "/mouse" or REASONIX_DISABLE_MOUSE at startup; trades away in-app
	// drag-select, the transcript scrollbar, and wheel-scroll while it's on,
	// since the terminal no longer forwards those events to Reasonix.
	mouseCaptureOff bool

	input       textarea.Model
	composerSel composerSelection
	composerMap composerLayoutCache
	// composerScrollOffset is an independent view offset used after the user
	// wheels inside an overflowing composer. The textarea keeps ownership of the
	// real insertion cursor; a subsequent edit or cursor key reattaches the view
	// to that cursor without the wheel having moved it.
	composerScrollOffset   int
	composerScrollDetached bool
	spinner                spinner.Model

	submittedInputs      []string
	submittedInputCursor int
	submittedInputDraft  string
	pastedBlocks         []pastedBlock
	nextPasteID          int

	state    tuiState
	runStart time.Time
	elapsed  int
	// retryAttempt/retryMax drive the transient "retrying (n/m)" indicator while
	// the provider re-attempts the connection; cleared by the next stream event.
	retryAttempt int
	retryMax     int
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

	// planMode mirrors the agent's plan-first workflow (Shift+Tab toggles it). The
	// marker rides in outgoing user messages so the cache-stable prompt prefix is
	// left untouched.
	planMode bool
	// sessionSwitch is set by replayActiveBranch to suppress the ClearScreen
	// flicker when the viewport content is completely rebuilt during a session
	// switch (#5441). Cleared after one Update cycle.
	sessionSwitch bool
	// yoloRestoreToolApprovalMode remembers the Ask/Auto base mode that Ctrl+Y
	// should restore after a desktop-style YOLO toggle.
	yoloRestoreToolApprovalMode string

	// pendingInterject queues input typed while a turn runs; each TurnDone
	// dequeues the front and submits it as the next turn.
	pendingInterject []string
	// queueEditCursor tracks which queued message the user is currently
	// browsing/editing via ↑/↓ during tuiRunning. -1 means "not browsing".
	queueEditCursor int
	// queueEditDraft saves the in-progress input text when the user first
	// presses ↑ to browse the queue, so it can be restored when the cursor
	// moves past the end.
	queueEditDraft string

	// history is a resumed session's messages, committed to scrollback once on
	// the first WindowSizeMsg so a reopened chat shows its prior transcript.
	history []provider.Message

	// reasoning accumulates the in-progress thinking stream (dim); pending
	// accumulates the in-progress answer (raw markdown). They are committed to
	// scrollback (reasoning collapsed by default, answer markdown-rendered) when they
	// finalize — at a tool/usage boundary or turn end — not previewed live, so
	// the bottom region stays a stable height. pendingCommit queues finalized
	// lines so a single Update emits exactly one ordered tea.Println.
	reasoning     *strings.Builder
	pending       *strings.Builder
	pendingCommit *[]string
	showReasoning bool // Ctrl+O / /verbose: show raw thinking text in the CLI
	cfg           *config.Config
	// reasoningLineIdx is the transcript index of the live "▎ thinking…" marker
	// while a reasoning block streams; it's rewritten to "▎ thought for Ns" when
	// the block closes. -1 when no block is open. transcriptDirty forces a
	// viewport re-feed after that in-place rewrite (length is unchanged).
	reasoningLineIdx int
	// reasoningTextIdx is the transcript index of the live reasoning text block
	// (the block right after the marker), streamed in as the model thinks and
	// removed when the block collapses (kept only in verbose mode). -1 when none.
	reasoningTextIdx int
	// reasoningView is a bounded trailing window (≤ reasoningViewMax bytes) of the
	// streaming thought, rendered live; the full text stays in reasoning for verbose.
	reasoningView []byte
	// reasoningNative is the Termux/native-scrollback path: reasoning is buffered
	// without a live transcript block, then appended once as a final summary.
	reasoningNative bool
	thinkStart      time.Time
	// answerIdx is the transcript index of the streaming answer block (rewritten in
	// place as completed paragraphs arrive); -1 when none is open. answerFlushed is
	// how many bytes of pending have already been rendered into it, so a Text packet
	// that doesn't close a new paragraph re-renders nothing.
	answerIdx     int
	answerFlushed int
	// toolStreamIdx is the transcript index of a running tool's live-output block
	// (streamed via ToolProgress under the tool card); -1 when none. toolStreamID
	// is the call ID it belongs to. Only a bounded tail is kept — the last few
	// complete lines (toolTail) plus the in-progress one (toolPartial) — so a
	// high-output command can't balloon memory or cost O(n²) re-splitting;
	// toolLineCount feeds the collapse summary.
	toolStreamIdx int
	toolStreamID  string
	toolTail      []string
	toolPartial   string
	toolLineCount int
	// shellOutputs stores the full accumulated output of each shell command
	// (tool IDs with "shell-" prefix), so the first 10 lines can be shown after
	// collapse and Ctrl+B can toggle the complete output.
	shellOutputs  map[string]string
	shellExpanded map[string]bool
	// shellTranscriptIdx maps a shell tool ID to the transcript index of its
	// collapsed output block, so Ctrl+B can rewrite it in place.
	shellTranscriptIdx map[string]int
	// toolLineCountByID keeps a switched-away tool's last line count so a late
	// ToolResult can still render "⎿ N lines" (shellOutputs only tracks "shell-" ids).
	toolLineCountByID map[string]int
	// toolStreamStart / toolStreamFrame drive the "⎿ working · Ns" line shown
	// under a dispatched tool that hasn't produced output yet, so a slow tool
	// reads as making progress rather than frozen.
	toolStreamStart time.Time
	toolStreamFrame int
	transcriptDirty bool
	// forceGotoBottom is set by replayActiveBranch and resetFreshContextView to
	// pin the viewport to the bottom after a session / branch / clear switch
	// regardless of the previous wasAtBottom state (#4584).
	forceGotoBottom bool
	eventCh         chan event.Event
	started         bool // banner + resumed history committed once

	// transcript holds every finalized line commitLine emits; the viewport
	// renders a scrollable window of it (alt-screen owns the grid, so there's no
	// native terminal scrollback). sel is the live left-drag text selection.
	transcript []string
	// transcriptSources runs parallel to transcript and retains raw, semantic
	// content for blocks whose layout depends on terminal width. Fixed blocks
	// keep their already-rendered text; markdown, user bubbles, reasoning, tool
	// cards, and replay bundles are regenerated after a resize.
	transcriptSources []transcriptSource
	wrappedLines      []string // transcript wrapped to viewport width (rendered each frame)
	viewport          viewport.Model
	sel               selection
	// autoScroll drives edge-drag scrolling: -1 up, +1 down, 0 off. dragX is the
	// column the drag is held at, so the ticker can extend the selection head.
	autoScroll int
	dragX      int
	// scrollbarDrag owns left-button drags that start on the transcript scrollbar
	// column. It is separate from text selection so the visual thumb is not a
	// dead target and dragging it never leaves a transcript selection behind.
	scrollbarDrag       bool
	scrollbarGrabOffset int
	// copyNoticeText is a transient "copied to clipboard" hint shown on the status
	// line after a mouse-drag, right-click, or Ctrl+C selection copy; "" when none
	// is showing. copyNoticeSeq guards its expiry tick so an older copy's timer
	// can't clear a newer notice — each copy bumps the sequence and only a tick
	// carrying the current sequence clears the text.
	copyNoticeText string
	copyNoticeSeq  int
	// clipboardImagePending keeps the footer honest while the platform clipboard
	// is being decoded and prevents repeated Ctrl+V presses from attaching the
	// same image multiple times before the first read completes.
	clipboardImagePending bool

	// The user bubble is echoed to scrollback immediately on Enter (bubbleStartIdx
	// marks where in the transcript it landed). It stays "un-sendable" until the
	// first response packet arrives: pressing Esc/Ctrl+C before then pops those
	// lines back off the transcript and restores the text to the input box, leaving
	// no trace. bubblePending is true from startTurn until the first packet confirms
	// the send or it's un-sent; turnDiscarded then swallows the turn's
	// already-buffered events until its TurnDone settles.
	pendingRestore string
	pendingPastes  []string
	bubbleStartIdx int
	bubblePending  bool
	turnDiscarded  bool

	// pendingApproval holds the tool-call approval currently shown in the banner
	// (nil when none). While set, the controller's run goroutine is blocked
	// awaiting ctrl.Approve and key input is captured to answer it.
	pendingApproval   *event.Approval
	approvalSelection int

	// chooser holds the `ask` tool's question card (nil when none). While set, the
	// run goroutine is blocked awaiting ctrl.AnswerQuestion and keys drive the card.
	chooser *chooser

	// rewind holds the Esc-Esc / "/rewind" picker (nil when closed); while set,
	// keys drive it and it renders as an overlay. lastEsc times the double-Esc
	// gesture that opens it on an empty composer.
	rewind *rewindPicker
	// resumePick is the interactive "/resume" session picker overlay. Non-nil
	// while the user browses saved sessions with ↑/↓ and confirms with Enter.
	resumePick *resumePicker
	// quickPick owns searchable single-choice overlays such as /model and
	// /provider. It never invokes a raw-mode prompt inside Bubble Tea.
	quickPick *quickPicker
	copyPick  *copyPicker
	lastEsc   time.Time

	// mcp is the interactive "/mcp" manager overlay. mcpDisabled tracks servers
	// turned off only for this chat session, matching the desktop connector
	// toggle's non-persistent semantics.
	mcp         *mcpManager
	mcpDisabled map[string]bool

	// clearConfirm is the destructive "/clear" confirmation overlay. It is separate
	// from /new because /clear discards the current transcript instead of saving it.
	clearConfirm *clearConfirm

	// lastCtrlCAt records when Ctrl+C was pressed while idle on an empty
	// composer, enabling a "press again to quit" confirmation pattern (1.5s
	// window). Reset when Ctrl+C clears non-empty input instead.
	lastCtrlCAt time.Time

	// mcpImport holds the interactive cc-switch MCP import picker (nil when
	// closed). It writes selected servers to config and hot-connects the ones that
	// can start successfully.
	mcpImport *mcpImportPicker

	// host is the running MCP servers (nil when no plugins). The TUI reads
	// prompts (slash commands), resources (@-references), and server status
	// (/mcp) from it.
	host *plugin.Host

	// commands are custom slash commands loaded from .reasonix/commands; each renders
	// its template with the typed args and sends the result as a turn.
	commands []command.Command

	// skills are the discoverable skills (built-in + user/project); each is offered
	// in the slash menu as "/<name>" and managed via /skills.
	skills []skill.Skill

	// skillPick is the interactive skill picker overlay for /skills. nil when closed.
	skillPick *skillPicker

	// buildController builds a fresh controller for a model/profile pair, carrying prior
	// history across and pinning auto-save to resumePath so the continued
	// conversation stays in one file (set by chatREPL; it must NOT touch this
	// model — the swap happens on the running copy). nil disables runtime
	// rebuild commands. modelRef is the active "provider/model" ref, marked
	// current in the picker. runtimeProfile stores boot's normalized token mode:
	// full (displayed as balanced), economy, or delivery. oldCtrl is the
	// outgoing controller, passed through so the replacement can carry forward
	// same-session tool grants and Plan-mode read-only command trust that
	// don't travel through carry/resumePath (see Controller.RestoreSessionAuthorizations).
	buildController func(spec controllerBuildSpec, carry []provider.Message, resumePath string, oldCtrl control.SessionAPI) (*control.Controller, error)
	modelRef        string
	runtimeProfile  string
	effortLevel     string // "" when the current provider/model has no configurable effort

	// leases owns the session lease guarding the TUI's active session file (set
	// by chatREPL; nil in tests and when persistence is disabled). Every in-TUI
	// operation that rebinds the controller to another session file must move
	// the lease first — see rebindSessionLease / followSessionLease.
	leases *control.SessionLeaseKeeper

	// outputStyle is the active output-style name (config agent.output_style),
	// shown as the current entry in the /output-style listing. "" = default.
	outputStyle string

	// diffMaxLines controls the max lines shown in a diff view. 0 = show all;
	// non-zero = fold at that many lines. Toggled by /diff-fold.
	diffMaxLines int

	// statuslineCmd is the user's custom status-line command (config
	// [statusline].command); "" disables it. statuslineOut caches its latest
	// one-line stdout, refreshed at startup and after each turn and rendered in
	// place of the built-in data row.
	statuslineCmd string
	statuslineOut string
	gitStatus     gitStatus

	// statusLineCount is the number of terminal rows the status block occupies
	// (wrapped working line + wrapped status line + wrapped data line). Updated
	// each frame via computeStatusLineCount so bottomRows can reserve the correct
	// height; starts at 2 (unwrapped) until first render.
	statusLineCount int

	// modelSwitchPending is true while any async controller rebuild is in flight.
	modelSwitchPending bool
	// pendingModelSwitch holds the tea.Cmd that triggers the async build. The
	// historical field name is retained because model, effort, skill refresh,
	// and work-mode changes all share the same atomic swap path.
	pendingModelSwitch tea.Cmd
	// oldControllers accumulates controllers retired by runtime switches.
	// They cannot be closed during the switch (Close runs SessionEnd hooks
	// and kills plugin subprocesses, both of which corrupt the terminal's
	// raw mode). Instead they are closed at process exit when the terminal
	// is already being restored.
	oldControllers []control.SessionAPI

	// completion is the live autocomplete menu (slash commands; @-refs later).
	completion completion
	// fileSearchCache memoizes fileref.Search by query so the bounded walk runs
	// once per @token fragment, not on every keystroke that re-renders the menu.
	fileSearchCache map[string][]string
}

type tuiState int

const (
	tuiIdle tuiState = iota
	tuiRunning
)

type controllerBuildSpec struct {
	ModelRef         string
	RuntimeProfile   string
	ToolApprovalMode string
	PlanMode         bool
	EffortOverride   *string
}

func (m *chatTUI) runtimeSwitchBusy() bool {
	if m == nil || m.ctrl == nil {
		return false
	}
	status := m.ctrl.RuntimeStatus()
	return status.Running || status.PendingPrompt || status.BackgroundJobs > 0 || m.pendingApproval != nil || m.chooser != nil
}

// agentEventMsg is one typed event from the agent's run loop.
type agentEventMsg event.Event

// maxEventDrain caps how many buffered events one Update coalesces before
// yielding to render, so a sustained output flood still shows live progress.
const maxEventDrain = 512

const resetMouseTracking = ansi.ResetModeMouseX10 +
	ansi.ResetModeMouseNormal +
	ansi.ResetModeMouseHighlight +
	ansi.ResetModeMouseButtonEvent +
	ansi.ResetModeMouseAnyEvent +
	ansi.ResetModeMouseExtSgr +
	ansi.ResetModeMouseExtUtf8 +
	ansi.ResetModeMouseExtUrxvt +
	ansi.ResetModeMouseExtSgrPixel

// compactDoneMsg reports that an async /compact pass returned. The card was
// already drawn from the CompactionDone event; this only surfaces a failure and
// snapshots on success.
type compactDoneMsg struct{ err error }

// tuiShutdownMsg asks the live TUI model to persist its current controller and
// quit. It is injected from the signal handler so shutdown does not snapshot a
// stale controller captured before an in-TUI rebuild.
type tuiShutdownMsg struct{}

// elapsedTickMsg fires once a second while a turn runs, driving the "thinking
// Ns" counter in the status line.
type elapsedTickMsg struct{}

// balanceMsg carries the result of an async wallet-balance fetch; text is the
// formatted readout ("" when none/failed).
type balanceMsg struct{ text string }

// statuslineMsg carries the latest custom status-line output (one line, ""
// when none/failed).
type statuslineMsg struct{ out string }

// gitStatusMsg carries the latest lightweight git readout for the built-in
// status line. Empty means "not a git worktree" or "git unavailable".
type gitStatusMsg struct{ status gitStatus }

// runStatusline runs the user's custom status-line command off the event loop,
// feeding it a small JSON context on stdin and returning its first stdout line.
// A no-op (nil) when no command is configured. Tight timeout so a slow script
// can't stall the UI; failures collapse to an empty line rather than an error.
func (m chatTUI) runStatusline() tea.Cmd {
	cmd := m.statuslineCmd
	if cmd == "" {
		return nil
	}
	used, window := m.ctrl.ContextSnapshot()
	cwd, _ := os.Getwd()
	payload, _ := json.Marshal(map[string]any{
		"model":         m.label,
		"contextUsed":   used,
		"contextWindow": window,
		"cwd":           cwd,
	})
	return func() tea.Msg { return statuslineMsg{out: runStatuslineCmd(cmd, string(payload))} }
}

const statuslineCommandTimeout = 2 * time.Second

// runStatuslineCmd runs a status-line command with the JSON context on stdin and
// returns its first stdout line (status lines are a single row). A tight timeout
// keeps a slow script from stalling the UI; any failure collapses to "".
func runStatuslineCmd(cmd, stdinPayload string) string {
	return runStatuslineCmdWithTimeout(cmd, stdinPayload, statuslineCommandTimeout)
}

func runStatuslineCmdWithTimeout(cmd, stdinPayload string, timeout time.Duration) string {
	res := hook.DefaultSpawner(context.Background(), hook.SpawnInput{
		Command: cmd,
		Stdin:   stdinPayload + "\n",
		Timeout: timeout,
	})
	out := strings.TrimSpace(res.Stdout)
	if i := strings.IndexByte(out, '\n'); i >= 0 {
		out = strings.TrimSpace(out[:i])
	}
	return out
}

func (m chatTUI) refreshGitStatus() tea.Cmd {
	if m.statuslineCmd != "" {
		return nil
	}
	return fetchGitStatus()
}

// modelSwitchMsg carries the result of an async /model switch. A nil err means
// the new controller is ready in ctrl; label/commands/skills/host mirror the
// fields that runModelSubcommand used to set synchronously. oldCtrl is the
// previous controller that must be closed after the switch — its cleanup
// (SessionEnd hooks, plugin subprocess kill) is deferred to a tea.Cmd so it
// runs after the render completes, avoiding corruption of the terminal's raw
// mode that would occur if Close() were called from the build goroutine.
type modelSwitchMsg struct {
	ref           string
	profile       string
	ctrl          control.SessionAPI
	oldCtrl       control.SessionAPI
	label         string
	commands      []command.Command
	skills        []skill.Skill
	host          *plugin.Host
	failurePrefix string
	successNotice string
	err           error
}

// fetchBalance queries the provider's wallet balance off the event loop. It's a
// no-op readout ("") when the provider declares no balance_url or the fetch
// fails, so the status line stays quiet rather than surfacing an error.
func fetchBalance(ctrl control.Status) tea.Cmd {
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
	sent    string
	display string
	restore string
	block   string
	errs    []string
}

type clipboardImageMsg struct {
	path string
	err  error
}

// newChatTUI assembles the initial model. The controller has already been wired
// with an event sink that feeds eventCh; the TUI issues commands to it and
// renders the events it emits. Label, history, host, and commands are read from
// the controller, so a resumed session pre-populates scrollback.
func newChatTUI(ctrl control.SessionAPI, missing string, eventCh chan event.Event, termW int) chatTUI {
	ti := textarea.New()
	configureChatTextarea(&ti)

	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = themeStyle(activeCLITheme.accent)

	commitBuf := []string{}
	nativeScrollback := detectTermuxTerminal()
	return chatTUI{
		ctrl:                 ctrl,
		label:                ctrl.Label(),
		missing:              missing,
		nativeScrollback:     nativeScrollback,
		mouseCaptureOff:      mouseCaptureOffByDefault(),
		input:                ti,
		spinner:              sp,
		submittedInputCursor: -1,
		queueEditCursor:      -1,
		nextPasteID:          1,
		reasoningLineIdx:     -1,
		reasoningTextIdx:     -1,
		answerIdx:            -1,
		toolStreamIdx:        -1,
		reasoning:            &strings.Builder{},
		pending:              &strings.Builder{},
		pendingCommit:        &commitBuf,
		diffMaxLines:         diffFoldLimit,
		showReasoning:        nativeScrollback,
		shellOutputs:         make(map[string]string),
		shellExpanded:        make(map[string]bool),
		shellTranscriptIdx:   make(map[string]int),
		toolLineCountByID:    make(map[string]int),
		eventCh:              eventCh,
		history:              ctrl.History(),
		host:                 ctrl.Host(),
		commands:             ctrl.Commands(),
		skills:               ctrl.SlashSkills(),
		viewport:             viewport.New(viewport.WithWidth(termW)),
		statusLineCount:      3,
	}
}

func transcriptContentWidth(termW int, nativeScrollback bool) int {
	if !nativeScrollback {
		termW-- // reserve the last column for the transcript scrollbar
	}
	return max(termW, 1)
}

// mouseCaptureOffByDefault lets a user opt out of in-app mouse capture for
// every run (e.g. a terminal/multiplexer combo where the native right-click
// menu and click-drag selection matter more than the scrollbar and
// wheel-scroll) without having to type "/mouse" each session.
func mouseCaptureOffByDefault() bool {
	v := strings.TrimSpace(os.Getenv("REASONIX_DISABLE_MOUSE"))
	return v != "" && v != "0"
}

func configureChatTextarea(ti *textarea.Model) {
	// Keep a stable two-cell input affordance, matching the prompt treatment in
	// other coding TUIs. Continuation rows receive two spaces so text and the
	// real terminal cursor stay aligned without repeating the arrow.
	ti.SetPromptFunc(composerPromptWidth, func(info textarea.PromptInfo) string {
		if info.LineNumber != 0 {
			return ""
		}
		if info.Focused {
			return accent("❯ ")
		}
		return dim("❯ ")
	})
	ti.CharLimit = 16384
	// The prompt and real terminal cursor already show where typing starts. Keep
	// the idle composer quiet; modal free-text questions set their own temporary
	// placeholder through refreshInputPlaceholder.
	ti.Placeholder = ""
	ti.DynamicHeight = true
	ti.MinHeight = 1
	ti.MaxHeight = maxInputRows
	ti.MaxContentHeight = ti.CharLimit
	ti.SetHeight(1)
	ti.ShowLineNumbers = false
	applyTextareaTheme(ti)
	// Use the real terminal cursor (not a styled virtual one) so View can place
	// it at the insertion point and IME candidate windows anchor to the input.
	ti.SetVirtualCursor(false)
	// Plain Enter submits (the chatTUI handler intercepts it), so the textarea's
	// own InsertNewline binding moves to Alt+Enter / Ctrl+J / Shift+Enter.
	ti.KeyMap.InsertNewline = key.NewBinding(key.WithKeys("alt+enter", "ctrl+j", "shift+enter"))
	ti.Focus()
}

func (m *chatTUI) refreshInputPlaceholder() {
	if m.chooserTyping() {
		m.input.Placeholder = i18n.M.AskTypeSomething
		return
	}
	m.input.Placeholder = ""
}

func isTermuxTerminal() bool {
	if os.Getenv("TERMUX_VERSION") != "" || os.Getenv("TERMUX_APP_PID") != "" || os.Getenv("TERMUX__PREFIX") != "" {
		return true
	}
	return strings.Contains(os.Getenv("PREFIX"), "/com.termux/")
}

var detectTermuxTerminal = isTermuxTerminal

func (m *chatTUI) rememberSubmittedInput(input string) {
	if strings.TrimSpace(input) == "" {
		return
	}
	if len(m.submittedInputs) == 0 || m.submittedInputs[len(m.submittedInputs)-1] != input {
		m.submittedInputs = append(m.submittedInputs, input)
	}
	m.submittedInputCursor = -1
	m.submittedInputDraft = ""
}

func (m *chatTUI) recallSubmittedInput(delta int) bool {
	if len(m.submittedInputs) == 0 {
		return false
	}
	cursor := m.submittedInputCursor
	if cursor < 0 {
		if delta > 0 {
			return false
		}
		if m.input.Line() != 0 {
			return false // first-line Up enters history; lower lines navigate the draft
		}
		m.submittedInputDraft = m.input.Value()
		cursor = len(m.submittedInputs) - 1
	} else {
		cursor += delta
	}

	if cursor < 0 {
		cursor = 0
	}
	if cursor >= len(m.submittedInputs) {
		m.submittedInputCursor = -1
		m.input.SetValue(m.submittedInputDraft)
		m.growInputToFit()
		return true
	}
	m.submittedInputCursor = cursor
	m.input.SetValue(m.submittedInputs[cursor])
	m.growInputToFit()
	return true
}

func (m *chatTUI) resetSubmittedInputRecall() {
	m.submittedInputCursor = -1
	m.submittedInputDraft = ""
}

// navigateQueue moves through the pending interject queue during tuiRunning.
// delta < 0 means ↑ (older), delta > 0 means ↓ (newer). Returns true if the
// input was updated.
func (m *chatTUI) navigateQueue(delta int) bool {
	if len(m.pendingInterject) == 0 {
		return false
	}
	cursor := m.queueEditCursor
	if cursor < 0 {
		if delta > 0 {
			return false // already at "new draft" — nothing newer
		}
		// First ↑: save the current draft and jump to the last queued item.
		m.queueEditDraft = m.input.Value()
		cursor = len(m.pendingInterject) - 1
	} else {
		cursor += delta
	}

	if cursor < 0 {
		cursor = 0
	}
	if cursor >= len(m.pendingInterject) {
		// Past the end: restore the draft the user was composing.
		m.queueEditCursor = -1
		m.input.SetValue(m.queueEditDraft)
		m.growInputToFit()
		return true
	}
	m.queueEditCursor = cursor
	m.input.SetValue(m.pendingInterject[cursor])
	m.growInputToFit()
	return true
}

// resetQueueNavigation resets the queue browsing cursor so the user returns to
// normal input mode. Any in-progress edit is discarded (the queued item keeps
// its previous value).
func (m *chatTUI) resetQueueNavigation() {
	m.queueEditCursor = -1
	m.queueEditDraft = ""
}

// renderQueueIndicator renders the pending-message queue as dim text to show
// above the input box when messages are queued during a running turn.
func (m chatTUI) renderQueueIndicator() string {
	if m.state != tuiRunning || len(m.pendingInterject) == 0 {
		return ""
	}
	queueStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240")) // dim grey
	highlightStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("250"))
	var lines []string
	for i, msg := range m.pendingInterject {
		preview := msg
		// Truncate long messages for the compact preview.
		runes := []rune(preview)
		if len(runes) > 50 {
			preview = string(runes[:47]) + "…"
		}
		cursor := " "
		style := queueStyle
		if m.queueEditCursor == i {
			cursor = "▸"
			style = highlightStyle
		}
		lines = append(lines, style.Render(fmt.Sprintf("  %s [%d] %s", cursor, i+1, preview)))
	}
	return strings.Join(lines, "\n")
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
		m.runStatusline(), // nil (no-op) unless a custom status line is configured
		m.refreshGitStatus(),
	)
}

func suspendWithMouseReset() tea.Cmd {
	return tea.Sequence(tea.Raw(resetMouseTracking), tea.Suspend)
}

func (m chatTUI) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	wasAtBottom := m.viewport.AtBottom()
	prevLines := len(m.transcript)
	prevWidth := m.width
	prevYOff := m.viewport.YOffset()
	var resizeAnchor transcriptResizeAnchor
	if size, ok := msg.(tea.WindowSizeMsg); ok && size.Width != m.width && !wasAtBottom {
		resizeAnchor = captureTranscriptResizeAnchor(m.transcript, m.viewport.Width(), prevYOff)
	}

	next, cmd := m.update(msg)
	cm := next.(chatTUI)

	contentW := transcriptContentWidth(cm.width, cm.nativeScrollback)
	cm.viewport.SetWidth(contentW)
	// Recompute the wrapped status-line count so bottomRows reserves the right
	// height for the viewport. Use cm.width (same as boxW in View()) so the
	// wrapping width matches what View() actually renders.
	cm.statusLineCount = cm.computeStatusLineCount(cm.width)
	// Keep the composer proportional to the live terminal instead of letting its
	// absolute row cap crowd the transcript and fixed status rows on short
	// windows. Textarea remains the owner of the scroll offset and caret reveal.
	cm.syncInputHeightLimit()
	cm.viewport.SetHeight(cm.transcriptHeight())
	if cm.width != prevWidth {
		cm.reflowTranscript(cm.width)
		// Selection coordinates are visual-line based and cannot survive a
		// semantic reflow without selecting unrelated text.
		cm.sel = selection{}
	}
	// Re-feed only when the content grew or the width changed (re-wrapping is
	// the expensive part); a bare scroll or spinner tick keeps the offset.
	if len(cm.transcript) != prevLines || cm.width != prevWidth || cm.transcriptDirty {
		wrapped := wrapTranscript(strings.Join(cm.transcript, "\n"), contentW)
		cm.viewport.SetContent(wrapped)
		cm.wrappedLines = strings.Split(wrapped, "\n")
		if wasAtBottom {
			cm.viewport.GotoBottom() // tail-follow: stay pinned to newest output
		} else if cm.width != prevWidth && resizeAnchor.valid {
			cm.viewport.SetYOffset(resizeAnchor.yOffset(cm.transcript, contentW))
		}
	}
	if cm.forceGotoBottom {
		cm.viewport.GotoBottom()
		cm.forceGotoBottom = false
	}
	cm.transcriptDirty = false
	// Any viewport scroll (wheel, PgUp/PgDn, edge auto-scroll, or tail-follow to
	// newest output) shifts the whole window. Some terminals (Warp) mishandle
	// the renderer's scroll/insert-line optimization and strand stale rows, so
	// force a full clear+redraw whenever the offset actually moved.
	if cm.viewport.YOffset() != prevYOff && !cm.nativeScrollback && !cm.sessionSwitch {
		return cm, tea.Batch(tea.ClearScreen, cmd)
	}
	cm.sessionSwitch = false
	return cm, cmd
}

// update runs the model's message handling. Update wraps it to keep the
// transcript viewport sized, fed, and tail-following after every message.
func (m chatTUI) update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd
	var inputBeforeSelection string

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.followComposerCursor()
		m.width = msg.Width
		m.height = msg.Height
		m.input.SetWidth(max(msg.Width-4, 1))
		// Commit the banner — and a resumed session's transcript — once, now
		// that the width is known.
		if !m.started {
			m.started = true
			history := append([]provider.Message(nil), m.history...)
			m.commitTranscriptSource(transcriptSource{
				kind: transcriptSourceReplayBundle, raw: m.missing, history: history,
			})
			m.history = nil
		}

	case tea.MouseWheelMsg:
		if m.mouseOverComposer(msg.X, msg.Y) {
			delta := 0
			switch msg.Button {
			case tea.MouseWheelUp:
				delta = -composerWheelRows
			case tea.MouseWheelDown:
				delta = composerWheelRows
			}
			if delta != 0 && m.scrollComposer(delta) {
				return m, nil
			}
		}
		// Outside the composer, or once its internal viewport has reached the
		// requested edge, continue the gesture in the transcript. This mirrors
		// ordinary nested-scroll behavior and avoids a dead wheel at boundaries.
		switch msg.Button {
		case tea.MouseWheelUp:
			m.viewport.ScrollUp(3)
		case tea.MouseWheelDown:
			m.viewport.ScrollDown(3)
		}
		return m, nil

	case tea.MouseClickMsg:
		// Match the complete terminal right-click convention while Reasonix owns
		// the mouse: copy an active selection, otherwise paste clipboard text into
		// the visible composer. Left-press begins a selection unless it lands on
		// the transcript scrollbar or a shell-output hint line.
		// Middle-click pastes tmux's current buffer when tmux owns the pane;
		// otherwise it follows the X11/Wayland PRIMARY-selection convention.
		if msg.Button == tea.MouseMiddle {
			if m.hideComposer() {
				return m, nil
			}
			cmds = append(cmds, pasteMiddleClick())
			return m, finalize(m, cmds)
		}
		if msg.Button == tea.MouseRight && m.validComposerSelection() && !m.composerSel.empty() {
			cmds = append(cmds, m.copySelectionWithNotice(m.selectedComposerText()))
			return m, finalize(m, cmds)
		}
		if msg.Button == tea.MouseRight && m.sel.active && !m.sel.empty() {
			text := m.selectedText()
			m.sel = selection{}
			cmds = append(cmds, m.copySelectionWithNotice(text))
			return m, finalize(m, cmds)
		}
		if msg.Button == tea.MouseRight && !m.hideComposer() {
			cmds = append(cmds, pasteClipboardText())
			return m, finalize(m, cmds)
		}
		if msg.Button == tea.MouseLeft {
			if at, ok := m.composerCaretAt(msg.X, msg.Y, false); ok {
				m.sel = selection{}
				m.autoScroll = 0
				m.setComposerCursor(at.offset)
				m.composerSel = composerSelection{
					active: true, anchor: at.offset, head: at.offset, value: m.input.Value(),
				}
				return m, nil
			}
			m.composerSel = composerSelection{}
		}
		if msg.Button == tea.MouseLeft && m.inScrollbar(msg.X, msg.Y) {
			m.sel = selection{}
			m.autoScroll = 0
			m.scrollbarDrag = true
			m.scrollbarGrabOffset = m.scrollbarGrabRowOffset(msg.Y)
			m.dragScrollbar(msg.Y)
			return m, nil
		}
		if msg.Button == tea.MouseLeft && msg.Y < m.viewport.Height() {
			// Check if the clicked line is a shell-output hint.
			lineIdx := m.viewport.YOffset() + msg.Y
			if lineIdx >= 0 && lineIdx < len(m.wrappedLines) {
				clicked := m.wrappedLines[lineIdx]
				if strings.Contains(clicked, "more lines") && strings.Contains(clicked, "Ctrl+B") {
					m.toggleShellOutput()
					return m, finalize(m, cmds)
				}
			}
			at := m.transcriptCaret(msg.X, msg.Y)
			m.sel = selection{active: true, anchor: at, head: at}
			m.autoScroll = 0
		}
		return m, nil

	case tea.MouseMotionMsg:
		if m.validComposerSelection() {
			if at, ok := m.composerCaretAt(msg.X, msg.Y, true); ok {
				m.composerSel.head = at.offset
			}
			return m, nil
		}
		if m.scrollbarDrag {
			m.dragScrollbar(msg.Y)
			return m, nil
		}
		// Drag extends the live selection (CellMotion only reports motion while
		// a button is held, so this is a drag). A drag held against the top or
		// bottom edge starts an auto-scroll ticker so the selection can run past
		// the visible window.
		if m.sel.active {
			m.sel.head = m.transcriptCaret(msg.X, msg.Y)
			m.dragX = msg.X
			prev := m.autoScroll
			m.autoScroll = edgeScrollDir(msg.Y, m.viewport.Height())
			if m.autoScroll != 0 && prev == 0 {
				return m, autoScrollTick()
			}
		}
		return m, nil

	case autoScrollMsg:
		// One edge-scroll step: scroll a single line, drag the selection head to
		// the edge row, and keep ticking until the drag ends, leaves the edge, or
		// the viewport can't scroll further (so it can't run away to the end).
		if !m.sel.active || m.autoScroll == 0 {
			return m, nil
		}
		edgeY := 0
		if m.autoScroll > 0 {
			m.viewport.ScrollDown(1)
			edgeY = m.viewport.Height() - 1
		} else {
			m.viewport.ScrollUp(1)
		}
		m.sel.head = m.transcriptCaret(m.dragX, edgeY)
		// Stop at the boundary so a held edge can't run away to the very end.
		if (m.autoScroll > 0 && m.viewport.AtBottom()) || (m.autoScroll < 0 && m.viewport.AtTop()) {
			m.autoScroll = 0
			return m, nil
		}
		return m, autoScrollTick()

	case tea.MouseReleaseMsg:
		if msg.Button == tea.MouseLeft && m.validComposerSelection() {
			if at, ok := m.composerCaretAt(msg.X, msg.Y, true); ok {
				m.composerSel.head = at.offset
				m.setComposerCursor(at.offset)
			}
			if m.composerSel.empty() {
				m.composerSel = composerSelection{}
				return m, nil
			}
			// The terminal cannot see Reasonix's application-owned highlight, and
			// macOS commonly consumes Cmd+C before it reaches the TUI. Copy on drag
			// release just like transcript selection so the visible selection always
			// has a usable clipboard result.
			cmds = append(cmds, m.copySelectionWithNotice(m.selectedComposerText()))
			return m, finalize(m, cmds)
		}
		// Release finalizes the selection: a real drag auto-copies it (native
		// terminal convention), while the highlight stays on as the visual
		// "what's selected" cue and a right-click can still re-copy it. A plain
		// click (no drag) clears any prior selection.
		if m.scrollbarDrag {
			m.dragScrollbar(msg.Y)
			m.scrollbarDrag = false
			m.scrollbarGrabOffset = 0
			return m, nil
		}
		m.autoScroll = 0 // stop edge auto-scroll
		if msg.Button == tea.MouseLeft && m.sel.active {
			if m.sel.empty() {
				m.sel = selection{}
			} else {
				cmds = append(cmds, m.copySelectionWithNotice(m.selectedText()))
			}
		}
		return m, finalize(m, cmds)

	case tea.PasteMsg:
		m.followComposerCursor()
		pasteBefore := m.input.Value()
		if m.state != tuiRunning && m.attachPastedImages(msg.Content) {
			if shouldClearWideInputChange(pasteBefore, m.input.Value()) {
				cmds = append(cmds, tea.ClearScreen)
			}
			return m, finalize(m, cmds)
		}
		if m.validComposerSelection() && !m.composerSel.empty() {
			inputBeforeSelection = pasteBefore
			m.deleteComposerSelection()
		}
		if ref, ok := pastedFileRef(msg.Content); ok {
			m.input.InsertString(ref + " ")
			m.growInputToFit()
			m.updateCompletion()
			if shouldClearWideInputChange(pasteBefore, m.input.Value()) {
				cmds = append(cmds, tea.ClearScreen)
			}
			return m, finalize(m, cmds)
		}
		if !m.chooserTyping() && m.pendingApproval == nil && m.rewind == nil && m.resumePick == nil && m.mcp == nil && m.clearConfirm == nil && m.mcpImport == nil && m.skillPick == nil && m.shouldFoldPaste(msg.Content) {
			m.insertFoldedPaste(msg.Content)
			m.growInputToFit()
			m.updateCompletion()
			if shouldClearWideInputChange(pasteBefore, m.input.Value()) {
				cmds = append(cmds, tea.ClearScreen)
			}
			return m, finalize(m, cmds)
		}

	case tea.KeyPressMsg:
		// Any keystroke dismisses a finished selection (copy is a right-click),
		// with a few exceptions: Ctrl/Super/Meta+C copies the selection, the
		// paste shortcuts keep it so the async clipboard result can replace
		// it, and Left/Right collapse it to its ordered start/end.
		sel := m.sel
		m.sel = selection{}
		if m.validComposerSelection() && !m.composerSel.empty() {
			switch {
			case msg.String() == "ctrl+c" || msg.String() == "super+c" || msg.String() == "meta+c":
				cmds = append(cmds, m.copySelectionWithNotice(m.selectedComposerText()))
				return m, finalize(m, cmds)
			case imagePasteShortcut(msg.String(), runtime.GOOS):
				// The asynchronous image result replaces the still-active
				// selection. Terminal text paste arrives separately as PasteMsg.
			case msg.String() == "left":
				start, _ := m.composerSel.ordered()
				m.composerSel = composerSelection{}
				m.setComposerCursor(start)
				return m, finalize(m, cmds)
			case msg.String() == "right":
				_, end := m.composerSel.ordered()
				m.composerSel = composerSelection{}
				m.setComposerCursor(end)
				return m, finalize(m, cmds)
			default:
				inputBeforeSelection = m.input.Value()
				if composerSelectionDeletes(msg, m.input.KeyMap) {
					m.deleteComposerSelection()
					m.growInputToFit()
					m.updateCompletion()
					if shouldClearWideInputChange(inputBeforeSelection, m.input.Value()) {
						cmds = append(cmds, tea.ClearScreen)
					}
					return m, finalize(m, cmds)
				}
				if composerSelectionReplaces(msg, m.input.KeyMap) {
					m.deleteComposerSelection()
				} else {
					m.composerSel = composerSelection{}
				}
			}
		}
		// Transcript scroll keys work in any state (PgUp/PgDn are never text).
		switch msg.String() {
		case "pgup":
			m.viewport.PageUp()
			return m, finalize(m, cmds)
		case "pgdown":
			m.viewport.PageDown()
			return m, finalize(m, cmds)
		case "ctrl+home":
			m.viewport.GotoTop()
			return m, finalize(m, cmds)
		case "ctrl+end":
			m.viewport.GotoBottom()
			return m, finalize(m, cmds)
		case "ctrl+z":
			return m, suspendWithMouseReset()
		}
		// From this point on the key belongs to the active control rather than
		// transcript navigation. Editing or moving the insertion cursor restores
		// the textarea's normal caret-following viewport.
		m.followComposerCursor()
		// A question card is modal: keys drive it. In its free-text ("Type
		// something") mode, the keystroke goes to the textarea — Enter confirms the
		// custom answer, Esc backs out of typing — so input/IME work as usual.
		if m.chooser != nil {
			if m.chooser.typing {
				switch msg.String() {
				case "enter":
					val := strings.TrimSpace(m.input.Value())
					m.input.Reset()
					m.chooser.typing = false
					m.refreshInputPlaceholder()
					if val == "" {
						return m, finalize(m, cmds)
					}
					m.chooser.custom[m.chooser.tab] = val
					m.chooser.sel[m.chooser.tab] = map[int]bool{}
					return m.chooserAdvance()
				case "esc":
					m.chooser.typing = false
					m.input.Reset()
					m.refreshInputPlaceholder()
					return m, finalize(m, cmds)
				}
				beforeInput := m.input.Value()
				var ic tea.Cmd
				m.input, ic = m.input.Update(msg)
				cmds = append(cmds, ic)
				m.growInputToFit()
				if shouldClearWideInputChange(beforeInput, m.input.Value()) {
					cmds = append(cmds, tea.ClearScreen)
				}
				return m, finalize(m, cmds)
			}
			return m.handleChooserKey(msg)
		}
		// The rewind picker is modal while open: keys navigate it.
		if m.rewind != nil {
			return m.handleRewindKey(msg)
		}
		// The MCP import picker is modal while open: keys select candidates.
		if m.mcpImport != nil {
			return m.handleMCPImportKey(msg)
		}
		// Copy picker is modal while open.
		if m.copyPick != nil {
			return m.handleCopyPickerKey(msg)
		}
		// The resume picker is modal while open: keys navigate it.
		if m.resumePick != nil {
			return m.handleResumePickerKey(msg)
		}
		// Searchable command pickers are modal while open.
		if m.quickPick != nil {
			return m.handleQuickPickerKey(msg)
		}
		// The MCP manager is modal while open: keys navigate it.
		if m.mcp != nil {
			return m.handleMCPManagerKey(msg)
		}
		// The destructive /clear confirmation is modal while open.
		if m.clearConfirm != nil {
			return m.handleClearConfirmKey(msg)
		}
		// The skill picker is modal while open: keys navigate it.
		if m.skillPick != nil {
			return m.handleSkillPickerKey(msg)
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
			case "up", "ctrl+p":
				m.moveCompletion(-1)
				return m, nil
			case "down", "ctrl+n":
				m.moveCompletion(1)
				return m, nil
			case "tab", "enter":
				if msg.String() == "enter" && (m.completionExactLabel() || m.completionBareOverlayCommand()) {
					m.completion = completion{}
					break // fall through to regular Enter and submit the command
				}
				// When Enter is pressed and the selected completion is already fully
				// present in the input, close the menu and submit instead of accepting
				// the same item again (/resume 1 still has /resume 10 as a prefix match).
				if msg.String() == "enter" && m.completionSelectedInsertPresent() {
					m.completion = completion{}
					break // fall through to regular Enter
				}
				m.acceptCompletion()
				return m, nil
			case "esc":
				m.completion = completion{}
				if m.state == tuiRunning {
					break // a turn is running — also cancel it via the main Esc handler
				}
				return m, nil
			}
		}
		switch msg.String() {
		case "up":
			if m.state == tuiRunning {
				if m.navigateQueue(-1) {
					return m, nil
				}
			} else if m.recallSubmittedInput(-1) {
				return m, nil
			}
		case "down":
			if m.state == tuiRunning {
				if m.navigateQueue(1) {
					return m, nil
				}
			} else if m.recallSubmittedInput(1) {
				return m, nil
			}
		case "enter":
			// Don't reset queue navigation — the Enter handler below needs
			// queueEditCursor to decide whether to save an edit or enqueue.
		default:
			m.resetSubmittedInputRecall()
			m.resetQueueNavigation()
		}
		if imagePasteShortcut(msg.String(), runtime.GOOS) {
			if m.state == tuiRunning {
				return m, nil
			}
			if cmd := m.beginClipboardImagePaste(); cmd != nil {
				cmds = append(cmds, cmd)
			}
			return m, finalize(m, cmds)
		}
		switch msg.String() {
		case "esc":
			// "Back out" of the most specific in-progress state: un-send a just-sent
			// turn (server not yet replied), cancel a streaming turn, or clear
			// typed-but-unsent input. Mode switches (normal/plan/YOLO) are
			// exclusively driven by Shift+Tab — Esc must not silently flip a
			// session from plan or YOLO back to a less-permissive mode. PR #3051
			// removed the YOLO half of this; plan mode was missed and is fixed
			// here. Scrollback is the terminal's now, so there's no viewport to
			// dismiss.
			switch {
			case m.state == tuiRunning && m.bubblePending:
				m.unsendPending()
			case m.state == tuiRunning:
				m.ctrl.Cancel()
				// Defensive: if the controller is no longer running (cancel
				// completed synchronously, e.g. for shell commands), transition
				// to idle immediately instead of waiting for TurnDone.
				if !m.ctrl.Running() {
					m.state = tuiIdle
					m.confirmBubbleSent()
				}
			default:
				// Idle (any mode): a double-Esc on an empty composer opens the
				// rewind picker (Claude Code's gesture); a first Esc just arms
				// it. Non-empty input clears as before.
				if strings.TrimSpace(m.input.Value()) == "" {
					if !m.lastEsc.IsZero() && time.Since(m.lastEsc) < 600*time.Millisecond {
						m.lastEsc = time.Time{}
						m.openRewind()
					} else {
						m.lastEsc = time.Now()
					}
				} else {
					m.input.Reset()
					m.pastedBlocks = nil
				}
			}
			return m, nil
		case "ctrl+c", "super+c", "meta+c":
			if m.state == tuiRunning {
				// Selection takes precedence: copy instead of cancel, same as idle.
				if sel.active && !sel.empty() {
					m.sel = sel
					text := m.selectedText()
					m.sel = selection{}
					cmds = append(cmds, m.copySelectionWithNotice(text))
					return m, finalize(m, cmds)
				}
				if m.bubblePending {
					m.unsendPending() // server not yet replied — restore text, leave no trace
				} else if m.cancelRequested() {
					m.ctrl.Cancel()
					return m, tea.Quit
				} else {
					m.ctrl.Cancel()
				}
				return m, nil
			}
			// Idle: an active text selection takes precedence over the
			// composer-clear / double-press-quit gestures. Standard terminal
			// convention is "Ctrl+C copies the selection" — the user can still
			// clear the input with a second Ctrl+C once the selection is gone.
			// Hoisting this branch above the clear branch also stops the
			// previous behaviour where Ctrl+C would dismiss a selection AND
			// wipe any draft text the user was typing — felt like the
			// selection was being silently lost.
			if sel.active && !sel.empty() {
				m.sel = sel // restore so selectedText() can read it
				text := m.selectedText()
				m.sel = selection{}
				cmds = append(cmds, m.copySelectionWithNotice(text))
				return m, finalize(m, cmds)
			}
			// No selection: if the composer has text, a single press clears it
			// (like Esc); on an empty composer a double-press within 1.5s quits.
			if strings.TrimSpace(m.input.Value()) != "" {
				m.input.Reset()
				m.pastedBlocks = nil
				m.lastCtrlCAt = time.Time{}
				return m, nil
			}
			if !m.lastCtrlCAt.IsZero() && time.Since(m.lastCtrlCAt) < 1500*time.Millisecond {
				return m, tea.Quit
			}
			m.lastCtrlCAt = time.Now()
			m.notice(i18n.M.CtrlCQuitHint)
			return m, finalize(m, nil)
		case "ctrl+d":
			return m, tea.Quit
		case "ctrl+l":
			if m.state != tuiRunning {
				m.finalizeStreamed()
				m.clearTranscriptDisplay()
				m.commitTranscriptSource(transcriptSource{kind: transcriptSourceBanner})
				m.transcriptDirty = true
				m.forceGotoBottom = true
				m.notice(i18n.M.SlashClsDone)
			}
			return m, finalize(m, cmds)
		case "ctrl+y", "super+y", "meta+y":
			m.toggleYoloMode()
			return m, nil
		case "ctrl+o":
			m.toggleVerboseReasoning(m.state != tuiRunning)
			return m, finalize(m, cmds)
		case "ctrl+b":
			m.toggleShellOutput()
			return m, finalize(m, cmds)
		case "shift+tab":
			// Shift+Tab toggles Plan only. Tool approval stays on its own axis:
			// Ask/Auto are explicit choices, and YOLO is a separate Ctrl+Y toggle.
			m.cycleMode()
			return m, nil
		case "enter":
			if m.state == tuiRunning {
				line := strings.TrimSpace(m.input.Value())
				if line == "" {
					m.viewport.GotoBottom()
					return m, nil
				}
				if m.queueEditCursor >= 0 && m.queueEditCursor < len(m.pendingInterject) {
					// Save the edited text back to the queue slot.
					m.pendingInterject[m.queueEditCursor] = m.expandPastedBlocks(line)
					m.notice(fmt.Sprintf("queue [%d] updated", m.queueEditCursor+1))
					m.queueEditCursor = -1
					m.queueEditDraft = ""
				} else {
					m.pendingInterject = append(m.pendingInterject, m.expandPastedBlocks(line))
					m.notice("feedback queued — will send when the current turn finishes")
					m.queueEditCursor = -1
					m.queueEditDraft = ""
				}
				m.input.Reset()
				m.pastedBlocks = nil
				return m, finalize(m, cmds)
			}
			if m.modelSwitchPending {
				return m, nil // ignore Enter while /model switch is building
			}
			line := strings.TrimSpace(m.input.Value())

			if line == "" {
				m.viewport.GotoBottom()
				return m, nil
			}
			if line == "exit" || line == "quit" || line == ":q" {
				return m, tea.Quit
			}
			m.rememberSubmittedInput(line)

			// "# <note>" quick-adds a memory line locally, no model turn. The
			// space keeps "#7" / "#issue" prompts from being swallowed.
			if note, ok := control.MemoryQuickAddNote(line); ok {
				m.input.Reset()
				m.pastedBlocks = nil
				if note == "" {
					m.notice(i18n.M.QuickRememberEmpty)
				} else if path, err := m.ctrl.QuickAdd(memory.ScopeProject, note); err != nil {
					m.notice("memory: " + err.Error())
				} else {
					m.notice(fmt.Sprintf(i18n.M.QuickRememberDoneFmt, path))
				}
				return m, finalize(m, cmds)
			}

			// "!<cmd>" runs a shell command directly, bypassing the model.
			if strings.HasPrefix(line, "!") {
				cmd := strings.TrimPrefix(line, "!")
				if strings.TrimSpace(cmd) == "" {
					m.input.Reset()
					m.pastedBlocks = nil
					m.notice(i18n.M.ShellExecEmpty)
					return m, finalize(m, cmds)
				}
				m.input.Reset()
				m.pastedBlocks = nil
				m.state = tuiRunning
				m.runStart = time.Now()
				m.elapsed = 0
				m.turnTokens = 0
				m.pendingRestore = line
				m.bubbleStartIdx = len(m.transcript)
				m.commitLine("")
				m.commitTranscriptSource(transcriptSource{
					kind: transcriptSourceUser, raw: line, planMode: m.planMode,
				})
				m.bubblePending = true
				m.turnDiscarded = false
				m.confirmBubbleSent() // shell events arrive instantly
				m.ctrl.RunShell(cmd)
				return m, tea.Batch(m.spinner.Tick, elapsedTick())
			}

			// Slash commands run locally without going through the model. A
			// '/'-leading line that's actually a dragged file path is an attachment,
			// not a command, so it's rewritten to an @reference instead.
			if control.SlashCodeCommentLine(line) {
				// Slash-prefixed code comments are prompt text, not commands.
				// Not a command. Fall through to normal message path.
			} else if strings.HasPrefix(line, "/") {
				if ref, ok := control.FileRefLine(line); ok {
					line = ref
				} else {
					m.input.Reset()
					m.pastedBlocks = nil
					cmds = append(cmds, m.runSlashCommand(line))
					return m, finalize(m, cmds)
				}
			}

			sentLine := m.expandPastedBlocks(line)
			m.input.Reset()

			// @references (local files / MCP resources, including inline image
			// attachments) are resolved off the event loop by the controller; the turn
			// starts when they resolve (refsResolvedMsg).
			if m.ctrl.HasRefs(sentLine) {
				cmds = append(cmds, m.resolveRefs(sentLine, sentLine, line))
				return m, finalize(m, cmds)
			}

			// Keep the expanded paste content as the raw turn, not the folded label,
			// so downstream consumers never see just the placeholder label.
			cmds = append(cmds, m.startTurnWithRaw(sentLine, sentLine, line, sentLine))
			return m, finalize(m, cmds)
		}

	case agentEventMsg:
		e := event.Event(msg)
		m.ingestEvent(e)
		turnDone := e.Kind == event.TurnDone
		gitMaybeChanged := e.Kind == event.ToolResult && !e.Tool.ReadOnly
		// Coalesce a burst: the goroutine that produced this event has already
		// exited (a Cmd reads the channel once), so it's safe to drain the events
		// already buffered and ingest them now. One re-wrap then covers the whole
		// batch instead of one per event — bounds the O(transcript) re-render cost
		// when bash output or reasoning floods in. Capped so a sustained flood
		// still yields to render periodically.
	drain:
		for drained := 0; drained < maxEventDrain; drained++ {
			select {
			case e2 := <-m.eventCh:
				m.ingestEvent(e2)
				if e2.Kind == event.TurnDone {
					turnDone = true
				}
				if e2.Kind == event.ToolResult && !e2.Tool.ReadOnly {
					gitMaybeChanged = true
				}
			default:
				break drain
			}
		}
		cmds = append(cmds, waitForAgentEvent(m.eventCh))
		// A turn just spent tokens (and money) — refresh the balance readout and
		// the custom status line (its context/cost inputs just changed).
		if turnDone {
			cmds = append(cmds, fetchBalance(m.ctrl))
			if c := m.runStatusline(); c != nil {
				cmds = append(cmds, c)
			}
			if len(m.pendingInterject) > 0 {
				interject := m.pendingInterject[0]
				m.pendingInterject = m.pendingInterject[1:]
				// Reset queue navigation — the indices shifted.
				m.queueEditCursor = -1
				m.queueEditDraft = ""
				cmds = append(cmds, m.startTurn(interject, interject, interject))
			}
		}
		if turnDone || gitMaybeChanged {
			if c := m.refreshGitStatus(); c != nil {
				cmds = append(cmds, c)
			}
		}

	case balanceMsg:
		m.balance = msg.text

	case statuslineMsg:
		m.statuslineOut = msg.out

	case gitStatusMsg:
		m.gitStatus = msg.status

	case compactDoneMsg:
		if msg.err != nil {
			m.notice(fmt.Sprintf("%s: %v", i18n.M.SlashCompactFailed, msg.err))
		} else {
			_ = m.ctrl.Snapshot()
			m.followSessionLease()
		}

	case tuiShutdownMsg:
		if m.ctrl != nil {
			_ = m.ctrl.Snapshot()
			m.followSessionLease()
		}
		return m, tea.Quit

	case modelSwitchMsg:
		m.modelSwitchPending = false
		m.pendingModelSwitch = nil
		if msg.err != nil {
			prefix := msg.failurePrefix
			if prefix == "" {
				prefix = "model"
			}
			m.notice(prefix + ": " + msg.err.Error())
			// Build failed — no old controller to retire. The kept controller
			// may still have been retargeted to a recovery branch by the
			// pre-switch snapshot, so the lease must follow it.
			m.followSessionLease()
		} else {
			m.ctrl = msg.ctrl
			m.label = msg.label
			m.commands = msg.commands
			m.skills = msg.skills
			m.host = msg.host
			m.modelRef = msg.ref
			if msg.profile != "" {
				m.runtimeProfile = msg.profile
			}
			m.refreshEffortStatus()
			// Stash the old controller for cleanup at exit. It cannot be
			// closed here or in the build goroutine — Close() runs
			// SessionEnd hooks and kills plugin subprocesses, both of
			// which corrupt bubbletea's terminal raw mode.
			if msg.oldCtrl != nil {
				m.oldControllers = append(m.oldControllers, msg.oldCtrl)
			}
			// The lease follows the controller's session file. Normally a
			// no-op (a carried conversation keeps its file); it moves when
			// the pre-switch snapshot recovered onto a recovery branch — a
			// fresh file created by this process, so failure is theoretical.
			m.followSessionLease()
			if msg.successNotice != "" {
				m.notice(msg.successNotice)
			} else {
				m.notice(fmt.Sprintf(i18n.M.ModelSwitchedFmt, m.label))
			}
			cmds = append(cmds, fetchBalance(m.ctrl))
			if c := m.runStatusline(); c != nil {
				cmds = append(cmds, c)
			}
			// Do NOT re-issue waitForAgentEvent here — the goroutine from the
			// last agentEventMsg handler is still blocked on the same channel.
			// Starting a second one creates a race: two goroutines compete on
			// p.Send (unbuffered), and the receiver may read them out of order,
			// garbling the streamed text (words appear reordered).
		}

	case promptResolvedMsg:
		switch {
		case msg.err != nil:
			m.commitLine(wrapForViewport(i18n.M.ErrorPrefix+" "+msg.err.Error(), m.width, activeCLITheme.warn))
		case strings.TrimSpace(msg.sent) == "":
			m.notice(i18n.M.SlashPromptEmpty)
		default:
			cmds = append(cmds, m.startTurn(msg.sent, msg.display, msg.display))
		}

	case mcpExternalDoneMsg:
		if msg.err != nil {
			m.notice(msg.label + ": " + msg.err.Error())
		} else if msg.target != "" {
			m.notice(msg.label + ": " + msg.target)
		}

	case refsResolvedMsg:
		for _, e := range msg.errs {
			m.notice(e) // surface a fetch failure but still send the turn
		}
		sent := msg.sent
		if msg.block != "" {
			sent = "Referenced context:\n\n" + msg.block + "\n\n" + msg.sent
		}
		// raw = msg.display (the expanded paste content, without resolved @-ref
		// payloads) — NOT msg.restore (the folded label). See the non-refs branch
		// above for why raw needs the expansion.
		cmds = append(cmds, m.startTurnWithRaw(sent, msg.display, msg.restore, msg.display))

	case clipboardImageMsg:
		m.clipboardImagePending = false
		if msg.err != nil {
			m.notice(fmt.Sprintf(i18n.M.ClipboardImagePasteFailedFmt, msg.err))
			break
		}
		imageBefore := m.input.Value()
		m.insertImageRef(msg.path)
		if shouldClearWideInputChange(imageBefore, m.input.Value()) {
			cmds = append(cmds, tea.ClearScreen)
		}

	case clipboardTextPasteMsg:
		if msg.remote {
			m.notice(i18n.M.ClipboardTextPasteRemoteHint)
			break
		}
		if msg.err != nil {
			m.notice(fmt.Sprintf(i18n.M.ClipboardTextPasteFailedFmt, msg.err))
			break
		}
		if msg.text == "" {
			break
		}
		// Re-enter through the canonical paste path so selection replacement,
		// folded blocks, file references, completion, and wide-cell repainting
		// behave exactly like the terminal's bracketed-paste event.
		return m.update(tea.PasteMsg{Content: msg.text})

	case clipboardCopyMsg:
		if msg.statusHint && msg.seq != m.copyNoticeSeq {
			break
		}
		label := i18n.M.MouseCopiedHint
		if !msg.statusHint {
			label = i18n.M.SlashCopyDone
		}
		if msg.osc52 || msg.err != nil {
			label = i18n.M.ClipboardCopyOSC52Hint
			if msg.err != nil {
				label = i18n.M.ClipboardCopyFallbackHint
			}
			cmds = append(cmds, tea.SetClipboard(msg.text))
		}
		if msg.statusHint {
			m.copyNoticeText = label
			cmds = append(cmds, copyNoticeExpire(msg.seq))
		} else {
			m.notice(label)
		}

	case copyNoticeExpireMsg:
		if msg.seq == m.copyNoticeSeq {
			m.copyNoticeText = ""
		}

	case elapsedTickMsg:
		if m.state == tuiRunning {
			m.elapsed = int(time.Since(m.runStart).Seconds())
			m.tickToolRunning()
			cmds = append(cmds, elapsedTick())
		}

	case spinner.TickMsg:
		if m.state == tuiRunning {
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(msg)
			cmds = append(cmds, cmd)
		}
	}

	beforeInput := m.input.Value()
	if inputBeforeSelection != "" {
		beforeInput = inputBeforeSelection
	}
	var ic tea.Cmd
	m.input, ic = m.input.Update(msg)
	cmds = append(cmds, ic)
	m.growInputToFit()
	// Re-filter the autocomplete menu against the freshly-edited input.
	if _, ok := msg.(tea.KeyPressMsg); ok {
		m.updateCompletion()
	}
	if shouldClearWideInputChange(beforeInput, m.input.Value()) {
		cmds = append(cmds, tea.ClearScreen)
	}

	return m, finalize(m, cmds)
}

var clearWideInputChanges = runtime.GOOS == "windows"

func shouldClearWideInputChange(before, after string) bool {
	return clearWideInputChanges &&
		before != after &&
		(hasWideInputCells(before) || hasWideInputCells(after))
}

func hasWideInputCells(s string) bool {
	return s != "" && visibleWidth(s) != utf8.RuneCountInString(s)
}

// finalize drains the committed-line queue and batches the turn's commands. In
// the default alt-screen path the queue is already mirrored in m.transcript. In
// Termux finalized lines are also emitted into the terminal's native scrollback.
func finalize(m chatTUI, cmds []tea.Cmd) tea.Cmd {
	if m.nativeScrollback && len(*m.pendingCommit) > 0 {
		out := strings.TrimRight(clampWidth(strings.Join(*m.pendingCommit, "\n"), m.width), "\n")
		*m.pendingCommit = (*m.pendingCommit)[:0]
		var prints []tea.Cmd
		for _, chunk := range chunkLines(out, m.scrollChunkHeight()) {
			prints = append(prints, tea.Println(chunk))
		}
		cmds = append(cmds, tea.Sequence(prints...))
		return tea.Batch(cmds...)
	}
	*m.pendingCommit = (*m.pendingCommit)[:0]
	return tea.Batch(cmds...)
}

func (m *chatTUI) clearTranscriptDisplay() {
	if m.pendingCommit != nil {
		*m.pendingCommit = (*m.pendingCommit)[:0]
	}
	m.transcript = nil
	m.transcriptSources = nil
	m.wrappedLines = nil
	m.viewport.SetContent("")
	m.shellOutputs = make(map[string]string)
	m.shellExpanded = make(map[string]bool)
	m.shellTranscriptIdx = make(map[string]int)
	m.toolLineCountByID = make(map[string]int)
	m.toolStreamID = ""
	m.toolStreamIdx = -1
	m.toolTail = nil
	m.toolPartial = ""
	m.toolLineCount = 0
}

// scrollChunkHeight is the largest block (in lines) finalize prints at once in
// native-scrollback mode, leaving room for the pinned bottom frame.
func (m chatTUI) scrollChunkHeight() int {
	if m.height <= 0 {
		return 100
	}
	if n := m.height - m.bottomRows(); n > 1 {
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
	m.appendTranscriptBlock(s, transcriptSource{kind: transcriptSourceFixed})
}

// commitSpacer separates the next block (a thinking marker or a tool line) from
// the previous one with a single blank line, skipping it at the top of the
// transcript or when a blank already trails so spacers never double up.
func (m *chatTUI) commitSpacer() {
	if n := len(m.transcript); n > 0 && strings.TrimSpace(m.transcript[n-1]) != "" {
		m.commitLine("")
	}
}

// bottomRows is the terminal-row height of the pinned bottom region: any open
// bottom panels (todo / approval / chooser / rewind / completion), the composer
// when visible, and the two fixed status rows. Full-screen managers such as MCP
// and skills normally render inside the main transcript area; in native
// scrollback mode they join the bottom rail because there is no main viewport.
func (m chatTUI) bottomRows() int {
	rows := 0
	for _, s := range []string{
		m.renderTodoPanel(),
		m.renderApprovalBanner(),
		m.renderChooser(),
		m.renderRewind(),
		m.renderMCPImport(),
		m.renderResumePicker(),
		m.renderQuickPicker(),
		m.renderCopyPicker(),
		m.renderCompletion(),
	} {
		if s != "" {
			rows += strings.Count(s, "\n") + 1
		}
	}
	// Remove the hardcoded working-line increment — it is counted inside
	// statusLineCount via computeStatusLineCount, which also accounts for
	// wrapping. The fallback to 2 (unwrapped) covers the initial frame and
	// tests that don't call Update first.
	if m.nativeScrollback {
		if main := m.renderMainManager(); main != "" {
			rows += strings.Count(main, "\n") + 1
		}
	}
	if footer := m.renderMainManagerFooter(); footer != "" {
		rows += strings.Count(footer, "\n") + 1
	}
	if !m.hideComposer() {
		rows += m.input.Height() + 2
	}
	if m.statusLineCount > 0 {
		return rows + m.statusLineCount
	}
	return rows + 2 // fallback for tests that don't set statusLineCount
}

// hideComposer is the single ownership gate for the bottom composer.
//
// Rule for new CLI panels:
//   - If a panel is modal and keystrokes navigate/confirm/cancel the panel, hide
//     the composer so users do not see an inactive chat input.
//   - If a panel is input-owned (autocomplete, or chooser free-text mode), keep
//     the composer visible because the textarea is the active control.
//
// Whenever a new slash-command overlay or approval-style prompt is added, update
// this function and the modal layout tests together. Otherwise the panel may
// reserve rows for a composer that cannot receive input, leaving a confusing
// blank/bordered area at the bottom of the TUI.
func (m chatTUI) hideComposer() bool {
	if m.mcp != nil || m.clearConfirm != nil || m.mcpImport != nil || m.skillPick != nil || m.resumePick != nil || m.quickPick != nil || m.copyPick != nil || m.rewind != nil || m.pendingApproval != nil {
		return true
	}
	return m.chooser != nil && !m.chooser.typing
}

// transcriptHeight is the row budget left for the transcript viewport once the
// pinned bottom region is accounted for (at least one row).
func (m chatTUI) transcriptHeight() int {
	if h := m.height - m.bottomRows(); h > 1 {
		return h
	}
	return 1
}

func (m chatTUI) renderMainManager() string {
	if card := m.renderMCPManager(); card != "" {
		return card
	}
	if card := m.renderClearConfirm(); card != "" {
		return card
	}
	return m.renderSkillPicker()
}

func managerContentPanelStyle(width int) lipgloss.Style {
	return choicePanelStyle.
		Border(lipgloss.NormalBorder(), true, false, false, false).
		Width(width)
}

func managerFooterPanelStyle(width int) lipgloss.Style {
	return choicePanelStyle.
		Border(lipgloss.NormalBorder(), false, false, true, false).
		Width(width)
}

func (m chatTUI) renderMainManagerFooter() string {
	hint := ""
	switch {
	case m.mcp != nil:
		hint = m.mcp.footerHint()
	case m.clearConfirm != nil:
		hint = "Enter confirm · y clear · n/Esc cancel"
	case m.skillPick != nil:
		hint = m.skillPickerFooterHint()
	}
	if strings.TrimSpace(hint) == "" {
		return ""
	}
	w := max(viewWidth(m.width), 40)
	return managerFooterPanelStyle(w).Render(dim(hint))
}

func (m chatTUI) renderTranscriptWithMainManager(card string) string {
	h := m.viewport.Height()
	if h <= 0 {
		return ""
	}
	cw := m.viewport.Width()
	if cw <= 0 {
		cw = max(m.width-1, 1)
	}

	cardLines := strings.Split(strings.TrimRight(card, "\n"), "\n")
	if len(cardLines) > h {
		cardLines = cardLines[:h]
	}
	maxTranscriptRows := h - len(cardLines)
	if maxTranscriptRows > 0 && len(cardLines) > 0 && len(m.wrappedLines) > 0 {
		maxTranscriptRows--
	}

	var rows []string
	if maxTranscriptRows > 0 {
		lines := m.wrappedLines
		start := max(0, len(lines)-maxTranscriptRows)
		rows = append(rows, lines[start:]...)
	}
	if len(rows) > 0 && len(cardLines) > 0 {
		rows = append(rows, "")
	}
	rows = append(rows, cardLines...)
	for len(rows) < h {
		rows = append(rows, "")
	}
	for i, row := range rows {
		rows[i] = padRight(ansi.Cut(row, 0, cw), cw)
	}
	return strings.Join(rows, "\n")
}

// reasoningViewMax bounds the live thinking buffer the streamed block renders
// from. Re-rendering the full chain of thought on every delta was O(n²) (a 2k-
// token thought churned ~4.7GB); rendering only the trailing window keeps each
// delta O(1). The full text still lives in m.reasoning for verbose mode.
const reasoningViewMax = 4096

// reasoningTailLines caps how many trailing visual lines the live block shows.
const reasoningTailLines = 12

// streamReasoning appends a chunk and rewrites the live reasoning block from a
// bounded trailing view (mirrors streamToolOutput), so the chain of thought is
// visible while the model works without re-rendering the whole thing per token.
func (m *chatTUI) streamReasoning(chunk string) {
	m.reasoning.WriteString(chunk) // full text retained for verbose mode
	if m.reasoningTextIdx < 0 {
		return
	}
	m.reasoningView = append(m.reasoningView, chunk...)
	if len(m.reasoningView) > reasoningViewMax {
		drop := len(m.reasoningView) - reasoningViewMax
		for drop < len(m.reasoningView) && !utf8.RuneStart(m.reasoningView[drop]) {
			drop++
		}
		m.reasoningView = m.reasoningView[:copy(m.reasoningView, m.reasoningView[drop:])]
	}
	raw := string(m.reasoningView)
	m.setTranscriptBlock(m.reasoningTextIdx, reasoningBlock(raw, m.width, reasoningTailLines), transcriptSource{
		kind: transcriptSourceReasoning, raw: raw, maxLines: reasoningTailLines,
	})
	m.transcriptDirty = true
}

// reasoningBlock renders raw thinking text as dim, width-wrapped lines under a
// "⎿" connector that ties the block to the "▎ thinking…" marker above it. A
// positive maxLines keeps only the trailing visual lines (the live view); 0
// renders all (verbose collapse).
func reasoningBlock(raw string, width, maxLines int) string {
	w := width - len([]rune(connector))
	if w < 8 {
		w = 8
	}
	var lines []string
	for _, ln := range strings.Split(strings.TrimRight(raw, "\n"), "\n") {
		for _, wl := range strings.Split(ansi.Wrap(expandTabs(ln), w, ""), "\n") {
			lines = append(lines, dim(wl))
		}
	}
	if maxLines > 0 && len(lines) > maxLines {
		lines = lines[len(lines)-maxLines:]
	}
	return connectorBlock(lines)
}

// toolStreamTailLines caps how many trailing output lines a running tool shows;
// the live block scrolls within this window so a chatty build doesn't flood.
const toolStreamTailLines = 8

// shellPreviewLines is how many lines of shell output to show by default after
// the command finishes. Ctrl+B toggles the full output.
const shellPreviewLines = 10

// shellExpandMaxLines caps how many lines Ctrl+B shows in expanded mode, so a
// very large output (e.g. thousands of lines) doesn't hang the TUI or push the
// input box off-screen.
const shellExpandMaxLines = 200

// streamToolOutput appends a chunk of a running tool's output and re-renders its
// live block (the last toolStreamTailLines lines) under the tool card, opening
// the block on the first chunk. Mirrors streamReasoning.
func (m *chatTUI) streamToolOutput(id, chunk string) {
	if id == "" {
		return
	}
	if m.toolStreamID != id {
		// Switching to a different id means either:
		//   (a) the previous tool finished and a new one is starting — collapse
		//       the current id's live block, then append a fresh slot at the
		//       end of the transcript.
		//   (b) late ToolProgress for an earlier (already dispatched and
		//       possibly collapsed) tool — reuse the slot beginToolRunning
		//       already wrote for that id, so the live block stays directly
		//       under the earlier tool's card rather than stacking at the end.
		if existingIdx, ok := m.shellTranscriptIdx[id]; ok && existingIdx >= 0 && existingIdx < len(m.transcript) {
			// Stash the switched-away id's live count before resetting it;
			// its late ToolResult reads it back via toolLineCountByID.
			if m.toolStreamID != "" && m.toolStreamID != id {
				n := m.toolLineCount
				if m.toolPartial != "" {
					n++
				}
				if n > 0 {
					m.toolLineCountByID[m.toolStreamID] = n
				}
			}
			m.toolStreamID = id
			m.toolStreamIdx = existingIdx
			m.toolTail = m.toolTail[:0]
			m.toolPartial = ""
			m.toolLineCount = 0
		} else {
			// Unknown id: collapse the active stream (its live count is intact).
			m.collapseToolOutput(m.toolStreamID, "")
			m.toolStreamID = id
			m.toolTail = m.toolTail[:0]
			m.toolPartial = ""
			m.toolLineCount = 0
			if m.nativeScrollback {
				m.toolStreamIdx = -1
			} else {
				m.toolStreamIdx = len(m.transcript)
				m.commitLine("")
			}
		}
	}
	// Accumulate full output for shell commands so Ctrl+B can expand it.
	if strings.HasPrefix(id, "shell-") {
		m.shellOutputs[id] += chunk
	}
	// Fold completed lines into the bounded tail; keep the trailing partial.
	data := m.toolPartial + chunk
	for {
		i := strings.IndexByte(data, '\n')
		if i < 0 {
			break
		}
		m.pushToolLine(strings.TrimRight(data[:i], "\r"))
		data = data[i+1:]
	}
	m.toolPartial = data

	vis := m.toolTail
	if m.toolPartial != "" {
		vis = append(append([]string{}, m.toolTail...), m.toolPartial)
	}
	if m.nativeScrollback {
		return
	}
	lines := make([]string, len(vis))
	for i, ln := range vis {
		lines[i] = dim(clampPlain(ln, m.width-len([]rune(connector))))
	}
	m.transcript[m.toolStreamIdx] = connectorBlock(lines)
	m.transcriptDirty = true
}

// pushToolLine appends a completed output line to the bounded tail, dropping the
// oldest when it exceeds the window (the backing array stays ≤ window+1).
func (m *chatTUI) pushToolLine(line string) {
	m.toolLineCount++
	m.toolTail = append(m.toolTail, line)
	if len(m.toolTail) > toolStreamTailLines {
		copy(m.toolTail, m.toolTail[1:])
		m.toolTail = m.toolTail[:toolStreamTailLines]
	}
}

// collapseToolOutput replaces a finished tool's live block with a dim
// "⎿ N lines" summary, so the scrollback keeps a marker of the run without the
// full output (which the model already received). For shell commands ("shell-"
// prefix), it shows the first shellPreviewLines with a Ctrl+B hint instead.
// No-op when id isn't streaming. resultOutput (the ToolResult's final output)
// is the last-resort line-count source when the live state was already reset.
func (m *chatTUI) collapseToolOutput(id, resultOutput string) {
	if m.nativeScrollback {
		if id == "" || m.toolStreamID != id {
			return
		}
		n := m.toolLineCount
		if m.toolPartial != "" {
			n++
		}
		if n > 0 {
			if full, ok := m.shellOutputs[id]; ok {
				lines := strings.Split(strings.TrimRight(full, "\n"), "\n")
				total := len(lines)
				if total > shellPreviewLines {
					preview := make([]string, shellPreviewLines+1)
					for i := 0; i < shellPreviewLines; i++ {
						preview[i] = dim(clampPlain(lines[i], m.width-len([]rune(connector))))
					}
					preview[shellPreviewLines] = dim(fmt.Sprintf("… %d more lines (Ctrl+B)", total-shellPreviewLines))
					m.commitLine(connectorBlock(preview))
				} else {
					rendered := make([]string, total)
					for i, ln := range lines {
						rendered[i] = dim(clampPlain(ln, m.width-len([]rune(connector))))
					}
					m.commitLine(connectorBlock(rendered))
				}
				m.shellTranscriptIdx[id] = len(m.transcript) - 1
			} else {
				m.commitLine(connectorBlock([]string{dim(fmt.Sprintf("%d lines", n))}))
			}
		}
		m.toolStreamIdx = -1
		m.toolStreamID = ""
		m.toolTail = m.toolTail[:0]
		m.toolPartial = ""
		m.toolLineCount = 0
		return
	}
	if m.toolStreamIdx < 0 || id == "" || m.toolStreamID != id {
		// Slot no longer active (another tool took over, or this id never
		// streamed). If beginToolRunning recorded a transcript index, collapse
		// in place so a late ToolResult doesn't leave raw streamed text behind.
		if idx, ok := m.shellTranscriptIdx[id]; ok && idx >= 0 && idx < len(m.transcript) {
			m.collapseShellSlot(id, idx, resultOutput)
		}
		return
	}
	m.collapseShellSlot(id, m.toolStreamIdx, resultOutput)
	m.toolStreamIdx = -1
	m.toolStreamID = ""
	m.toolTail = m.toolTail[:0]
	m.toolPartial = ""
	m.toolLineCount = 0
}

// collapseShellSlot finalises a tool's live block at idx. Used both by the
// active-tool path (idx == toolStreamIdx, streaming state intact) and the
// late-result path (idx recorded in shellTranscriptIdx at dispatch). Line-count
// sources, in order: live streaming state, shellOutputs ("shell-" ids only),
// the per-id count stashed by streamToolOutput, then the ToolResult's output.
func (m *chatTUI) collapseShellSlot(id string, idx int, resultOutput string) {
	m.transcriptDirty = true
	n := -1
	if id == m.toolStreamID {
		// Prefer the larger of the live count and resultOutput: resultOutput
		// is the authoritative end-state, the live state may lag behind it.
		n = m.toolLineCount
		if m.toolPartial != "" {
			n++
		}
		if resultOutput != "" {
			fromResult := len(strings.Split(strings.TrimRight(resultOutput, "\n"), "\n"))
			if fromResult > n {
				n = fromResult
			}
		}
	}
	if n < 0 {
		if full, ok := m.shellOutputs[id]; ok {
			n = len(strings.Split(strings.TrimRight(full, "\n"), "\n"))
		} else if c, ok := m.toolLineCountByID[id]; ok {
			n = c
		} else if resultOutput != "" {
			n = len(strings.Split(strings.TrimRight(resultOutput, "\n"), "\n"))
		}
	}
	if n < 0 {
		// Nothing applies (e.g. a late result for a non-"shell-" id that never
		// streamed): treat as zero rather than fabricate a "-1 lines" count.
		n = 0
	}
	if n == 0 {
		// Tool finished with no output: clear the "working…" placeholder but
		// keep the slot (shellTranscriptIdx still points here for late progress).
		m.transcript[idx] = ""
		return
	}
	if full, ok := m.shellOutputs[id]; ok {
		// Shell command: show first N lines + hint.
		lines := strings.Split(strings.TrimRight(full, "\n"), "\n")
		total := len(lines)
		if total > shellPreviewLines {
			preview := make([]string, shellPreviewLines+1)
			for i := 0; i < shellPreviewLines; i++ {
				preview[i] = dim(clampPlain(lines[i], m.width-len([]rune(connector))))
			}
			preview[shellPreviewLines] = dim(fmt.Sprintf("… %d more lines (Ctrl+B)", total-shellPreviewLines))
			m.transcript[idx] = connectorBlock(preview)
		} else {
			rendered := make([]string, total)
			for i, ln := range lines {
				rendered[i] = dim(clampPlain(ln, m.width-len([]rune(connector))))
			}
			m.transcript[idx] = connectorBlock(rendered)
		}
	} else {
		m.transcript[idx] = connectorBlock([]string{dim(fmt.Sprintf("%d lines", n))})
	}
	m.shellTranscriptIdx[id] = idx
}

// toggleShellOutput expands or collapses the output of the most recent shell
// command. When expanded, up to shellExpandMaxLines lines are shown; when
// collapsed, only the first shellPreviewLines are shown. Called on Ctrl+B.
func (m *chatTUI) toggleShellOutput() {
	// Find the most recent shell output that has a transcript entry.
	var lastID string
	lastIdx := -1
	for id, idx := range m.shellTranscriptIdx {
		if idx >= 0 && idx < len(m.transcript) && idx > lastIdx {
			lastID = id
			lastIdx = idx
		}
	}
	if lastID == "" {
		return
	}
	full, ok := m.shellOutputs[lastID]
	if !ok {
		return
	}
	lines := strings.Split(strings.TrimRight(full, "\n"), "\n")
	total := len(lines)
	innerW := m.width - len([]rune(connector))
	if innerW < 10 {
		innerW = 80
	}

	if m.shellExpanded[lastID] {
		// Collapse back to preview.
		m.shellExpanded[lastID] = false
		if total > shellPreviewLines {
			preview := make([]string, shellPreviewLines+1)
			for i := 0; i < shellPreviewLines; i++ {
				preview[i] = dim(clampPlain(lines[i], innerW))
			}
			preview[shellPreviewLines] = dim(fmt.Sprintf("… %d more lines (Ctrl+B)", total-shellPreviewLines))
			m.transcript[lastIdx] = connectorBlock(preview)
		}
	} else {
		// Expand: show up to shellExpandMaxLines lines.
		m.shellExpanded[lastID] = true
		show := total
		if show > shellExpandMaxLines {
			show = shellExpandMaxLines
		}
		rendered := make([]string, show)
		for i := 0; i < show; i++ {
			rendered[i] = dim(clampPlain(lines[i], innerW))
		}
		if total > shellExpandMaxLines {
			rendered = append(rendered, dim(fmt.Sprintf("… %d more lines", total-shellExpandMaxLines)))
		}
		m.transcript[lastIdx] = connectorBlock(rendered)
	}
	m.transcriptDirty = true
	if m.nativeScrollback {
		m.commitLine(m.transcript[lastIdx])
	}
}

// toolWorkingFrames is the braille spinner cycled once per second on the
// "⎿ working · Ns" line of a tool that hasn't streamed output yet.
var toolWorkingFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// beginToolRunning opens an empty live block under a just-dispatched tool card,
// keyed by the call id. tickToolRunning fills it with a "working · Ns" line each
// second; if the tool later streams output, streamToolOutput reuses the same
// block; collapseToolOutput closes it on the result.
func (m *chatTUI) beginToolRunning(id string) {
	if id == "" {
		return
	}
	m.toolStreamID = id
	m.toolTail = m.toolTail[:0]
	m.toolPartial = ""
	m.toolLineCount = 0
	// Clear accumulated output for this tool ID so a re-run (e.g. repeated
	// !pwd with the same "shell-pwd" id) doesn't append to old output.
	delete(m.shellOutputs, id)
	m.toolStreamStart = time.Now()
	m.toolStreamFrame = 0
	if m.nativeScrollback {
		m.toolStreamIdx = -1
		return
	}
	m.toolStreamIdx = len(m.transcript)
	m.commitLine(connectorBlock([]string{dim(fmt.Sprintf(i18n.M.ChatToolWorkingFmt, toolWorkingFrames[0], 0))}))
	// Remember the transcript slot for this id so a late ToolProgress for a
	// previously dispatched (and possibly already collapsed) tool can reuse
	// it instead of appending a fresh slot at the end of the transcript. For
	// back-to-back tool calls this keeps each tool's live block directly
	// under its own card.
	m.shellTranscriptIdx[id] = m.toolStreamIdx
}

// tickToolRunning re-renders the working line of a tool that's dispatched but
// hasn't produced output yet. A no-op once output streams in or no tool runs.
func (m *chatTUI) tickToolRunning() {
	if m.nativeScrollback {
		return
	}
	if m.toolStreamIdx < 0 || m.toolLineCount != 0 || m.toolPartial != "" {
		return
	}
	m.toolStreamFrame++
	frame := toolWorkingFrames[m.toolStreamFrame%len(toolWorkingFrames)]
	secs := int(time.Since(m.toolStreamStart).Seconds())
	m.transcript[m.toolStreamIdx] = connectorBlock([]string{dim(fmt.Sprintf(i18n.M.ChatToolWorkingFmt, frame, secs))})
	m.transcriptDirty = true
}

// commitReasoning closes the live thinking block: the "▎ thinking…" marker is
// rewritten to a dim "▎ thought for Ns" summary and the streamed text below it is
// removed (collapsed) — kept only in verbose mode. The viewport re-wraps from
// m.transcript, so the change is flagged via transcriptDirty.
func (m *chatTUI) commitReasoning() {
	if m.reasoningNative {
		if strings.TrimSpace(m.reasoning.String()) != "" || !m.thinkStart.IsZero() {
			secs := int(time.Since(m.thinkStart).Seconds())
			m.commitSpacer()
			m.commitLine(dim(fmt.Sprintf("  ▎ "+i18n.M.ChatThoughtForFmt, secs)))
			if m.showReasoning && strings.TrimSpace(m.reasoning.String()) != "" {
				m.commitLine(reasoningBlock(m.reasoning.String(), m.width, 0))
			}
		}
		m.reasoning.Reset()
		m.reasoningView = m.reasoningView[:0]
		m.reasoningNative = false
		m.thinkStart = time.Time{}
		return
	}
	if m.reasoningLineIdx < 0 {
		return
	}
	secs := int(time.Since(m.thinkStart).Seconds())
	m.setTranscriptBlock(m.reasoningLineIdx, dim(fmt.Sprintf("  ▎ "+i18n.M.ChatThoughtForFmt, secs)), transcriptSource{kind: transcriptSourceFixed})
	if m.reasoningTextIdx >= 0 {
		if m.showReasoning && strings.TrimSpace(m.reasoning.String()) != "" {
			raw := m.reasoning.String()
			m.setTranscriptBlock(m.reasoningTextIdx, reasoningBlock(raw, m.width, 0), transcriptSource{
				kind: transcriptSourceReasoning, raw: raw,
			})
		} else {
			m.removeTranscriptBlock(m.reasoningTextIdx)
		}
	}
	m.transcriptDirty = true
	m.reasoning.Reset()
	m.reasoningView = m.reasoningView[:0]
	m.reasoningLineIdx = -1
	m.reasoningTextIdx = -1
}

// commitReasoningBeforeAnswer closes a real reasoning block and leaves exactly
// one blank transcript row before the assistant answer. Answers that start
// without reasoning keep their existing compact placement.
func (m *chatTUI) commitReasoningBeforeAnswer() {
	hadReasoning := m.reasoningNative || m.reasoningLineIdx >= 0
	m.commitReasoning()
	if hadReasoning {
		m.commitSpacer()
	}
}

// streamAnswer renders the answer streamed so far up to its last completed
// paragraph (flushableMarkdownPrefix) and writes it as one transcript block,
// rewritten in place as later paragraphs land — so a long reply appears chunk by
// chunk instead of all at once on turn end. The trailing, still-streaming block
// stays buffered (a half-written fence/list never renders early), and it only
// re-renders when a new paragraph actually closes.
func (m *chatTUI) streamAnswer() {
	if m.nativeScrollback {
		return
	}
	prefix := flushableMarkdownPrefix(m.pending.String())
	if len(prefix) <= m.answerFlushed {
		return
	}
	source := transcriptSource{kind: transcriptSourceMarkdown, raw: prefix}
	m.answerFlushed = len(prefix)
	if m.answerIdx < 0 {
		m.answerIdx = len(m.transcript)
		m.commitTranscriptSource(source)
	} else {
		block := m.renderTranscriptSource(source, m.width)
		m.setTranscriptBlock(m.answerIdx, block, source)
		m.transcriptDirty = true
	}
}

// commitPending freezes the full accumulated answer as markdown — overwriting the
// streamed block if one is open (streamAnswer), else committing fresh. Joining
// commitReasoning then commitPending puts the answer on its own line, restoring
// the thinking→answer break the renderer strips.
func (m *chatTUI) commitPending() {
	if m.pending.Len() == 0 {
		m.answerIdx = -1
		m.answerFlushed = 0
		return
	}
	raw := m.pending.String()
	source := transcriptSource{kind: transcriptSourceMarkdown, raw: raw}
	if m.answerIdx < 0 {
		m.commitTranscriptSource(source)
	} else {
		block := m.renderTranscriptSource(source, m.width)
		m.setTranscriptBlock(m.answerIdx, block, source)
		m.transcriptDirty = true
	}
	m.pending.Reset()
	m.answerIdx = -1
	m.answerFlushed = 0
}

// flushableMarkdownPrefix returns the longest prefix of buf made of complete
// markdown blocks — text up to the last blank line outside any open fenced code
// block. A blank line inside a ``` / ~~~ fence isn't a boundary, so a half-written
// code block stays buffered until it closes.
func flushableMarkdownPrefix(buf string) string {
	lines := strings.Split(buf, "\n")
	inFence := false
	boundary := -1
	for i, ln := range lines {
		t := strings.TrimSpace(ln)
		if strings.HasPrefix(t, "```") || strings.HasPrefix(t, "~~~") {
			inFence = !inFence
			continue
		}
		if !inFence && t == "" {
			boundary = i
		}
	}
	if boundary <= 0 {
		return ""
	}
	return strings.Join(lines[:boundary], "\n")
}

// planApprovalTool is the Tool name the controller puts on the ApprovalRequest it
// emits to gate a plan (mirrors control's constant). The banner, status line, and
// approval handler key on it to render the plan-specific prompt and to keep the
// [plan] tag in sync when the user starts execution or exits without executing.
const planApprovalTool = "exit_plan_mode"

type approvalChoice struct {
	label           string
	allow           bool
	allowForSession bool
	persistToConfig bool
	exitPlan        bool
}

func approvalChoices(a *event.Approval) []approvalChoice {
	if a == nil {
		return nil
	}
	var decisions []approvalChoice
	fresh := a.Fresh || control.RequiresFreshHumanApprovalTool(a.Tool)
	switch {
	case isRecoveryApprovalEvent(a):
		if a.Recovery != nil && a.Recovery.CanGrantTask {
			// allowForSession is reused only as a local UI marker. The recovery
			// handler maps it to a task-scoped semantic grant, never a session rule.
			decisions = []approvalChoice{{allow: true}, {allow: true, allowForSession: true}, {}}
		} else {
			decisions = []approvalChoice{{allow: true}, {}}
		}
	case a.Tool == planApprovalTool:
		decisions = []approvalChoice{{allow: true}, {}, {exitPlan: true}}
	case fresh && freshApprovalAllowsSession(a.Tool):
		decisions = []approvalChoice{{allow: true}, {allow: true, allowForSession: true}, {}}
	case fresh:
		decisions = []approvalChoice{{allow: true}, {}}
	default:
		decisions = []approvalChoice{
			{allow: true},
			{allow: true, allowForSession: true},
			{allow: true, allowForSession: true, persistToConfig: true},
			{},
		}
	}
	labels := approvalChoiceLabels(a)
	for i := range decisions {
		if i < len(labels) {
			decisions[i].label = labels[i]
		}
	}
	return decisions
}

func approvalChoiceLabels(a *event.Approval) []string {
	choices := i18n.M.FreshHumanApprovalChoices
	fresh := a.Fresh || control.RequiresFreshHumanApprovalTool(a.Tool)
	if isRecoveryApprovalEvent(a) {
		if isRecoveryPlanChangeApproval(a) {
			choices = i18n.M.RecoveryPlanChangeChoices
		} else {
			choices = i18n.M.RecoveryApprovalChoices
		}
		if !isRecoveryPlanChangeApproval(a) && a.Recovery != nil && a.Recovery.CanGrantTask {
			choices = i18n.M.RecoveryTaskGrantChoices
		}
	} else if a.Tool == planApprovalTool {
		choices = i18n.M.PlanApprovalChoices
	} else if !fresh {
		exactSessionRule := permission.SessionGrantRuleForScope(a.Tool, a.Subject)
		exactPersistentRule := permission.RememberRuleForScope(a.Tool, a.Subject)
		choices = fmt.Sprintf(i18n.M.ToolApprovalChoices, exactSessionRule, exactPersistentRule)
	}
	if a.Tool == control.SandboxEscapeApprovalTool {
		choices = i18n.M.SandboxEscapeApprovalChoices
	}
	if a.Tool == control.ManagedConfigWriteApprovalTool {
		choices = i18n.M.ConfigWriteApprovalChoices
	}
	if a.Tool == agent.PlanModeReadOnlyCommandApprovalTool {
		choices = i18n.M.PlanModeReadOnlyCommandChoices
	}
	if !fresh && a.Tool == "bash" && permission.BashCommandPrefix(a.Subject) != "" {
		prefixRule := permission.RememberRuleForScope(a.Tool, a.Subject)
		choices = fmt.Sprintf(i18n.M.BashPrefixChoices, prefixRule, prefixRule)
	}
	var labels []string
	for _, line := range strings.Split(choices, "\n") {
		line = strings.TrimSpace(line)
		if len(line) < 3 || line[0] < '1' || line[0] > '9' || line[1] != '.' {
			continue
		}
		labels = append(labels, strings.TrimSpace(line[2:]))
	}
	if isRecoveryApprovalEvent(a) && a.Recovery != nil && a.Recovery.CanGrantTask && len(labels) > 1 {
		if scope := strings.TrimSpace(a.Recovery.TaskGrantScope); scope != "" {
			labels[1] += " — " + scope
		}
	}
	return labels
}

// handleApprovalKey resolves a pending approval from a keystroke and re-arms the
// listener. 1/y/Enter allows once, 2/a allows for the rest of the session,
// 3/p writes an "always allow" rule to the config file for ordinary tool
// approvals. Fresh two-choice prompts use 2 for deny, while n/Esc and legacy 4
// still deny. Plan prompts use 1 to execute, 2/n/Esc to keep planning, and 3 to
// reject the pending plan and leave plan mode without executing it.
// Ctrl-C cancels the whole turn via the run context. For a plan approval
// (planApprovalTool), starting execution or explicitly exiting without execution
// drops the local [plan] tag and turns plan mode off on the controller.
func (m chatTUI) handleApprovalKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	choices := approvalChoices(m.pendingApproval)
	answer := func(choice approvalChoice) (tea.Model, tea.Cmd) {
		allow, session, persist := choice.allow, choice.allowForSession, choice.persistToConfig
		if isRecoveryApprovalEvent(m.pendingApproval) {
			action := agent.RecoveryActionRevise
			if allow {
				action = agent.RecoveryActionContinue
				if session {
					action = agent.RecoveryActionContinueTask
				}
			}
			_ = m.ctrl.ResolveRecovery(m.pendingApproval.ID, action, "")
			m.pendingApproval = nil
			return m, nil
		}
		if m.pendingApproval.Tool == planApprovalTool && (allow || choice.exitPlan) {
			m.planMode = false
			m.ctrl.SetPlanMode(false)
		}
		m.ctrl.Approve(m.pendingApproval.ID, allow, session, persist)
		m.pendingApproval = nil
		return m, nil
	}
	switch msg.String() {
	case "ctrl+c":
		m.ctrl.Cancel()
		return answer(approvalChoice{})
	case "up", "k", "ctrl+p":
		if m.approvalSelection < 0 && len(choices) > 0 {
			m.approvalSelection = 0
		} else if m.approvalSelection > 0 {
			m.approvalSelection--
		}
		return m, nil
	case "down", "j", "ctrl+n":
		if m.approvalSelection < len(choices)-1 {
			m.approvalSelection++
		}
		return m, nil
	case "enter":
		if m.approvalSelection >= 0 && m.approvalSelection < len(choices) {
			return answer(choices[m.approvalSelection])
		}
		return m, nil
	case "esc":
		return answer(approvalChoice{})
	}
	lower := strings.ToLower(msg.String())
	if len(lower) == 1 && lower[0] >= '1' && lower[0] <= '9' {
		idx := int(lower[0] - '1')
		if idx < len(choices) {
			return answer(choices[idx])
		}
		// Legacy muscle memory: tool approvals historically numbered deny as 4.
		// Honor 4 as deny even when the current prompt shows fewer rows, matching
		// the "legacy 4 still deny" contract in this function's doc comment.
		if lower == "4" {
			return answer(approvalChoice{})
		}
		return m, nil
	}
	switch lower {
	case "y":
		if len(choices) > 0 {
			return answer(choices[0])
		}
	case "a":
		for _, choice := range choices {
			if choice.allowForSession && !choice.persistToConfig {
				return answer(choice)
			}
		}
	case "p":
		for _, choice := range choices {
			if choice.persistToConfig {
				return answer(choice)
			}
		}
	case "n":
		return answer(approvalChoice{})
	}
	return m, nil
}

func isRecoveryApprovalEvent(a *event.Approval) bool {
	return a != nil && (a.Kind == recovery.ApprovalKindRecovery || a.Recovery != nil)
}

func isRecoveryPlanChangeApproval(a *event.Approval) bool {
	if !isRecoveryApprovalEvent(a) || a.Recovery == nil {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(a.Recovery.ChangeKind)) {
	case string(recovery.ChangeStrategy), string(recovery.ChangeScope):
		return true
	default:
		return false
	}
}

func freshApprovalAllowsSession(toolName string) bool {
	return toolName == control.SandboxEscapeApprovalTool || toolName == control.ManagedConfigWriteApprovalTool
}

var (
	// Input box: only top + bottom borders, no sides. The concrete colors are
	// refreshed from the active CLI theme during startup.
	inputBoxStyle    lipgloss.Style
	todoPanelStyle   lipgloss.Style
	statusBlockStyle lipgloss.Style
	workingStyle     lipgloss.Style
)

func (m chatTUI) cancelRequested() bool {
	if m.state != tuiRunning || m.ctrl == nil {
		return false
	}
	return m.ctrl.CancelRequested()
}

func (m chatTUI) runningWorkingLine(cancelRequested, styled bool) string {
	if m.state != tuiRunning {
		return ""
	}
	if m.retryAttempt > 0 && !cancelRequested {
		return fmt.Sprintf("  "+i18n.M.ChatStatusRetryingFmt, m.spinner.View(), m.retryAttempt, m.retryMax)
	}

	var working string
	if cancelRequested {
		working = fmt.Sprintf("  "+i18n.M.ChatStatusCancellingFmt, m.spinner.View(), m.elapsed)
	} else {
		working = fmt.Sprintf("  "+i18n.M.ChatStatusThinkingFmt, m.spinner.View(), m.elapsed)
	}
	if m.turnTokens > 0 {
		working += " · ↓" + shortTokens(m.turnTokens)
	}
	if n := len(m.pendingInterject); n > 0 {
		var queued string
		if n == 1 {
			queued = " · ✎ feedback queued"
		} else {
			queued = fmt.Sprintf(" · ✎ %d queued", n)
		}
		if styled {
			working += dim(queued)
		} else {
			working += queued
		}
	}
	return working
}

func (m chatTUI) View() tea.View {
	boxW := m.width
	if boxW < 10 {
		boxW = 10
	}
	hideComposer := m.hideComposer()
	shellMode := strings.HasPrefix(strings.TrimSpace(m.input.Value()), "!")
	cancelRequested := m.cancelRequested()
	var box string
	if !hideComposer {
		style := inputBoxStyle.Width(boxW)
		if shellMode {
			style = style.BorderForeground(lipgloss.Color(statusShellColor.hex))
		}
		box = style.Render(m.renderComposerInput())
	}

	var modeTag string
	if shellMode {
		modeTag = lipgloss.NewStyle().
			Background(lipgloss.Color(statusShellColor.hex)).
			Foreground(lipgloss.Color("#ffffff")).
			Bold(true).
			Padding(0, 1).
			Render("Shell")
	} else {
		color := statusAutoColor
		foreground := "#111827"
		switch {
		case m.ctrl.AutoApproveTools():
			color = statusYoloColor
			foreground = "#ffffff"
		case m.planMode:
			color = statusPlanColor
			foreground = "#ffffff"
		}
		modeTag = lipgloss.NewStyle().
			Background(lipgloss.Color(color.hex)).
			Foreground(lipgloss.Color(foreground)).
			Bold(true).
			Padding(0, 1).
			Render(m.modeTagText())
	}

	primaryStatus := m.primaryStatusLine(modeTag, shellMode, cancelRequested)
	// The spinning "thinking…" indicator is its own line ABOVE the input box (shown
	// only while a turn runs); the status/data rows stay below. This mirrors Claude
	// Code: live progress over the composer, shortcuts + stats under it.
	working := m.runningWorkingLine(cancelRequested, true)
	// Bottom region pinned under the transcript viewport: optional panels, the
	// composer when visible, then the two status rows. Its height feeds
	// transcriptHeight so the viewport above fills exactly the rest of the screen.
	var parts []string
	rowsAboveBox := 0 // terminal rows occupied by panels/working line before the composer
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
	if card := m.renderMCPImport(); card != "" {
		parts = append(parts, card)
		rowsAboveBox += strings.Count(card, "\n") + 1
	}
	if card := m.renderResumePicker(); card != "" {
		parts = append(parts, card)
		rowsAboveBox += strings.Count(card, "\n") + 1
	}
	if card := m.renderQuickPicker(); card != "" {
		parts = append(parts, card)
		rowsAboveBox += strings.Count(card, "\n") + 1
	}
	if card := m.renderCopyPicker(); card != "" {
		parts = append(parts, card)
		rowsAboveBox += strings.Count(card, "\n") + 1
	}
	if menu := m.renderCompletion(); menu != "" {
		parts = append(parts, menu)
		rowsAboveBox += strings.Count(menu, "\n") + 1
	}
	if m.nativeScrollback {
		if card := m.renderMainManager(); card != "" {
			parts = append(parts, card)
			rowsAboveBox += strings.Count(card, "\n") + 1
		}
	}
	// Layout: the working spinner (when running), then the composer when visible,
	// then the persistent status block. Wide terminals keep two information rows
	// separated by a quiet rule: interaction + model/profile, then flexible Git
	// + fixed telemetry. Narrow
	// terminals break only between those semantic groups. Padding to full width
	// prevents stale cells.
	if working != "" {
		parts = append(parts, workingStyle.Width(boxW).MaxWidth(boxW).Render(wrapStatusLine(working, boxW)))
		rowsAboveBox++
	}
	if footer := m.renderMainManagerFooter(); footer != "" {
		parts = append(parts, footer)
		rowsAboveBox += strings.Count(footer, "\n") + 1
	}
	statusBlock := m.renderStatusBlock(primaryStatus, boxW)
	if !hideComposer {
		if qi := m.renderQueueIndicator(); qi != "" {
			parts = append(parts, qi)
			rowsAboveBox += strings.Count(qi, "\n") + 1
		}
		parts = append(parts, box)
	}
	parts = append(parts, statusBlockStyle.Width(boxW).MaxWidth(boxW).Render(statusBlock))

	if m.nativeScrollback {
		v := tea.NewView(strings.Join(parts, "\n"))
		if !hideComposer {
			if cur := m.composerCursor(); cur != nil {
				cur.X += 1
				cur.Y += rowsAboveBox + 1
				v.Cursor = cur
			}
		}
		return v
	}

	// Full-screen frame: the transcript viewport on top (it pads to exactly its
	// height), the pinned bottom region beneath. Alt-screen owns the grid, so
	// resize repaints cleanly — no scrollback reflow, no ghost borders.
	mainArea := m.renderTranscript()
	if card := m.renderMainManager(); card != "" {
		mainArea = m.renderTranscriptWithMainManager(card)
	}
	v := tea.NewView(mainArea + "\n" + strings.Join(parts, "\n"))
	v.AltScreen = true
	if m.mouseCaptureOff {
		// Release the mouse to the terminal: native click-drag selection and
		// right-click context menu work again, at the cost of the in-app
		// scrollbar, wheel-scroll, and drag-select while it's off.
		v.MouseMode = tea.MouseModeNone
	} else {
		v.MouseMode = tea.MouseModeCellMotion // wheel targets the hovered scroll region; text selection is handled in-app
	}
	// Anchor the real terminal cursor at the textarea's insertion point only when
	// the composer is visible. input.Cursor() is relative to the textarea; offset
	// by the viewport height + rows above + the box's top border row (+1 column
	// for PaddingLeft).
	if !hideComposer {
		if cur := m.composerCursor(); cur != nil {
			cur.X += 1
			cur.Y += m.viewport.Height() + rowsAboveBox + 1
			v.Cursor = cur
		}
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
			return themeStyle(activeCLITheme.danger).Render(body)
		case pct >= 60:
			return themeStyle(activeCLITheme.warn).Render(body)
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
		return themeStyle(activeCLITheme.danger).Render(fmt.Sprintf("%s ctx (%d%%) · compacting soon", shortTokens(used), pct))
	case left <= 10:
		return themeStyle(activeCLITheme.warn).Render(body)
	default:
		return dim(body)
	}
}

func cacheRateLabel(format string, hit, denom int) string {
	if denom <= 0 {
		return ""
	}
	return fmt.Sprintf(format, fmt.Sprintf("%.2f%%", float64(hit)*100/float64(denom)))
}

// cacheTag renders both prompt cache-hit rates for the status line —
// "turn hit 88.00% · avg 78.00%": the single-turn rate (latest turn, the higher/steeper
// number on a non-compacting DeepSeek session) and the session-aggregate rate
// Σhit/Σ(hit+miss) (the steadier, cost-oriented number that matches the legacy
// dashboard). "" before any cache tokens have been reported.
func (m chatTUI) cacheStatus() (body string, rate float64, ok bool) {
	now := ""
	nowRate := 0.0
	if u := m.ctrl.LastUsage(); u != nil {
		// Only render when the provider actually reports cache token fields:
		// falling back to PromptTokens as the denominator painted a bogus
		// "turn hit 0.00%" for providers with no prompt-cache support.
		now = cacheRateLabel(i18n.M.ChatStatusCacheNowFmt, u.CacheHitTokens, u.CacheHitTokens+u.CacheMissTokens)
		if denom := u.CacheHitTokens + u.CacheMissTokens; denom > 0 {
			nowRate = float64(u.CacheHitTokens) * 100 / float64(denom)
		}
	}
	avg := ""
	avgRate := 0.0
	if hit, miss := m.ctrl.SessionCache(); hit+miss > 0 {
		avg = cacheRateLabel(i18n.M.ChatStatusCacheAvgFmt, hit, hit+miss)
		avgRate = float64(hit) * 100 / float64(hit+miss)
	}
	switch {
	case now != "" && avg != "":
		return now + " · " + avg, avgRate, true
	case now != "":
		return now, nowRate, true
	case avg != "":
		return avg, avgRate, true
	}
	return "", 0, false
}

func (m chatTUI) cacheTag() string {
	body, _, ok := m.cacheStatus()
	if !ok {
		return ""
	}
	return dim(body)
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

func (m chatTUI) workModeTag() string {
	if m.runtimeProfile == "" {
		return ""
	}
	return dim(fmt.Sprintf(i18n.M.WorkModeStatusFmt, runtimeProfileDisplay(m.runtimeProfile)))
}

func (m chatTUI) effortTag() string {
	if m.effortLevel == "" {
		return ""
	}
	value := footerValue(m.effortLevel)
	if m.effortLevel != "auto" {
		value = themeStyle(activeCLITheme.info).Bold(true).Render(m.effortLevel)
	}
	return footerMetric(i18n.M.ChatStatusEffortLabel, value)
}

// mouseTag is a persistent status-line marker while mouseCaptureOff is on, so
// the loss of in-app scrollbar/wheel-scroll/drag-select reads as a deliberate
// state rather than a bug the user has to guess at.
func (m chatTUI) mouseTag() string {
	if !m.mouseCaptureOff {
		return ""
	}
	return dim(i18n.M.MouseCaptureTag)
}

// shortTokens prints token counts compactly: 1_500 → "1.5K", 142_000 → "142.0K", 1_000_000 → "1.0M".
func shortTokens(n int) string {
	switch {
	case n >= 999_950:
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	case n >= 1_000:
		return fmt.Sprintf("%.1fK", float64(n)/1_000)
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
	var text string
	var planDetails []string
	if m.pendingApproval.Tool == planApprovalTool {
		text = i18n.M.PlanApprovalPrompt
	} else if isRecoveryPlanChangeApproval(m.pendingApproval) {
		text = i18n.M.RecoveryPlanDecisionPrompt
		if rec := m.pendingApproval.Recovery; rec != nil {
			if before := compactApprovalPlan(rec.PlanBefore); before != "" {
				planDetails = append(planDetails, fmt.Sprintf(i18n.M.RecoveryPlanBeforeFmt, truncateSubject(before, w)))
			}
			if after := compactApprovalPlan(rec.PlanAfter); after != "" {
				planDetails = append(planDetails, fmt.Sprintf(i18n.M.RecoveryPlanAfterFmt, truncateSubject(after, w)))
			}
		}
	} else {
		name, detail := approvalToolDetails(m.pendingApproval.Tool)
		subj := strings.TrimSpace(m.pendingApproval.Subject)
		if subj != "" {
			subj = " " + truncateSubject(subj, w)
		}
		text = strings.TrimSpace(fmt.Sprintf(i18n.M.ToolApprovalPromptFmt, name, subj, detail, ""))
	}
	if reason := strings.TrimSpace(m.pendingApproval.Reason); reason != "" {
		text += " · " + truncateSubject(reason, w)
	}
	if len(planDetails) > 0 {
		text += "\n" + strings.Join(planDetails, "\n")
	}
	var b strings.Builder
	b.WriteString("⏸ " + text + "\n")
	for i, choice := range approvalChoices(m.pendingApproval) {
		b.WriteString(rowLine(i == m.approvalSelection, i+1, "", choice.label, false) + "\n")
	}
	b.WriteString(dim("↑/↓ navigate · Enter select · y/a/p/n shortcuts"))
	return choicePanelStyle.Width(w).Render(b.String())
}

func compactApprovalPlan(plan string) string {
	return strings.Join(strings.Fields(strings.ReplaceAll(strings.TrimSpace(plan), "\n", " · ")), " ")
}

// approvalToolDetails turns provider-visible tool IDs into user-facing labels.
// MCP tools are advertised as mcp__<server>__<tool>; showing the short tool name
// first keeps the approval prompt readable while preserving the source.
func approvalToolDetails(toolName string) (name, detail string) {
	if toolName == agent.PlanModeReadOnlyCommandApprovalTool {
		return i18n.M.ApprovalToolLabelPlanModeReadOnly, fmt.Sprintf(i18n.M.ToolApprovalSourceFmt, i18n.M.ToolApprovalBuiltIn)
	}
	if toolName == control.SandboxEscapeApprovalTool {
		return i18n.M.ApprovalToolLabelSandboxEscape, fmt.Sprintf(i18n.M.ToolApprovalSourceFmt, i18n.M.ToolApprovalBuiltIn)
	}
	if toolName == control.ManagedConfigWriteApprovalTool {
		return i18n.M.ApprovalToolLabelConfigWrite, fmt.Sprintf(i18n.M.ToolApprovalSourceFmt, i18n.M.ToolApprovalBuiltIn)
	}
	if server, short, ok := tool.SplitMCPName(toolName); ok {
		lines := []string{}
		if strings.EqualFold(short, "understand_image") {
			lines = append(lines, i18n.M.ToolApprovalImageUse)
		}
		lines = append(lines, fmt.Sprintf(i18n.M.ToolApprovalSourceFmt, server))
		return short, strings.Join(lines, "\n")
	}
	return approvalToolLabel(toolName), fmt.Sprintf(i18n.M.ToolApprovalSourceFmt, i18n.M.ToolApprovalBuiltIn)
}

func approvalToolLabel(toolName string) string {
	switch toolName {
	case "bash":
		return i18n.M.ApprovalToolLabelBash
	case "edit_file":
		return i18n.M.ApprovalToolLabelEditFile
	case "write_file":
		return i18n.M.ApprovalToolLabelWriteFile
	case "multi_edit":
		return i18n.M.ApprovalToolLabelMultiEdit
	case "move_file":
		return i18n.M.ApprovalToolLabelMoveFile
	case "web_fetch":
		return i18n.M.ApprovalToolLabelWebFetch
	case "run_skill":
		return i18n.M.ApprovalToolLabelRunSkill
	case "remember":
		return i18n.M.ApprovalToolLabelRemember
	case "forget":
		return i18n.M.ApprovalToolLabelForget
	default:
		return toolName
	}
}

// todoPanelMaxRows caps how many task lines the pinned panel shows; a long list
// is truncated with a "+N more" footer so the bottom region stays compact.
const todoPanelMaxRows = 8

type todoPanelTodo struct {
	Content    string `json:"content"`
	Status     string `json:"status"`
	ActiveForm string `json:"activeForm"`
	Level      int    `json:"level"`
}

// renderTodoPanel renders the task list pinned above the input from the latest
// todo_write call (m.todoArgs): a "Tasks done/total" header, completed items
// dimmed/checked, the in-progress one highlighted (its activeForm if given),
// pending ones muted. It returns "" when there's no list or every item is done,
// so the panel appears while work is outstanding and clears itself when finished.
func (m chatTUI) renderTodoPanel() string {
	var p struct {
		Todos []todoPanelTodo `json:"todos"`
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
	start, end := todoPanelWindow(p.Todos)
	if start > 0 {
		b.WriteString(dim(fmt.Sprintf("  +%d above", start)) + "\n")
	}
	for _, t := range p.Todos[start:end] {
		indent := "  "
		if t.Level >= 1 {
			indent = "      " // sub-steps sit under their phase
		}
		switch t.Status {
		case "completed":
			b.WriteString(indent + green("✔") + " " + dim(t.Content) + "\n")
		case "in_progress":
			label := t.Content
			if t.ActiveForm != "" {
				label = t.ActiveForm
			}
			b.WriteString(indent + yellow("▶ "+label) + "\n")
		default:
			b.WriteString(indent + dim("○ "+t.Content) + "\n")
		}
	}
	if end < len(p.Todos) {
		b.WriteString(dim(fmt.Sprintf("  +%d more", len(p.Todos)-end)) + "\n")
	}
	return todoPanelStyle.Width(max(m.width, 10)).Render(strings.TrimRight(b.String(), "\n"))
}

func todoPanelWindow(todos []todoPanelTodo) (int, int) {
	if len(todos) <= todoPanelMaxRows {
		return 0, len(todos)
	}
	active := -1
	for i, t := range todos {
		if t.Status == "in_progress" {
			active = i
			break
		}
	}
	if active < 0 {
		return 0, todoPanelMaxRows
	}
	start := active - todoPanelMaxRows/2
	if start < 0 {
		start = 0
	}
	if maxStart := len(todos) - todoPanelMaxRows; start > maxStart {
		start = maxStart
	}
	return start, start + todoPanelMaxRows
}

// truncateSubject trims a tool subject so the approval banner fits one line.
func truncateSubject(s string, width int) string {
	max := width - 28
	if max < 16 {
		max = 16
	}
	return ansi.Truncate(s, max, "…")
}

// wrapStatusLine wraps a status line to `width` visible columns, ANSI-aware,
// so text that exceeds one row flows onto additional lines instead of being
// truncated with an ellipsis. Wrapping is permissive — spaces are preferred
// break points — and works within the alt-screen view so there is no scrollback
// artifact.
func wrapStatusLine(s string, width int) string {
	if width <= 0 || s == "" {
		return s
	}
	return ansi.Hardwrap(s, width, true)
}

// computeStatusLineCount returns the number of terminal rows the status block
// (working line + first status line + optional data band) will occupy after
// wrapping to `width`. It mirrors the construction in View() so the reserved
// height matches the rendered height exactly — the load-bearing invariant for
// bottomRows().
// Use the same width (m.width) that View() passes to wrapStatusLine.
func (m chatTUI) computeStatusLineCount(width int) int {
	if m.ctrl == nil {
		return 3 // two information rows plus their divider
	}
	shellMode := strings.HasPrefix(strings.TrimSpace(m.input.Value()), "!")
	cancelRequested := m.cancelRequested()

	// Replicate the first status line (mode tag + state) from View().
	// ModeTag is rendered with Padding(0,1) in View() — add the same padding
	// here so the visible width matches exactly.
	modeTag := " " + m.modeTagText() + " "
	if shellMode {
		modeTag = " Shell "
	}
	primaryStatus := m.primaryStatusLine(modeTag, shellMode, cancelRequested)
	statusBlock := m.renderStatusBlock(primaryStatus, width)

	// Replicate the working (spinner) line from View(), shown only while a turn runs.
	working := m.runningWorkingLine(cancelRequested, false)

	// Count wrapped rows for every piece that View() renders as wrapped.
	var lines int
	if m.state == tuiRunning {
		// working (spinner) line — wraps independently of the status block below.
		lines += strings.Count(wrapStatusLine(working, width), "\n") + 1
	}
	lines += strings.Count(statusBlock, "\n") + 1
	return lines
}

// The composer grows with its content up to this comfort cap. The effective
// cap is lowered for short terminals by syncInputHeightLimit, after which the
// textarea scrolls internally and keeps the caret visible.
const maxInputRows = 8

const (
	composerBorderRows = 2
	minTranscriptRows  = 3
)
const foldedPasteMinChars = 1000
const foldedPasteMinLines = 5

type pastedBlock struct {
	label string
	text  string
	image bool // an image attachment: expands to its bare @ref, not a wrapped block
}

func (m *chatTUI) chooserTyping() bool {
	return m.chooser != nil && m.chooser.typing
}

// inputHeightLimit returns the number of visible textarea rows that fit without
// letting the complete composer block consume more than half the terminal or
// pushing the transcript below its minimum useful height. Panel and wrapped
// status rows are treated as fixed bottom chrome and remain outside the input
// viewport.
func (m chatTUI) inputHeightLimit() int {
	if m.height <= 0 {
		return maxInputRows
	}

	limit := maxInputRows
	// Match the bounded-composer convention used by other coding TUIs: borders
	// are part of the half-screen budget, not extra rows added afterward.
	halfScreen := max(1, m.height/2-composerBorderRows)
	limit = min(limit, halfScreen)

	// bottomRows includes the current composer. Remove it to get the fixed
	// panels/status budget, then reserve the input borders and a readable slice
	// of transcript. On extremely short terminals one editable row still wins.
	fixedBottomRows := m.bottomRows()
	if !m.hideComposer() {
		fixedBottomRows -= m.input.Height() + composerBorderRows
	}
	available := max(1, m.height-fixedBottomRows-composerBorderRows-minTranscriptRows)
	return max(1, min(limit, available))
}

func (m *chatTUI) syncInputHeightLimit() {
	limit := m.inputHeightLimit()
	if m.input.MaxHeight == limit {
		return
	}
	m.followComposerCursor()
	m.input.MaxHeight = limit
	// SetWidth recalculates DynamicHeight from the full soft-wrapped content,
	// clamping the visible viewport to the new limit while preserving the text.
	m.input.SetWidth(max(m.width-4, 1))
}

func (m *chatTUI) growInputToFit() {
	if m.input.DynamicHeight {
		return
	}
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

// cycleMode handles the Shift+Tab gesture using the same three safe modes users
// see in Claude Code: Ask → Auto → Plan → Ask. YOLO stays outside this cycle and
// remains an explicit Ctrl+Y choice.
func (m *chatTUI) cycleMode() {
	if m.ctrl == nil || m.ctrl.ToolApprovalMode() == control.ToolApprovalYolo {
		return
	}
	switch {
	case m.planMode:
		m.planMode = false
		m.ctrl.SetToolApprovalMode(control.ToolApprovalAsk)
	case m.ctrl.ToolApprovalMode() == control.ToolApprovalDontAsk:
		m.ctrl.SetToolApprovalMode(control.ToolApprovalAsk)
	case m.ctrl.ToolApprovalMode() == control.ToolApprovalAsk:
		m.ctrl.SetToolApprovalMode(control.ToolApprovalAuto)
	case m.ctrl.ToolApprovalMode() == control.ToolApprovalAuto:
		m.planMode = true
		m.ctrl.SetToolApprovalMode(control.ToolApprovalAsk)
		m.ctrl.ClearGoal()
	}
	m.ctrl.SetPlanMode(m.planMode)
}

func (m chatTUI) desktopShortcutLayout() bool {
	return m.cfg != nil && m.cfg.UIShortcutLayout() == "desktop"
}

func (m *chatTUI) toggleYoloMode() {
	if m.ctrl == nil {
		return
	}
	if m.ctrl.ToolApprovalMode() == control.ToolApprovalYolo {
		restore := m.yoloRestoreToolApprovalMode
		if restore != control.ToolApprovalAuto {
			restore = control.ToolApprovalAsk
		}
		m.ctrl.SetToolApprovalMode(restore)
		m.yoloRestoreToolApprovalMode = ""
		return
	}
	restore := m.ctrl.ToolApprovalMode()
	if restore != control.ToolApprovalAuto {
		restore = control.ToolApprovalAsk
	}
	m.yoloRestoreToolApprovalMode = restore
	m.ctrl.SetToolApprovalMode(control.ToolApprovalYolo)
}

func (m chatTUI) modeTagText() string {
	goalMode := strings.TrimSpace(m.ctrl.Goal()) != "" && m.ctrl.GoalStatus() == control.GoalStatusRunning
	toolApprovalMode := m.ctrl.ToolApprovalMode()
	if m.desktopShortcutLayout() {
		switch {
		case m.planMode && toolApprovalMode == control.ToolApprovalYolo:
			return "Plan+YOLO"
		case goalMode && toolApprovalMode == control.ToolApprovalYolo:
			return "Goal+YOLO"
		case toolApprovalMode == control.ToolApprovalYolo:
			return "YOLO"
		case m.planMode:
			return "Plan"
		case goalMode && toolApprovalMode == control.ToolApprovalAuto:
			return "Goal+Auto"
		case goalMode:
			return "Goal"
		case toolApprovalMode == control.ToolApprovalAuto:
			return "Auto"
		case toolApprovalMode == control.ToolApprovalDontAsk:
			return "Don't Ask"
		default:
			return "Ask"
		}
	}
	switch {
	case m.planMode && toolApprovalMode == control.ToolApprovalYolo:
		return "Plan+YOLO"
	case m.planMode && toolApprovalMode == control.ToolApprovalAuto:
		return "Plan+Approve"
	case goalMode && toolApprovalMode == control.ToolApprovalYolo:
		return "Goal+YOLO"
	case goalMode && toolApprovalMode == control.ToolApprovalAuto:
		return "Goal+Approve"
	case toolApprovalMode == control.ToolApprovalYolo:
		return "YOLO"
	case toolApprovalMode == control.ToolApprovalAuto:
		return "Auto+Approve"
	case toolApprovalMode == control.ToolApprovalDontAsk:
		return "Don't Ask"
	case m.planMode:
		return "Plan"
	case goalMode:
		return "Goal"
	default:
		return "Auto"
	}
}

func (m *chatTUI) toggleVerboseReasoning(notify bool) {
	m.showReasoning = !m.showReasoning
	if m.cfg != nil {
		_ = m.cfg.SetShowReasoning(m.showReasoning)
		_ = m.cfg.Save()
	}
	if !notify {
		return
	}
	if m.showReasoning {
		m.notice("verbose on — thinking text will be shown")
	} else {
		m.notice("verbose off — thinking text will stay collapsed")
	}
}

// toggleMouseCapture flips whether Reasonix owns the mouse. It's session-only
// (unlike /verbose, this accommodates the terminal/multiplexer at hand rather
// than recording a lasting preference) — mirrors nativeScrollback, which is
// likewise never persisted to config. Clears any in-app selection/scrollbar
// drag in flight so a stale one can't be found mid-gesture once the terminal
// starts intercepting the events that would have finished it.
func (m *chatTUI) toggleMouseCapture() {
	m.mouseCaptureOff = !m.mouseCaptureOff
	m.sel = selection{}
	m.composerSel = composerSelection{}
	m.scrollbarDrag = false
	m.autoScroll = 0
	if m.mouseCaptureOff {
		m.notice(i18n.M.MouseCaptureOffHint)
	} else {
		m.notice(i18n.M.MouseCaptureOnHint)
	}
}

// startTurn commits the user bubble to scrollback, resets the turn accumulator,
// and kicks off the controller turn. `sent` goes to the model uncomposed (the
// controller frames it with any plan marker); `displayed` is what the transcript
// shows, and `restore` is what Esc puts back while the bubble is still deferred.
func (m *chatTUI) startTurn(sent, displayed, restore string) tea.Cmd {
	return m.startTurnWithRaw(sent, displayed, restore, sent)
}

// startTurnWithRaw is startTurn plus an explicit unresolved user prompt. This
// keeps reference-expanded model input separate from the text shown/restored by
// the frontend.
func (m *chatTUI) startTurnWithRaw(sent, displayed, restore, raw string) tea.Cmd {
	return m.startControllerTurn(displayed, restore, func() { m.ctrl.SendWithRaw(sent, raw) })
}

// startControllerTurn owns the TUI-side turn setup for controller entry points.
// Most prompts use SendWithRaw; slash-invoked skills use SubmitDisplay so the
// controller can choose inline vs isolated subagent execution from the live
// skill's RunAs metadata without the TUI reimplementing that policy.
func (m *chatTUI) startControllerTurn(displayed, restore string, start func()) tea.Cmd {
	// Flush any half-streamed leftover before the new turn (defensive).
	m.commitReasoning()
	m.commitPending()

	// Echo the user bubble to scrollback now so it appears the instant Enter is
	// pressed, not when the server's first packet lands. It stays un-sendable until
	// then: Esc before the reply pops these lines back off (unsendPending) and
	// restores the text to the input box, leaving nothing stranded.
	m.pendingRestore = restore
	m.pendingPastes = m.pasteLabelsIn(restore)
	m.bubbleStartIdx = len(m.transcript)
	m.commitLine("") // blank line separating turns
	m.commitTranscriptSource(transcriptSource{
		kind: transcriptSourceUser, raw: displayed, planMode: m.planMode,
	})
	m.bubblePending = true
	m.turnDiscarded = false

	m.state = tuiRunning
	m.runStart = time.Now()
	m.elapsed = 0
	m.turnTokens = 0
	// The controller owns the run goroutine, its context, and cancellation; it
	// streams events to eventCh and emits TurnDone when the turn settles.
	start()
	return tea.Batch(m.spinner.Tick, elapsedTick())
}

// confirmBubbleSent marks the already-echoed user bubble as really sent once a
// turn's first response packet arrives, so Esc no longer un-sends it (it cancels
// the stream instead). Also called defensively at turn end. A no-op once confirmed.
func (m *chatTUI) confirmBubbleSent() {
	if !m.bubblePending {
		return
	}
	m.bubblePending = false
	m.pendingRestore = ""
}

// unsendPending "un-sends" the in-flight turn while the server hasn't replied yet
// (bubblePending): it pops the echoed bubble back off the transcript, restores the
// just-sent text to the input box, and cancels the request — marking the turn
// discarded so its already-buffered events reach nothing. Once a packet has arrived
// the bubble is confirmed and this path isn't taken (Esc cancels normally instead).
func (m *chatTUI) unsendPending() {
	m.input.SetValue(m.pendingRestore)
	m.growInputToFit()
	m.truncateTranscriptBlocks(m.bubbleStartIdx)
	m.transcriptDirty = true
	m.bubblePending = false
	m.pendingRestore = ""
	m.pendingPastes = nil
	m.turnDiscarded = true
	m.ctrl.Cancel()
}

// ingestEvent routes one typed event from the agent. Reasoning (dim) and answer
// free-text accumulate in their live buffers; every other event first finalizes
// the reasoning and answer streamed so far, then commits its own line —
// preserving order. Switching on the event Kind replaces the old prefix-sniffing
// of a flattened byte stream: the structure is now explicit.
func (m *chatTUI) ingestEvent(e event.Event) {
	if e.Kind == event.Retrying {
		m.retryAttempt = e.RetryAttempt
		m.retryMax = e.RetryMax
		return
	}
	// Any other event means the connection got past the retry window (or the turn
	// ended), so the transient "retrying" indicator clears.
	m.retryAttempt = 0
	m.retryMax = 0
	if m.turnDiscarded {
		// The turn was un-sent (Esc before any packet); swallow whatever was already
		// buffered for it until it settles, so nothing lands in scrollback.
		if e.Kind == event.TurnDone {
			m.turnDiscarded = false
			m.state = tuiIdle
		}
		return
	}
	// The first packet of any kind means the server replied — confirm the send so
	// Esc cancels the stream instead of un-sending. TurnStarted is local (emitted
	// before the request) and TurnDone is handled in its own case.
	if e.Kind != event.TurnStarted && e.Kind != event.TurnDone {
		m.confirmBubbleSent()
	}
	switch e.Kind {
	case event.Reasoning:
		if m.nativeScrollback {
			if !m.reasoningNative {
				m.thinkStart = time.Now()
				m.reasoningNative = true
			}
			m.streamReasoning(e.Text)
			break
		}
		if m.reasoningLineIdx < 0 {
			// Show the marker plus a live text block the moment thinking starts; the
			// text streams in below it and the block collapses to "thought for Ns"
			// when it closes (kept expanded only in verbose mode).
			m.commitSpacer()
			m.thinkStart = time.Now()
			m.reasoningLineIdx = len(m.transcript)
			m.commitLine(dim("  ▎ " + i18n.M.ChatThinking))
			m.reasoningTextIdx = len(m.transcript)
			m.commitLine("")
			m.reasoningView = m.reasoningView[:0]
		}
		m.streamReasoning(e.Text)

	case event.Text:
		m.commitReasoningBeforeAnswer()
		m.pending.WriteString(e.Text)
		m.streamAnswer()

	case event.Message:
		// The answer stream is complete — freeze reasoning + the markdown answer.
		m.commitReasoning()
		m.commitPending()

	case event.ToolDispatch:
		// The early (partial) dispatch only carries the name — the full dispatch
		// with args prints the line. Same-ID preview refreshes are ignored because
		// native scrollback cannot replace an already-printed diff card.
		if e.Tool.Partial || e.Tool.Refreshed {
			break
		}
		m.finalizeStreamed()
		switch e.Tool.Name {
		case "todo_write":
			// The result decides whether this list becomes canonical; dispatch only
			// means the model asked for an update.
		case planApprovalTool:
			// No longer a tool, but guard anyway: the plan is the assistant's reply.
		default:
			m.commitSpacer()
			if block := diffBlock(e.Tool.Name, e.Tool.Args, e.Tool.FileDiff, m.width, m.diffMaxLines); block != nil {
				for _, ln := range block {
					m.commitLine(ln)
				}
				break
			}
			m.commitTranscriptSource(transcriptSource{
				kind: transcriptSourceToolCard, raw: e.Tool.Name, aux: e.Tool.Args,
			})
			m.beginToolRunning(e.Tool.ID)
		}

	case event.ToolProgress:
		m.streamToolOutput(e.Tool.ID, e.Tool.Output)

	case event.ToolResult:
		// A successful result is silent (it only feeds the model); a blocked/failed
		// call surfaces a red "⏺ Verb ⊘ <reason>" card. A live-output block (bash)
		// collapses to a one-line "⎿ N lines" summary first. Pass the final
		// output so collapseToolOutput has a last-resort source for the line
		// count when the live state was already reset by a back-to-back tool.
		m.collapseToolOutput(e.Tool.ID, e.Tool.Output)
		if e.Tool.Name == "todo_write" && e.Tool.Err == "" {
			m.todoArgs = e.Tool.Args
		}
		if e.Tool.Err != "" {
			m.finalizeStreamed()
			m.commitLine("  " + red("●") + " " + bold(toolDisplayName(e.Tool.Name)) + " " + red("⊘ "+e.Tool.Err))
		}

	case event.Usage:
		if e.Usage != nil {
			m.turnTokens += e.Usage.CompletionTokens
		}
		if line := renderTurnReceipt(e.Usage, e.Pricing, e.CacheDiagnostics); line != "" {
			m.finalizeStreamed()
			m.commitSpacer()
			m.commitTranscriptSource(transcriptSource{kind: transcriptSourceTurnReceipt, raw: line})
		}

	case event.Notice:
		glyph := "·"
		if e.Level == event.LevelWarn {
			glyph = "!"
		}
		m.finalizeStreamed()
		m.commitLine(fmt.Sprintf("  %s %s", glyph, e.Text))

	case event.GuardianAssessment:
		m.finalizeStreamed()
		g := e.Guardian
		line := fmt.Sprintf("Guardian %s · %s", g.Outcome, g.Tool)
		if g.Subject != "" {
			line += " · " + truncateSubject(g.Subject, m.width)
		}
		if g.RiskLevel != "" {
			line += " · risk=" + g.RiskLevel
		}
		if g.UserAuthorization != "" {
			line += " · authorization=" + g.UserAuthorization
		}
		if g.Rationale != "" {
			line += " · " + g.Rationale
		}
		if g.Outcome == "deny" {
			m.commitLine("  ! " + line)
		} else {
			m.commitLine("  · " + line)
		}

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
		m.approvalSelection = 0
		if isRecoveryPlanChangeApproval(&a) {
			// A plan decision must start neutral: Enter alone cannot make Auto's
			// strategy/scope choice for the user.
			m.approvalSelection = -1
		}

	case event.AskRequest:
		// The `ask` tool raised a question card; the run goroutine blocks until
		// ctrl.AnswerQuestion resolves it. Keys drive the card while it's set.
		m.finalizeStreamed()
		m.chooser = newChooser(e.Ask)

	case event.MCPSurfaceReady:
		if m.ctrl != nil {
			m.host = m.ctrl.Host()
		}
		m.refreshMCPManager()

	case event.TurnDone:
		// The turn settled — freeze anything still streaming, surface a real error,
		// and gate a plan-mode proposal on the user's approval. Autosave already
		// happened in Controller so every frontend shares the same activity-time
		// semantics.
		m.commitReasoning()
		m.commitPending()
		// The bubble was echoed on Enter and an un-sent turn is swallowed above
		// (turnDiscarded), so any turn reaching here keeps its bubble in scrollback;
		// just clear the un-sendable flag.
		m.confirmBubbleSent()
		m.state = tuiIdle
		m.queueEditCursor = -1
		m.queueEditDraft = ""
		m.clearSubmittedPastes()
		if e.Outcome == event.TurnOutcomeRecoveryPaused && e.Err != nil && e.Err.Error() != "" {
			m.commitLine(wrapForViewport("⏸ "+e.Err.Error(), m.width, activeCLITheme.info))
		} else if e.Err != nil && e.Err.Error() != "" && !strings.Contains(e.Err.Error(), "context canceled") {
			m.commitLine(wrapForViewport(i18n.M.ErrorPrefix+" "+e.Err.Error(), m.width, activeCLITheme.warn))
		}
		// Plan-mode approval is now driven by the controller (it emits an
		// ApprovalRequest when a plan-mode turn produces a proposal), so there's
		// nothing to detect here.
	}
}

// finalizeStreamed freezes any in-progress reasoning + answer into scrollback so
// a following event line lands after them, preserving chronological order.
func (m *chatTUI) finalizeStreamed() {
	m.collapseToolOutput(m.toolStreamID, "")
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
	typedCmd := strings.TrimSpace(strings.SplitN(input, " ", 2)[0])

	if strings.HasPrefix(typedCmd, "/mcp__") {
		return m.runMCPPrompt(input)
	}
	cmd := canonicalBuiltinSlashCommand(typedCmd)

	switch cmd {
	case "/compact":
		m.echoLocalCommand(input)
		// Compaction makes a (network) summarizer call; run it off the Update loop
		// so the TUI doesn't freeze. The CompactionStarted/Done events render the
		// card as they arrive; compactDoneMsg only handles the terminal error /
		// snapshot once the pass returns. Any text after "/compact" is focus
		// guidance steering what the summary keeps.
		focus := strings.TrimSpace(strings.TrimPrefix(input, typedCmd))
		return func() tea.Msg { return compactDoneMsg{err: m.ctrl.Compact(context.Background(), focus)} }
	case "/new":
		m.echoLocalCommand(input)
		if err := m.ctrl.NewSession(); err != nil {
			m.notice(fmt.Sprintf("%s: %v", i18n.M.SlashNewFailed, err))
			return nil
		}
		m.followSessionLease()
		// Native scrollback keeps the old transcript; mark the fork with a fresh banner.
		m.resetFreshContextView(false)
		m.notice(i18n.M.SlashNewDone)
	case "/clear":
		m.echoLocalCommand(input)
		m.clearConfirm = &clearConfirm{confirm: 1}
	case "/cls":
		m.echoLocalCommand(input)
		m.finalizeStreamed()
		m.clearTranscriptDisplay()
		m.commitLine(strings.TrimRight(
			renderTUIBanner(m.label, "", transcriptContentWidth(m.width, m.nativeScrollback)), "\n"))
		m.transcriptDirty = true
		m.forceGotoBottom = true
		m.notice(i18n.M.SlashClsDone)
	case "/resume":
		m.runResumeCommand(input)
	case "/status":
		m.echoLocalCommand(input)
		m.showStatusDetails()
	case "/rename":
		m.runRenameCommand(input)
	case "/todo":
		m.echoLocalCommand(input)
		// Dismiss the pinned task list; a later todo_write brings it back.
		m.todoArgs = ""
		m.notice(i18n.M.SlashTodoCleared)
	case "/verbose":
		m.toggleVerboseReasoning(true)
	case "/mouse":
		m.toggleMouseCapture()
	case "/sandbox":
		m.echoLocalCommand(input)
		m.showSandboxStatus()
	case "/effort":
		return m.runEffortCommand(input)
	case "/work-mode", "/profile":
		m.echoLocalCommand(input)
		return m.runWorkModeCommand(input)
	case "/reasoning-language":
		m.echoLocalCommand(input)
		m.runReasoningLanguageCommand(input)
	case "/rewind":
		m.echoLocalCommand(input)
		m.openRewind()
	case "/tree":
		m.echoLocalCommand(input)
		m.showBranchTree()
	case "/branch":
		m.echoLocalCommand(input)
		m.runBranchCommand(input)
	case "/switch":
		m.echoLocalCommand(input)
		m.runSwitchCommand(input)
	case "/mcp":
		m.echoLocalCommand(input)
		m.runMCPSubcommand(input)
	case "/remote":
		m.echoLocalCommand(input)
		m.showRemoteHosts()
	case "/plugin", "/plugins":
		m.echoLocalCommand(input)
		m.runPluginSubcommand(input)
	case "/model":
		m.echoLocalCommand(input)
		m.runModelSubcommand(input)
		if m.pendingModelSwitch != nil {
			return m.pendingModelSwitch
		}
	case "/provider":
		m.echoLocalCommand(input)
		m.runProviderCommand(input)
		if m.pendingModelSwitch != nil {
			return m.pendingModelSwitch
		}
	case "/skill", "/skills":
		m.echoLocalCommand(input)
		m.runSkillSubcommand(input)
		if m.pendingModelSwitch != nil {
			return m.pendingModelSwitch
		}
	case "/hooks":
		m.echoLocalCommand(input)
		m.runHooksSubcommand(input)
	case "/reload-cmd":
		m.echoLocalCommand(input)
		if m.ctrl == nil {
			m.notice("controller not ready")
			return nil
		}
		if m.ctrl.Running() {
			m.notice("wait for the current turn to finish, then retry /reload-cmd")
			return nil
		}
		prev := len(m.commands)
		err := m.ctrl.ReloadCommands(context.Background())
		m.commands = m.ctrl.Commands()
		m.updateCompletion()
		if err != nil {
			m.notice("reload-cmd: " + err.Error())
			return nil
		}
		m.notice(fmt.Sprintf("commands reloaded: %d → %d commands", prev, len(m.commands)))

	case "/paste-image":
		return m.beginClipboardImagePaste()
	case "/output-style", "/output-styles":
		m.echoLocalCommand(input)
		styles := outputstyle.List(outputstyle.Dirs())
		if len(styles) == 0 {
			m.notice(i18n.M.OutputStyleNone)
		} else {
			m.commitLine(renderOutputStyles(m.width, styles, m.outputStyle))
		}
	case "/diff-fold":
		m.echoLocalCommand(input)
		if m.diffMaxLines == 0 {
			m.diffMaxLines = diffFoldLimit
			m.notice(fmt.Sprintf(i18n.M.DiffFoldEnabledFmt, diffFoldLimit))
		} else {
			m.diffMaxLines = 0
			m.notice(i18n.M.DiffFoldDisabled)
		}
	case "/theme":
		m.echoLocalCommand(input)
		m.runThemeSubcommand(input)
	case "/language":
		m.echoLocalCommand(input)
		m.runLanguageSubcommand(input)
	case "/help":
		m.echoLocalCommand(input)
		m.showHelp()
	case "/memory":
		m.echoLocalCommand(input)
		m.showMemory()
	case "/migrate", "/migration":
		m.echoLocalCommand(input)
		migration.RunLegacyRescueCommand(strings.TrimSpace(strings.TrimPrefix(input, typedCmd)), event.FuncSink(func(e event.Event) {
			if e.Kind == event.Notice {
				m.notice(e.Text)
			}
		}))
	case "/goal":
		return m.runGoalSubcommand(input)
	case "/remember":
		note := strings.TrimSpace(strings.TrimPrefix(input, typedCmd))
		if note == "" {
			m.notice("nothing to remember")
		} else if path, err := m.ctrl.QuickAdd(memory.ScopeProject, note); err != nil {
			m.notice("memory: " + err.Error())
		} else {
			m.notice("remembered → " + path)
		}
	case "/quit", "/exit":
		return tea.Quit
	case "/copy":
		return m.runCopyCommand(input)
	case "/export":
		m.runExportCommand(input)
	case "/forget":
		m.forgetMemory(strings.TrimSpace(strings.TrimPrefix(input, typedCmd)))
	default:
		// A custom command wins over a skill of the same name; both resolve to a turn.
		if sent, ok := m.ctrl.CustomCommand(input); ok {
			return m.startTurn(sent, input, input)
		}
		if _, ok := m.ctrl.RunSkill(input); ok {
			fields := strings.Fields(input)
			name := strings.TrimPrefix(fields[0], "/")
			for _, sk := range m.ctrl.Skills() {
				if sk.Name == name && sk.RunAs == skill.RunSubagent && len(fields) == 1 {
					m.echoLocalCommand(input)
					m.notice("usage: /" + name + " <task>")
					return nil
				}
			}
			return m.startControllerTurn(input, input, func() { m.ctrl.SubmitDisplay(input, input) })
		}
		m.notice(fmt.Sprintf("%s: %s", i18n.M.SlashUnknown, cmd))
	}
	return nil
}

// showStatusDetails keeps diagnostics available without permanently crowding
// the two-line composer footer.
func (m *chatTUI) showStatusDetails() {
	var lines []string
	lines = append(lines, viewHeader("%s", "Session status"))
	mode := "Ask"
	if m.ctrl != nil {
		mode = m.modeTagText()
	}
	lines = append(lines, "  mode       "+mode)
	model := strings.TrimSpace(m.modelRef)
	if model == "" {
		model = strings.TrimSpace(m.label)
	}
	if model != "" {
		lines = append(lines, "  model      "+model)
	}
	if m.ctrl != nil {
		if tag := m.contextTag(); tag != "" {
			lines = append(lines, "  context    "+tag)
		}
	}
	if tag := m.workModeTag(); tag != "" {
		lines = append(lines, "  profile    "+tag)
	}
	if m.effortLevel != "" {
		// The persistent footer uses an uppercase semantic label. The expanded
		// diagnostic view keeps its sentence-like wording for readability.
		lines = append(lines, "  effort     effort "+m.effortLevel)
	}
	if m.ctrl != nil {
		if tag := m.cacheTag(); tag != "" {
			lines = append(lines, "  cache      "+tag)
		}
	}
	if tag := m.gitTag(); tag != "" {
		lines = append(lines, "  git        "+tag)
	}
	if m.ctrl != nil {
		if tag := m.jobsTag(); tag != "" {
			lines = append(lines, "  jobs       "+tag)
		}
	}
	if m.balance != "" {
		lines = append(lines, "  balance    "+m.balance)
	}
	if tag := m.mouseTag(); tag != "" {
		lines = append(lines, "  mouse      "+tag)
	}
	m.commitLine(strings.Join(lines, "\n"))
}

func (m *chatTUI) runGoalSubcommand(input string) tea.Cmd {
	cmd, ok := control.ParseGoalCommand(input)
	if !ok {
		m.echoLocalCommand(input)
		m.notice(i18n.M.GoalEmpty)
		return nil
	}
	switch cmd.Action {
	case control.GoalCommandSet:
		m.planMode = false
		m.ctrl.SetPlanMode(false)
		m.ctrl.SetGoalWithResearchMode(cmd.Text, cmd.ResearchMode)
		m.ctrl.GoalStrict(cmd.Strict)
		m.notice(fmt.Sprintf(i18n.M.GoalSetFmt, control.ShortGoalForNotice(cmd.Text)))
		return m.startTurn("Start pursuing the active goal now.", input, input)
	case control.GoalCommandClear:
		m.echoLocalCommand(input)
		m.ctrl.ClearGoal()
		m.notice(i18n.M.GoalCleared)
	default:
		m.echoLocalCommand(input)
		goal := m.ctrl.Goal()
		if strings.TrimSpace(goal) == "" {
			m.notice(i18n.M.GoalEmpty)
		} else {
			m.notice(fmt.Sprintf(i18n.M.GoalCurrentFmt, goal))
		}
	}
	return nil
}

// runCopyCommand copies the Nth-latest assistant message from the current turn
// (after the last user message) to the clipboard.
//
//   - "/copy"   — shows a numbered list of assistant messages to choose from.
//   - "/copy N" — copies the Nth message directly (1 = most recent).
//
// Counting does not cross user message boundaries.
func (m *chatTUI) runCopyCommand(input string) tea.Cmd {
	m.echoLocalCommand(input)
	// "/copy N" copies the Nth-newest assistant message directly (1 = most
	// recent), matching the picker's newest-first ordering. A bare "/copy"
	// (or a non-numeric argument) opens the interactive picker instead.
	arg := strings.TrimSpace(strings.TrimPrefix(input, "/copy"))
	if n, err := strconv.Atoi(arg); err == nil && n > 0 {
		msgs := m.ctrl.History()
		parts := copyAssistantParts(msgs)
		if len(parts) == 0 {
			m.notice(i18n.M.SlashCopyEmpty)
			return nil
		}
		// copyAssistantParts is oldest-first; index 0 of the reversed slice
		// is the most recent, so "/copy 1" = parts[len-1].
		idx := len(parts) - n
		if idx < 0 || idx >= len(parts) {
			m.notice(i18n.M.SlashCopyEmpty)
			return nil
		}
		return copyToClipboard(parts[idx])
	}
	m.openCopyPicker()
	return nil
}

// firstLine returns the first non-empty line of s, truncated to 80 runes.
func firstLine(s string) string {
	for _, line := range strings.Split(s, "\n") {
		if t := strings.TrimSpace(line); t != "" {
			runes := []rune(t)
			if len(runes) > 80 {
				return string(runes[:77]) + "..."
			}
			return t
		}
	}
	return "..."
}

// copyAssistantParts returns the Content of assistant messages after the last
// user message in msgs, skipping empty strings and model placeholders ("…", "...").
// The result is chronological (oldest first).
func copyAssistantParts(msgs []provider.Message) []string {
	lastUserIdx := -1
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == provider.RoleUser {
			lastUserIdx = i
			break
		}
	}
	start := lastUserIdx + 1
	if lastUserIdx < 0 {
		start = 0
	}
	var parts []string
	for i := start; i < len(msgs); i++ {
		if msgs[i].Role != provider.RoleAssistant {
			continue
		}
		c := strings.TrimSpace(msgs[i].Content)
		if c == "" || c == "..." || c == "…" {
			continue
		}
		parts = append(parts, c)
	}
	return parts
}

// runExportCommand exports the entire session as a markdown file, excluding
// system messages, reasoning/thinking content, and tool calls/results.
func (m *chatTUI) runExportCommand(input string) {
	m.echoLocalCommand(input)
	msgs := m.ctrl.History()
	if len(msgs) == 0 {
		m.notice(i18n.M.SlashExportEmpty)
		return
	}

	var b strings.Builder
	b.WriteString("# reasonix session\n\n")
	lastRole := provider.Role("")
	exportedMessages := 0
	for _, msg := range msgs {
		switch msg.Role {
		case provider.RoleUser:
			// Skip internal steer messages.
			if _, isSteer := agent.SteerText(msg.Content); isSteer {
				continue
			}
			content := exportUserContent(msg.Content)
			if content == "" {
				continue
			}
			if lastRole != provider.RoleUser {
				b.WriteString("## User\n\n")
			}
			b.WriteString(content)
			b.WriteString("\n\n")
			exportedMessages++
			lastRole = provider.RoleUser
		case provider.RoleAssistant:
			content := strings.TrimSpace(msg.Content)
			if content == "" {
				continue
			}
			if lastRole != provider.RoleAssistant {
				b.WriteString("## Assistant\n\n")
			}
			b.WriteString(content)
			b.WriteString("\n\n")
			exportedMessages++
			lastRole = provider.RoleAssistant
		}
	}
	if exportedMessages == 0 {
		m.notice(i18n.M.SlashExportEmpty)
		return
	}

	// Choose a filename. If the workspace has a root, save there; otherwise
	// the current directory. Use a timestamp-based name.
	dir := "."
	if m.ctrl != nil {
		if wr := m.ctrl.WorkspaceRoot(); wr != "" {
			dir = wr
		}
	}
	ts := time.Now().Format("20060102-150405")
	filename := fmt.Sprintf("session-%s.md", ts)
	path := filepath.Join(dir, filename)
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		m.notice(fmt.Sprintf("%s: %v", i18n.M.SlashUnknown, err))
		return
	}
	m.notice(fmt.Sprintf(i18n.M.SlashExportDoneFmt, path))
}

func exportUserContent(content string) string {
	content = control.StripComposePrefixes(content)
	content = control.StripReferencedContextPrefix(content)
	return strings.TrimSpace(content)
}

func (m *chatTUI) echoLocalCommand(input string) {
	input = strings.TrimSpace(input)
	if input == "" {
		return
	}
	m.commitLine(dim("  › " + input))
}

// commandNames renders the custom command list for /help, "" when there are none.
func (m *chatTUI) commandNames() string {
	names := make([]string, 0, len(m.commands))
	for _, c := range m.commands {
		if !c.Hidden {
			names = append(names, "/"+c.Name)
		}
	}
	return strings.Join(names, " · ")
}

// showSandboxStatus displays the current sandbox configuration and whether
// the OS sandbox backend is available. It reads from the stored config so
// the user can inspect sandbox state without leaving the TUI (closes #3316).
func (m *chatTUI) showSandboxStatus() {
	if m.cfg == nil {
		m.notice("sandbox: config not loaded")
		return
	}
	bash := m.cfg.BashMode()
	network := m.cfg.Sandbox.Network
	available := sandbox.Available()
	roots := m.cfg.WriteRoots()

	var b strings.Builder
	b.WriteString("sandbox\n")
	b.WriteString("  phase 0  file-writer confinement\n")
	if len(roots) > 0 {
		fmt.Fprintf(&b, "    write_roots  %s\n", strings.Join(roots, ", "))
	}
	if m.cfg.Sandbox.WorkspaceRoot != "" {
		fmt.Fprintf(&b, "    workspace_root  %s\n", m.cfg.Sandbox.WorkspaceRoot)
	}
	if len(m.cfg.Sandbox.AllowWrite) > 0 {
		fmt.Fprintf(&b, "    allow_write  %s\n", strings.Join(m.cfg.Sandbox.AllowWrite, ", "))
	}
	b.WriteString("  phase 1  OS bash sandbox\n")
	fmt.Fprintf(&b, "    bash        %s", bash)
	if bash == "enforce" && !available {
		b.WriteString(" (unavailable: no OS sandbox on this host; bash execution is refused. " + sandbox.UnavailableRemediation() + ")")
	}
	b.WriteString("\n")
	fmt.Fprintf(&b, "    network     %v\n", network)
	m.notice(b.String())
}

// runMCPSubcommand handles "/mcp" (status), "/mcp add …" (connect a server live
// and persist it), and "/mcp remove <name>" (disconnect + drop from config). Add
// connects synchronously — like /compact, an explicit command may briefly block
// the UI while the handshake runs.
func (m *chatTUI) runMCPSubcommand(input string) {
	args := tokenizeArgs(input) // args[0] == "/mcp"
	if len(args) < 2 {
		m.openMCPManager("")
		return
	}
	switch args[1] {
	case "list", "ls":
		// The completion menu offers "list"; treat it as the status view (same as
		// the legacy /mcp output) rather than an unknown subcommand.
		m.showMCPStatus()
	case "show":
		if len(args) < 3 {
			m.notice("usage: /mcp show <name>")
			return
		}
		m.openMCPManager(args[2])
	case "tools":
		if len(args) < 3 {
			m.notice("usage: /mcp tools <name>")
			return
		}
		m.openMCPManager(args[2])
		if m.mcp != nil {
			m.mcp.stage = mcpStageTools
		}
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
	case "connect":
		if len(args) < 3 {
			m.notice("usage: /mcp connect <name>")
			return
		}
		n, err := m.ctrl.ConnectConfiguredMCPServer(args[2])
		if err != nil {
			m.notice("mcp connect: " + err.Error())
			return
		}
		m.host = m.ctrl.Host()
		m.notice(fmt.Sprintf("connected %s — %d tools (available next message)", args[2], n))
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
	case "import":
		m.openMCPImportPicker()
	default:
		m.notice("unknown /mcp subcommand " + args[1] + " — try: /mcp, /mcp list, /mcp show, /mcp add, /mcp connect, /mcp import, /mcp remove")
	}
}

// showMCPStatus queues the connected MCP servers, their counts, and the prompt
// commands / resource refs they expose — the discovery surface for /mcp.
func (m *chatTUI) showMCPStatus() {
	if m.host == nil || (len(m.host.Servers()) == 0 && len(m.host.Failures()) == 0) {
		m.notice(i18n.M.SlashMCPNone)
		return
	}
	m.commitLine(renderMCPStatus(m.width, m.host.Servers(), m.host.Prompts(), m.host.Resources(), m.host.Failures()))
}

// notice queues a dim informational line to scrollback.
func (m *chatTUI) notice(note string) {
	m.commitLine(dim("  · " + note))
}

// showRemoteHosts renders a read-only summary of configured remote hosts. The
// remote session lives in a `reasonix serve` on the remote host, so connecting
// happens from a terminal (`reasonix remote connect`), not inside this chat.
func (m *chatTUI) showRemoteHosts() {
	cfg, err := config.Load()
	if err != nil {
		m.notice(err.Error())
		return
	}
	if len(cfg.Remote.Hosts) == 0 {
		m.notice(i18n.M.RemoteNoHostsHint)
		return
	}
	var b strings.Builder
	for _, h := range cfg.Remote.Hosts {
		target := h.Host
		if h.User != "" {
			target = h.User + "@" + target
		}
		if h.Port != 0 && h.Port != 22 {
			target = fmt.Sprintf("%s:%d", target, h.Port)
		}
		fmt.Fprintf(&b, "  · %s  %s\n", h.Name, target)
	}
	fmt.Fprintf(&b, "  run `reasonix remote connect <name>` in a terminal to open the remote workspace")
	m.commitLine(dim(b.String()))
}

// resolveRefs resolves a line's @references off the event loop via the
// controller, delivering a refsResolvedMsg with the tagged context block.
func (m *chatTUI) resolveRefs(sent, display, restore string) tea.Cmd {
	return func() tea.Msg {
		block, errs := m.ctrl.ResolveRefs(context.Background(), sent)
		return refsResolvedMsg{sent: sent, display: display, restore: restore, block: block, errs: errs}
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

// replaySectionsFor turns a loaded session into scrollback blocks. Normal tool
// results remain quiet, while interrupted-turn reasoning and tool cards replay
// from provider-excluded LocalOnly records so restart matches the live view.
func replaySectionsFor(history []provider.Message, width int) []string {
	var out []string
	for _, m := range history {
		if m.LocalOnly {
			if reasoning := strings.TrimSpace(m.ReasoningContent); reasoning != "" {
				out = append(out, dim("  ▎ "+i18n.M.ChatThinking)+"\n"+reasoningBlock(reasoning, width, 0)+"\n\n")
			}
			if body := strings.TrimSpace(m.Content); body != "" {
				out = append(out, renderAssistantMarkdown(body, width)+"\n\n")
			}
			for _, call := range m.ToolCalls {
				out = append(out, toolCard(call.Name, "", width)+"\n\n")
			}
			if m.InterruptedTurn != nil {
				out = append(out, fmt.Sprintf("  · %s\n\n", interruptedTurnDisplayNotice()))
			}
			continue
		}
		switch m.Role {
		case provider.RoleUser:
			// Steer messages are surfaced as a notice line, not a user bubble.
			if steerText, isSteer := agent.SteerText(m.Content); isSteer {
				out = append(out, fmt.Sprintf("  ↪ %s\n\n", steerText))
				continue
			}
			content := control.StripComposePrefixes(m.Content)
			out = append(out, renderUserBubble(content, width, false)+"\n\n")
		case provider.RoleAssistant:
			if reasoning := strings.TrimSpace(m.ReasoningContent); reasoning != "" {
				out = append(out, dim("  ▎ "+i18n.M.ChatThinking)+"\n"+reasoningBlock(reasoning, width, 0)+"\n\n")
			}
			body := strings.TrimSpace(m.Content)
			if body != "" {
				out = append(out, renderAssistantMarkdown(body, width)+"\n\n")
			}
			for _, call := range m.ToolCalls {
				out = append(out, toolCard(call.Name, call.Arguments, width)+"\n\n")
			}
		}
	}
	return out
}

func interruptedTurnDisplayNotice() string {
	return i18n.M.InterruptedRecovery
}

// renderTUIBanner is the title + tip + optional missing-key warning printed once
// at the top of the session.
func renderTUIBanner(label, missing string, width int) string {
	var b strings.Builder
	b.WriteString(accent("◆") + " " + bold("reasonix") + "  " + dim("· "+label) + "\n")
	b.WriteString(dim("  "+i18n.M.ChatTip) + "\n")
	if missing != "" {
		b.WriteString(wrapForViewport("  ! "+missing, width, activeCLITheme.warn) + "\n")
	}
	return b.String()
}

// wrapForViewport hard-wraps text to fit width columns and colours every line.
func wrapForViewport(text string, width int, fg cliColor) string {
	if width <= 0 {
		width = 80
	}
	return themeStyle(fg).Width(width).Render(text)
}

// renderUserBubble renders the just-submitted prompt as a transcript line. Keep
// it visually lighter than the real bottom composer so a fresh session does not
// look like it has a second input box in the transcript.
func renderUserBubble(line string, width int, planMode bool) string {
	line = displayLineForImageRefs(line)
	prefix := "› "
	if planMode {
		prefix = "› [plan] "
	}
	if !colorEnabled {
		return "│ " + prefix + line
	}
	return "  " + accent(prefix+line)
}

var cliImageRefRe = regexp.MustCompile(`(?:^|\s)@\.reasonix/attachments/clipboard-\d{8}-\d{6}\.\d+(?:-(?:\d{6}|[a-f0-9]{8}))?\.(?:png|jpg|jpeg|gif|webp)`)

func displayLineForImageRefs(line string) string {
	idx := 0
	out := cliImageRefRe.ReplaceAllStringFunc(line, func(_ string) string {
		idx++
		return " [image" + strconv.Itoa(idx) + "]"
	})
	return strings.TrimSpace(out)
}

// eventSink is the event.Sink the agent emits to in TUI mode. Each event
// becomes an agentEventMsg. The channel is generously buffered so streaming
// bursts don't back-pressure the agent goroutine.
type eventSink struct {
	ch chan<- event.Event
}

func (s *eventSink) Emit(e event.Event) { s.ch <- e }
