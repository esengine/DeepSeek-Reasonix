package builtin

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"reasonix/internal/base/diff"
	"reasonix/internal/contract/tool"
	"reasonix/internal/safety/sandbox"
)

// networkSpellings are spelled as network paths on Windows and are absolute on
// every platform, so the tools take them verbatim when the Windows reading of a
// path is held.
var networkTargets = []string{
	"//evil/share/x.txt",
	"//127.0.0.1/c$/windows/win.ini",
	"//LOCALHOST/C$/x.txt",
	"//?/UNC/evil/share/x.txt",
	"///evil/share/x.txt",
	"//evil./share /x.txt",
}

func holdWindowsPaths(t *testing.T) *int {
	t.Helper()
	prev, prevLookup := windowsPaths, lookupExisting
	lookups := new(int)
	windowsPaths = true
	lookupExisting = func(p string) (string, error) {
		*lookups++
		return prevLookup(p)
	}
	t.Cleanup(func() { windowsPaths, lookupExisting = prev, prevLookup })
	return lookups
}

func isNetworkCode(err error) bool {
	var r tool.Refusal
	return errors.As(err, &r) && r.Code == CodeNetworkPathOutsideScope
}

func workspaceTools(t *testing.T, dir string, extra ...func(*Workspace)) map[string]tool.Tool {
	t.Helper()
	ws := Workspace{Dir: dir}
	for _, f := range extra {
		f(&ws)
	}
	out := map[string]tool.Tool{}
	for _, tl := range ws.Tools() {
		out[tl.Name()] = tl
	}
	return out
}

func mustJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestReadToolsRefuseNetworkPathBeforeAnyLookup(t *testing.T) {
	lookups := holdWindowsPaths(t)
	tools := workspaceTools(t, t.TempDir())
	for _, target := range networkTargets {
		calls := []struct {
			tool string
			args map[string]any
		}{
			{"read_file", map[string]any{"path": target}},
			{"ls", map[string]any{"path": target}},
			{"ls", map[string]any{"path": target, "recursive": true}},
			{"glob", map[string]any{"pattern": target}},
			{"glob", map[string]any{"pattern": target + "/**/*.txt"}},
			{"grep", map[string]any{"pattern": "fonts", "path": target}},
			{"code_index", map[string]any{"action": "outline", "path": target}},
		}
		for _, c := range calls {
			*lookups = 0
			_, err := tools[c.tool].Execute(context.Background(), mustJSON(t, c.args))
			if !isNetworkCode(err) {
				t.Errorf("%s(%q): err = %v, want the network-path refusal", c.tool, target, err)
			}
			if *lookups != 0 {
				t.Errorf("%s(%q): %d lookups before the refusal, want 0", c.tool, target, *lookups)
			}
		}
	}
}

func TestWriteToolsRefuseNetworkPathBeforeAnyLookup(t *testing.T) {
	lookups := holdWindowsPaths(t)
	tools := workspaceTools(t, t.TempDir())
	for _, target := range networkTargets {
		calls := map[string]map[string]any{
			"write_file":    {"path": target, "content": "x"},
			"edit_file":     {"path": target, "old_string": "a", "new_string": "b"},
			"multi_edit":    {"path": target, "edits": []map[string]any{{"old_string": "a", "new_string": "b"}}},
			"notebook_edit": {"path": target, "cell_number": 0, "new_source": "x", "edit_mode": "replace"},
			"move_file":     {"source_path": target, "destination_path": filepath.Join(t.TempDir(), "d")},
			"delete_range":  {"path": target, "start_anchor": "a", "end_anchor": "b"},
			"delete_symbol": {"path": target, "name": "f"},
		}
		for name, args := range calls {
			*lookups = 0
			raw := mustJSON(t, args)
			_, err := tools[name].Execute(context.Background(), raw)
			if !isNetworkCode(err) {
				t.Errorf("%s(%q): err = %v, want the network-path refusal", name, target, err)
			}
			if pv, ok := tools[name].(tool.Previewer); ok {
				if _, err := pv.Preview(context.Background(), raw); err == nil || (name != "delete_range" && name != "delete_symbol" && !isNetworkCode(err)) {
					t.Errorf("%s(%q) preview: err = %v, want a refusal", name, target, err)
				}
			}
			if wr, ok := tools[name].(tool.WritePathResolver); ok {
				if paths, err := wr.WritePaths(raw); !isNetworkCode(err) || len(paths) != 0 {
					t.Errorf("%s(%q) WritePaths = %v, %v, want the refusal and no path", name, target, paths, err)
				}
			}
			if *lookups != 0 {
				t.Errorf("%s(%q): %d lookups before the refusal, want 0", name, target, *lookups)
			}
		}
	}
}

