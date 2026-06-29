package builtin

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/tool"
)

func TestResolveIn(t *testing.T) {
	workDir := filepath.Join(t.TempDir(), "proj")
	absolute := filepath.Join(t.TempDir(), "etc", "passwd")
	cases := []struct {
		workDir, p, want string
	}{
		{"", "foo.go", "foo.go"}, // empty workDir: unchanged
		{"", "", ""},             // empty workDir: unchanged
		{workDir, "foo.go", filepath.Join(workDir, "foo.go")},                  // relative joins
		{workDir, "a/b.go", filepath.Join(workDir, "a", "b.go")},               // nested relative
		{workDir, ".", workDir},                                                // "." targets the root
		{workDir, "", workDir},                                                 // empty targets the root
		{workDir, absolute, absolute},                                          // absolute honored verbatim
		{workDir, "../escape", filepath.Join(filepath.Dir(workDir), "escape")}, // join cleans (confiner enforces)
	}
	for _, c := range cases {
		if got := resolveIn(c.workDir, c.p); got != c.want {
			t.Errorf("resolveIn(%q, %q) = %q, want %q", c.workDir, c.p, got, c.want)
		}
	}
}

// TestWorkspaceBindsReadAndWrite checks that relative paths land inside the
// workspace directory rather than the process cwd, for both a reader and a
// writer, and that write confinement defaults to the workspace.
func TestWorkspaceBindsReadAndWrite(t *testing.T) {
	dir := t.TempDir()
	ws := Workspace{Dir: dir}
	tools := byName(ws.Tools())

	// write_file with a relative path writes inside the workspace.
	wf := tools["write_file"]
	if _, err := wf.Execute(context.Background(), argsJSON(t, map[string]any{"path": "out.txt", "content": "hi\n"})); err != nil {
		t.Fatalf("write: %v", err)
	}
	if b, err := os.ReadFile(filepath.Join(dir, "out.txt")); err != nil || string(b) != "hi\n" {
		t.Fatalf("file not written into workspace: %q err=%v", b, err)
	}

	// read_file with the same relative path reads it back.
	rf := tools["read_file"]
	out, err := rf.Execute(context.Background(), argsJSON(t, map[string]any{"path": "out.txt"}))
	if err != nil || !strings.Contains(out, "hi") {
		t.Fatalf("read back: out=%q err=%v", out, err)
	}
}

// TestWorkspaceWriteConfinement confirms the default write root is the workspace
// dir: a relative write succeeds, an absolute write outside it is refused.
func TestWorkspaceWriteConfinement(t *testing.T) {
	dir := t.TempDir()
	outside := filepath.Join(t.TempDir(), "evil.txt")
	wf := byName(Workspace{Dir: dir}.Tools())["write_file"]

	// Inside the workspace: allowed.
	if _, err := wf.Execute(context.Background(), argsJSON(t, map[string]any{"path": "ok.txt", "content": "x"})); err != nil {
		t.Fatalf("in-workspace write should succeed: %v", err)
	}
	// Absolute path outside the workspace: refused by the confiner.
	if _, err := wf.Execute(context.Background(), argsJSON(t, map[string]any{"path": outside, "content": "x"})); err == nil {
		t.Error("write outside the workspace should be refused")
	}
}

