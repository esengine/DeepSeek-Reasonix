package cli

import (
	"encoding/json"
	"errors"
	"reasonix/internal/i18n"
	"strings"
	"testing"

	"github.com/charmbracelet/colorprofile"

	"github.com/charmbracelet/x/ansi"

	"reasonix/internal/provider"
)

func TestAssistantMarkdownHasIdentityAndIndentedBody(t *testing.T) {
	defer restoreThemeForTest(activeColorProfile, activeCLITheme)
	activeColorProfile = colorprofile.NoTTY
	configureCLITheme("dark")

	rendered := renderAssistantMarkdown("A concise answer that wraps across the available width.", 32)
	lines := strings.Split(ansi.Strip(rendered), "\n")
	if len(lines) < 4 {
		t.Fatalf("assistant block should contain a header, gap, and wrapped body:\n%s", rendered)
	}
	if lines[0] != "  ◆ Reasonix" {
		t.Fatalf("assistant header = %q, want %q", lines[0], "  ◆ Reasonix")
	}
	if lines[1] != "" {
		t.Fatalf("assistant header/body separator = %q, want blank row", lines[1])
	}
	for i, line := range lines[2:] {
		if line != "" && !strings.HasPrefix(line, assistantTranscriptIndent) {
			t.Fatalf("assistant body row %d lacks the two-cell gutter: %q", i+2, line)
		}
		if width := visibleWidth(line); width > 32 {
			t.Fatalf("assistant row %d width = %d, want <= 32: %q", i+2, width, line)
		}
	}
}

func TestReplaySectionsKeepAssistantIdentity(t *testing.T) {
	defer restoreThemeForTest(activeColorProfile, activeCLITheme)
	activeColorProfile = colorprofile.NoTTY
	configureCLITheme("dark")

	sections := replaySectionsFor([]provider.Message{
		{Role: provider.RoleUser, Origin: provider.MessageOriginHost, Content: "<pinned_context_revision>private pinned body</pinned_context_revision>"},
		{Role: provider.RoleUser, Content: "Which version?"},
		{Role: provider.RoleAssistant, Content: "Version 1.2.3"},
	}, 48)
	if len(sections) != 2 {
		t.Fatalf("replay sections = %d, want user and assistant", len(sections))
	}
	if plain := ansi.Strip(strings.Join(sections, "")); strings.Contains(plain, "private pinned body") {
		t.Fatalf("replay exposed a pinned revision: %q", plain)
	}
	if plain := ansi.Strip(sections[1]); !strings.HasPrefix(plain, "  ◆ Reasonix\n\n  Version 1.2.3") {
		t.Fatalf("replayed assistant answer lost its identity: %q", plain)
	}
}

func TestReplaySectionsRestoreInterruptedLocalOutput(t *testing.T) {
	defer restoreThemeForTest(activeColorProfile, activeCLITheme)
	activeColorProfile = colorprofile.NoTTY
	configureCLITheme("dark")

	sections := replaySectionsFor([]provider.Message{
		{Role: provider.RoleUser, Content: "change config"},
		{
			Role: provider.RoleTool, ToolCallID: provider.LocalOnlyToolID, Name: provider.LocalOnlyToolName,
			LocalOnly: true, Content: "partial answer", ReasoningContent: "checking config",
			ToolCalls:       []provider.ToolCall{{ID: "p1", Name: "write_file"}},
			InterruptedTurn: &provider.InterruptedTurnRecovery{Pending: true},
		},
	}, 64)
	plain := ansi.Strip(strings.Join(sections, ""))
	for _, want := range []string{"change config", "checking config", "partial answer", "Write", "bounded recovery summary"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("replayed interrupted history missing %q:\n%s", want, plain)
		}
	}
}

func TestReplaySectionsRestoreFinalReadinessRecoveryHint(t *testing.T) {
	sections := replaySectionsFor([]provider.Message{{
		Role: provider.RoleTool, ToolCallID: provider.LocalOnlyToolID, Name: provider.LocalOnlyToolName, LocalOnly: true,
		FinalReadinessRecovery: &provider.FinalReadinessRecovery{Pending: true, Missing: []string{"verification"}},
	}}, 64)
	plain := ansi.Strip(strings.Join(sections, ""))
	if !strings.Contains(plain, "/continue-checks") {
		t.Fatalf("replayed readiness pause lacks recovery command: %q", plain)
	}
}

