package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"reasonix/internal/agent"
	"reasonix/internal/checkpoint"
	"reasonix/internal/config"
	"reasonix/internal/control"
	"reasonix/internal/event"
	"reasonix/internal/i18n"
	"reasonix/internal/provider"
)

type blockingTurnRunner struct{ started chan struct{} }

type stubbornTurnRunner struct {
	started chan struct{}
	release chan struct{}
}

const tinyPNGBase64 = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg=="

func TestMain(m *testing.M) {
	old := detectTermuxTerminal
	detectTermuxTerminal = func() bool { return false }

	// Pin the UI language for the whole cli test binary. Production code
	// (cli.Run) calls i18n.DetectLanguage("") which resolves the host locale from
	// the environment (REASONIX_LANG/LC_ALL/LC_MESSAGES/LANG) and installs it as
	// the global i18n.M. On a non-English dev machine that flips M to e.g.
	// Chinese, and tests that exercise the CLI entry point (acp_test.go,
	// cli_test.go) don't restore it — so later tests asserting English UI strings
	// fail, but only when the whole package runs, not in isolation. Forcing a
	// deterministic English environment keeps the suite independent of the host
	// locale (matching CI). Tests that need another language still set it
	// explicitly via i18n.DetectLanguage(lang) with their own cleanup.
	os.Unsetenv("REASONIX_LANG")
	os.Unsetenv("LC_ALL")
	os.Unsetenv("LC_MESSAGES")
	os.Setenv("LANG", "en_US.UTF-8")
	i18n.DetectLanguage("en")

	code := m.Run()
	detectTermuxTerminal = old
	os.Exit(code)
}

func (r *blockingTurnRunner) Run(ctx context.Context, _ string) error {
	close(r.started)
	<-ctx.Done()
	return ctx.Err()
}

func (r *stubbornTurnRunner) Run(ctx context.Context, _ string) error {
	close(r.started)
	<-r.release
	return ctx.Err()
}

type recordingTurnRunner struct {
	inputs               []string
	memoryCompilerInputs []string
}

func (r *recordingTurnRunner) Run(ctx context.Context, input string) error {
	r.inputs = append(r.inputs, input)
	// The memory compiler's source_event is set by the orchestrator from the
	// controller's `raw` value. Capture it so we can prove the CLI passes the
	// EXPANDED paste (not the folded label) — the label would starve the model
	// of the pasted content once the compiler's contract replaces the user turn.
	if source, ok := agent.MemoryCompilerSourceInputFromContext(ctx); ok {
		r.memoryCompilerInputs = append(r.memoryCompilerInputs, source)
	}
	return nil
}

func waitForCLIEvent(t *testing.T, ch <-chan event.Event, kind event.Kind) {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for {
		select {
		case e := <-ch:
			if e.Kind == kind {
				return
			}
		case <-deadline:
			t.Fatalf("timed out waiting for event %v", kind)
		}
	}
}

func writeTUIImageCapabilityConfig(t *testing.T, root string) {
	t.Helper()
	cfg := config.Default()
	cfg.DefaultModel = "custom/text-only"
	cfg.Providers = []config.ProviderEntry{{
		Name:         "custom",
		Kind:         "openai",
		BaseURL:      "https://example.invalid/v1",
		Models:       []string{"text-only", "vision-pro"},
		VisionModels: []string{"vision-pro"},
	}}
	if err := cfg.SaveTo(filepath.Join(root, "reasonix.toml")); err != nil {
		t.Fatalf("save config: %v", err)
	}
}

func saveTestImageAttachment(t *testing.T, root string) string {
	t.Helper()
	t.Chdir(root)
	path, err := control.SaveImageDataURL("data:image/png;base64," + tinyPNGBase64)
	if err != nil {
		t.Fatalf("SaveImageDataURL: %v", err)
	}
	return path
}

