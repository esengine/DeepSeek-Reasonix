package agent

import (
	"context"
	"io/fs"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"reasonix/internal/base/fileutil"
)

// walkReference is the sequential walk the parallel one stands in for, kept
// verbatim so the two can be held to one answer.
func walkReference(ctx context.Context, root string, limit int) workspaceScan {
	state := map[string]pathState{}
	complete, overLimit := true, false
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if ctx.Err() != nil {
			complete = false
			return filepath.SkipAll
		}
		if err != nil {
			complete = false
			return nil
		}
		if d.IsDir() {
			if fileutil.IsVCSStoreDir(d.Name()) && path != root {
				return filepath.SkipDir
			}
			return nil
		}
		if len(state) >= limit {
			complete, overLimit = false, true
			return filepath.SkipAll
		}
		info, err := d.Info()
		if err != nil {
			complete = false
			return nil
		}
		state[path] = pathState{exists: true, size: info.Size(), modTime: info.ModTime().UnixNano(), mode: info.Mode()}
		return nil
	})
	if err != nil {
		complete = false
	}
	return workspaceScan{state: state, complete: complete, overLimit: overLimit}
}

func TestParallelScanAnswersAsTheSequentialWalk(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	write := func(rel string) {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(rel), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, rel := range []string{"a.txt", "src/b.go", "src/deep/c.go", "src/deep/deeper/d.go", ".git/HEAD", "sub/.git/config", "sub/e.md", "node_modules/x/index.js"} {
		write(rel)
	}
	for i := range 300 {
		write("wide/f" + string(rune('a'+i%26)) + string(rune('a'+i/26)) + ".txt")
	}
	if err := os.WriteFile(filepath.Join(outside, "o.txt"), []byte("o"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "linked")
	if runtime.GOOS == "windows" {
		_ = exec.Command("cmd", "/c", "mklink", "/J", link, outside).Run()
	} else {
		_ = os.Symlink(outside, link)
	}

	for _, limit := range []int{1000, 308, 307, 50, 1} {
		want := walkReference(t.Context(), root, limit)
		got := scanWorkspaceTo(t.Context(), root, limit)
		if got.complete != want.complete || got.overLimit != want.overLimit {
			t.Fatalf("limit %d: complete/over = %v/%v, sequential %v/%v", limit, got.complete, got.overLimit, want.complete, want.overLimit)
		}
		if want.complete && !maps.Equal(got.state, want.state) {
			t.Fatalf("limit %d: %d entries, sequential %d", limit, len(got.state), len(want.state))
		}
	}
	file := filepath.Join(root, "a.txt")
	if got, want := scanWorkspaceTo(t.Context(), file, 10), walkReference(t.Context(), file, 10); !maps.Equal(got.state, want.state) || got.complete != want.complete {
		t.Fatalf("a file as the root: %+v, sequential %+v", got, want)
	}
	missing := filepath.Join(root, "nope")
	if got := scanWorkspaceTo(t.Context(), missing, 10); got.complete {
		t.Fatal("a root that does not exist was reported complete")
	}
}

func TestWorkspaceScanLimitIsTheDocumentedBoundary(t *testing.T) {
	if workspaceScanLimit != 50_000 {
		t.Fatalf("workspaceScanLimit = %d; REASONIX.md documents completeness below 50k files", workspaceScanLimit)
	}
}

func TestScanLimitOnlyTightens(t *testing.T) {
	for _, tc := range []struct{ set, want int }{
		{0, workspaceScanLimit},
		{64, 64},
		{workspaceScanLimit * 10, workspaceScanLimit},
	} {
		a := &Agent{agentConfig: agentConfig{workspaceScanLimit: tc.set}}
		if got := a.scanLimit(); got != tc.want {
			t.Errorf("set %d: scanLimit = %d, want %d", tc.set, got, tc.want)
		}
	}
}
