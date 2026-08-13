package cli

import (
	"context"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"reasonix/internal/control"
	"reasonix/internal/event"
	"reasonix/internal/i18n"
)

// extensionStubCtrl stubs the SessionAPI surface the extension slash dispatch
// and completion read; every other method panics via the embedded nil
// interface, which keeps these tests focused on the extension paths.
type extensionStubCtrl struct {
	control.SessionAPI
	actions     []control.ExtensionActionView
	customSent  string
	customFound bool
	invokeName  string
	invokeArgs  map[string]string
	invokeMsg   string
	invokeErr   error
}

func (s *extensionStubCtrl) ExtensionActions() []control.ExtensionActionView { return s.actions }
func (s *extensionStubCtrl) CustomCommand(string) (string, bool) {
	return s.customSent, s.customFound
}
func (s *extensionStubCtrl) RunSkill(string) (string, bool) { return "", false }
func (s *extensionStubCtrl) SendWithRaw(string, string)     {}
func (s *extensionStubCtrl) InvokeExtensionAction(_ context.Context, name string, args map[string]string) (string, error) {
	s.invokeName, s.invokeArgs = name, args
	return s.invokeMsg, s.invokeErr
}

func floatPtr(v float64) *float64 { return &v }

func statusPayload(severity string) *event.ExtensionSurfacePayload {
	return &event.ExtensionSurfacePayload{
		PluginID: "alpha", SurfaceID: "s1", Kind: event.ExtensionSurfaceStatus,
		Status: &event.ExtensionStatusView{Label: "building", Detail: "3 of 9", Severity: severity},
	}
}

func TestExtensionStatusLineSeverity(t *testing.T) {
	tests := []struct {
		severity string
		glyph    string
	}{
		{"info", "·"},
		{"", "·"},
		{"warn", "!"},
		{"error", "✗"},
	}
	for _, tt := range tests {
		line := ansi.Strip(extensionStatusLine(statusPayload(tt.severity)))
		want := tt.glyph + " [alpha] building: 3 of 9"
		if !strings.Contains(line, want) {
			t.Errorf("severity %q: line = %q, want %q", tt.severity, line, want)
		}
	}
	if got := ansi.Strip(extensionStatusLine(nil)); got != "" {
		t.Fatalf("nil payload = %q, want empty", got)
	}
	// Progress appends a percentage.
	p := statusPayload("info")
	p.Status.Progress = floatPtr(0.5)
	if line := ansi.Strip(extensionStatusLine(p)); !strings.Contains(line, "(50%)") {
		t.Fatalf("progress line = %q, want (50%%)", line)
	}
}

func TestExtensionNotificationLine(t *testing.T) {
	p := &event.ExtensionSurfacePayload{
		PluginID: "alpha", SurfaceID: "n1", Kind: event.ExtensionSurfaceNotification,
		Notification: &event.ExtensionNotificationView{Title: "Deploy done", Body: "v2 live", Severity: "warn"},
	}
	line := ansi.Strip(extensionNotificationLine(p))
	if !strings.Contains(line, "! [alpha] Deploy done — v2 live") {
		t.Fatalf("notification line = %q", line)
	}
}

func TestExtensionCardLines(t *testing.T) {
	card := &event.ExtensionCardView{
		Title:    "CI status",
		Text:     "all green",
		Fields:   []event.ExtensionKeyValue{{Key: "branch", Value: "main"}},
		Progress: floatPtr(1),
		Actions:  []event.ExtensionActionRef{{ActionID: "rerun", Label: "Rerun"}},
	}
	lines := extensionCardLines("alpha", card, 80)
	joined := ansi.Strip(strings.Join(lines, "\n"))
	for _, want := range []string{"◆ CI status", "all green", "branch: main", "100%", "/alpha:rerun", "Rerun"} {
		if !strings.Contains(joined, want) {
			t.Errorf("card missing %q:\n%s", want, joined)
		}
	}

	// Empty title falls back to the plugin id; markdown body renders through
	// the same renderer as assistant answers (content passes through).
	md := extensionCardLines("alpha", &event.ExtensionCardView{Markdown: "**bold** body"}, 80)
	plain := ansi.Strip(strings.Join(md, "\n"))
	if !strings.Contains(plain, "◆ alpha") || !strings.Contains(plain, "bold") {
		t.Fatalf("markdown card = %q", plain)
	}
}

