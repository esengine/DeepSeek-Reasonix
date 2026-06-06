package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"

	"reasonix/internal/agent"
	"reasonix/internal/command"
	"reasonix/internal/control"
	"reasonix/internal/event"
	"reasonix/internal/provider"
)

// writeAt creates dir/rel (with parents) holding content, for fs-backed tests.
func writeAt(t *testing.T, dir, rel, content string) {
	t.Helper()
	p := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestSlashCompletionFilterAndAccept(t *testing.T) {
	m := newTestChatTUI()
	m.input.SetValue("/co")
	m.updateCompletion()

	if !m.completion.active || m.completion.kind != compSlash {
		t.Fatalf("typing /co should open the slash menu: %+v", m.completion)
	}
	// Only /compact matches the "/co" prefix among the built-ins.
	if len(m.completion.items) != 1 || m.completion.items[0].label != "/compact" {
		t.Fatalf("filter = %v, want just /compact", labels(m.completion.items))
	}

	m.acceptCompletion()
	if got := m.input.Value(); got != "/compact " {
		t.Errorf("accept should fill the input, got %q", got)
	}
	if m.completion.active {
		t.Error("menu should close after accept")
	}
}

func TestSlashCompletionIncludesCustomCommands(t *testing.T) {
	m := newTestChatTUI()
	m.commands = []command.Command{{Name: "review", Description: "review the diff"}}
	m.input.SetValue("/re")
	m.updateCompletion()

	if !hasLabel(m.completion.items, "/review") {
		t.Errorf("custom command should appear in completion: %v", labels(m.completion.items))
	}
}

func TestCompletionClosesOnSpaceAndNonMatch(t *testing.T) {
	m := newTestChatTUI()

	m.input.SetValue("/compact ") // space → typing args, not naming a command
	m.updateCompletion()
	if m.completion.active {
		t.Error("menu should close once a space is typed (now entering args)")
	}

	m.input.SetValue("/zzz") // no command matches
	m.updateCompletion()
	if m.completion.active {
		t.Error("menu should close when nothing matches")
	}

	m.input.SetValue("hello") // not a slash line
	m.updateCompletion()
	if m.completion.active {
		t.Error("menu should be inactive for non-slash input")
	}
}

func TestMoveCompletionWraps(t *testing.T) {
	m := newTestChatTUI()
	m.completion = completion{active: true, kind: compSlash, items: []compItem{{label: "/a"}, {label: "/b"}, {label: "/c"}}, sel: 0}
	m.moveCompletion(-1)
	if m.completion.sel != 2 {
		t.Errorf("up from first should wrap to last, got %d", m.completion.sel)
	}
	m.moveCompletion(1)
	if m.completion.sel != 0 {
		t.Errorf("down from last should wrap to first, got %d", m.completion.sel)
	}
}

func TestActiveAtToken(t *testing.T) {
	cases := []struct {
		val     string
		wantTok string
		wantOK  bool
		wantAt  int
	}{
		{"@", "", true, 0},
		{"look at @src/m", "src/m", true, 8},
		{"@internal/agent/", "internal/agent/", true, 0},
		{"a@b.com", "", false, 0},  // '@' not whitespace-preceded → not a ref
		{"@foo bar", "", false, 0}, // cursor token after the space isn't an @ref
		{"plain text", "", false, 0},
	}
	for _, c := range cases {
		at, tok, ok := activeAtToken(c.val)
		if ok != c.wantOK || (ok && (tok != c.wantTok || at != c.wantAt)) {
			t.Errorf("activeAtToken(%q) = (%d,%q,%v), want (%d,%q,%v)", c.val, at, tok, ok, c.wantAt, c.wantTok, c.wantOK)
		}
	}
}

func TestSplitPathToken(t *testing.T) {
	cases := []struct{ in, dir, frag string }{
		{"main", "", "main"},
		{"internal/age", "internal/", "age"},
		{"a/b/c", "a/b/", "c"},
		{"internal/", "internal/", ""},
	}
	for _, c := range cases {
		if d, f := splitPathToken(c.in); d != c.dir || f != c.frag {
			t.Errorf("splitPathToken(%q) = (%q,%q), want (%q,%q)", c.in, d, f, c.dir, c.frag)
		}
	}
}

// TestFileItemsOneLevel verifies @ completion lists exactly one directory level
// (no recursion): a subdir shows as a descendable entry, its contents do not.
func TestFileItemsOneLevel(t *testing.T) {
	dir := t.TempDir()
	writeAt(t, dir, "alpha.go", "x")
	writeAt(t, dir, "sub/deep.go", "y") // creates sub/ with a file inside
	writeAt(t, dir, ".hidden", "z")

	m := newTestChatTUI()
	items := m.fileItems(dir + "/") // token = "<tmp>/", frag = ""

	if !hasLabel(items, "alpha.go") {
		t.Errorf("file alpha.go should be listed: %v", labels(items))
	}
	if !hasLabel(items, "sub/") {
		t.Errorf("subdir should be listed as 'sub/': %v", labels(items))
	}
	if hasLabel(items, "deep.go") {
		t.Errorf("nested file deep.go must NOT be listed (one level only): %v", labels(items))
	}
	if hasLabel(items, ".hidden") {
		t.Errorf("hidden file should be skipped unless frag starts with '.': %v", labels(items))
	}
	// The subdir entry must be a descend (accepting it navigates into it).
	for _, it := range items {
		if it.label == "sub/" && !it.descend {
			t.Error("directory entry should be a descend")
		}
	}
}

func TestFileItemsSearchesBasenameAtTopLevel(t *testing.T) {
	orig, _ := os.Getwd()
	defer os.Chdir(orig)

	dir := t.TempDir()
	writeAt(t, dir, "frontend/wailsjs/runtime/runtime.js", "x")
	writeAt(t, dir, "node_modules/pkg/runtime.js", "noise")
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	m := newTestChatTUI()
	items := m.fileItems("runtime.js")

	if !hasLabel(items, "frontend/wailsjs/runtime/runtime.js") {
		t.Fatalf("top-level @runtime.js should offer nested file path, got %v", labels(items))
	}
	if hasLabel(items, "node_modules/pkg/runtime.js") {
		t.Fatalf("file search should skip node_modules noise, got %v", labels(items))
	}
}

func TestFileItemsSearchRespectsMenuCap(t *testing.T) {
	orig, _ := os.Getwd()
	defer os.Chdir(orig)

	dir := t.TempDir()
	for i := 0; i < maxCompItems; i++ {
		writeAt(t, dir, filepath.Join("aa-dir-"+fmt.Sprintf("%03d", i), "file.txt"), "x")
	}
	writeAt(t, dir, "nested/aa-deep.js", "y")
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	m := newTestChatTUI()
	items := m.fileItems("aa")

	if len(items) != maxCompItems {
		t.Fatalf("fileItems should stay capped at %d entries, got %d", maxCompItems, len(items))
	}
	if hasLabel(items, "nested/aa-deep.js") {
		t.Fatalf("search result should not exceed capped menu: %v", labels(items))
	}
}

func TestFileItemsHiddenWhenDotTyped(t *testing.T) {
	dir := t.TempDir()
	writeAt(t, dir, ".hidden", "z")
	m := newTestChatTUI()
	items := m.fileItems(dir + "/.") // frag = "." → show hidden
	if !hasLabel(items, ".hidden") {
		t.Errorf("hidden file should appear when frag starts with '.': %v", labels(items))
	}
}

// TestSlashArgCompletionMCPSubcommands proves explicit help syntax opens the
// subcommand menu; a bare trailing space stays submit-ready.
func TestSlashArgCompletionMCPSubcommands(t *testing.T) {
	m := newTestChatTUI()
	m.input.SetValue("/mcp?")
	m.updateCompletion()
	if !m.completion.active || m.completion.kind != compSlashArg {
		t.Fatalf("/mcp? should open the argument menu: %+v", m.completion)
	}
	for _, want := range []string{"add", "connect", "remove", "show", "tools"} {
		if !hasLabel(m.completion.items, want) {
			t.Errorf("subcommand %q missing: %v", want, labels(m.completion.items))
		}
	}
	if hasLabel(m.completion.items, "list") {
		t.Errorf("redundant list subcommand should be hidden from /mcp? menu: %v", labels(m.completion.items))
	}
	m.acceptCompletion()
	if got := m.input.Value(); got != "/mcp add " {
		t.Fatalf("accepting /mcp? subcommand should replace ? with command, got %q", got)
	}

	m.input.SetValue("/mcp ")
	m.updateCompletion()
	if m.completion.active {
		t.Fatalf("/mcp <space> should not open the argument menu: %+v", m.completion)
	}
}

// TestSlashArgCompletionMCPFilterAndAccept proves the typed prefix filters the
// subcommands and that accepting replaces only the current token (not the line).
func TestSlashArgCompletionMCPFilterAndAccept(t *testing.T) {
	m := newTestChatTUI()
	m.input.SetValue("/mcp re")
	m.updateCompletion()
	if len(m.completion.items) != 1 || m.completion.items[0].label != "remove" {
		t.Fatalf("/mcp re should filter to remove, got %v", labels(m.completion.items))
	}
	m.acceptCompletion()
	if got := m.input.Value(); got != "/mcp remove " {
		t.Errorf("accept should replace just the token, got %q want %q", got, "/mcp remove ")
	}
}

// TestSlashArgCompletionMCPAddFlags proves add offers transport flags once the
// token starts with "-", and stays quiet for the free-form server name.
func TestSlashArgCompletionMCPAddFlags(t *testing.T) {
	m := newTestChatTUI()
	m.input.SetValue("/mcp add myserver --h")
	m.updateCompletion()
	if !hasLabel(m.completion.items, "--http") {
		t.Errorf("--h should offer --http: %v", labels(m.completion.items))
	}

	m.input.SetValue("/mcp add my")
	m.updateCompletion()
	if m.completion.active {
		t.Error("the free-form server name should not open a menu")
	}
}

// TestSlashCompletionMCPDoesNotAutoDescend proves accepting "/mcp" keeps the
// bare command submit-ready; only an explicitly typed trailing space opens the
// subcommand menu.
func TestSlashCompletionMCPDoesNotAutoDescend(t *testing.T) {
	m := newTestChatTUI()
	m.input.SetValue("/mcp")
	m.updateCompletion()
	m.acceptCompletion()
	if got := m.input.Value(); got != "/mcp" {
		t.Fatalf("accepting /mcp should keep %q, got %q", "/mcp", got)
	}
	if m.completion.active {
		t.Fatalf("accepting /mcp should not chain into the subcommand menu: %+v", m.completion)
	}
}

func TestEnterOnExactMCPSubmitsManager(t *testing.T) {
	isolateUserConfig(t)
	m := newTestChatTUI()
	m.input.SetValue("/mcp")
	m.updateCompletion()
	if !m.completion.active {
		t.Fatal("typing /mcp should show slash completion before Enter")
	}
	if m.completion.kind == compSlashArg {
		t.Fatalf("typing exact /mcp should not open subcommand completion: %+v", m.completion)
	}

	got, _ := m.update(tea.KeyPressMsg{Code: tea.KeyEnter})
	next := got.(chatTUI)
	if next.mcp == nil || next.mcp.stage != mcpStageList {
		t.Fatalf("Enter on exact /mcp should open manager, got %#v", next.mcp)
	}
}

func TestEnterOnMCPWithTrailingSpaceSubmitsManager(t *testing.T) {
	isolateUserConfig(t)
	m := newTestChatTUI()
	m.input.SetValue("/mcp ")
	m.updateCompletion()
	if m.completion.active {
		t.Fatalf("/mcp <space> should stay submit-ready before Enter: %+v", m.completion)
	}

	got, _ := m.update(tea.KeyPressMsg{Code: tea.KeyEnter})
	next := got.(chatTUI)
	if next.mcp == nil || next.mcp.stage != mcpStageList {
		t.Fatalf("Enter on bare /mcp arg menu should open manager, got %#v", next.mcp)
	}
}

// TestSlashArgCompletionRemoveNoHost proves "/mcp remove " stays closed when no
// servers are connected (nothing to suggest), rather than showing an empty box.
func TestSlashArgCompletionRemoveNoHost(t *testing.T) {
	m := newTestChatTUI()
	m.input.SetValue("/mcp remove ")
	m.updateCompletion()
	if m.completion.active {
		t.Error("remove with no connected servers should not open a menu")
	}
}

func TestSlashArgCompletionSwitchBranches(t *testing.T) {
	dir := t.TempDir()
	exec := agent.New(nil, nil, agent.NewSession("sys"), agent.Options{}, event.Discard)
	exec.Session().Add(provider.Message{Role: provider.RoleUser, Content: "root prompt"})
	ctrl := control.New(control.Options{Executor: exec, SessionDir: dir, Label: "test"})
	rootPath := filepath.Join(dir, "root.jsonl")
	ctrl.SetSessionPath(rootPath)
	if err := ctrl.Snapshot(); err != nil {
		t.Fatal(err)
	}

	child := agent.NewSession("sys")
	child.Add(provider.Message{Role: provider.RoleUser, Content: "child prompt"})
	childPath := filepath.Join(dir, "child.jsonl")
	if err := child.Save(childPath); err != nil {
		t.Fatal(err)
	}
	if err := agent.SaveBranchMeta(childPath, agent.BranchMeta{Name: "experiment", ParentID: agent.BranchID(rootPath)}); err != nil {
		t.Fatal(err)
	}

	m := newTestChatTUI()
	m.ctrl = ctrl
	m.input.SetValue("/switch exp")
	m.updateCompletion()
	if !m.completion.active || m.completion.kind != compSlashArg {
		t.Fatalf("/switch should open branch completion: %+v", m.completion)
	}
	if len(m.completion.items) != 1 || m.completion.items[0].label != "child" {
		t.Fatalf("branch completion = %v, want child", labels(m.completion.items))
	}
}

func TestSlashArgCompletionLanguage(t *testing.T) {
	m := newTestChatTUI()
	m.input.SetValue("/language ")
	m.updateCompletion()
	if !m.completion.active || m.completion.kind != compSlashArg {
		t.Fatalf("/language should open arg completion: %+v", m.completion)
	}
	for _, want := range []string{"auto", "en", "zh"} {
		if !hasLabel(m.completion.items, want) {
			t.Fatalf("/language completion missing %q: %v", want, labels(m.completion.items))
		}
	}
}

func TestSlashArgCompletionAutoPlan(t *testing.T) {
	m := newTestChatTUI()
	m.input.SetValue("/auto-plan ")
	m.updateCompletion()
	if !m.completion.active || m.completion.kind != compSlashArg {
		t.Fatalf("/auto-plan should open arg completion: %+v", m.completion)
	}
	for _, want := range []string{"off", "on"} {
		if !hasLabel(m.completion.items, want) {
			t.Fatalf("/auto-plan completion missing %q: %v", want, labels(m.completion.items))
		}
	}
	if hasLabel(m.completion.items, "ask") {
		t.Fatalf("/auto-plan completion should not include legacy ask: %v", labels(m.completion.items))
	}
}

func labels(items []compItem) []string {
	out := make([]string, len(items))
	for i, it := range items {
		out[i] = it.label
	}
	return out
}

func hasLabel(items []compItem, label string) bool {
	for _, it := range items {
		if it.label == label {
			return true
		}
	}
	return false
}