func TestMoveFileRefusesNetworkDestination(t *testing.T) {
	lookups := holdWindowsPaths(t)
	dir := t.TempDir()
	src := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(src, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := workspaceTools(t, dir)["move_file"].Execute(context.Background(),
		mustJSON(t, map[string]any{"source_path": src, "destination_path": "//evil/share/d.txt"}))
	if !isNetworkCode(err) {
		t.Fatalf("err = %v, want the network-path refusal", err)
	}
	if _, statErr := os.Stat(src); statErr != nil {
		t.Fatalf("source must be untouched: %v", statErr)
	}
	_ = lookups
}

func TestNetworkWorkspaceKeepsItsOwnPaths(t *testing.T) {
	lookups := holdWindowsPaths(t)
	root := "//fileserver/team/proj"
	tools := workspaceTools(t, root, func(w *Workspace) { w.WriteRoots = []string{root} })

	inside := []string{root + "/a.txt", "//FILESERVER/Team/PROJ/sub/a.txt", "sub/a.txt", "//?/UNC/fileserver/team/proj/a.txt"}
	for _, p := range inside {
		raw := mustJSON(t, map[string]any{"path": p, "content": "x"})
		if _, err := tools["write_file"].(tool.WritePathResolver).WritePaths(raw); isNetworkCode(err) {
			t.Errorf("WritePaths(%q) refused inside the network workspace: %v", p, err)
		}
		if err := confine(tools["write_file"].(writeFile).roots, resolveIn(root, p)); isNetworkCode(err) {
			t.Errorf("confine(%q) refused inside the network workspace: %v", p, err)
		}
		_, err := tools["read_file"].Execute(context.Background(), mustJSON(t, map[string]any{"path": p}))
		if isNetworkCode(err) {
			t.Errorf("read_file(%q) refused inside the network workspace: %v", p, err)
		}
	}
	for _, p := range []string{"//fileserver/team/other/a.txt", "//fileserver/team/proj2/a.txt", "//evil/team/proj/a.txt", "//fileserver/team/proj/../other/a.txt"} {
		_, err := tools["read_file"].Execute(context.Background(), mustJSON(t, map[string]any{"path": p}))
		if !isNetworkCode(err) {
			t.Errorf("read_file(%q): err = %v, want the network-path refusal", p, err)
		}
		if err := confine(tools["write_file"].(writeFile).roots, p); !isNetworkCode(err) {
			t.Errorf("confine(%q) = %v, want the network-path refusal", p, err)
		}
	}
	_ = lookups
}

func TestRegisteredNetworkFolderRefIsReadable(t *testing.T) {
	holdWindowsPaths(t)
	resolver := NewPathResolver()
	resolver.RegisterReadRoot("ext1", "//dropped/share/folder")
	tools := workspaceTools(t, t.TempDir(), func(w *Workspace) { w.ReadPaths = resolver })
	_, err := tools["read_file"].Execute(context.Background(), mustJSON(t, map[string]any{"path": "ext1/a.txt"}))
	if isNetworkCode(err) {
		t.Fatalf("a folder the user registered must stay readable: %v", err)
	}
	_, err = tools["read_file"].Execute(context.Background(), mustJSON(t, map[string]any{"path": "//dropped/share/other/a.txt"}))
	if !isNetworkCode(err) {
		t.Fatalf("a path beside the registered folder: err = %v, want the refusal", err)
	}
}

func TestLocalPathsAreUnaffectedByTheNetworkRule(t *testing.T) {
	holdWindowsPaths(t)
	dir := t.TempDir()
	file := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(file, []byte("hello\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	tools := workspaceTools(t, dir)
	for _, p := range []string{file, "a.txt", "./a.txt"} {
		out, err := tools["read_file"].Execute(context.Background(), mustJSON(t, map[string]any{"path": p}))
		if err != nil || out == "" {
			t.Errorf("read_file(%q) = %q, %v", p, out, err)
		}
	}
	if _, err := tools["write_file"].Execute(context.Background(), mustJSON(t, map[string]any{"path": "b.txt", "content": "x"})); err != nil {
		t.Errorf("write_file inside the workspace: %v", err)
	}
	var change diff.Change
	if pv, ok := tools["write_file"].(tool.Previewer); ok {
		var err error
		if change, err = pv.Preview(context.Background(), mustJSON(t, map[string]any{"path": "c.txt", "content": "x"})); err != nil || change.Added == 0 {
			t.Errorf("preview inside the workspace = %+v, %v", change, err)
		}
	}
}

func TestNetworkRuleIsWindowsOnly(t *testing.T) {
	prev := windowsPaths
	windowsPaths = false
	t.Cleanup(func() { windowsPaths = prev })
	if runtime.GOOS == "windows" {
		t.Skip("the path rules cannot be turned off on Windows")
	}
	_, err := workspaceTools(t, t.TempDir())["read_file"].Execute(context.Background(),
		mustJSON(t, map[string]any{"path": "//evil/share/x.txt"}))
	if isNetworkCode(err) {
		t.Fatalf("`//x` is a local path off Windows, got %v", err)
	}
}

func TestBoundedDeleteTargetDoesNotLookUpANetworkPath(t *testing.T) {
	lookups := holdWindowsPaths(t)
	root := t.TempDir()
	b := bash{workDir: root, sb: sandbox.Spec{WriteRoots: []string{root}}}
	for _, target := range networkTargets {
		*lookups = 0
		if b.boundedDeleteTarget(target) {
			t.Errorf("boundedDeleteTarget(%q) accepted a network path", target)
		}
		if *lookups != 0 {
			t.Errorf("boundedDeleteTarget(%q): %d lookups, want 0", target, *lookups)
		}
	}
}

func TestUnconfinedWritersStillRefuseANetworkPath(t *testing.T) {
	lookups := holdWindowsPaths(t)
	for _, target := range networkTargets {
		*lookups = 0
		if err := confine(nil, target); !isNetworkCode(err) {
			t.Errorf("confine(nil, %q) = %v, want the network-path refusal, as Preview gives", target, err)
		}
		if *lookups != 0 {
			t.Errorf("confine(nil, %q): %d lookups, want 0", target, *lookups)
		}
	}
	if err := confine(nil, filepath.Join(t.TempDir(), "x")); err != nil {
		t.Errorf("unconfined local path refused: %v", err)
	}
}