func TestScrollbarThumb(t *testing.T) {
	if _, size := scrollbarThumb(10, 0, 5); size != 0 {
		t.Errorf("content within viewport should have no thumb, got size %d", size)
	}
	if start, _ := scrollbarThumb(10, 0, 100); start != 0 {
		t.Errorf("at top the thumb starts at row 0, got %d", start)
	}
	const h, total = 10, 100
	if start, size := scrollbarThumb(h, total-h, total); start+size != h {
		t.Errorf("at bottom the thumb reaches the last row: start=%d size=%d h=%d", start, size, h)
	}
}

func TestEdgeScrollDir(t *testing.T) {
	const h = 10
	if got := edgeScrollDir(0, h); got != -1 {
		t.Errorf("top edge dir = %d, want -1", got)
	}
	if got := edgeScrollDir(h-1, h); got != 1 {
		t.Errorf("bottom edge dir = %d, want 1", got)
	}
	if got := edgeScrollDir(h/2, h); got != 0 {
		t.Errorf("middle dir = %d, want 0", got)
	}
}

func TestSelSpan(t *testing.T) {
	start, end, cw := selPos{line: 1, col: 3}, selPos{line: 3, col: 5}, 20
	for _, tc := range []struct {
		idx         int
		wantOK      bool
		wantLo, wHi int
	}{
		{0, false, 0, 0}, // above
		{1, true, 3, cw}, // first line: anchor col → right edge
		{2, true, 0, cw}, // middle line: full width
		{3, true, 0, 5},  // last line: left edge → head col
		{4, false, 0, 0}, // below
	} {
		lo, hi, ok := selSpan(tc.idx, start, end, cw)
		if ok != tc.wantOK || (ok && (lo != tc.wantLo || hi != tc.wHi)) {
			t.Errorf("selSpan(%d) = (%d,%d,%v), want (%d,%d,%v)", tc.idx, lo, hi, ok, tc.wantLo, tc.wHi, tc.wantOK)
		}
	}
}

func TestSelectedTextMultiLine(t *testing.T) {
	m := newTestChatTUI()
	m.wrappedLines = []string{"hello world", "second line", "third row"}
	m.sel = selection{active: true, anchor: selPos{line: 0, col: 6}, head: selPos{line: 2, col: 5}}

	if got, want := m.selectedText(), "world\nsecond line\nthird"; got != want {
		t.Errorf("selectedText() = %q, want %q", got, want)
	}

	// A zero-width selection (plain click) copies nothing.
	m.sel = selection{active: true, anchor: selPos{line: 0, col: 3}, head: selPos{line: 0, col: 3}}
	if got := m.selectedText(); got != "" {
		t.Errorf("empty selection should yield no text, got %q", got)
	}
}

func TestSelectedTextRestoresMathWithoutReusingRawColumns(t *testing.T) {
	defer restoreThemeForTest(activeColorProfile, activeCLITheme)
	activeColorProfile = colorprofile.NoTTY
	configureCLITheme("dark")

	m := newTestChatTUI()
	m.width = 80
	contentWidth := transcriptContentWidth(m.width, m.nativeScrollback)
	m.viewport.SetWidth(contentWidth)
	source := transcriptSource{kind: transcriptSourceMarkdown, raw: `before $\alpha$ after`}
	rendered := m.renderTranscriptSource(source, m.width)
	m.transcript = []string{rendered}
	m.transcriptSources = []transcriptSource{source}
	m.wrappedLines = strings.Split(wrapTranscript(rendered, contentWidth), "\n")

	lineIndex := -1
	for i, line := range m.wrappedLines {
		if strings.Contains(ansi.Strip(line), "before α after") {
			lineIndex = i
			break
		}
	}
	if lineIndex < 0 {
		t.Fatalf("rendered transcript did not contain the math line:\n%s", ansi.Strip(rendered))
	}

	plain := ansi.Strip(m.wrappedLines[lineIndex])
	before, _, ok := strings.Cut(plain, "α")
	before0, _, ok0 := strings.Cut(plain, "after")
	if !ok || !ok0 {
		t.Fatalf("math line = %q", plain)
	}
	formulaCol := ansi.StringWidth(before)
	afterCol := ansi.StringWidth(before0)

	m.sel = selection{
		active: true,
		anchor: selPos{line: lineIndex, col: formulaCol},
		head:   selPos{line: lineIndex, col: formulaCol + ansi.StringWidth("α")},
	}
	if got, want := m.selectedText(), `$\alpha$`; got != want {
		t.Fatalf("formula selection = %q, want %q", got, want)
	}

	m.sel = selection{
		active: true,
		anchor: selPos{line: lineIndex, col: afterCol},
		head:   selPos{line: lineIndex, col: afterCol + ansi.StringWidth("after")},
	}
	if got, want := m.selectedText(), "after"; got != want {
		t.Fatalf("text after formula = %q, want %q", got, want)
	}
}