func TestWorkspaceMoveFileBindsAndConfines(t *testing.T) {
	dir := t.TempDir()
	outside := filepath.Join(t.TempDir(), "evil.txt")
	if err := os.WriteFile(filepath.Join(dir, "a.md"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	mv := byName(Workspace{Dir: dir}.Tools())["move_file"]

	if _, err := mv.Execute(context.Background(), argsJSON(t, map[string]any{"source_path": "a.md", "destination_path": "docs/a.md"})); err != nil {
		t.Fatalf("move inside workspace should succeed: %v", err)
	}
	if b, err := os.ReadFile(filepath.Join(dir, "docs", "a.md")); err != nil || string(b) != "hello" {
		t.Fatalf("file not moved inside workspace: %q err=%v", b, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := mv.Execute(context.Background(), argsJSON(t, map[string]any{"source_path": "b.md", "destination_path": outside})); err == nil {
		t.Fatal("move outside the workspace should be refused")
	}
}

// TestWorkspaceBashDir checks bash runs in the workspace directory.
func TestWorkspaceBashDir(t *testing.T) {
	dir := t.TempDir()
	b := byName(Workspace{Dir: dir}.Tools())["bash"]
	out, err := b.Execute(context.Background(), argsJSON(t, map[string]any{"command": "pwd"}))
	if err != nil {
		t.Fatalf("bash: %v", err)
	}
	// macOS /tmp is a symlink to /private/tmp; compare on the resolved base name.
	if !strings.Contains(out, filepath.Base(dir)) {
		t.Errorf("bash cwd = %q, want to contain %q", strings.TrimSpace(out), filepath.Base(dir))
	}
}

// TestWorkspacePreviewBinds confirms a workspace-bound writer previews the file
// inside its directory when given a relative path.
func TestWorkspacePreviewBinds(t *testing.T) {
	dir := t.TempDir()
	wf := byName(Workspace{Dir: dir}.Tools())["write_file"]
	p, ok := wf.(tool.Previewer)
	if !ok {
		t.Fatal("write_file should be a Previewer")
	}
	change, err := p.Preview(argsJSON(t, map[string]any{"path": "new.txt", "content": "a\n"}))
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if change.Path != filepath.Join(dir, "new.txt") {
		t.Errorf("preview path = %q, want inside workspace", change.Path)
	}
}

// TestWorkspaceEnabledFilter checks the enabled whitelist.
func TestWorkspaceEnabledFilter(t *testing.T) {
	got := byName(Workspace{Dir: t.TempDir()}.Tools("read_file", "bash", "todo_write", "wait"))
	if len(got) != 4 || got["read_file"] == nil || got["bash"] == nil || got["todo_write"] == nil || got["wait"] == nil {
		t.Fatalf("enabled filter returned %d tools: %v", len(got), keys(got))
	}
}

func TestWorkspacePreservesSessionLevelBuiltins(t *testing.T) {
	got := byName(Workspace{Dir: t.TempDir()}.Tools())
	for _, name := range []string{
		"todo_write",
		"complete_step",
		"bash_output",
		"kill_shell",
		"wait",
		"move_file",
		"notebook_edit",
	} {
		if got[name] == nil {
			t.Fatalf("workspace tools missing %q; got %v", name, keys(got))
		}
	}
}

func TestWorkspaceToolSchemasStableAcrossRoots(t *testing.T) {
	firstRoot := t.TempDir()
	secondRoot := t.TempDir()

	first := workspaceSchemasJSON(t, firstRoot)
	second := workspaceSchemasJSON(t, secondRoot)

	if first != second {
		t.Fatalf("workspace tool schemas should not depend on workspace root:\nfirst=%s\nsecond=%s", first, second)
	}
	if strings.Contains(first, firstRoot) || strings.Contains(first, secondRoot) {
		t.Fatalf("workspace paths must not leak into tool schemas: %s", first)
	}

	resolver := NewPathResolver()
	resolver.RegisterReadRoot("__reasonix_external_folder/schema/root", t.TempDir())
	withResolver := workspaceSchemasJSONWithResolver(t, firstRoot, resolver)
	if first != withResolver {
		t.Fatalf("workspace tool schemas should not depend on external read roots:\nfirst=%s\nwith=%s", first, withResolver)
	}
}

// TestWorkspaceEmptyDirUnchanged confirms a zero-Dir workspace yields tools that
// behave exactly like the process-cwd built-ins (relative path unchanged).
func TestWorkspaceEmptyDirUnchanged(t *testing.T) {
	tools := Workspace{}.Tools()
	if len(tools) == 0 {
		t.Fatal("expected tools")
	}
	// A zero-value read_file and the workspace's read_file are equivalent: both
	// resolve "foo" against the process cwd.
	if resolveIn("", "foo") != "foo" {
		t.Fatal("empty workspace should leave paths unresolved")
	}
}

func TestWorkspaceReadToolsResolveExternalReadRoots(t *testing.T) {
	workspace := t.TempDir()
	external := t.TempDir()
	if err := os.MkdirAll(filepath.Join(external, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	externalFile := filepath.Join(external, "src", "outside.txt")
	if err := os.WriteFile(externalFile, []byte("outside\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	token := "__reasonix_external_folder/abc123/External"
	resolver := NewPathResolver()
	resolver.RegisterReadRoot(token, external)
	tools := byName(Workspace{Dir: workspace, ReadPaths: resolver}.Tools("read_file", "ls", "grep", "glob"))

	readOut := runTool(t, tools["read_file"], map[string]any{"path": token + "/src/outside.txt"})
	if !strings.Contains(readOut, "1→outside") {
		t.Fatalf("read_file external token output = %q, want file content", readOut)
	}

	lsOut := runTool(t, tools["ls"], map[string]any{"path": token + "/src"})
	if !strings.Contains(lsOut, "outside.txt") {
		t.Fatalf("ls external token output = %q, want outside.txt", lsOut)
	}

	grepOut := runTool(t, tools["grep"], map[string]any{"pattern": "outside", "path": token})
	if !strings.Contains(grepOut, token+"/src/outside.txt:1:outside") {
		t.Fatalf("grep external token output = %q, want token path hit", grepOut)
	}
	if strings.Contains(grepOut, filepath.ToSlash(external)) {
		t.Fatalf("grep external token output leaked local path: %q", grepOut)
	}

	globOut := runTool(t, tools["glob"], map[string]any{"pattern": token + "/**/*.txt"})
	if !strings.Contains(globOut, token+"/src/outside.txt") {
		t.Fatalf("glob external token output = %q, want token path hit", globOut)
	}
	if strings.Contains(globOut, filepath.ToSlash(external)) {
		t.Fatalf("glob external token output leaked local path: %q", globOut)
	}

	assertExternalToolError(t, tools["read_file"], map[string]any{"path": token + "/src/missing.txt"}, token+"/src/missing.txt", external)
	assertExternalToolError(t, tools["ls"], map[string]any{"path": token + "/missing"}, token+"/missing", external)
	assertExternalToolError(t, tools["grep"], map[string]any{"pattern": "outside", "path": token + "/missing"}, token+"/missing", external)
	assertExternalToolError(t, tools["glob"], map[string]any{"pattern": token + "/missing/**/*.go"}, token+"/missing/**/*.go", external)
}

// --- helpers ---

func byName(tools []tool.Tool) map[string]tool.Tool {
	m := make(map[string]tool.Tool, len(tools))
	for _, t := range tools {
		m[t.Name()] = t
	}
	return m
}

func keys(m map[string]tool.Tool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func workspaceSchemasJSON(t *testing.T, dir string) string {
	return workspaceSchemasJSONWithResolver(t, dir, nil)
}

func workspaceSchemasJSONWithResolver(t *testing.T, dir string, resolver *PathResolver) string {
	t.Helper()
	reg := tool.NewRegistry()
	for _, tt := range (Workspace{Dir: dir, ReadPaths: resolver}).Tools() {
		reg.Add(tt)
	}
	b, err := json.Marshal(reg.Schemas())
	if err != nil {
		t.Fatalf("marshal schemas: %v", err)
	}
	return string(b)
}

func assertExternalToolError(t *testing.T, tl tool.Tool, args map[string]any, wantTokenPath, externalRoot string) {
	t.Helper()
	_, err := tl.Execute(context.Background(), argsJSON(t, args))
	if err == nil {
		t.Fatalf("%s should fail for missing external path", tl.Name())
	}
	msg := err.Error()
	if !strings.Contains(msg, wantTokenPath) {
		t.Fatalf("%s error = %q, want token path %q", tl.Name(), msg, wantTokenPath)
	}
	if strings.Contains(msg, filepath.ToSlash(externalRoot)) || strings.Contains(msg, externalRoot) {
		t.Fatalf("%s error leaked external root: %q", tl.Name(), msg)
	}
}