// TestEscCancelsRunningTurnWithCompletionOpen reproduces the report that Esc
// (unlike Ctrl+C) did not stop a running turn: an active completion menu
// captured Esc to close itself and returned before reaching the running-turn
// cancel branch, while Ctrl+C — not in the completion switch — fell through.
func TestEscCancelsRunningTurnWithCompletionOpen(t *testing.T) {
	r := &blockingTurnRunner{started: make(chan struct{})}
	ctrl := control.New(control.Options{Runner: r, Sink: event.Discard, SessionDir: t.TempDir(), Label: "test"})
	ctrl.Send("hi")
	<-r.started // the turn is in flight and cancellable

	m := newTestChatTUI()
	m.ctrl = ctrl
	m.state = tuiRunning
	m.completion.active = true // e.g. a "/" typed into the composer while waiting

	_, _ = m.update(tea.KeyPressMsg{Code: tea.KeyEscape})

	deadline := time.Now().Add(2 * time.Second)
	for ctrl.Running() {
		if time.Now().After(deadline) {
			t.Fatal("Esc did not cancel the running turn (completion menu swallowed it)")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// TestTranscriptMirrorsCommits proves the alt-screen migration's foundation:
// every line commitLine sends to native scrollback is also captured in the
// transcript buffer (the future viewport's content source), in order.
func TestTranscriptMirrorsCommits(t *testing.T) {
	m := newTestChatTUI()
	m.ingestEvent(event.Event{Kind: event.ToolDispatch, Tool: event.Tool{Name: "read_file", Args: `{"path":"x"}`}})
	m.ingestEvent(event.Event{Kind: event.Notice, Level: event.LevelInfo, Text: "compacted"})

	if len(m.transcript) != len(*m.pendingCommit) {
		t.Fatalf("transcript (%d) and pendingCommit (%d) should hold the same lines", len(m.transcript), len(*m.pendingCommit))
	}
	for i := range m.transcript {
		if m.transcript[i] != (*m.pendingCommit)[i] {
			t.Errorf("line %d mismatch: transcript=%q pendingCommit=%q", i, m.transcript[i], (*m.pendingCommit)[i])
		}
	}
}

func TestTermuxNativeScrollbackCommitsFinalAnswer(t *testing.T) {
	m := newTestChatTUI()
	m.nativeScrollback = true
	m.pending.WriteString("first paragraph\n\nsecond paragraph")

	m.streamAnswer()
	if len(*m.pendingCommit) != 0 {
		t.Fatalf("Termux native scrollback should not commit rewritten streaming blocks, got %v", *m.pendingCommit)
	}

	m.commitPending()
	if got := strings.Join(*m.pendingCommit, "\n"); !strings.Contains(got, "first paragraph") || !strings.Contains(got, "second paragraph") {
		t.Fatalf("final answer was not committed to native scrollback: %v", *m.pendingCommit)
	}
}

func TestTermuxNativeScrollbackDefaultsToExpandedReasoning(t *testing.T) {
	old := detectTermuxTerminal
	detectTermuxTerminal = func() bool { return true }
	t.Cleanup(func() { detectTermuxTerminal = old })

	ctrl := control.New(control.Options{})
	m := newChatTUI(ctrl, "", make(chan event.Event, 1), 80)
	if !m.nativeScrollback {
		t.Fatal("Termux should use native scrollback")
	}
	if !m.showReasoning {
		t.Fatal("Termux should expand reasoning by default because live viewport reasoning is unavailable")
	}
	m.width = 80

	m.ingestEvent(event.Event{Kind: event.Reasoning, Text: "reasoning details"})
	m.ingestEvent(event.Event{Kind: event.Text, Text: "answer"})
	got := strings.Join(*m.pendingCommit, "\n")
	if !strings.Contains(got, "reasoning details") {
		t.Fatalf("Termux reasoning was not expanded into native scrollback: %q", got)
	}
}

// TestCompletionMenuFixedWidth verifies that the completion menu pads every
// line (items + footer) to m.width so delta rendering always writes exactly the
// same column count — no trailing characters for \033[K to leave behind.
func TestCompletionMenuFixedWidth(t *testing.T) {
	ctrl := control.New(control.Options{})
	m := newChatTUI(ctrl, "", make(chan event.Event, 1), 80)
	m.width = 80
	m.completion.active = true
	m.completion.items = []compItem{
		{label: "review"},
		{label: "clear", hint: "start fresh"},
	}
	m.completion.sel = 1
	m.completion.kind = compSlash

	out := m.renderCompletion()
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	// items + footer = 3 lines
	if len(lines) != 3 {
		t.Fatalf("completion menu should have 3 lines (2 items + footer), got %d:\n%s", len(lines), out)
	}
	for i, line := range lines {
		if got := ansi.StringWidth(line); got != 80 {
			t.Errorf("line %d visual width = %d, want 80: %q", i, got, line)
		}
	}
}

// TestCompletionMenuPadsWithNonBreakingSpaces verifies the fixed-width padding
// is not ordinary ASCII space. Ultraviolet treats trailing ASCII spaces as
// clearable cells and may emit EL/ECH erase sequences; mintty can leave stale
// halves of CJK glyphs when those sequences clear Chinese skill descriptions.
func TestCompletionMenuPadsWithNonBreakingSpaces(t *testing.T) {
	ctrl := control.New(control.Options{})
	m := newChatTUI(ctrl, "", make(chan event.Event, 1), 80)
	m.width = 80
	m.completion.active = true
	m.completion.items = []compItem{
		{label: "/土壤", hint: "分析土壤墒情"},
		{label: "/巡田", hint: "识别病虫害"},
	}
	m.completion.sel = 0
	m.completion.kind = compSlash

	out := m.renderCompletion()
	for i, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		if got := ansi.StringWidth(line); got != 80 {
			t.Fatalf("line %d visual width = %d, want 80: %q", i, got, line)
		}
		if !strings.HasSuffix(line, "\u00a0") {
			t.Fatalf("line %d should end with non-breaking padding, got %q", i, line)
		}
		if strings.HasSuffix(line, " ") {
			t.Fatalf("line %d should not end with clearable ASCII space, got %q", i, line)
		}
	}
}

// TestTranscriptViewportSizing proves the viewport tracks the terminal size and
// gets the rows left over after the pinned bottom region (input box + 2 status
// rows = 5 with an empty 1-line composer), and is fed the committed transcript.
func TestTranscriptViewportSizing(t *testing.T) {
	ctrl := control.New(control.Options{})
	m := newChatTUI(ctrl, "", make(chan event.Event, 1), 80)

	m0, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = m0.(chatTUI)

	if got := m.bottomRows(); got != 5 {
		t.Fatalf("bottomRows with an empty composer = %d, want 5 (input 1 + border 2 + status 2)", got)
	}
	if m.viewport.Width() != 79 {
		t.Errorf("viewport content width = %d, want 79 (terminal 80 - 1 scrollbar column)", m.viewport.Width())
	}
	if want := m.transcriptHeight(); m.viewport.Height() != want || want != 19 {
		t.Errorf("viewport height = %d, transcriptHeight = %d, want 19 (24-5)", m.viewport.Height(), want)
	}
	if m.viewport.TotalLineCount() == 0 {
		t.Errorf("viewport should hold the committed banner after the first resize")
	}
}

// TestStatusLineWrapAccounting proves that computeStatusLineCount correctly
// predicts the rendered row count of the status block (working + mode/state line
// + data line) when wrapping is triggered on a narrow terminal, and that
// bottomRows reserves the right height so the viewport fills the screen without
// overlap.
func TestStatusLineWrapAccounting(t *testing.T) {
	ctrl := control.New(control.Options{})
	m := newChatTUI(ctrl, "", make(chan event.Event, 1), 30)

	// Narrow terminal: mode+state line and data line will both wrap.
	m0, _ := m.Update(tea.WindowSizeMsg{Width: 30, Height: 12})
	m = m0.(chatTUI)

	// At width 30 the status block should be detectably wrapped.
	if m.statusLineCount <= 2 {
		t.Fatalf("statusLineCount on a narrow terminal (30 cols) = %d, want > 2 (wrapping should be detected)", m.statusLineCount)
	}

	// Verify the height budget covers the full screen.
	if got := m.transcriptHeight() + m.bottomRows(); got != m.height {
		t.Fatalf("transcriptHeight(%d) + bottomRows(%d) = %d, want %d (full screen height)",
			m.transcriptHeight(), m.bottomRows(), got, m.height)
	}

	// When running, the working line should increase statusLineCount.
	idleCount := m.statusLineCount
	m.state = tuiRunning
	m.elapsed = 5
	m.turnTokens = 100
	// Push an interject so the working line is longer.
	m.pendingInterject = []string{"feedback"}
	m.statusLineCount = m.computeStatusLineCount(m.width)
	runCount := m.statusLineCount
	if runCount <= idleCount {
		t.Fatalf("statusLineCount when running (%d) should be > idle (%d)", runCount, idleCount)
	}

	// Reset and test that a custom statusline command is also counted.
	m.state = tuiIdle
	m.pendingInterject = nil
	m.statuslineCmd = "custom"
	m.statuslineOut = "model: claude-3 · ctx: 45% · tokens: 128K · cache: 87% · rate: 1.2s · jobs: 3 running · balance: ¥152.30"
	m0, _ = m.Update(tea.WindowSizeMsg{Width: 35, Height: 12})
	m = m0.(chatTUI)
	if m.statusLineCount <= 2 {
		t.Fatalf("statusLineCount with custom statusline on 35 cols = %d, want > 2 (custom output should wrap)", m.statusLineCount)
	}
	if got := m.transcriptHeight() + m.bottomRows(); got != m.height {
		t.Fatalf("with custom statusline: transcriptHeight(%d) + bottomRows(%d) = %d, want %d",
			m.transcriptHeight(), m.bottomRows(), got, m.height)
	}
}

// TestStatusLineRenderedHeightMatchesBudget proves that the actual rendered
// line count of View()'s bottom area matches what bottomRows() predicts,
// specifically at the CJK 2-char-overflow boundary where an off-by-one would
// hide the bottom row of the viewport.
func TestStatusLineRenderedHeightMatchesBudget(t *testing.T) {
	ctrl := control.New(control.Options{})
	m := newChatTUI(ctrl, "", make(chan event.Event, 1), 46)

	// Manually set a long git repo/branch so the status line contains CJK.
	m.missing = ""
	m.gitStatus = gitStatus{Repo: "我的项目名字", Branch: "我的分支"}

	m0, _ := m.Update(tea.WindowSizeMsg{Width: 46, Height: 12})
	m = m0.(chatTUI)

	if m.statusLineCount <= 2 {
		t.Fatalf("statusLineCount at width 46 with CJK = %d, want > 2", m.statusLineCount)
	}

	// Verify that computeStatusLineCount matches the actual rendered line count.
	// Strip ANSI from the full view, then reconstruct what bottomRows expects.
	viewStr := ansi.Strip(m.View().Content)
	allLines := strings.Split(viewStr, "\n")
	totalLines := len(allLines)

	// The total should be m.height (full terminal height).
	if totalLines != m.height {
		t.Fatalf("View() total lines = %d, want %d (terminal height)", totalLines, m.height)
	}

	// transcriptHeight() lines should be the viewport, the rest is bottom rows.
	if got, want := m.transcriptHeight()+m.bottomRows(), m.height; got != want {
		t.Fatalf("transcriptHeight(%d) + bottomRows(%d) = %d, want %d",
			m.transcriptHeight(), m.bottomRows(), got, want)
	}

	// Also verify the invariant holds at narrower widths.
	for _, w := range []int{44, 42, 40, 35, 30, 25, 20} {
		m0, _ = m.Update(tea.WindowSizeMsg{Width: w, Height: 12})
		m = m0.(chatTUI)
		viewStr2 := ansi.Strip(m.View().Content)
		allLines2 := strings.Split(viewStr2, "\n")
		if len(allLines2) != m.height {
			t.Errorf("width=%d: View() total lines = %d, want %d", w, len(allLines2), m.height)
		}
		if got, want := m.transcriptHeight()+m.bottomRows(), m.height; got != want {
			t.Errorf("width=%d: transcriptHeight(%d) + bottomRows(%d) = %d, want %d",
				w, m.transcriptHeight(), m.bottomRows(), got, want)
		}
	}
}

func TestManualNewlineGrowsComposerWithoutHidingFirstLine(t *testing.T) {
	ctrl := control.New(control.Options{})
	m := newChatTUI(ctrl, "", make(chan event.Event, 1), 40)

	m0, _ := m.Update(tea.WindowSizeMsg{Width: 40, Height: 12})
	m = m0.(chatTUI)
	m.input.SetValue("first line")

	m0, _ = m.Update(tea.KeyPressMsg{Code: 'j', Mod: tea.ModCtrl})
	m = m0.(chatTUI)

	if got := m.input.Height(); got != 2 {
		t.Fatalf("input height after Ctrl+J = %d, want 2", got)
	}
	if got := m.input.ScrollYOffset(); got != 0 {
		t.Fatalf("input scroll offset after Ctrl+J = %d, want 0 so the first line remains visible", got)
	}
}

func TestManualNewlineCanExceedVisibleComposerRows(t *testing.T) {
	ctrl := control.New(control.Options{})
	m := newChatTUI(ctrl, "", make(chan event.Event, 1), 40)

	m0, _ := m.Update(tea.WindowSizeMsg{Width: 40, Height: 12})
	m = m0.(chatTUI)
	m.input.SetValue("first line")

	for range maxInputRows + 1 {
		m0, _ = m.Update(tea.KeyPressMsg{Code: 'j', Mod: tea.ModCtrl})
		m = m0.(chatTUI)
	}

	if got, want := strings.Count(m.input.Value(), "\n"), maxInputRows+1; got != want {
		t.Fatalf("manual newlines preserved = %d, want %d", got, want)
	}
	if got := m.input.Height(); got != maxInputRows {
		t.Fatalf("visible input height = %d, want capped at %d", got, maxInputRows)
	}
}

func TestSoftWrappedInputGrowsComposerAndShrinksTranscript(t *testing.T) {
	ctrl := control.New(control.Options{})
	m := newChatTUI(ctrl, "", make(chan event.Event, 1), 24)

	m0, _ := m.Update(tea.WindowSizeMsg{Width: 24, Height: 12})
	m = m0.(chatTUI)
	initialViewportHeight := m.viewport.Height()

	m0, _ = m.Update(tea.PasteMsg{Content: strings.Repeat("x", 60)})
	m = m0.(chatTUI)

	if got := m.input.Height(); got <= 1 {
		t.Fatalf("input height after soft-wrapped paste = %d, want > 1", got)
	}
	if got := m.viewport.Height(); got >= initialViewportHeight {
		t.Fatalf("viewport height after composer growth = %d, want less than initial %d", got, initialViewportHeight)
	}
}

func TestMCPManagerHidesComposerBox(t *testing.T) {
	ctrl := control.New(control.Options{})
	m := newChatTUI(ctrl, "", make(chan event.Event, 1), 80)
	m.mcp = &mcpManager{stage: mcpStageList, snapshot: mcpSnapshot{servers: []mcpServerView{
		{Name: "github", Transport: "stdio", Status: "deferred", Configured: true, Tier: "background"},
	}}}

	m0, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = m0.(chatTUI)

	footerRows := strings.Count(m.renderMainManagerFooter(), "\n") + 1
	if got, want := m.bottomRows(), footerRows+m.statusLineCount; got != want {
		t.Fatalf("bottomRows with MCP manager = %d, want %d (footer + status rows; manager content renders in main area)", got, want)
	}
	if !m.hideComposer() {
		t.Fatal("MCP manager should hide the composer")
	}
	content := ansi.Strip(m.View().Content)
	if !strings.Contains(content, "Manage MCP servers") {
		t.Fatalf("MCP manager missing from view:\n%s", content)
	}
	if !strings.Contains(content, "Enter for details") {
		t.Fatalf("MCP footer hint missing from view:\n%s", content)
	}
	if !strings.Contains(content, "· MCP") {
		t.Fatalf("MCP status line missing from view:\n%s", content)
	}
}

func TestClearCommandRequiresConfirmationAndDiscardsSession(t *testing.T) {
	dir := t.TempDir()
	sess := agent.NewSession("sys")
	sess.Add(provider.Message{Role: provider.RoleUser, Content: "old context"})
	exec := agent.New(nil, nil, sess, agent.Options{}, event.Discard)
	path := filepath.Join(dir, "session.jsonl")
	ctrl := control.New(control.Options{Executor: exec, SystemPrompt: "sys", SessionDir: dir, SessionPath: path, Label: "test"})
	if err := ctrl.Snapshot(); err != nil {
		t.Fatal(err)
	}
	m := newChatTUI(ctrl, "", make(chan event.Event, 1), 80)

	if cmd := m.runSlashCommand("/clear"); cmd != nil {
		t.Fatal("/clear should open a local confirmation without returning a command")
	}
	if m.clearConfirm == nil {
		t.Fatal("/clear should open a confirmation prompt")
	}
	if m.clearConfirm.confirm != 1 {
		t.Fatalf("/clear confirmation should default to cancel, got %d", m.clearConfirm.confirm)
	}
	m0, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = m0.(chatTUI)
	footerRows := strings.Count(m.renderMainManagerFooter(), "\n") + 1
	if got, want := m.bottomRows(), footerRows+m.statusLineCount; got != want {
		t.Fatalf("bottomRows with /clear confirmation = %d, want %d (footer + status rows; confirmation renders in main area)", got, want)
	}
	if !m.hideComposer() {
		t.Fatal("/clear confirmation should hide the composer")
	}
	content := ansi.Strip(m.View().Content)
	if !strings.Contains(content, "Clear current context without saving?") {
		t.Fatalf("/clear confirmation prompt missing from view:\n%s", content)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("session should still exist before confirmation: %v", err)
	}
	if current := exec.Session().Snapshot(); len(current) != 2 {
		t.Fatalf("context changed before confirmation: %+v", current)
	}

	next, _ := m.handleClearConfirmKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = next.(chatTUI)
	if m.clearConfirm != nil {
		t.Fatal("Enter on default cancel should close the confirmation")
	}
	if ctrl.SessionPath() != path {
		t.Fatal("cancelled /clear should not rotate the session path")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("cancelled /clear should keep the session file: %v", err)
	}

	m.runSlashCommand("/clear")
	m.shellOutputs["shell-old"] = "old shell output\n"
	m.shellExpanded["shell-old"] = true
	m.shellTranscriptIdx["shell-old"] = 2
	next, _ = m.handleClearConfirmKey(tea.KeyPressMsg{Code: 'y'})
	m = next.(chatTUI)
	if ctrl.SessionPath() == path {
		t.Fatal("confirmed /clear should rotate to a fresh session path")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("confirmed /clear should remove the old transcript, stat err=%v", err)
	}
	current := exec.Session().Snapshot()
	if len(current) != 1 || current[0].Role != provider.RoleSystem || current[0].Content != "sys" {
		t.Fatalf("cleared context = %+v, want only system prompt", current)
	}
	if len(m.transcript) == 0 || strings.Contains(strings.Join(m.transcript, "\n"), "old context") {
		t.Fatalf("TUI transcript was not reset after /clear: %+v", m.transcript)
	}
	if len(m.shellTranscriptIdx) != 0 || len(m.shellOutputs) != 0 || len(m.shellExpanded) != 0 {
		t.Fatalf("confirmed /clear should reset shell display state: idx=%v outputs=%v expanded=%v",
			m.shellTranscriptIdx, m.shellOutputs, m.shellExpanded)
	}
}

func TestClsClearsTranscriptDisplayState(t *testing.T) {
	m := newTestChatTUI()
	*m.pendingCommit = append(*m.pendingCommit, "stale pending")
	m.transcript = []string{"banner", "shell card", "old shell output"}
	m.wrappedLines = []string{"banner", "shell card", "old shell output"}
	m.shellOutputs["shell-old"] = strings.Repeat("old shell output\n", shellPreviewLines+1)
	m.shellExpanded["shell-old"] = false
	m.shellTranscriptIdx["shell-old"] = 2
	m.toolLineCountByID["shell-old"] = 3
	m.toolStreamID = "shell-old"
	m.toolStreamIdx = 2
	m.toolTail = []string{"old shell output"}
	m.toolPartial = "partial"
	m.toolLineCount = 4

	if cmd := m.runSlashCommand("/cls"); cmd != nil {
		t.Fatal("/cls should clear locally without returning a command")
	}
	if len(*m.pendingCommit) != len(m.transcript) {
		t.Fatalf("pendingCommit should only contain the fresh cleared-screen transcript, pending=%v transcript=%v",
			*m.pendingCommit, m.transcript)
	}
	if len(m.shellTranscriptIdx) != 0 || len(m.shellOutputs) != 0 || len(m.shellExpanded) != 0 {
		t.Fatalf("/cls should reset shell display state: idx=%v outputs=%v expanded=%v",
			m.shellTranscriptIdx, m.shellOutputs, m.shellExpanded)
	}
	if len(m.toolLineCountByID) != 0 || m.toolStreamID != "" || m.toolStreamIdx != -1 || len(m.toolTail) != 0 || m.toolPartial != "" || m.toolLineCount != 0 {
		t.Fatalf("/cls should reset live tool display state: counts=%v id=%q idx=%d tail=%v partial=%q lines=%d",
			m.toolLineCountByID, m.toolStreamID, m.toolStreamIdx, m.toolTail, m.toolPartial, m.toolLineCount)
	}

	before := strings.Join(m.transcript, "\n")
	m.toggleShellOutput()
	if after := strings.Join(m.transcript, "\n"); after != before {
		t.Fatalf("Ctrl+B after /cls should not rewrite the cleared transcript:\nbefore=%s\nafter=%s", before, after)
	}
	if strings.Contains(before, "old shell output") || strings.Contains(before, "/cls") {
		t.Fatalf("/cls should keep only the fresh banner/notice, got:\n%s", before)
	}
}

func TestMainManagerFollowsTranscriptWithoutTopPadding(t *testing.T) {
	ctrl := control.New(control.Options{})
	m := newChatTUI(ctrl, "", make(chan event.Event, 1), 80)
	m0, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 20})
	m = m0.(chatTUI)
	m.wrappedLines = []string{"reasonix", "› /mcp"}

	out := ansi.Strip(m.renderTranscriptWithMainManager("Manage MCP servers\n1 servers"))
	lines := strings.Split(out, "\n")
	if len(lines) < 4 {
		t.Fatalf("rendered manager area too short:\n%s", out)
	}
	if !strings.Contains(lines[0], "reasonix") || !strings.Contains(lines[1], "/mcp") {
		t.Fatalf("transcript lines should stay above manager:\n%s", out)
	}
	if strings.TrimSpace(lines[2]) != "" {
		t.Fatalf("expected one separator line before manager, got %q in:\n%s", lines[2], out)
	}
	if !strings.Contains(lines[3], "Manage MCP servers") {
		t.Fatalf("manager should follow transcript immediately, got line 3 %q in:\n%s", lines[3], out)
	}
}

func TestMarkdownDividerFitsTranscriptContentWidth(t *testing.T) {
	ctrl := control.New(control.Options{})
	m := newChatTUI(ctrl, "", make(chan event.Event, 1), 80)
	m0, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 20})
	m = m0.(chatTUI)

	wantW := transcriptContentWidth(80, false)
	if m.viewport.Width() != wantW {
		t.Fatalf("viewport width = %d, want transcript content width %d", m.viewport.Width(), wantW)
	}
	rule := strings.TrimRight(m.renderer.Render("---"), "\n")
	lines := strings.Split(wrapTranscript(rule, m.viewport.Width()), "\n")
	if len(lines) != 1 {
		t.Fatalf("markdown divider wrapped into %d lines at width %d: %q", len(lines), m.viewport.Width(), lines)
	}
	if w := visibleWidth(lines[0]); w != m.viewport.Width() {
		t.Fatalf("markdown divider width = %d, want %d: %q", w, m.viewport.Width(), lines[0])
	}
}

func TestTranscriptContentWidthReservesScrollbarColumn(t *testing.T) {
	if got := transcriptContentWidth(80, false); got != 79 {
		t.Fatalf("transcriptContentWidth(80, false) = %d, want 79", got)
	}
	if got := transcriptContentWidth(80, true); got != 80 {
		t.Fatalf("transcriptContentWidth(80, true) = %d, want 80", got)
	}
	if got := transcriptContentWidth(0, false); got != 1 {
		t.Fatalf("transcriptContentWidth(0, false) = %d, want 1", got)
	}
}