func TestSelectedTextRestoresMathFromReplayBundle(t *testing.T) {
	defer restoreThemeForTest(activeColorProfile, activeCLITheme)
	activeColorProfile = colorprofile.NoTTY
	configureCLITheme("dark")

	m := newTestChatTUI()
	m.width = 80
	contentWidth := transcriptContentWidth(m.width, m.nativeScrollback)
	m.viewport.SetWidth(contentWidth)
	source := transcriptSource{
		kind: transcriptSourceReplayBundle,
		history: []provider.Message{
			{Role: provider.RoleAssistant, Content: `before $\alpha$ after`},
			{LocalOnly: true, Content: `local $\beta$ recovery`},
		},
	}
	rendered := m.renderTranscriptSource(source, m.width)
	m.transcript = []string{rendered}
	m.transcriptSources = []transcriptSource{source}
	m.wrappedLines = strings.Split(wrapTranscript(rendered, contentWidth), "\n")

	lineIndex := -1
	formulaCol := -1
	for i, line := range m.wrappedLines {
		plain := ansi.Strip(line)
		before, _, ok := strings.Cut(plain, "α")
		if !ok {
			continue
		}
		lineIndex = i
		formulaCol = ansi.StringWidth(before)
		break
	}
	if lineIndex < 0 {
		t.Fatalf("rendered replay bundle did not contain the formula:\n%s", ansi.Strip(rendered))
	}

	m.sel = selection{
		active: true,
		anchor: selPos{line: lineIndex, col: formulaCol},
		head:   selPos{line: lineIndex, col: formulaCol + ansi.StringWidth("α")},
	}
	if got, want := m.selectedText(), `$\alpha$`; got != want {
		t.Fatalf("replayed formula selection = %q, want %q", got, want)
	}

	copyLines, ok := m.copyTranscriptLines()
	if !ok {
		t.Fatal("copy rendition diverged from the displayed replay bundle")
	}
	sourcesByID := make(map[string]string)
	for _, line := range copyLines {
		for _, span := range line.math {
			if source, exists := sourcesByID[span.id]; exists && source != span.source {
				t.Fatalf("formula marker %q reused for %q and %q", span.id, source, span.source)
			}
			sourcesByID[span.id] = span.source
		}
	}
	if len(sourcesByID) != 2 {
		t.Fatalf("replay formula markers = %v, want two unique formulas", sourcesByID)
	}
	foundSources := make(map[string]bool)
	for _, source := range sourcesByID {
		foundSources[source] = true
	}
	for _, want := range []string{`$\alpha$`, `$\beta$`} {
		if !foundSources[want] {
			t.Fatalf("replay formula markers = %v, missing %q", sourcesByID, want)
		}
	}
}

func TestSelectedTextPreservesProseAroundMath(t *testing.T) {
	defer restoreThemeForTest(activeColorProfile, activeCLITheme)
	activeColorProfile = colorprofile.NoTTY
	configureCLITheme("dark")

	m := newTestChatTUI()
	m.width = 80
	contentWidth := transcriptContentWidth(m.width, m.nativeScrollback)
	m.viewport.SetWidth(contentWidth)
	source := transcriptSource{kind: transcriptSourceMarkdown, raw: `before $\frac{1}{2}$ after`}
	rendered := m.renderTranscriptSource(source, m.width)
	m.transcript = []string{rendered}
	m.transcriptSources = []transcriptSource{source}
	m.wrappedLines = strings.Split(wrapTranscript(rendered, contentWidth), "\n")

	for i, line := range m.wrappedLines {
		plain := ansi.Strip(line)
		before, _, ok := strings.Cut(plain, "before")
		endByte := strings.Index(plain, " after")
		if !ok || endByte < 0 {
			continue
		}
		startCol := ansi.StringWidth(before)
		endCol := ansi.StringWidth(plain[:endByte+len(" after")])
		m.sel = selection{
			active: true,
			anchor: selPos{line: i, col: startCol},
			head:   selPos{line: i, col: endCol},
		}
		if got, want := m.selectedText(), `before $\frac{1}{2}$ after`; got != want {
			t.Fatalf("mixed selection = %q, want %q", got, want)
		}
		return
	}
	t.Fatalf("rendered transcript did not contain the expected mixed line:\n%s", ansi.Strip(rendered))
}

