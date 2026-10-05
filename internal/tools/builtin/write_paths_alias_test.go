package builtin

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"reasonix/internal/base/fileutil"
	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/tool"
)

func TestAmbiguousWriterPathsRetainGrantTargets(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows aliases")
	}
	root, err := fileutil.ResolveExistingPath(testenv.TempDir(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, writer := range ConfineWriters([]string{root}, SessionDataGuard{}, ManagedConfigPaths{}) {
		t.Run(writer.Name(), func(t *testing.T) {
			want := []string{filepath.Join(root, "fixture")}
			if writer.Name() == "move_file" {
				want = []string{filepath.Join(root, "source"), filepath.Join(root, "destination")}
			}
			for _, path := range want {
				if err := os.WriteFile(path, []byte("prior"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			values := map[string]string{"path": want[0] + ".", "source_path": want[0] + ".", "destination_path": want[len(want)-1] + "."}
			args, _ := json.Marshal(values)
			paths, err := writer.(tool.WritePathResolver).WritePaths(args)
			if !errors.Is(err, fileutil.ErrAmbiguousPath) || len(paths) != len(want) {
				t.Fatalf("grant paths=%v err=%v", paths, err)
			}
			for i := range want {
				if paths[i] != want[i] {
					t.Fatalf("path %d=%q want %q", i, paths[i], want[i])
				}
			}
		})
	}
}

func TestAmbiguousMoveDoesNotHideInvalidDestination(t *testing.T) {
	root := testenv.TempDir(t)
	args, _ := json.Marshal(map[string]string{"source_path": filepath.Join(root, "source.")})
	paths, err := ResolveWritePaths(root, nil, args, true)
	if err == nil || errors.Is(err, fileutil.ErrAmbiguousPath) || len(paths) != 0 {
		t.Fatalf("invalid destination: paths=%v err=%v", paths, err)
	}
}

func TestWritersKeepTheCallersPathWhileClaimsResolveIt(t *testing.T) {
	real := testenv.TempDir(t)
	link := filepath.Join(testenv.TempDir(t), "ws")
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	path := filepath.Join(link, "a.txt")
	if err := os.WriteFile(path, []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var receipt string
	views := NewFileViews()
	read := readFile{workDir: link, views: views}
	write := writeFile{workDir: link, views: views, receipt: func(p string, _ bool, _ []byte) { receipt = p }}

	if err := runViewTool(t, read, map[string]string{"path": path}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("user edit\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := runViewTool(t, write, map[string]string{"path": path, "content": "model\n"})
	if !errors.Is(err, ErrFileChangedSinceSeen) {
		t.Fatalf("overwrite of a user edit through a symlinked workspace: err = %v", err)
	}
	if got, _ := os.ReadFile(path); string(got) != "user edit\n" {
		t.Fatalf("user edit lost: %q", got)
	}
	if err := runViewTool(t, read, map[string]string{"path": path}); err != nil {
		t.Fatal(err)
	}
	if err := runViewTool(t, write, map[string]string{"path": path, "content": "model\n"}); err != nil {
		t.Fatal(err)
	}
	if receipt != path {
		t.Fatalf("receipt names %q, want the path the caller used %q", receipt, path)
	}

	args, _ := json.Marshal(map[string]string{"path": path})
	claimed, err := write.WritePaths(args)
	if err != nil || len(claimed) != 1 {
		t.Fatalf("claim = %v, %v", claimed, err)
	}
	want, err := fileutil.ResolveExistingPath(path)
	if err != nil || claimed[0] != want {
		t.Fatalf("claim = %q, want the resolved %q (%v)", claimed[0], want, err)
	}
}