func TestModalPanelsHideComposerBox(t *testing.T) {
	ask := event.Ask{
		ID: "ask-1",
		Questions: []event.AskQuestion{{
			ID:     "q1",
			Prompt: "Pick one",
			Options: []event.AskOption{{
				Label: "Option A",
			}},
		}},
	}
	tests := []struct {
		name   string
		setup  func(*chatTUI)
		render func(chatTUI) string
	}{
		{
			name: "resume picker",
			setup: func(m *chatTUI) {
				m.resumePick = &resumePicker{sessions: []agent.SessionInfo{{
					Path:    "one.jsonl",
					Preview: "previous task",
					Turns:   3,
				}}, sel: 0, active: -1}
			},
			render: func(m chatTUI) string { return m.renderResumePicker() },
		},
		{
			name: "rewind picker",
			setup: func(m *chatTUI) {
				m.rewind = &rewindPicker{metas: []checkpoint.Meta{{
					Turn:   0,
					Prompt: "fix the parser",
				}}, sel: 0}
			},
			render: func(m chatTUI) string { return m.renderRewind() },
		},
		{
			name: "approval prompt",
			setup: func(m *chatTUI) {
				m.pendingApproval = &event.Approval{ID: "approval-1", Tool: "bash", Subject: "echo hi"}
			},
			render: func(m chatTUI) string { return m.renderApprovalBanner() },
		},
		{
			name: "ask chooser",
			setup: func(m *chatTUI) {
				m.chooser = newChooser(ask)
			},
			render: func(m chatTUI) string { return m.renderChooser() },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := control.New(control.Options{})
			m := newChatTUI(ctrl, "", make(chan event.Event, 1), 80)
			tt.setup(&m)

			m0, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
			m = m0.(chatTUI)

			card := tt.render(m)
			if card == "" {
				t.Fatalf("%s panel did not render", tt.name)
			}
			cardRows := strings.Count(card, "\n") + 1
			if got, want := m.bottomRows(), cardRows+m.statusLineCount; got != want {
				t.Fatalf("bottomRows with %s = %d, want %d (panel + status rows, no composer box)", tt.name, got, want)
			}
		})
	}
}

func TestInputOwnedOverlaysKeepComposerBox(t *testing.T) {
	ask := event.Ask{
		ID: "ask-1",
		Questions: []event.AskQuestion{{
			ID:     "q1",
			Prompt: "Pick one",
			Options: []event.AskOption{{
				Label: "Option A",
			}},
		}},
	}
	tests := []struct {
		name   string
		setup  func(*chatTUI)
		render func(chatTUI) string
	}{
		{
			name: "ask free text",
			setup: func(m *chatTUI) {
				m.chooser = newChooser(ask)
				m.chooser.typing = true
			},
			render: func(m chatTUI) string { return m.renderChooser() },
		},
		{
			name: "completion menu",
			setup: func(m *chatTUI) {
				m.input.SetValue("/")
				m.completion = completion{active: true, kind: compSlash, items: []compItem{{label: "/mcp"}}, sel: 0}
			},
			render: func(m chatTUI) string { return m.renderCompletion() },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := control.New(control.Options{})
			m := newChatTUI(ctrl, "", make(chan event.Event, 1), 80)
			tt.setup(&m)

			m0, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
			m = m0.(chatTUI)

			if m.hideComposer() {
				t.Fatalf("%s should keep the composer visible", tt.name)
			}
			panel := tt.render(m)
			if panel == "" {
				t.Fatalf("%s panel did not render", tt.name)
			}
			panelRows := strings.Count(panel, "\n") + 1
			if got, want := m.bottomRows(), panelRows+m.input.Height()+2+m.statusLineCount; got != want {
				t.Fatalf("bottomRows with %s = %d, want %d (panel + composer box + status rows)", tt.name, got, want)
			}
		})
	}
}

// TestIngestEventRoutesByKind proves each event Kind lands in the right place:
// reasoning shows a live marker with streaming text, while tool dispatch, blocked
// results, usage, notices, and coordinator phases each commit as their own
// scrollback line. Routing is by Kind, not by sniffing line prefixes.
func TestIngestEventRoutesByKind(t *testing.T) {
	// Reasoning shows a marker plus the live thinking text streamed below it.
	m := newTestChatTUI()
	m.ingestEvent(event.Event{Kind: event.Reasoning, Text: "weighing options"})
	if len(m.transcript) != 2 || !strings.Contains(m.transcript[0], "thinking") {
		t.Errorf("reasoning should show a live marker, transcript=%v", m.transcript)
	}
	if !strings.Contains(m.transcript[1], "weighing options") {
		t.Errorf("reasoning text should stream live, transcript=%v", m.transcript)
	}

	for _, tc := range []struct {
		name string
		ev   event.Event
		want string
	}{
		{"dispatch", event.Event{Kind: event.ToolDispatch, Tool: event.Tool{Name: "read_file", Args: `{"path":"x"}`}}, "● Read(x)"},
		{"blocked", event.Event{Kind: event.ToolResult, Tool: event.Tool{Name: "bash", Err: "blocked by permission policy"}}, "● Bash ⊘ blocked by permission policy"},
		{"usage", event.Event{Kind: event.Usage, Usage: &provider.Usage{PromptTokens: 1000, CompletionTokens: 200, TotalTokens: 1200, CacheHitTokens: 900, CacheMissTokens: 100}}, "  · 1200 tok"},
		{"usage-diagnostics", event.Event{Kind: event.Usage, Usage: &provider.Usage{PromptTokens: 1000, CompletionTokens: 200, TotalTokens: 1200}, CacheDiagnostics: &event.CacheDiagnostics{PrefixChanged: true, PrefixChangeReasons: []string{"tools"}}}, "cache prefix changed: tools"},
		{"notice-info", event.Event{Kind: event.Notice, Level: event.LevelInfo, Text: "compacted 8 messages → summary"}, "  · compacted 8 messages → summary"},
		{"notice-warn", event.Event{Kind: event.Notice, Level: event.LevelWarn, Text: "response truncated: hit max output tokens"}, "  ! response truncated: hit max output tokens"},
		{"phase", event.Event{Kind: event.Phase, Text: "planner · planning"}, "[planner · planning]"},
	} {
		m := newTestChatTUI()
		m.ingestEvent(tc.ev)
		got := *m.pendingCommit
		if len(got) != 1 || !strings.Contains(got[0], tc.want) {
			t.Errorf("%s: committed=%v, want a single line containing %q", tc.name, got, tc.want)
		}
	}

	// A successful tool result is silent — it only feeds the model.
	m = newTestChatTUI()
	m.ingestEvent(event.Event{Kind: event.ToolResult, Tool: event.Tool{Name: "read_file", Output: "contents"}})
	if len(*m.pendingCommit) != 0 {
		t.Errorf("successful tool result should be silent, committed=%v", *m.pendingCommit)
	}
}

func TestIngestEventShowsReasoningInVerboseMode(t *testing.T) {
	m := newTestChatTUI()
	m.showReasoning = true

	m.ingestEvent(event.Event{Kind: event.Reasoning, Text: "weighing options"})
	if !strings.Contains(m.reasoning.String(), "weighing options") {
		t.Errorf("verbose reasoning should buffer the text, got %q", m.reasoning.String())
	}
}

// TestUserBubbleEchoedImmediately proves the user bubble is committed to scrollback
// the moment the turn starts, not deferred to the server's first packet. The first
// real packet only confirms the send (closing the un-send window); a local
// TurnStarted must not, so Esc can still un-send until the server actually replies.
func TestUserBubbleEchoedImmediately(t *testing.T) {
	m := newTestChatTUI()
	// Stand in for startTurn's immediate echo (no controller in the unit harness).
	m.bubbleStartIdx = len(m.transcript)
	m.commitLine("")
	m.commitLine(renderUserBubble("hello world", m.width, m.planMode))
	m.bubblePending = true
	m.state = tuiRunning

	if !strings.Contains(strings.Join(m.transcript, "\n"), "hello world") {
		t.Fatalf("bubble should be echoed to scrollback immediately, got %v", m.transcript)
	}

	// TurnStarted is emitted locally before the request — it must not confirm.
	m.ingestEvent(event.Event{Kind: event.TurnStarted})
	if !m.bubblePending {
		t.Fatalf("TurnStarted should leave the send un-sendable, pending=%v", m.bubblePending)
	}

	// The first real packet confirms the send; a reasoning packet also shows its
	// live thinking marker.
	m.ingestEvent(event.Event{Kind: event.Reasoning, Text: "thinking…"})
	if m.bubblePending {
		t.Fatalf("first packet should confirm the send")
	}
	if !strings.Contains(strings.Join(m.transcript, "\n"), "thinking") {
		t.Errorf("reasoning packet should show the thinking marker, got %v", m.transcript)
	}
}

func TestUserBubbleIsLightweightTranscriptLine(t *testing.T) {
	prevColor := colorEnabled
	colorEnabled = true
	defer func() { colorEnabled = prevColor }()

	got := renderUserBubble("hello world", 80, false)
	plain := ansi.Strip(got)
	if !strings.Contains(plain, "› hello world") {
		t.Fatalf("user bubble missing prompt text: %q", plain)
	}
	if got == plain {
		t.Fatalf("user bubble should use themed foreground color when color is enabled: %q", got)
	}
	if w := ansi.StringWidth(plain); w > 20 {
		t.Fatalf("user bubble should not render as a full-width input-like block, width=%d text=%q", w, plain)
	}
}

// TestUnsendDiscardsBufferedEvents proves that after an un-send (Esc before any
// packet) the turn's already-buffered events are swallowed — nothing reaches
// scrollback — and its TurnDone settles the model back to idle.
func TestUnsendDiscardsBufferedEvents(t *testing.T) {
	m := newTestChatTUI()
	m.state = tuiRunning
	m.turnDiscarded = true // the state unsendPending leaves behind

	m.ingestEvent(event.Event{Kind: event.Reasoning, Text: "late thinking"})
	m.ingestEvent(event.Event{Kind: event.Text, Text: "late answer"})
	if len(*m.pendingCommit) != 0 || m.reasoning.Len() != 0 || m.pending.Len() != 0 {
		t.Fatalf("a discarded turn should swallow buffered events, committed=%v", *m.pendingCommit)
	}

	m.ingestEvent(event.Event{Kind: event.TurnDone})
	if m.turnDiscarded || m.state != tuiIdle {
		t.Fatalf("TurnDone should clear the discard and return to idle, discarded=%v state=%v", m.turnDiscarded, m.state)
	}
	if len(*m.pendingCommit) != 0 {
		t.Errorf("a discarded turn should leave nothing in scrollback, committed=%v", *m.pendingCommit)
	}
}

// TestAnswerTextStartingWithBracketStaysInAnswer locks in the win of the typed
// event stream: model answer text starting with "[" — a markdown link, a slice
// literal, even a quoted "[… · planning]" — is a Text event, so it can never be
// mistaken for a coordinator phase marker the way prefix-sniffing a flattened
// byte stream once could. It stays in the answer buffer and renders as markdown.
func TestAnswerTextStartingWithBracketStaysInAnswer(t *testing.T) {
	for _, txt := range []string{
		"[link](https://example.com)",
		"[1, 2, 3]",
		"[planner · planning] (the model quoting a marker)",
	} {
		m := newTestChatTUI()
		m.ingestEvent(event.Event{Kind: event.Text, Text: txt})
		if len(*m.pendingCommit) != 0 {
			t.Errorf("answer text %q should stay live, not commit as an event line: %v", txt, *m.pendingCommit)
		}
		if m.pending.String() != txt {
			t.Errorf("answer text should buffer verbatim, got %q want %q", m.pending.String(), txt)
		}
	}
}

// TestInsertNewlineKeyBinding verifies newChatTUI actually wires shift+enter
// into the textarea's InsertNewline binding (plain Enter submits, so a newline
// needs a modifier). It exercises the real constructor, not a hand-built binding.
func TestInsertNewlineKeyBinding(t *testing.T) {
	ctrl := control.New(control.Options{})
	m := newChatTUI(ctrl, "", make(chan event.Event, 1), 80)
	keys := m.input.KeyMap.InsertNewline.Keys()
	found := false
	for _, k := range keys {
		if k == "shift+enter" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("newChatTUI InsertNewline should include shift+enter, got %v", keys)
	}
}

func TestCtrlHomeEndScrollKeyBindings(t *testing.T) {
	ctrl := control.New(control.Options{})
	ch := make(chan event.Event, 1)
	notice := agentEventMsg(event.Event{Kind: event.Notice, Level: event.LevelInfo, Text: "line"})
	adv := func(m chatTUI, msg tea.Msg) chatTUI {
		n, _ := m.Update(msg)
		return n.(chatTUI)
	}

	cur := adv(newChatTUI(ctrl, "", ch, 80), tea.WindowSizeMsg{Width: 80, Height: 8})
	for i := 0; i < 12; i++ {
		cur = adv(cur, notice)
	}
	// Viewport should be at the bottom after output.
	if !cur.viewport.AtBottom() {
		t.Fatal("viewport should start at the bottom after streaming output")
	}

	// Ctrl+Home should scroll to the top.
	cur = adv(cur, tea.KeyPressMsg{Code: tea.KeyHome, Mod: tea.ModCtrl})
	if !cur.viewport.AtTop() {
		t.Fatalf("ctrl+home should scroll to top, AtTop=%v, YOffset=%d", cur.viewport.AtTop(), cur.viewport.YOffset())
	}

	// Ctrl+End should scroll back to the bottom.
	cur = adv(cur, tea.KeyPressMsg{Code: tea.KeyEnd, Mod: tea.ModCtrl})
	if !cur.viewport.AtBottom() {
		t.Fatalf("ctrl+end should scroll to bottom, AtBottom=%v, YOffset=%d", cur.viewport.AtBottom(), cur.viewport.YOffset())
	}
}