func TestExtensionFormLines(t *testing.T) {
	lines := extensionFormLines("alpha", &event.ExtensionFormView{Title: "Setup", Message: "pick options"})
	joined := ansi.Strip(strings.Join(lines, "\n"))
	if !strings.Contains(joined, "◆ Setup") || !strings.Contains(joined, "pick options") ||
		!strings.Contains(joined, i18n.M.ExtFormFieldsHint) {
		t.Fatalf("form card = %q", joined)
	}
}

func TestExtensionSurfaceLinesDispatch(t *testing.T) {
	if got := extensionSurfaceLines(nil, 80); got != nil {
		t.Fatalf("nil payload = %v, want nil", got)
	}
	if got := extensionSurfaceLines(statusPayload("info"), 80); got != nil {
		t.Fatalf("status payload is not a surface card: %v", got)
	}
	p := &event.ExtensionSurfacePayload{
		PluginID: "alpha", Kind: event.ExtensionSurfaceCard,
		Card: &event.ExtensionCardView{Title: "t"},
	}
	if got := extensionSurfaceLines(p, 80); len(got) == 0 {
		t.Fatal("card payload produced no lines")
	}
}

func TestParseExtensionActionArgs(t *testing.T) {
	if got := parseExtensionActionArgs(nil); got != nil {
		t.Fatalf("no fields = %v, want nil", got)
	}
	got := parseExtensionActionArgs([]string{"k=v", "extra", "empty="})
	if got["k"] != "v" || got["arg1"] != "extra" || got["empty"] != "" {
		t.Fatalf("args = %v", got)
	}
}

func TestMatchExtensionAction(t *testing.T) {
	ctrl := &extensionStubCtrl{actions: []control.ExtensionActionView{{
		PluginID: "alpha", ActionID: "act1", Label: "Act one", Slash: "/alpha:act1",
	}}}
	action, ok := matchExtensionAction(ctrl, "/alpha:act1")
	if !ok || action.Slash != "/alpha:act1" {
		t.Fatalf("match = %+v, %v", action, ok)
	}
	if _, ok := matchExtensionAction(ctrl, "/alpha:other"); ok {
		t.Fatal("undeclared action matched")
	}
	if _, ok := matchExtensionAction(ctrl, "/plain"); ok {
		t.Fatal("non-action slash matched")
	}
	if _, ok := matchExtensionAction(nil, "/alpha:act1"); ok {
		t.Fatal("nil controller matched")
	}
}

func TestSlashCompletionIncludesExtensionActions(t *testing.T) {
	m := newTestChatTUI()
	m.ctrl = &extensionStubCtrl{actions: []control.ExtensionActionView{{
		PluginID: "alpha", ActionID: "act1", Label: "Act one", Slash: "/alpha:act1",
	}}}
	m.input.SetValue("/alpha")
	m.updateCompletion()

	if !m.completion.active {
		t.Fatal("slash menu did not open for /alpha")
	}
	var item *compItem
	for i := range m.completion.items {
		if m.completion.items[i].label == "/alpha:act1" {
			item = &m.completion.items[i]
		}
	}
	if item == nil {
		t.Fatalf("extension action missing from completion: %v", labels(m.completion.items))
	}
	if item.insert != "/alpha:act1 " || !strings.Contains(item.hint, "plugin alpha") || !strings.Contains(item.hint, "Act one") {
		t.Fatalf("completion item = %+v", item)
	}
}