func TestSelectedTextRestoresMathWrappedAcrossDisplayLinesOnce(t *testing.T) {
	defer restoreThemeForTest(activeColorProfile, activeCLITheme)
	activeColorProfile = colorprofile.NoTTY
	configureCLITheme("dark")

	m := newTestChatTUI()
	m.width = 10
	contentWidth := transcriptContentWidth(m.width, m.nativeScrollback)
	m.viewport.SetWidth(contentWidth)
	const latex = `\alpha+\beta+\gamma+\delta+\epsilon+\zeta`
	source := transcriptSource{kind: transcriptSourceMarkdown, raw: `$` + latex + `$`}
	rendered := m.renderTranscriptSource(source, m.width)
	m.transcript = []string{rendered}
	m.transcriptSources = []transcriptSource{source}
	m.wrappedLines = strings.Split(wrapTranscript(rendered, contentWidth), "\n")

	copyLines, ok := m.copyTranscriptLines()
	if !ok {
		t.Fatal("copy rendition diverged from the displayed transcript")
	}
	firstLine, lastLine := -1, -1
	firstCol, lastCol := 0, 0
	for i, line := range copyLines {
		if len(line.math) == 0 {
			continue
		}
		if firstLine < 0 {
			firstLine = i
			firstCol = line.math[0].start
		}
		lastLine = i
		lastCol = line.math[len(line.math)-1].end
	}
	if firstLine < 0 || lastLine <= firstLine {
		t.Fatalf("expected formula to wrap across lines:\n%s", ansi.Strip(rendered))
	}

	m.sel = selection{
		active: true,
		anchor: selPos{line: firstLine, col: firstCol},
		head:   selPos{line: lastLine, col: lastCol},
	}
	if got, want := m.selectedText(), `$`+latex+`$`; got != want {
		t.Fatalf("wrapped formula selection = %q, want %q", got, want)
	}
}

func TestCopyToClipboard(t *testing.T) {
	t.Setenv("SSH_CONNECTION", "")
	t.Setenv("SSH_CLIENT", "")
	t.Setenv("SSH_TTY", "")
	previous := writeNativeClipboardText
	t.Cleanup(func() { writeNativeClipboardText = previous })

	var written string
	writeNativeClipboardText = func(text string) error {
		written = text
		return nil
	}
	message := copyToClipboard("hello")()
	got, ok := message.(clipboardCopyMsg)
	if !ok {
		t.Fatalf("copyToClipboard returned %T, want clipboardCopyMsg", message)
	}
	if written != "hello" || got.text != "hello" || got.err != nil || got.osc52 {
		t.Fatalf("native clipboard result = %+v, written %q", got, written)
	}

	wantErr := errors.New("clipboard unavailable")
	writeNativeClipboardText = func(string) error { return wantErr }
	got = copyToClipboard("fallback")().(clipboardCopyMsg)
	if !errors.Is(got.err, wantErr) || got.osc52 {
		t.Fatalf("failed native clipboard result = %+v", got)
	}

	t.Setenv("SSH_CONNECTION", "host 22 client 1234")
	writeNativeClipboardText = func(string) error {
		t.Fatal("SSH copy must not write the remote host's native clipboard")
		return nil
	}
	got = copyToClipboard("remote")().(clipboardCopyMsg)
	if !got.osc52 || got.text != "remote" {
		t.Fatalf("SSH clipboard result = %+v, want OSC 52", got)
	}
}

func TestReplaySearchMissingSourcesIsExplicitAndKeepsSummary(t *testing.T) {
	render := func(s string, _ int) string { return s }
	for _, msg := range []provider.Message{
		{Role: provider.RoleAssistant, ServerSearch: []provider.ServerSearchCall{{ID: "s", SourcesStatus: provider.SourcesNotProvided}}},
		{Role: provider.RoleTool, Name: "web_search", Content: `{"sources_status":"not_provided","summary":"retained search summary"}`},
	} {
		got := strings.Join(replaySectionsForWithAssistantRenderer([]provider.Message{msg}, 80, render), "")
		if !strings.Contains(got, i18n.M.SearchSourcesNotProvided) {
			t.Fatal("missing source notice")
		}
		if msg.Role == provider.RoleTool && !strings.Contains(got, "retained search summary") {
			t.Fatal("summary lost")
		}
	}
	old := provider.Message{Role: provider.RoleAssistant, ServerSearch: []provider.ServerSearchCall{{ID: "s", Raw: json.RawMessage(`[]`)}}}
	if got := searchHistorySections(old, 80, render); len(got) != 0 {
		t.Fatal("inferred unrecorded old status")
	}
}