func TestMouseWheelAndPageKeysScrollTranscript(t *testing.T) {
	ctrl := control.New(control.Options{})
	ch := make(chan event.Event, 1)
	notice := agentEventMsg(event.Event{Kind: event.Notice, Level: event.LevelInfo, Text: "line"})
	adv := func(m chatTUI, msg tea.Msg) chatTUI {
		n, _ := m.Update(msg)
		return n.(chatTUI)
	}

	cur := adv(newChatTUI(ctrl, "", ch, 80), tea.WindowSizeMsg{Width: 80, Height: 10})
	for i := 0; i < 40; i++ {
		cur = adv(cur, notice)
	}
	if !cur.viewport.AtBottom() {
		t.Fatal("viewport should start at bottom after overflowing output")
	}
	bottom := cur.viewport.YOffset()
	if bottom <= cur.viewport.Height()+3 {
		t.Fatalf("test transcript did not overflow enough: bottom=%d height=%d", bottom, cur.viewport.Height())
	}

	cur = adv(cur, tea.MouseWheelMsg{Button: tea.MouseWheelUp})
	if got, want := cur.viewport.YOffset(), bottom-3; got != want {
		t.Fatalf("wheel-up YOffset = %d, want %d", got, want)
	}

	cur = adv(cur, tea.MouseWheelMsg{Button: tea.MouseWheelDown})
	if got := cur.viewport.YOffset(); got != bottom {
		t.Fatalf("wheel-down should return by one wheel step, YOffset=%d want bottom=%d", got, bottom)
	}

	cur = adv(cur, tea.KeyPressMsg{Code: tea.KeyPgUp})
	pageUp := cur.viewport.YOffset()
	if got, want := pageUp, bottom-cur.viewport.Height(); got != want {
		t.Fatalf("PageUp YOffset = %d, want %d", got, want)
	}

	cur = adv(cur, tea.KeyPressMsg{Code: tea.KeyPgDown})
	if got := cur.viewport.YOffset(); got != bottom {
		t.Fatalf("PageDown should return to bottom from one page up, YOffset=%d want %d", got, bottom)
	}
}

func TestRunningStreamPreservesScrolledReadingPosition(t *testing.T) {
	ctrl := control.New(control.Options{})
	ch := make(chan event.Event, 1)
	notice := agentEventMsg(event.Event{Kind: event.Notice, Level: event.LevelInfo, Text: "line"})
	adv := func(m chatTUI, msg tea.Msg) chatTUI {
		n, _ := m.Update(msg)
		return n.(chatTUI)
	}

	cur := adv(newChatTUI(ctrl, "", ch, 80), tea.WindowSizeMsg{Width: 80, Height: 10})
	for i := 0; i < 40; i++ {
		cur = adv(cur, notice)
	}
	cur.state = tuiRunning
	cur = adv(cur, tea.MouseWheelMsg{Button: tea.MouseWheelUp})
	readOffset := cur.viewport.YOffset()
	if cur.viewport.AtBottom() {
		t.Fatal("wheel-up should leave the bottom before streaming output arrives")
	}

	cur = adv(cur, agentEventMsg(event.Event{Kind: event.Text, Text: "streamed paragraph\n\n"}))
	if cur.viewport.AtBottom() {
		t.Fatal("streaming output must not yank a scrolled-up reader back to bottom")
	}
	if got := cur.viewport.YOffset(); got != readOffset {
		t.Fatalf("streaming output should preserve reading offset, got %d want %d", got, readOffset)
	}

	cur = adv(cur, tea.MouseWheelMsg{Button: tea.MouseWheelDown})
	if got, want := cur.viewport.YOffset(), readOffset+3; got != want {
		t.Fatalf("wheel-down while running should move one wheel step, got %d want %d", got, want)
	}
	if cur.viewport.AtBottom() {
		t.Fatal("one wheel-down step from the reading position should not jump straight to bottom")
	}
}

func TestTranscriptScrollbarClickAndDrag(t *testing.T) {
	ctrl := control.New(control.Options{})
	ch := make(chan event.Event, 1)
	notice := agentEventMsg(event.Event{Kind: event.Notice, Level: event.LevelInfo, Text: "line"})
	adv := func(m chatTUI, msg tea.Msg) chatTUI {
		n, _ := m.Update(msg)
		return n.(chatTUI)
	}

	cur := adv(newChatTUI(ctrl, "", ch, 80), tea.WindowSizeMsg{Width: 80, Height: 10})
	for i := 0; i < 40; i++ {
		cur = adv(cur, notice)
	}
	cur.viewport.GotoTop()
	barX := cur.viewport.Width()
	bottomRow := cur.viewport.Height() - 1

	cur = adv(cur, tea.MouseClickMsg{X: barX, Y: 0, Button: tea.MouseLeft})
	if cur.sel.active {
		t.Fatal("clicking the scrollbar must not start transcript selection")
	}
	if !cur.scrollbarDrag {
		t.Fatal("left-click on scrollbar should start scrollbar drag")
	}

	cur = adv(cur, tea.MouseMotionMsg{X: barX, Y: bottomRow, Button: tea.MouseLeft})
	if !cur.viewport.AtBottom() {
		t.Fatalf("dragging scrollbar to bottom should reach bottom, YOffset=%d", cur.viewport.YOffset())
	}
	if cur.sel.active {
		t.Fatal("dragging the scrollbar must not leave a transcript selection")
	}

	cur = adv(cur, tea.MouseReleaseMsg{X: barX, Y: bottomRow, Button: tea.MouseLeft})
	if cur.scrollbarDrag {
		t.Fatal("mouse release should end scrollbar drag")
	}
	if cur.sel.active {
		t.Fatal("scrollbar release must not create a text selection")
	}

	cur.viewport.GotoTop()
	cur = adv(cur, tea.MouseClickMsg{X: barX - 1, Y: 0, Button: tea.MouseLeft})
	if !cur.sel.active {
		t.Fatal("clicking the transcript content column next to the scrollbar should still start selection")
	}
}

func TestEchoLocalCommandAddsTranscriptMarker(t *testing.T) {
	m := newTestChatTUI()
	m.echoLocalCommand("  /tree  ")
	if len(*m.pendingCommit) != 1 {
		t.Fatalf("pending commits = %d, want 1", len(*m.pendingCommit))
	}
	if got := (*m.pendingCommit)[0]; !strings.Contains(got, "› /tree") {
		t.Fatalf("command echo = %q, want /tree marker", got)
	}
}

func isolateUserConfig(t *testing.T) {
	t.Helper()
	root := t.TempDir()
	t.Setenv("HOME", root)
	t.Setenv("REASONIX_CREDENTIALS_STORE", "file")
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
	t.Setenv("AppData", filepath.Join(root, "AppData")) // os.UserConfigDir reads AppData on Windows
	t.Chdir(root)
}

func TestEffortCommandWritesCurrentDeepSeekProvider(t *testing.T) {
	isolateUserConfig(t)

	m := newTestChatTUI()
	m.ctrl = control.New(control.Options{Label: "deepseek-flash"})
	m.modelRef = "deepseek-flash/deepseek-v4-flash"
	m.buildController = func(_ string, _ []provider.Message, _ string) (*control.Controller, error) {
		return control.New(control.Options{Label: "deepseek-flash"}), nil
	}

	cmd := m.runEffortCommand("/effort max")
	if cmd == nil {
		t.Fatal("/effort max should return a rebuild command")
	}

	configPath := config.UserConfigPath()
	body, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read saved config: %v", err)
	}
	if !strings.Contains(string(body), `effort      = "max"`) {
		t.Fatalf("saved config missing effort=max:\n%s", body)
	}
}

func TestEffortCommandRejectsUnsupportedProvider(t *testing.T) {
	isolateUserConfig(t)

	m := newTestChatTUI()
	m.ctrl = control.New(control.Options{Label: "mimo-pro"})
	m.modelRef = "mimo-pro/mimo-v2.5-pro"
	m.buildController = func(_ string, _ []provider.Message, _ string) (*control.Controller, error) {
		return control.New(control.Options{Label: "mimo-pro"}), nil
	}

	if cmd := m.runEffortCommand("/effort max"); cmd != nil {
		t.Fatal("unsupported provider should not rebuild")
	}
	if _, err := os.Stat(config.UserConfigPath()); !os.IsNotExist(err) {
		t.Fatalf("unsupported provider should not write config, stat err=%v", err)
	}
}

func TestEffortCommandAutoClearsProviderEffort(t *testing.T) {
	isolateUserConfig(t)

	m := newTestChatTUI()
	m.ctrl = control.New(control.Options{Label: "deepseek-flash"})
	m.modelRef = "deepseek-flash/deepseek-v4-flash"
	m.buildController = func(_ string, _ []provider.Message, _ string) (*control.Controller, error) {
		return control.New(control.Options{Label: "deepseek-flash"}), nil
	}

	if cmd := m.runEffortCommand("/effort max"); cmd == nil {
		t.Fatal("/effort max should return a rebuild command")
	}
	if cmd := m.runEffortCommand("/effort auto"); cmd == nil {
		t.Fatal("/effort auto should return a rebuild command")
	}
	body, err := os.ReadFile(config.UserConfigPath())
	if err != nil {
		t.Fatalf("read saved config: %v", err)
	}
	section := providerSection(string(body), "deepseek-flash")
	if strings.Contains(section, `effort      = "`) {
		t.Fatalf("auto should clear saved deepseek-flash effort:\n%s", section)
	}
}

func TestAutoPlanCommandPersistsAndUpdatesController(t *testing.T) {
	isolateUserConfig(t)

	runner := &recordingTurnRunner{}
	events := make(chan event.Event, 4)
	ctrl := control.New(control.Options{
		AutoPlan: "off",
		Runner:   runner,
		Sink: event.FuncSink(func(e event.Event) {
			events <- e
		}),
	})
	m := newTestChatTUI()
	m.ctrl = ctrl

	m.runAutoPlanCommand("/auto-plan on")

	body, err := os.ReadFile(config.UserConfigPath())
	if err != nil {
		t.Fatalf("read saved config: %v", err)
	}
	if !strings.Contains(string(body), `auto_plan   = "on"`) {
		t.Fatalf("saved config missing auto_plan=on:\n%s", body)
	}
	input := "实现 GitHub issue #2395：\n- 新增配置项\n- 自动判断复杂任务\n- 补测试和文档"
	ctrl.Send(input)
	waitForCLIEvent(t, events, event.TurnDone)
	if len(runner.inputs) != 1 || !strings.HasPrefix(runner.inputs[0], control.PlanModeMarker) {
		t.Fatalf("/auto-plan on should affect current controller, inputs=%q", runner.inputs)
	}
}

func TestAutoPlanCommandWritesUserConfigNotProjectConfig(t *testing.T) {
	isolateUserConfig(t)
	projectPath := filepath.Join(mustGetwd(t), "reasonix.toml")
	if err := os.WriteFile(projectPath, []byte("[agent]\nauto_plan = \"off\"\n"), 0o644); err != nil {
		t.Fatalf("write project config: %v", err)
	}

	m := newTestChatTUI()
	m.ctrl = control.New(control.Options{AutoPlan: "off"})
	m.runAutoPlanCommand("/auto-plan on")

	userBody, err := os.ReadFile(config.UserConfigPath())
	if err != nil {
		t.Fatalf("read user config: %v", err)
	}
	if !strings.Contains(string(userBody), `auto_plan   = "on"`) {
		t.Fatalf("user config missing auto_plan=on:\n%s", userBody)
	}
	projectBody, err := os.ReadFile(projectPath)
	if err != nil {
		t.Fatalf("read project config: %v", err)
	}
	if string(projectBody) != "[agent]\nauto_plan = \"off\"\n" {
		t.Fatalf("/auto-plan should not rewrite project config:\n%s", projectBody)
	}
}

func TestReasoningLanguageCommandPersistsAndUpdatesController(t *testing.T) {
	isolateUserConfig(t)

	ctrl := control.New(control.Options{ReasoningLanguage: "auto"})
	m := newTestChatTUI()
	m.ctrl = ctrl

	m.runReasoningLanguageCommand("/reasoning-language zh")

	body, err := os.ReadFile(config.UserConfigPath())
	if err != nil {
		t.Fatalf("read saved config: %v", err)
	}
	if !strings.Contains(string(body), `reasoning_language = "zh"`) {
		t.Fatalf("saved config missing reasoning_language=zh:\n%s", body)
	}
	composed := ctrl.Compose("hello")
	if !strings.HasPrefix(composed, "<reasoning-language>") || !strings.Contains(composed, "Simplified Chinese") {
		t.Fatalf("/reasoning-language zh should affect current controller, got %q", composed)
	}
}

func TestReasoningLanguageCommandWritesUserConfigNotProjectConfig(t *testing.T) {
	isolateUserConfig(t)
	projectPath := filepath.Join(mustGetwd(t), "reasonix.toml")
	if err := os.WriteFile(projectPath, []byte("[agent]\nreasoning_language = \"en\"\n"), 0o644); err != nil {
		t.Fatalf("write project config: %v", err)
	}

	m := newTestChatTUI()
	m.ctrl = control.New(control.Options{ReasoningLanguage: "en"})
	m.runReasoningLanguageCommand("/reasoning-language zh")

	userBody, err := os.ReadFile(config.UserConfigPath())
	if err != nil {
		t.Fatalf("read user config: %v", err)
	}
	if !strings.Contains(string(userBody), `reasoning_language = "zh"`) {
		t.Fatalf("user config missing reasoning_language=zh:\n%s", userBody)
	}
	projectBody, err := os.ReadFile(projectPath)
	if err != nil {
		t.Fatalf("read project config: %v", err)
	}
	if string(projectBody) != "[agent]\nreasoning_language = \"en\"\n" {
		t.Fatalf("/reasoning-language should not rewrite project config:\n%s", projectBody)
	}
}