func TestRunSlashCommandInvokesExtensionAction(t *testing.T) {
	m := newTestChatTUI()
	ctrl := &extensionStubCtrl{
		actions:   []control.ExtensionActionView{{PluginID: "alpha", ActionID: "act1", Slash: "/alpha:act1"}},
		invokeMsg: "rerun scheduled",
	}
	m.ctrl = ctrl

	cmd := m.runSlashCommand("/alpha:act1 k=v extra")
	if cmd == nil {
		t.Fatal("extension action returned no cmd")
	}
	// The command line echoes synchronously; the invocation itself is async.
	if plain := ansi.Strip(strings.Join(m.transcript, "\n")); !strings.Contains(plain, "› /alpha:act1 k=v extra") {
		t.Fatalf("echo missing, transcript = %q", plain)
	}
	msg, ok := cmd().(extensionActionMsg)
	if !ok {
		t.Fatalf("cmd delivered %T, want extensionActionMsg", cmd())
	}
	if msg.err != nil || msg.message != "rerun scheduled" {
		t.Fatalf("msg = %+v", msg)
	}
	if ctrl.invokeName != "/alpha:act1" || ctrl.invokeArgs["k"] != "v" || ctrl.invokeArgs["arg1"] != "extra" {
		t.Fatalf("invoked %q with %v", ctrl.invokeName, ctrl.invokeArgs)
	}
}

func TestRunSlashCommandResolutionOrder(t *testing.T) {
	// A custom command of the same name wins; the extension action never fires.
	m := newTestChatTUI()
	ctrl := &extensionStubCtrl{
		actions:     []control.ExtensionActionView{{PluginID: "alpha", ActionID: "act1", Slash: "/alpha:act1"}},
		customSent:  "expanded",
		customFound: true,
	}
	m.ctrl = ctrl
	if cmd := m.runSlashCommand("/alpha:act1"); cmd == nil {
		t.Fatal("custom command branch returned no cmd")
	}
	if ctrl.invokeName != "" {
		t.Fatalf("extension action invoked despite custom command: %q", ctrl.invokeName)
	}
	if m.pendingRestore != "/alpha:act1" {
		t.Fatalf("custom command should start a turn (bubble pending), pendingRestore = %q", m.pendingRestore)
	}

	// Nothing matches → the extension action never fires and the line falls
	// through to the unknown-slash behavior: sent as a regular message with a
	// visible notice (#5756).
	m2 := newTestChatTUI()
	m2.ctrl = &extensionStubCtrl{}
	if cmd := m2.runSlashCommand("/alpha:act1"); cmd == nil {
		t.Fatal("unknown slash should start a regular-message turn")
	}
	if plain := ansi.Strip(strings.Join(m2.transcript, "\n")); !strings.Contains(plain, "unknown command: /alpha:act1") {
		t.Fatalf("unknown notice missing, transcript = %q", plain)
	}
	if m2.pendingRestore != "/alpha:act1" {
		t.Fatalf("unknown slash should be sent as a regular message, pendingRestore = %q", m2.pendingRestore)
	}
}

func TestIngestExtensionEvents(t *testing.T) {
	m := newTestChatTUI()
	m.ingestEvent(event.Event{Kind: event.ExtensionStatus, Extension: statusPayload("warn")})
	m.ingestEvent(event.Event{Kind: event.ExtensionSurface, Extension: &event.ExtensionSurfacePayload{
		PluginID: "alpha", Kind: event.ExtensionSurfaceCard,
		Card: &event.ExtensionCardView{Title: "CI", Text: "green"},
	}})
	m.ingestEvent(event.Event{Kind: event.ExtensionSurface, Extension: &event.ExtensionSurfacePayload{
		PluginID: "alpha", Kind: event.ExtensionSurfaceNotification,
		Notification: &event.ExtensionNotificationView{Title: "heads up", Severity: "error"},
	}})

	plain := ansi.Strip(strings.Join(m.transcript, "\n"))
	for _, want := range []string{"! [alpha] building: 3 of 9", "◆ CI", "green", "✗ [alpha] heads up"} {
		if !strings.Contains(plain, want) {
			t.Errorf("transcript missing %q:\n%s", want, plain)
		}
	}

	// A nil payload stays silent instead of panicking.
	m2 := newTestChatTUI()
	m2.ingestEvent(event.Event{Kind: event.ExtensionSurface})
	if len(m2.transcript) != 0 {
		t.Fatalf("nil extension payload committed lines: %v", m2.transcript)
	}
}
