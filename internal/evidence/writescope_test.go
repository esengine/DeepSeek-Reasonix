package evidence

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestClassifyWriteScopeTempIsScratch(t *testing.T) {
	cases := []string{"/tmp/btc_klines.py"}
	if runtime.GOOS == "darwin" {
		cases = append(cases, "/private/tmp/btc_klines.py")
	}
	for _, path := range cases {
		if got := ClassifyWriteScope(path, "/home/dev/project", nil); got != WriteScopeScratch {
			t.Fatalf("ClassifyWriteScope(%q) = %s, want scratch", path, got)
		}
	}
}

func TestClassifyWriteScopeWorkspaceAndOutside(t *testing.T) {
	root := t.TempDir()
	inside := filepath.Join(root, "internal", "agent", "agent.go")
	if got := ClassifyWriteScope(inside, root, nil); got != WriteScopeWorkspace {
		t.Fatalf("abs workspace path = %s, want workspace", got)
	}
	if got := ClassifyWriteScope("internal/agent/agent.go", root, nil); got != WriteScopeWorkspace {
		t.Fatalf("rel workspace path = %s, want workspace", got)
	}
	if got := ClassifyWriteScope("parser.go", "", nil); got != WriteScopeWorkspace {
		t.Fatalf("relative without root = %s, want workspace", got)
	}
	if got := ClassifyWriteScope("/home/dev/Notes/idea.md", root, nil); got != WriteScopeOutside {
		t.Fatalf("home file = %s, want outside", got)
	}
}

func TestClassifyWriteScopeSessionTempRoot(t *testing.T) {
	scratch := t.TempDir()
	path := filepath.Join(scratch, "probe.py")
	if got := ClassifyWriteScope(path, t.TempDir(), []string{scratch}); got != WriteScopeScratch {
		t.Fatalf("session temp = %s, want scratch", got)
	}
	volumeRoot := filepath.VolumeName(os.TempDir()) + string(filepath.Separator)
	named := filepath.Join(volumeRoot, "reasonix-unowned-scope", "reasonix-session-tmp-abc", "probe.py")
	if got := ClassifyWriteScope(named, t.TempDir(), nil); got != WriteScopeOutside {
		t.Fatalf("unowned named temp = %s, want outside", got)
	}
}

func TestClassifyWriteScopeKeepsLexicalWorkspaceSymlinksInWorkspace(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "link")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	path := filepath.Join(root, "link", "probe.py")
	if got := ClassifyWriteScope(path, root, nil); got != WriteScopeWorkspace {
		t.Fatalf("workspace symlink path = %s, want workspace for the safety layer", got)
	}
}

func TestClassifyWriteScopeScratchSymlinkIntoWorkspaceIsWorkspace(t *testing.T) {
	workspace := t.TempDir()
	scratch := t.TempDir()
	link := filepath.Join(scratch, "workspace-link")
	if err := os.Symlink(workspace, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	path := filepath.Join(link, "probe.py")
	if got := ClassifyWriteScope(path, workspace, []string{scratch}); got != WriteScopeWorkspace {
		t.Fatalf("scratch alias into workspace = %s, want workspace", got)
	}
}