func TestMemoryV5CommandWritesUserConfigNotProjectConfig(t *testing.T) {
	isolateUserConfig(t)
	projectPath := filepath.Join(mustGetwd(t), "reasonix.toml")
	if err := os.WriteFile(projectPath, []byte("[agent]\nmemory_compiler = { enabled = true }\n"), 0o644); err != nil {
		t.Fatalf("write project config: %v", err)
	}

	m := newTestChatTUI()
	m.ctrl = control.New(control.Options{})
	m.runMemoryV5Command("/memory-v5 off")

	userBody, err := os.ReadFile(config.UserConfigPath())
	if err != nil {
		t.Fatalf("read user config: %v", err)
	}
	if !strings.Contains(string(userBody), `memory_compiler = { enabled = false, verbosity = "observe" }`) {
		t.Fatalf("user config missing memory_compiler off:\n%s", userBody)
	}
	projectBody, err := os.ReadFile(projectPath)
	if err != nil {
		t.Fatalf("read project config: %v", err)
	}
	if string(projectBody) != "[agent]\nmemory_compiler = { enabled = true }\n" {
		t.Fatalf("/memory-v5 should not rewrite project config:\n%s", projectBody)
	}
}

func TestLanguageCommandSwitchesImmediatelyAndPersists(t *testing.T) {
	isolateUserConfig(t)
	i18n.DetectLanguage("en")
	t.Cleanup(func() { i18n.DetectLanguage("en") })

	m := newTestChatTUI()
	m.runLanguageSubcommand("/language zh")

	if i18n.M.ChatStatusIdle != "就绪" {
		t.Fatalf("/language zh did not switch active catalogue, idle=%q", i18n.M.ChatStatusIdle)
	}
	body, err := os.ReadFile(config.UserConfigPath())
	if err != nil {
		t.Fatalf("read saved config: %v", err)
	}
	if !strings.Contains(string(body), `language      = "zh"`) {
		t.Fatalf("saved config missing language=zh:\n%s", body)
	}
}

func TestLanguageCommandAutoClearsPinnedLanguage(t *testing.T) {
	isolateUserConfig(t)
	i18n.DetectLanguage("en")
	t.Cleanup(func() { i18n.DetectLanguage("en") })

	m := newTestChatTUI()
	m.runLanguageSubcommand("/language zh")
	m.runLanguageSubcommand("/language auto")

	cfg := config.LoadForEdit(config.UserConfigPath())
	if cfg.Language != "" {
		t.Fatalf("auto should clear saved language override, got %q", cfg.Language)
	}
}

func TestLanguageCommandAutoClearsLowerPriorityUserOverride(t *testing.T) {
	isolateUserConfig(t)
	t.Setenv("REASONIX_LANG", "")
	t.Setenv("LC_ALL", "")
	t.Setenv("LC_MESSAGES", "")
	t.Setenv("LANG", "")
	i18n.DetectLanguage("en")
	t.Cleanup(func() { i18n.DetectLanguage("en") })

	userPath := config.UserConfigPath()
	userCfg := config.LoadForEdit(userPath)
	if err := userCfg.SetLanguage("zh"); err != nil {
		t.Fatalf("set user language: %v", err)
	}
	if err := userCfg.SaveTo(userPath); err != nil {
		t.Fatalf("save user config: %v", err)
	}
	projectCfg := config.Default()
	if err := projectCfg.SaveTo("reasonix.toml"); err != nil {
		t.Fatalf("save project config: %v", err)
	}

	m := newTestChatTUI()
	m.runLanguageSubcommand("/language auto")

	userCfg = config.LoadForEdit(userPath)
	if userCfg.Language != "" {
		t.Fatalf("/language auto should clear lower-priority user override, got %q", userCfg.Language)
	}
	loaded, err := config.Load()
	if err != nil {
		t.Fatalf("load merged config: %v", err)
	}
	if loaded.Language != "" {
		t.Fatalf("merged config should be auto-detect after clearing overrides, got %q", loaded.Language)
	}
}

func providerSection(body, name string) string {
	needle := `name        = "` + name + `"`
	start := strings.Index(body, needle)
	if start < 0 {
		return ""
	}
	end := strings.Index(body[start+len(needle):], "\n[[providers]]")
	if end < 0 {
		return body[start:]
	}
	return body[start : start+len(needle)+end]
}

func TestSubmittedInputRecallWithArrowKeys(t *testing.T) {
	m := newTestChatTUI()
	m.rememberSubmittedInput("first")
	m.rememberSubmittedInput("second")
	m.input.SetValue("draft")

	up := tea.KeyPressMsg{Code: tea.KeyUp}
	down := tea.KeyPressMsg{Code: tea.KeyDown}

	model, _ := m.Update(up)
	m = model.(chatTUI)
	if got := m.input.Value(); got != "second" {
		t.Fatalf("first up should recall latest input, got %q", got)
	}

	model, _ = m.Update(up)
	m = model.(chatTUI)
	if got := m.input.Value(); got != "first" {
		t.Fatalf("second up should recall older input, got %q", got)
	}

	model, _ = m.Update(down)
	m = model.(chatTUI)
	if got := m.input.Value(); got != "second" {
		t.Fatalf("down should move toward newer input, got %q", got)
	}

	model, _ = m.Update(down)
	m = model.(chatTUI)
	if got := m.input.Value(); got != "draft" {
		t.Fatalf("down past newest should restore draft, got %q", got)
	}
}

func TestQueueNavigationWithArrowKeys(t *testing.T) {
	m := newTestChatTUI()
	m.state = tuiRunning
	m.pendingInterject = []string{"queued one", "queued two", "queued three"}
	m.input.SetValue("my draft")

	up := tea.KeyPressMsg{Code: tea.KeyUp}
	down := tea.KeyPressMsg{Code: tea.KeyDown}

	// First ↑ should save draft and jump to last queued item.
	model, _ := m.Update(up)
	m = model.(chatTUI)
	if got := m.input.Value(); got != "queued three" {
		t.Fatalf("first up: want %q, got %q", "queued three", got)
	}
	if m.queueEditCursor != 2 {
		t.Fatalf("first up: cursor should be 2, got %d", m.queueEditCursor)
	}

	// Second ↑ should move to "queued two".
	model, _ = m.Update(up)
	m = model.(chatTUI)
	if got := m.input.Value(); got != "queued two" {
		t.Fatalf("second up: want %q, got %q", "queued two", got)
	}

	// ↓ should move back to "queued three".
	model, _ = m.Update(down)
	m = model.(chatTUI)
	if got := m.input.Value(); got != "queued three" {
		t.Fatalf("down: want %q, got %q", "queued three", got)
	}

	// ↓ past the end should restore the draft.
	model, _ = m.Update(down)
	m = model.(chatTUI)
	if got := m.input.Value(); got != "my draft" {
		t.Fatalf("down past end: want %q, got %q", "my draft", got)
	}
	if m.queueEditCursor != -1 {
		t.Fatalf("down past end: cursor should be -1, got %d", m.queueEditCursor)
	}
}

func TestQueueNavigationClampAtStart(t *testing.T) {
	m := newTestChatTUI()
	m.state = tuiRunning
	m.pendingInterject = []string{"only item"}
	m.input.SetValue("draft")

	up := tea.KeyPressMsg{Code: tea.KeyUp}
	// First ↑ jumps to the only item.
	model, _ := m.Update(up)
	m = model.(chatTUI)
	if got := m.input.Value(); got != "only item" {
		t.Fatalf("first up: want %q, got %q", "only item", got)
	}
	// Second ↑ should clamp at index 0 (not go negative).
	model, _ = m.Update(up)
	m = model.(chatTUI)
	if m.queueEditCursor != 0 {
		t.Fatalf("second up: cursor should clamp at 0, got %d", m.queueEditCursor)
	}
	if got := m.input.Value(); got != "only item" {
		t.Fatalf("second up: value should stay %q, got %q", "only item", got)
	}
}

func TestQueueNavigationNoOpWhenEmpty(t *testing.T) {
	m := newTestChatTUI()
	m.state = tuiRunning
	m.input.SetValue("hello")

	up := tea.KeyPressMsg{Code: tea.KeyUp}
	model, _ := m.Update(up)
	m = model.(chatTUI)
	if got := m.input.Value(); got != "hello" {
		t.Fatalf("empty queue: input should be unchanged, got %q", got)
	}
}

func TestQueueEditSavesOnEnter(t *testing.T) {
	m := newTestChatTUI()
	m.state = tuiRunning
	m.pendingInterject = []string{"original one", "original two"}

	up := tea.KeyPressMsg{Code: tea.KeyUp}
	model, _ := m.Update(up)
	m = model.(chatTUI)
	if m.queueEditCursor != 1 {
		t.Fatalf("cursor should be 1 after up, got %d", m.queueEditCursor)
	}

	// Edit the queued message.
	m.input.SetValue("edited two")
	enter := tea.KeyPressMsg{Code: tea.KeyEnter}
	model, _ = m.Update(enter)
	m = model.(chatTUI)

	if m.pendingInterject[1] != "edited two" {
		t.Fatalf("queue[1] should be %q, got %q", "edited two", m.pendingInterject[1])
	}
	if m.pendingInterject[0] != "original one" {
		t.Fatalf("queue[0] should be unchanged, got %q", m.pendingInterject[0])
	}
	if m.queueEditCursor != -1 {
		t.Fatalf("cursor should reset after enter, got %d", m.queueEditCursor)
	}
}

func TestQueueNewMessageOnEnterDuringRunning(t *testing.T) {
	m := newTestChatTUI()
	m.state = tuiRunning
	m.pendingInterject = []string{"existing"}

	m.input.SetValue("new message")
	enter := tea.KeyPressMsg{Code: tea.KeyEnter}
	model, _ := m.Update(enter)
	m = model.(chatTUI)

	if len(m.pendingInterject) != 2 {
		t.Fatalf("queue should have 2 items, got %d", len(m.pendingInterject))
	}
	if m.pendingInterject[1] != "new message" {
		t.Fatalf("queue[1] should be %q, got %q", "new message", m.pendingInterject[1])
	}
}

func TestQueuedFoldedPasteExpandsBeforeInterjectSend(t *testing.T) {
	runner := &recordingTurnRunner{}
	events := make(chan event.Event, 8)
	ctrl := control.New(control.Options{
		Runner:     runner,
		Sink:       event.FuncSink(func(e event.Event) { events <- e }),
		SessionDir: t.TempDir(),
		Label:      "test",
	})
	m := newTestChatTUI()
	m.ctrl = ctrl
	m.eventCh = make(chan event.Event, 8)
	m.state = tuiRunning

	pasted := strings.Repeat("queued pasted content\n", 10)
	model, _ := m.Update(tea.PasteMsg{Content: pasted})
	m = model.(chatTUI)

	display := strings.TrimSpace(m.input.Value())
	if !strings.Contains(display, "[Pasted text #1") {
		t.Fatalf("paste should be folded, got %q", display)
	}

	model, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = model.(chatTUI)

	if len(m.pendingInterject) != 1 {
		t.Fatalf("queue should have 1 item, got %d", len(m.pendingInterject))
	}
	queued := m.pendingInterject[0]
	if queued == display {
		t.Fatalf("queued interject kept the folded placeholder: %q", queued)
	}
	for _, want := range []string{
		"queued pasted content",
		"--- Begin [Pasted text #1",
		"--- End [Pasted text #1",
	} {
		if !strings.Contains(queued, want) {
			t.Fatalf("queued interject missing %q in:\n%s", want, queued)
		}
	}

	model, _ = m.Update(agentEventMsg(event.Event{Kind: event.TurnDone}))
	m = model.(chatTUI)
	waitForCLIEvent(t, events, event.TurnDone)

	if len(runner.inputs) != 1 {
		t.Fatalf("runner should receive queued interject, inputs=%q", runner.inputs)
	}
	sent := runner.inputs[0]
	if sent == display {
		t.Fatalf("runner received the folded placeholder: %q", sent)
	}
	if !strings.Contains(sent, "queued pasted content") {
		t.Fatalf("runner input missing pasted content:\n%s", sent)
	}
}

func TestQueueNavigationResetOnNonUpDownKey(t *testing.T) {
	m := newTestChatTUI()
	m.state = tuiRunning
	m.pendingInterject = []string{"queued"}

	up := tea.KeyPressMsg{Code: tea.KeyUp}
	model, _ := m.Update(up)
	m = model.(chatTUI)
	if m.queueEditCursor != 0 {
		t.Fatalf("cursor should be 0 after up, got %d", m.queueEditCursor)
	}

	// A regular key should reset the queue navigation cursor.
	letter := tea.KeyPressMsg{Code: 'a'}
	model, _ = m.Update(letter)
	m = model.(chatTUI)
	if m.queueEditCursor != -1 {
		t.Fatalf("cursor should reset on non-up/down key, got %d", m.queueEditCursor)
	}
}

func TestQueueIndicatorRendering(t *testing.T) {
	m := newTestChatTUI()
	m.state = tuiRunning
	m.pendingInterject = []string{"first msg", "second msg"}

	qi := m.renderQueueIndicator()
	if qi == "" {
		t.Fatal("queue indicator should not be empty when queue has items and running")
	}
	if !strings.Contains(qi, "[1]") || !strings.Contains(qi, "[2]") {
		t.Fatalf("queue indicator should contain [1] and [2], got %q", qi)
	}
	if !strings.Contains(qi, "first msg") || !strings.Contains(qi, "second msg") {
		t.Fatalf("queue indicator should show message previews, got %q", qi)
	}

	// Highlight marker should appear for the browsed item.
	m.queueEditCursor = 1
	qi = m.renderQueueIndicator()
	if !strings.Contains(qi, "▸") {
		t.Fatalf("queue indicator should show ▸ for browsed item, got %q", qi)
	}
}

func TestQueueIndicatorHiddenWhenIdle(t *testing.T) {
	m := newTestChatTUI()
	m.state = tuiIdle
	m.pendingInterject = []string{"queued"}

	if qi := m.renderQueueIndicator(); qi != "" {
		t.Fatalf("queue indicator should be empty when idle, got %q", qi)
	}
}

// TestViewAltScreenFillsHeight proves the switch to alt-screen: View requests
// the alt buffer with mouse reporting for wheel scrolling and in-app text
// selection, and the frame is exactly the terminal height (the transcript
// viewport pads to fill above the pinned bottom region).
func TestViewAltScreenFillsHeight(t *testing.T) {
	ctrl := control.New(control.Options{})
	m := newChatTUI(ctrl, "", make(chan event.Event, 1), 80)
	m.nativeScrollback = false
	m0, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	v := m0.(chatTUI).View()

	if !v.AltScreen {
		t.Error("View must request alt-screen so resize repaints the whole grid")
	}
	if v.MouseMode != tea.MouseModeCellMotion {
		t.Error("View must enable mouse so the wheel scrolls the transcript")
	}
	if lines := strings.Count(v.Content, "\n") + 1; lines != 24 {
		t.Errorf("alt-screen frame = %d lines, want 24 (full terminal height)", lines)
	}
}

