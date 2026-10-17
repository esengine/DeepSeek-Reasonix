package control

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"reasonix/internal/tool/builtin"
)

func TestExternalFileNativeAndDriveTokensReachReadTool(t *testing.T) {
	workspace := t.TempDir()
	path := filepath.Join(t.TempDir(), "selected app.txt")
	if err := os.WriteFile(path, []byte("selected file contents\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	resolver := builtin.NewPathResolver()
	c := newOwnedTestController(t, Options{WorkspaceRoot: workspace, ExternalFolderToolRefs: resolver})
	token, _, err := c.RegisterExternalFileRef(path)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS == "windows" && !strings.Contains(token, ":/") {
		t.Fatalf("expected native Windows drive token, got %q", token)
	}
	canonical, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatal(err)
	}
	driveToken := "C:/Reasonix Attachments/selected app.txt"
	resolver.RegisterReadRoot(driveToken, canonical)
	readers := (builtin.Workspace{Dir: workspace, ReadPaths: resolver}).Tools("read_file")
	if len(readers) != 1 {
		t.Fatalf("readers=%d", len(readers))
	}
	for _, ref := range []string{token, driveToken} {
		resolved, ok := resolver.Resolve(ref)
		if !ok || !resolved.External || resolved.Path != canonical || resolved.Root != canonical {
			t.Fatalf("token=%q resolved=%+v ok=%v", ref, resolved, ok)
		}
		args, err := json.Marshal(map[string]string{"path": ref})
		if err != nil {
			t.Fatal(err)
		}
		out, err := readers[0].Execute(context.Background(), args)
		if err != nil || !strings.Contains(out, "selected file contents") {
			t.Fatalf("token=%q output=%q err=%v", ref, out, err)
		}
		for _, ungranted := range []string{filepath.ToSlash(filepath.Dir(ref)), ref + "/../sibling.txt"} {
			if _, ok := resolver.Resolve(ungranted); ok {
				t.Fatalf("file grant authorized %q", ungranted)
			}
		}
	}
}