func TestViewTermuxUsesNativeScrollback(t *testing.T) {
	ctrl := control.New(control.Options{})
	m := newChatTUI(ctrl, "", make(chan event.Event, 1), 80)
	m.nativeScrollback = true
	m0, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	v := m0.(chatTUI).View()

	if v.AltScreen {
		t.Error("Termux view must stay in the normal screen so native touch scrollback works")
	}
	if v.MouseMode != tea.MouseModeNone {
		t.Error("Termux view must not enable mouse mode because it prevents soft-keyboard focus")
	}
	if lines := strings.Count(v.Content, "\n") + 1; lines >= 24 {
		t.Errorf("Termux view should render only the pinned bottom frame, got %d full-screen lines", lines)
	}
}

// TestTranscriptTailFollow proves the viewport pins to newest output while the
// user is at the bottom, and stops yanking once the user scrolls up.
func TestTranscriptTailFollow(t *testing.T) {
	ctrl := control.New(control.Options{})
	adv := func(m chatTUI, msg tea.Msg) chatTUI {
		n, _ := m.Update(msg)
		return n.(chatTUI)
	}
	notice := agentEventMsg(event.Event{Kind: event.Notice, Level: event.LevelInfo, Text: "line"})

	cur := adv(newChatTUI(ctrl, "", make(chan event.Event, 1), 80), tea.WindowSizeMsg{Width: 80, Height: 8})
	for i := 0; i < 12; i++ { // overflow the short viewport so there's room to scroll
		cur = adv(cur, notice)
	}
	if !cur.viewport.AtBottom() {
		t.Fatal("new output while pinned should keep the viewport at the bottom")
	}

	cur = adv(cur, tea.MouseWheelMsg{Button: tea.MouseWheelUp})
	if cur.viewport.AtBottom() {
		t.Fatal("wheel-up should break the bottom pin")
	}

	cur = adv(cur, notice)
	if cur.viewport.AtBottom() {
		t.Error("new output while scrolled up must preserve the reading position")
	}
}

// TestEmptyEnterScrollsToBottom proves that pressing Enter with an empty composer
// scrolls the viewport to the bottom in both idle and running states, so the user
// can quickly tail-follow after scrolling up to read history.
func TestEmptyEnterScrollsToBottom(t *testing.T) {
	ctrl := control.New(control.Options{})
	ch := make(chan event.Event, 1)
	notice := agentEventMsg(event.Event{Kind: event.Notice, Level: event.LevelInfo, Text: "line"})
	adv := func(m chatTUI, msg tea.Msg) chatTUI {
		n, _ := m.Update(msg)
		return n.(chatTUI)
	}

	// --- idle state ---
	t.Run("idle", func(t *testing.T) {
		cur := adv(newChatTUI(ctrl, "", ch, 80), tea.WindowSizeMsg{Width: 80, Height: 8})
		for i := 0; i < 12; i++ {
			cur = adv(cur, notice)
		}
		// Scroll up to leave the bottom.
		cur = adv(cur, tea.MouseWheelMsg{Button: tea.MouseWheelUp})
		if cur.viewport.AtBottom() {
			t.Fatal("wheel-up should break the bottom pin")
		}
		// Empty enter → should snap back to bottom.
		cur = adv(cur, tea.KeyPressMsg{Code: tea.KeyEnter})
		if !cur.viewport.AtBottom() {
			t.Error("empty enter while idle should scroll viewport to bottom")
		}
	})

	// --- running state ---
	t.Run("running", func(t *testing.T) {
		cur := adv(newChatTUI(ctrl, "", ch, 80), tea.WindowSizeMsg{Width: 80, Height: 8})
		for i := 0; i < 12; i++ {
			cur = adv(cur, notice)
		}
		cur.state = tuiRunning
		cur = adv(cur, tea.MouseWheelMsg{Button: tea.MouseWheelUp})
		if cur.viewport.AtBottom() {
			t.Fatal("wheel-up should break the bottom pin")
		}
		cur = adv(cur, tea.KeyPressMsg{Code: tea.KeyEnter})
		if !cur.viewport.AtBottom() {
			t.Error("empty enter while running should scroll viewport to bottom")
		}
	})
}

// TestForceGotoBottomScrollsWithoutTranscriptChange keeps the force-bottom
// contract independent from transcript length, width, or dirty-state changes.
func TestForceGotoBottomScrollsWithoutTranscriptChange(t *testing.T) {
	ctrl := control.New(control.Options{})
	ch := make(chan event.Event, 1)
	notice := agentEventMsg(event.Event{Kind: event.Notice, Level: event.LevelInfo, Text: "line"})
	adv := func(m chatTUI, msg tea.Msg) (chatTUI, tea.Cmd) {
		n, cmd := m.Update(msg)
		return n.(chatTUI), cmd
	}
	next := func(m chatTUI, msg tea.Msg) chatTUI {
		n, _ := adv(m, msg)
		return n
	}

	cur := next(newChatTUI(ctrl, "", ch, 80), tea.WindowSizeMsg{Width: 80, Height: 8})
	for i := 0; i < 12; i++ {
		cur = next(cur, notice)
	}
	if !cur.viewport.AtBottom() {
		t.Fatal("new output while pinned should keep the viewport at the bottom")
	}

	cur = next(cur, tea.MouseWheelMsg{Button: tea.MouseWheelUp})
	if cur.viewport.AtBottom() {
		t.Fatal("wheel-up should break the bottom pin")
	}

	cur.forceGotoBottom = true
	cur.transcriptDirty = false
	cur, cmd := adv(cur, tea.WindowSizeMsg{Width: 80, Height: 8})

	if !cur.viewport.AtBottom() {
		t.Fatalf("forceGotoBottom should scroll without transcript changes, YOffset=%d", cur.viewport.YOffset())
	}
	if cur.forceGotoBottom {
		t.Fatal("forceGotoBottom should be cleared after scrolling")
	}
	if cmd == nil {
		t.Fatal("regular forceGotoBottom scroll jump should request ClearScreen")
	}
}

func TestSessionSwitchSuppressesOneClearScreen(t *testing.T) {
	ctrl := control.New(control.Options{})
	ch := make(chan event.Event, 1)
	notice := agentEventMsg(event.Event{Kind: event.Notice, Level: event.LevelInfo, Text: "line"})
	adv := func(m chatTUI, msg tea.Msg) (chatTUI, tea.Cmd) {
		n, cmd := m.Update(msg)
		return n.(chatTUI), cmd
	}
	next := func(m chatTUI, msg tea.Msg) chatTUI {
		n, _ := adv(m, msg)
		return n
	}

	cur := next(newChatTUI(ctrl, "", ch, 80), tea.WindowSizeMsg{Width: 80, Height: 8})
	for i := 0; i < 12; i++ {
		cur = next(cur, notice)
	}
	cur = next(cur, tea.MouseWheelMsg{Button: tea.MouseWheelUp})
	if cur.viewport.AtBottom() {
		t.Fatal("wheel-up should break the bottom pin")
	}

	cur.sessionSwitch = true
	cur.forceGotoBottom = true
	cur.transcriptDirty = false
	cur, cmd := adv(cur, tea.WindowSizeMsg{Width: 80, Height: 8})

	if cmd != nil {
		t.Fatal("session switch rebuild should suppress the ClearScreen scroll-jump workaround once")
	}
	if cur.sessionSwitch {
		t.Fatal("sessionSwitch should be cleared after one Update")
	}
	if !cur.viewport.AtBottom() {
		t.Fatalf("session switch should still land at bottom, YOffset=%d", cur.viewport.YOffset())
	}

	cur = next(cur, tea.MouseWheelMsg{Button: tea.MouseWheelUp})
	cur.forceGotoBottom = true
	cur, cmd = adv(cur, tea.WindowSizeMsg{Width: 80, Height: 8})
	if cmd == nil {
		t.Fatal("later scroll jumps must still request ClearScreen")
	}
	if cur.sessionSwitch {
		t.Fatal("sessionSwitch should remain false after the suppressed cycle")
	}
}

func TestReplayActiveBranchClearsPlanModeAndMarksSessionSwitch(t *testing.T) {
	m := newTestChatTUI()
	m.ctrl = control.New(control.Options{})
	m.planMode = true
	m.ctrl.SetPlanMode(true)
	m.sessionSwitch = false

	m.replayActiveBranch("switched branch")

	if m.planMode || m.ctrl.PlanMode() {
		t.Fatalf("replay should clear plan mode on both TUI and controller, tui=%v controller=%v", m.planMode, m.ctrl.PlanMode())
	}
	if !m.sessionSwitch {
		t.Fatal("replay should mark the next Update as a session switch")
	}
}

func TestFoldedPasteUsesPlaceholderAndExpandsOnSend(t *testing.T) {
	m := newTestChatTUI()
	pasted := "{\n  \"a\": 1,\n  \"b\": 2,\n  \"c\": 3,\n  \"d\": 4\n}"
	if !shouldFoldPastedText(pasted) {
		t.Fatal("five-line paste should fold")
	}

	m.insertFoldedPaste(pasted)
	display := m.input.Value()
	if display != "[Pasted text #1 · 6 lines] " {
		t.Fatalf("display = %q", display)
	}

	sent := m.expandPastedBlocks(display)
	for _, want := range []string{
		"--- Begin [Pasted text #1 · 6 lines] ---",
		`"d": 4`,
		"--- End [Pasted text #1 · 6 lines] ---",
	} {
		if !strings.Contains(sent, want) {
			t.Fatalf("expanded paste missing %q in:\n%s", want, sent)
		}
	}
}

func TestTextOnlyModelSendsPastedImageRefsForToolUse(t *testing.T) {
	workspace := t.TempDir()
	writeTUIImageCapabilityConfig(t, workspace)
	path := saveTestImageAttachment(t, workspace)

	runner := &recordingTurnRunner{}
	events := make(chan event.Event, 8)
	m := newTestChatTUI()
	m.ctrl = control.New(control.Options{
		Runner: runner,
		Sink: event.FuncSink(func(e event.Event) {
			events <- e
		}),
		WorkspaceRoot: workspace,
		ModelRef:      "custom/text-only",
	})
	m.pastedBlocks = []pastedBlock{{label: "[image #1]", text: "@" + path, image: true}}
	m.input.SetValue("describe [image #1] please")

	model, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = model.(chatTUI)
	if cmd == nil {
		t.Fatal("text-only image ref send should resolve refs before starting the turn")
	}
	msg := cmd()
	if _, ok := msg.(refsResolvedMsg); !ok {
		t.Fatalf("enter cmd = %T, want refsResolvedMsg", msg)
	}
	model, _ = m.Update(msg)
	m = model.(chatTUI)
	waitForCLIEvent(t, events, event.TurnDone)

	if len(runner.inputs) != 1 {
		t.Fatalf("text-only model should send the image ref for tool use, inputs=%q", runner.inputs)
	}
	if !strings.Contains(runner.inputs[0], "@"+path) {
		t.Fatalf("runner input should retain the image ref context, got %q", runner.inputs[0])
	}
	if !strings.Contains(runner.inputs[0], "OCR/image/vision tool") {
		t.Fatalf("runner input should mention tool-based image handling, got %q", runner.inputs[0])
	}
	if got := strings.Join(m.transcript, "\n"); strings.Contains(got, "will not receive images directly") {
		t.Fatalf("text-only model should not block image refs that tools can read, transcript=%q", got)
	}
}

func TestVisionModelAllowsSendingPastedImageRefs(t *testing.T) {
	workspace := t.TempDir()
	writeTUIImageCapabilityConfig(t, workspace)
	path := saveTestImageAttachment(t, workspace)

	runner := &recordingTurnRunner{}
	events := make(chan event.Event, 8)
	m := newTestChatTUI()
	m.ctrl = control.New(control.Options{
		Runner: runner,
		Sink: event.FuncSink(func(e event.Event) {
			events <- e
		}),
		WorkspaceRoot: workspace,
		ModelRef:      "custom/vision-pro",
	})
	m.pastedBlocks = []pastedBlock{{label: "[image #1]", text: "@" + path, image: true}}
	m.input.SetValue("describe [image #1] please")

	model, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = model.(chatTUI)
	if cmd == nil {
		t.Fatal("vision model send should resolve refs before starting the turn")
	}
	msg := cmd()
	if _, ok := msg.(refsResolvedMsg); !ok {
		t.Fatalf("enter cmd = %T, want refsResolvedMsg", msg)
	}
	model, _ = m.Update(msg)
	m = model.(chatTUI)
	waitForCLIEvent(t, events, event.TurnDone)

	if len(runner.inputs) != 1 {
		t.Fatalf("vision model should send exactly one turn, inputs=%q", runner.inputs)
	}
	if !strings.Contains(runner.inputs[0], "@"+path) {
		t.Fatalf("runner input should retain the image ref context, got %q", runner.inputs[0])
	}
	if got := strings.Join(m.transcript, "\n"); strings.Contains(got, "will not receive images directly") {
		t.Fatalf("vision-capable model should not warn about image input, transcript=%q", got)
	}
}

// TestPasteFoldExpandOnSubmit verifies that a folded paste is fully expanded
// before being sent to the controller (the LLM sees the actual content, not just
// the placeholder label).
func TestPasteFoldExpandOnSubmit(t *testing.T) {
	r := &recordingTurnRunner{}
	events := make(chan event.Event, 64)
	ctrl := control.New(control.Options{
		Runner:     r,
		Sink:       event.FuncSink(func(e event.Event) { events <- e }),
		SessionDir: t.TempDir(),
		Label:      "test",
	})

	m := newTestChatTUI()
	m.ctrl = ctrl
	m.eventCh = make(chan event.Event, 64)

	// Simulate a multi-line paste that meets the fold threshold (≥5 lines).
	pasted := strings.Repeat("line of pasted content\n", 10)
	model, _ := m.Update(tea.PasteMsg{Content: pasted})
	m = model.(chatTUI)

	display := m.input.Value()
	if !strings.Contains(display, "[Pasted text #1") {
		t.Fatalf("paste should be folded, got: %q", display)
	}
	if len(m.pastedBlocks) != 1 {
		t.Fatalf("expected 1 pastedBlock, got %d", len(m.pastedBlocks))
	}

	// Simulate pressing Enter to submit.
	// NOTE: in a real terminal KeyEnter has empty Text, so String() returns "enter".
	model, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = model.(chatTUI)

	waitForCLIEvent(t, events, event.TurnDone)

	if len(r.inputs) == 0 {
		t.Fatal("runner.Run was not called — the paste was never submitted")
	}
	sentToRunner := r.inputs[0]
	t.Logf("sent to runner (%d bytes):\n%s", len(sentToRunner), sentToRunner)

	// The runner must receive the FULL expanded paste, not just the label.
	if !strings.Contains(sentToRunner, "line of pasted content") {
		t.Fatalf("runner received only the placeholder label, not the expanded paste content.\nGot: %q", sentToRunner)
	}
	// Verify the expanded markers are present.
	if !strings.Contains(sentToRunner, "--- Begin [Pasted text #1") {
		t.Fatalf("missing Begin marker in runner input.\nGot: %q", sentToRunner)
	}
	if !strings.Contains(sentToRunner, "--- End [Pasted text #1") {
		t.Fatalf("missing End marker in runner input.\nGot: %q", sentToRunner)
	}

	// The memory compiler (enabled by default) replaces the user turn with an
	// execution contract whose source_event is the controller's `raw` value.
	// If `raw` were the folded label, the model would only ever see
	// "[Pasted text #1 · N lines]" and never the pasted content. Assert the
	// source_event carries the EXPANDED content.
	if len(r.memoryCompilerInputs) == 0 {
		t.Fatal("memory compiler source input was not set on the context")
	}
	mcSource := r.memoryCompilerInputs[0]
	if strings.Contains(mcSource, "[Pasted text #1") && !strings.Contains(mcSource, "line of pasted content") {
		t.Fatalf("memory compiler source_event has the folded label but not the expanded content:\n%q", mcSource)
	}
	if !strings.Contains(mcSource, "line of pasted content") {
		t.Fatalf("memory compiler source_event must contain the expanded paste content, got:\n%q", mcSource)
	}
}

func TestPasteMsgFoldsBeforeTextareaConsumesNewlines(t *testing.T) {
	m := newTestChatTUI()
	model, _ := m.Update(tea.PasteMsg{Content: "1\n2\n3\n4\n5"})
	got := model.(chatTUI)
	if got.input.Value() != "[Pasted text #1 · 5 lines] " {
		t.Fatalf("input = %q", got.input.Value())
	}
	if got.input.Height() != 1 {
		t.Fatalf("folded paste should keep one input row, got %d", got.input.Height())
	}
}

func TestUnsendRestoresFoldedPastePlaceholder(t *testing.T) {
	m := newTestChatTUI()
	m.ctrl = control.New(control.Options{})
	m.bubbleStartIdx = len(m.transcript)
	m.commitLine("")
	m.commitLine(renderUserBubble("expanded JSON", m.width, m.planMode))
	m.pendingRestore = "[Pasted text #1 · 5 lines] 这是什么?"
	m.bubblePending = true
	m.state = tuiRunning

	m.unsendPending()

	if got := m.input.Value(); got != "[Pasted text #1 · 5 lines] 这是什么?" {
		t.Fatalf("restored input = %q", got)
	}
	if len(m.transcript) != m.bubbleStartIdx {
		t.Fatalf("un-send should pop the echoed bubble, transcript=%v", m.transcript)
	}
	if m.pendingRestore != "" || m.bubblePending {
		t.Fatalf("pending state not cleared: restore=%q pending=%v", m.pendingRestore, m.bubblePending)
	}
}

func TestApprovalToolDetailsShortensMCPNames(t *testing.T) {
	name, detail := approvalToolDetails("mcp__minimax-coding-plan-mcp__understand_image")
	if name != "understand_image" {
		t.Fatalf("name = %q, want understand_image", name)
	}
	for _, want := range []string{"provided image input", "minimax-coding-plan-mcp"} {
		if !strings.Contains(detail, want) {
			t.Errorf("detail = %q, want it to contain %q", detail, want)
		}
	}

	name, detail = approvalToolDetails("bash")
	if name != "bash" || !strings.Contains(detail, "built-in") {
		t.Errorf("built-in details = (%q, %q), want bash + built-in source", name, detail)
	}
}

// TestSlashQuitExit verifies that /quit and /exit slash commands return tea.Quit,
// providing an alternative to Ctrl+D and the bare "quit"/"exit" text commands.
func TestSlashQuitExit(t *testing.T) {
	m := newTestChatTUI()
	for _, cmd := range []string{"/quit", "/exit"} {
		got := m.runSlashCommand(cmd)
		if got == nil {
			t.Errorf("%s should return a quit cmd, got nil", cmd)
			continue
		}
		msg := got()
		if _, ok := msg.(tea.QuitMsg); !ok {
			t.Errorf("%s cmd should produce QuitMsg, got %T", cmd, msg)
		}
	}
}

func TestSlashMigrateShowsProgress(t *testing.T) {
	isolateCLIConfigHome(t)
	m := newTestChatTUI()

	if cmd := m.runSlashCommand("/migrate"); cmd != nil {
		t.Fatal("/migrate should run locally without returning a command")
	}
	out := strings.Join(m.transcript, "\n")
	for _, want := range []string{
		"/migrate",
		"migration rescue: checking legacy config and credentials",
		"migration rescue: scanning legacy memory",
		"migration rescue: scanning legacy sessions",
		"migration rescue complete:",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in transcript:\n%s", want, out)
		}
	}
}

func TestSlashMigrateFromImportsExplicitSessions(t *testing.T) {
	home := isolateCLIConfigHome(t)
	legacySessions := filepath.Join(home, "Old Reasonix", "sessions")
	if err := os.MkdirAll(legacySessions, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacySessions, "old-chat.jsonl"), []byte(`{"role":"user","content":"hello from old install"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := newTestChatTUI()

	input := `/migrate --from "` + filepath.Dir(legacySessions) + `"`
	if cmd := m.runSlashCommand(input); cmd != nil {
		t.Fatal("/migrate --from should run locally without returning a command")
	}
	out := strings.Join(m.transcript, "\n")
	for _, want := range []string{
		input,
		"migration rescue: scanning explicit legacy sessions from " + filepath.Dir(legacySessions),
		"imported 1 past session(s) from " + legacySessions,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in transcript:\n%s", want, out)
		}
	}
}

// TestDoubleCtrlCQuit verifies that Ctrl+C while idle requires a double-press
// within the 1.5s window to actually quit. A single press shows a hint; a
// second press within the window returns tea.Quit.
func TestDoubleCtrlCQuit(t *testing.T) {
	ctrl := control.New(control.Options{})
	m := newChatTUI(ctrl, "", make(chan event.Event, 1), 80)
	ctrlC := tea.KeyPressMsg{Code: 'c', Mod: 4} // 4 = ModCtrl

	// First Ctrl+C while idle: arms quit, flushes hint via finalize cmd.
	out, cmd := m.Update(ctrlC)
	if cmd == nil {
		t.Error("first Ctrl+C should return a finalize cmd to flush the hint")
	}
	m2, ok := out.(chatTUI)
	if !ok {
		t.Fatalf("Update returned %T, want chatTUI", out)
	}
	if m2.lastCtrlCAt.IsZero() {
		t.Error("first Ctrl+C should set lastCtrlCAt")
	}

	// Second Ctrl+C within window: returns tea.Quit.
	out2, cmd2 := m2.Update(ctrlC)
	if cmd2 == nil {
		t.Error("second Ctrl+C within window should return a quit cmd")
	}
	_ = out2

	// Window expired: re-arms instead of quitting (still flushes hint via finalize).
	m3 := m2
	m3.lastCtrlCAt = time.Now().Add(-2 * time.Second)
	out4, cmd4 := m3.Update(ctrlC)
	if cmd4 == nil {
		t.Error("expired Ctrl+C should return a finalize cmd to flush the re-armed hint")
	}
	m4, ok := out4.(chatTUI)
	if !ok {
		t.Fatalf("Update returned %T, want chatTUI", out4)
	}
	// lastCtrlCAt should be refreshed to now.
	if time.Since(m4.lastCtrlCAt) > time.Second {
		t.Error("expired Ctrl+C should refresh lastCtrlCAt")
	}
}

func TestSecondCtrlCQuitsAfterCancelIsAlreadyRequested(t *testing.T) {
	r := &stubbornTurnRunner{started: make(chan struct{}), release: make(chan struct{})}
	ctrl := control.New(control.Options{Runner: r, Sink: event.Discard, SessionDir: t.TempDir(), Label: "test"})
	ctrl.Send("hi")
	<-r.started
	defer close(r.release)

	m := newTestChatTUI()
	m.ctrl = ctrl
	m.state = tuiRunning
	ctrlC := tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}

	_, firstCmd := m.Update(ctrlC)
	if firstCmd != nil {
		t.Fatal("first Ctrl+C while running should request cancel, not quit")
	}
	if st := ctrl.RuntimeStatus(); !st.Running || !st.CancelRequested {
		t.Fatalf("first Ctrl+C status = %+v, want running cancel requested", st)
	}

	_, secondCmd := m.Update(ctrlC)
	if secondCmd == nil {
		t.Fatal("second Ctrl+C after cancel request should quit")
	}
	if msg := secondCmd(); msg != (tea.QuitMsg{}) {
		t.Fatalf("second Ctrl+C command = %T, want tea.QuitMsg", msg)
	}
}

func TestRunningStatusShowsCancelRequested(t *testing.T) {
	r := &stubbornTurnRunner{started: make(chan struct{}), release: make(chan struct{})}
	ctrl := control.New(control.Options{Runner: r, Sink: event.Discard, SessionDir: t.TempDir(), Label: "test"})
	ctrl.Send("hi")
	<-r.started
	defer close(r.release)

	m := newTestChatTUI()
	m.ctrl = ctrl
	m.state = tuiRunning
	m.width = 80
	m.height = 24
	ctrl.Cancel()

	view := ansi.Strip(m.View().Content)
	if !strings.Contains(view, "stopping") {
		t.Fatalf("running status after cancel should show stopping feedback:\n%s", view)
	}
}

func TestCtrlZResetsMouseTrackingBeforeSuspend(t *testing.T) {
	m := newTestChatTUI()
	ctrlZ := tea.KeyPressMsg{Code: 'z', Mod: tea.ModCtrl}

	_, cmd := m.Update(ctrlZ)
	if cmd == nil {
		t.Fatal("expected Ctrl+Z to return a suspend sequence")
	}
	msg := cmd()
	seq := reflect.ValueOf(msg)
	if seq.Kind() != reflect.Slice || seq.Len() != 2 {
		t.Fatalf("expected Ctrl+Z to return a two-command sequence, got %T", msg)
	}
	first, ok := seq.Index(0).Interface().(tea.Cmd)
	if !ok {
		t.Fatalf("first sequence item is %T, want tea.Cmd", seq.Index(0).Interface())
	}
	raw, ok := first().(tea.RawMsg)
	if !ok {
		t.Fatalf("first Ctrl+Z command = %T, want tea.RawMsg", first())
	}
	if got := fmt.Sprint(raw.Msg); got != resetMouseTracking {
		t.Fatalf("Ctrl+Z mouse reset = %q, want %q", got, resetMouseTracking)
	}
	second, ok := seq.Index(1).Interface().(tea.Cmd)
	if !ok {
		t.Fatalf("second sequence item is %T, want tea.Cmd", seq.Index(1).Interface())
	}
	if msg := second(); msg != (tea.SuspendMsg{}) {
		t.Fatalf("second Ctrl+Z command = %T, want tea.SuspendMsg", msg)
	}
}

// TestCtrlCClearsInput verifies that a single Ctrl+C while idle with non-empty
// input clears the composer without arming the double-press quit gesture.
func TestCtrlCClearsInput(t *testing.T) {
	m := newTestChatTUI()
	m.input.SetValue("hello world")
	ctrlC := tea.KeyPressMsg{Code: 'c', Mod: 4}

	out, _ := m.Update(ctrlC)
	m2 := out.(chatTUI)

	if strings.TrimSpace(m2.input.Value()) != "" {
		t.Errorf("Ctrl+C should clear non-empty input, got %q", m2.input.Value())
	}
	if !m2.lastCtrlCAt.IsZero() {
		t.Error("Ctrl+C on non-empty input should not arm the quit gesture")
	}
}

// TestCtrlCClearsThenDoublePressQuits verifies the full user flow: Ctrl+C on
// non-empty input clears it, then two more presses on the empty composer quit.
func TestCtrlCClearsThenDoublePressQuits(t *testing.T) {
	m := newTestChatTUI()
	m.input.SetValue("draft text")
	ctrlC := tea.KeyPressMsg{Code: 'c', Mod: 4}

	// First press: clear input.
	out, _ := m.Update(ctrlC)
	m2 := out.(chatTUI)
	if strings.TrimSpace(m2.input.Value()) != "" {
		t.Fatal("first Ctrl+C should clear input")
	}

	// Second press (on empty): arm quit.
	out2, _ := m2.Update(ctrlC)
	m3 := out2.(chatTUI)
	if m3.lastCtrlCAt.IsZero() {
		t.Error("Ctrl+C on empty input should arm quit")
	}

	// Third press (within window): quit.
	out3, cmd := m3.Update(ctrlC)
	if cmd == nil {
		t.Error("double Ctrl+C on empty input should quit")
	}
	_ = out3
}

// TestCtrlCCopySelection verifies that Ctrl+C while idle on an empty composer
// with an active text selection copies the selected text to clipboard instead
// of arming the double-press quit gesture.
func TestCtrlCCopySelection(t *testing.T) {
	m := newTestChatTUI()
	ctrlC := tea.KeyPressMsg{Code: 'c', Mod: 4}

	// Set up an active selection: anchor < head so there's something to copy.
	// selection uses content-line coordinates; transcript needs at least one line.
	m.transcript = []string{"hello world"}
	m.wrappedLines = []string{"hello world"}
	m.sel = selection{active: true, anchor: selPos{line: 0, col: 0}, head: selPos{line: 0, col: 5}}

	out, cmd := m.Update(ctrlC)
	m2, ok := out.(chatTUI)
	if !ok {
		t.Fatalf("Update returned %T, want chatTUI", out)
	}

	// Selection should be cleared after copy.
	if m2.sel.active {
		t.Error("selection should be cleared after Ctrl+C copy")
	}

	// Should NOT arm the quit gesture.
	if !m2.lastCtrlCAt.IsZero() {
		t.Error("Ctrl+C on active selection should not arm the quit gesture")
	}

	// Should return a command (clipboard copy + finalize).
	if cmd == nil {
		t.Fatal("Ctrl+C on selection should return a cmd (clipboard + finalize)")
	}

	// Execute the command (copyToClipboard → OSC 52).
	cmd()

	// Second Ctrl+C should now arm quit (selection is gone).
	_, cmd2 := m2.Update(ctrlC)
	if cmd2 == nil {
		t.Error("Ctrl+C after copy should arm quit (return a finalize cmd)")
	}
}

// TestAgentEventCoalescesBurst proves one update drains the buffered event burst
// behind the delivered event, so a flood collapses into a single re-render.
func TestAgentEventCoalescesBurst(t *testing.T) {
	m := newTestChatTUI()
	m.eventCh = make(chan event.Event, 16)
	m.eventCh <- event.Event{Kind: event.ToolProgress, Tool: event.Tool{ID: "b1", Output: "l1\n"}}
	m.eventCh <- event.Event{Kind: event.ToolProgress, Tool: event.Tool{ID: "b1", Output: "l2\n"}}
	m.eventCh <- event.Event{Kind: event.ToolProgress, Tool: event.Tool{ID: "b1", Output: "l3\n"}}

	next, _ := m.update(agentEventMsg(event.Event{Kind: event.ToolDispatch, Tool: event.Tool{ID: "b1", Name: "bash", Args: `{"command":"x"}`}}))
	cm := next.(chatTUI)

	if cm.toolLineCount != 3 {
		t.Fatalf("burst not coalesced into one update: toolLineCount=%d, want 3", cm.toolLineCount)
	}
	if len(m.eventCh) != 0 {
		t.Errorf("channel should be fully drained, %d left", len(m.eventCh))
	}
}

func TestShortTokens(t *testing.T) {
	cases := []struct {
		n    int
		want string
	}{
		{0, "0"},
		{999, "999"},
		{1000, "1.0K"},
		{1500, "1.5K"},
		{1999, "2.0K"},
		{9999, "10.0K"},
		{142000, "142.0K"},
		{999999, "1.0M"},
		{1000000, "1.0M"},
		{1500000, "1.5M"},
	}
	for _, tc := range cases {
		t.Run(fmt.Sprintf("n=%d", tc.n), func(t *testing.T) {
			got := shortTokens(tc.n)
			if got != tc.want {
				t.Errorf("shortTokens(%d) = %q, want %q", tc.n, got, tc.want)
			}
		})
	}
}

func TestTruncateSubject(t *testing.T) {
	cases := []struct {
		name  string
		input string
		width int
	}{
		{"short ASCII", "rm file", 60},
		{"long ASCII", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", 60},
		{"CJK at 60", "日本語の文章は通常、表示幅が広いため、端末の横幅を超えてしまうことがあります。", 60},
		{"CJK at 30", "日本語の文章は通常、表示幅が広いため、端末の横幅を超えてしまうことがあります。", 30},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := truncateSubject(tc.input, tc.width)
			wantMax := tc.width - 28
			if wantMax < 16 {
				wantMax = 16
			}
			w := ansi.StringWidth(got)
			if w > wantMax {
				t.Errorf("truncateSubject(%q, %d) = %q (width %d), want visible width <= %d", tc.input, tc.width, got, w, wantMax)
			}
		})
	}
}

// TestCtrlCCopyBeatsClearInput — regression for the bug where an active
// selection AND a non-empty composer both existed: Ctrl+C used to wipe the
// draft text and discard the selection. The fix hoists the selection-copy
// branch above the clear-input branch so the user's draft survives. After
// the copy the user can still press Ctrl+C again to clear the composer.
func TestCtrlCCopyBeatsClearInput(t *testing.T) {
	m := newTestChatTUI()
	m.input.SetValue("draft I'm typing") // non-empty composer
	m.transcript = []string{"selected text"}
	m.wrappedLines = []string{"selected text"}
	m.sel = selection{active: true, anchor: selPos{line: 0, col: 0}, head: selPos{line: 0, col: 8}}

	ctrlC := tea.KeyPressMsg{Code: 'c', Mod: 4}
	out, cmd := m.Update(ctrlC)
	m2 := out.(chatTUI)

	// Draft text must survive the selection copy.
	if got := m2.input.Value(); got != "draft I'm typing" {
		t.Errorf("composer draft wiped by Ctrl+C copy; got %q, want preserved", got)
	}
	if cmd == nil {
		t.Fatal("expected clipboard cmd")
	}
	if batch, ok := cmd().(tea.BatchMsg); ok {
		for _, c := range batch {
			c()
		}
	}

	// Second Ctrl+C (no selection, non-empty composer) clears the draft.
	out2, _ := m2.Update(ctrlC)
	m3 := out2.(chatTUI)
	if got := m3.input.Value(); got != "" {
		t.Errorf("second Ctrl+C should clear composer; got %q", got)
	}
}

// TestEscInPlanModeDoesNotExitPlan — regression for the part of PR #3051 that
// was missed: Esc was still falling into the case m.planMode branch. The
// Shift+Tab cycle is the only path that flips plan mode; Esc must only
// rewind / clear input. PR #3051 already removed the equivalent YOLO branch;
// the m.ctrl.SetBypass path is exercised end-to-end in control/yolo_test.go
// and intentionally not duplicated here.
func TestEscInPlanModeDoesNotExitPlan(t *testing.T) {
	m := newTestChatTUI()
	m.planMode = true

	esc := tea.KeyPressMsg{Code: tea.KeyEsc}
	out, _ := m.Update(esc)
	m2 := out.(chatTUI)

	if !m2.planMode {
		t.Error("Esc must not exit plan mode; only Shift+Tab should")
	}
}

func TestDesktopShortcutLayoutShiftTabTogglesPlanOnly(t *testing.T) {
	m := newTestChatTUI()
	m.ctrl = control.New(control.Options{})
	m.ctrl.SetToolApprovalMode(control.ToolApprovalAuto)
	m.cfg = config.Default()
	if err := m.cfg.SetUIShortcutLayout("desktop"); err != nil {
		t.Fatal(err)
	}

	shiftTab := tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift}
	out, _ := m.Update(shiftTab)
	m = out.(chatTUI)
	if !m.planMode || !m.ctrl.PlanMode() {
		t.Fatalf("first Shift+Tab should enter plan mode, tui=%v controller=%v", m.planMode, m.ctrl.PlanMode())
	}
	if got := m.ctrl.ToolApprovalMode(); got != control.ToolApprovalAuto {
		t.Fatalf("Shift+Tab changed approval mode to %q, want auto", got)
	}

	out, _ = m.Update(shiftTab)
	m = out.(chatTUI)
	if m.planMode || m.ctrl.PlanMode() {
		t.Fatalf("second Shift+Tab should leave plan mode, tui=%v controller=%v", m.planMode, m.ctrl.PlanMode())
	}
	if got := m.ctrl.ToolApprovalMode(); got != control.ToolApprovalAuto {
		t.Fatalf("second Shift+Tab changed approval mode to %q, want auto", got)
	}
}

func TestDesktopShortcutLayoutShiftTabClearsGoalWhenEnteringPlan(t *testing.T) {
	m := newTestChatTUI()
	m.ctrl = control.New(control.Options{})
	m.ctrl.SetGoal("ship the shortcut redesign")
	m.cfg = config.Default()
	if err := m.cfg.SetUIShortcutLayout("desktop"); err != nil {
		t.Fatal(err)
	}

	out, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})
	m = out.(chatTUI)
	if !m.planMode || !m.ctrl.PlanMode() {
		t.Fatalf("Shift+Tab should enter plan mode, tui=%v controller=%v", m.planMode, m.ctrl.PlanMode())
	}
	if got := m.ctrl.Goal(); got != "" {
		t.Fatalf("Shift+Tab entering plan should clear goal, got %q", got)
	}
}

func TestDesktopShortcutLayoutCtrlYTogglesYolo(t *testing.T) {
	m := newTestChatTUI()
	m.ctrl = control.New(control.Options{})
	m.cfg = config.Default()
	if err := m.cfg.SetUIShortcutLayout("desktop"); err != nil {
		t.Fatal(err)
	}

	ctrlY := tea.KeyPressMsg{Code: 'y', Mod: tea.ModCtrl}
	out, _ := m.Update(ctrlY)
	m = out.(chatTUI)
	if got := m.ctrl.ToolApprovalMode(); got != control.ToolApprovalYolo {
		t.Fatalf("Ctrl+Y approval mode = %q, want yolo", got)
	}

	out, _ = m.Update(ctrlY)
	m = out.(chatTUI)
	if got := m.ctrl.ToolApprovalMode(); got != control.ToolApprovalAsk {
		t.Fatalf("second Ctrl+Y approval mode = %q, want ask", got)
	}
}

func TestDesktopShortcutLayoutCtrlYRestoresAutoAfterYolo(t *testing.T) {
	m := newTestChatTUI()
	m.ctrl = control.New(control.Options{})
	m.ctrl.SetToolApprovalMode(control.ToolApprovalAuto)
	m.cfg = config.Default()
	if err := m.cfg.SetUIShortcutLayout("desktop"); err != nil {
		t.Fatal(err)
	}

	ctrlY := tea.KeyPressMsg{Code: 'y', Mod: tea.ModCtrl}
	out, _ := m.Update(ctrlY)
	m = out.(chatTUI)
	if got := m.ctrl.ToolApprovalMode(); got != control.ToolApprovalYolo {
		t.Fatalf("Ctrl+Y approval mode = %q, want yolo", got)
	}

	out, _ = m.Update(ctrlY)
	m = out.(chatTUI)
	if got := m.ctrl.ToolApprovalMode(); got != control.ToolApprovalAuto {
		t.Fatalf("second Ctrl+Y approval mode = %q, want restored auto", got)
	}
}

func TestClassicShortcutLayoutCtrlYTogglesYolo(t *testing.T) {
	m := newTestChatTUI()
	m.ctrl = control.New(control.Options{})
	m.cfg = config.Default()
	if err := m.cfg.SetUIShortcutLayout("classic"); err != nil {
		t.Fatal(err)
	}

	ctrlY := tea.KeyPressMsg{Code: 'y', Mod: tea.ModCtrl}
	out, cmd := m.Update(ctrlY)
	if cmd != nil {
		t.Fatal("Ctrl+Y should toggle YOLO directly, not return a paste command")
	}
	m = out.(chatTUI)
	if got := m.ctrl.ToolApprovalMode(); got != control.ToolApprovalYolo {
		t.Fatalf("Ctrl+Y approval mode = %q, want yolo", got)
	}

	out, _ = m.Update(ctrlY)
	m = out.(chatTUI)
	if got := m.ctrl.ToolApprovalMode(); got != control.ToolApprovalAsk {
		t.Fatalf("second Ctrl+Y approval mode = %q, want ask", got)
	}
}

func TestPrimaryYShortcutRestoresAutoUnderClassicShortcutLayout(t *testing.T) {
	m := newTestChatTUI()
	m.ctrl = control.New(control.Options{})
	m.ctrl.SetToolApprovalMode(control.ToolApprovalAuto)
	m.cfg = config.Default()
	if err := m.cfg.SetUIShortcutLayout("classic"); err != nil {
		t.Fatal(err)
	}

	cmdY := tea.KeyPressMsg{Code: 'y', Mod: tea.ModSuper}
	out, _ := m.Update(cmdY)
	m = out.(chatTUI)
	if got := m.ctrl.ToolApprovalMode(); got != control.ToolApprovalYolo {
		t.Fatalf("Cmd/Super+Y approval mode = %q, want yolo", got)
	}

	out, _ = m.Update(cmdY)
	m = out.(chatTUI)
	if got := m.ctrl.ToolApprovalMode(); got != control.ToolApprovalAuto {
		t.Fatalf("second Cmd/Super+Y approval mode = %q, want restored auto", got)
	}
}

func TestDesktopShortcutLayoutDoesNotStealCompletionTab(t *testing.T) {
	m := newTestChatTUI()
	m.ctrl = control.New(control.Options{})
	m.cfg = config.Default()
	if err := m.cfg.SetUIShortcutLayout("desktop"); err != nil {
		t.Fatal(err)
	}
	m.input.SetValue("/")
	m.completion = completion{
		active:      true,
		kind:        compSlash,
		items:       []compItem{{label: "/mcp", insert: "/mcp ", descend: true}},
		replaceFrom: 0,
	}

	out, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	m = out.(chatTUI)
	if got := m.ctrl.ToolApprovalMode(); got != control.ToolApprovalAsk {
		t.Fatalf("completion Tab changed approval mode to %q", got)
	}
	if got := m.input.Value(); got != "/mcp " {
		t.Fatalf("completion Tab input = %q, want /mcp ", got)
	}
}

func TestShiftTabStillTogglesPlanUnderClassicShortcutLayout(t *testing.T) {
	m := newTestChatTUI()
	m.ctrl = control.New(control.Options{})
	m.cfg = config.Default()
	if err := m.cfg.SetUIShortcutLayout("classic"); err != nil {
		t.Fatal(err)
	}

	out, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})
	m = out.(chatTUI)
	if !m.planMode || !m.ctrl.PlanMode() {
		t.Fatalf("Shift+Tab should toggle plan mode, tui=%v controller=%v", m.planMode, m.ctrl.PlanMode())
	}
	if got := m.ctrl.ToolApprovalMode(); got != control.ToolApprovalAsk {
		t.Fatalf("Shift+Tab changed approval mode to %q", got)
	}
}
