package sessiondisplay

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"

	"reasonix/internal/control"
)

func TestMessageKeyStable(t *testing.T) {
	const want = "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad"
	if got := MessageKey("abc"); got != want {
		t.Fatalf("MessageKey = %q, want %q", got, want)
	}
}

func TestRecordResolveAndFallback(t *testing.T) {
	dir := t.TempDir()
	sessionPath := filepath.Join(dir, "session.jsonl")
	content := "expanded prompt"
	display := "[Pasted text #1 · 5 lines]"

	if err := Record(dir, sessionPath, content, display); err != nil {
		t.Fatalf("Record: %v", err)
	}
	if got := Resolve(dir, sessionPath, content); got != display {
		t.Fatalf("Resolve = %q, want %q", got, display)
	}
	composed := control.PlanModeMarker + "\n\nvisible prompt"
	if got := Resolve(dir, sessionPath, composed); got != "visible prompt" {
		t.Fatalf("fallback Resolve = %q, want visible prompt", got)
	}

	resolver := ResolverFromMap(Load(dir), sessionPath)
	loaded := Load(dir)
	loaded[filepath.Base(sessionPath)][MessageKey(content)] = "mutated after resolver"
	if got := resolver(content); got != display {
		t.Fatalf("resolver did not retain its snapshot: got %q, want %q", got, display)
	}
}

func TestLoadMissingMalformedAndUTF8BOM(t *testing.T) {
	dir := t.TempDir()
	if got := Load(dir); got == nil || len(got) != 0 {
		t.Fatalf("missing Load = %#v, want non-nil empty map", got)
	}
	if err := os.WriteFile(Path(dir), []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := Load(dir); got == nil || len(got) != 0 {
		t.Fatalf("malformed Load = %#v, want non-nil empty map", got)
	}
	body := []byte("\xef\xbb\xbf{\"session.jsonl\":{\"" + MessageKey("prompt") + "\":\"display\"}}")
	if err := os.WriteFile(Path(dir), body, 0o600); err != nil {
		t.Fatal(err)
	}
	if got := Resolve(dir, filepath.Join(dir, "session.jsonl"), "prompt"); got != "display" {
		t.Fatalf("UTF-8 BOM Resolve = %q, want display", got)
	}
}

func TestSavePreservesModeAndRemoveDeletesEmptySidecar(t *testing.T) {
	dir := t.TempDir()
	first := "first.jsonl"
	second := "second.jsonl"
	displays := Map{
		first:  {MessageKey("one"): "display one"},
		second: {MessageKey("two"): "display two"},
	}
	if err := Save(dir, displays); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(Path(dir))
		if err != nil {
			t.Fatal(err)
		}
		if got := info.Mode().Perm(); got != 0o600 {
			t.Fatalf("new sidecar mode = %o, want 600", got)
		}
		if err := os.Chmod(Path(dir), 0o640); err != nil {
			t.Fatal(err)
		}
		if err := Save(dir, displays); err != nil {
			t.Fatalf("replacement Save: %v", err)
		}
		info, err = os.Stat(Path(dir))
		if err != nil {
			t.Fatal(err)
		}
		if got := info.Mode().Perm(); got != 0o640 {
			t.Fatalf("replacement sidecar mode = %o, want preserved 640", got)
		}
	}

	if err := RemoveKey(dir, first); err != nil {
		t.Fatalf("RemoveKey first: %v", err)
	}
	if got := Load(dir); len(got) != 1 || got[second] == nil {
		t.Fatalf("after first removal = %#v", got)
	}
	if err := Remove(dir, filepath.Join(dir, second)); err != nil {
		t.Fatalf("Remove second: %v", err)
	}
	if _, err := os.Stat(Path(dir)); !os.IsNotExist(err) {
		t.Fatalf("empty sidecar still exists: %v", err)
	}
}

func TestPruneUsesComposableOwnership(t *testing.T) {
	dir := t.TempDir()
	if err := Save(dir, Map{
		"live.jsonl":    {MessageKey("live"): "live display"},
		"missing.jsonl": {MessageKey("missing"): "missing display"},
	}); err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	if err := Prune(dir, func(key string) bool {
		seen[key] = true
		return key == "live.jsonl"
	}); err != nil {
		t.Fatalf("Prune: %v", err)
	}
	if !seen["live.jsonl"] || !seen["missing.jsonl"] {
		t.Fatalf("ownership callback saw %v", seen)
	}
	if got := Load(dir); len(got) != 1 || got["live.jsonl"] == nil {
		t.Fatalf("pruned displays = %#v", got)
	}
	if err := Prune(dir, nil); err != nil {
		t.Fatalf("Prune all: %v", err)
	}
	if _, err := os.Stat(Path(dir)); !os.IsNotExist(err) {
		t.Fatalf("empty pruned sidecar still exists: %v", err)
	}
}

func TestConcurrentRecordDoesNotLoseUpdates(t *testing.T) {
	dir := t.TempDir()
	assertConcurrentRecords(t, dir, dir, 96)
}

func TestCanonicalDirectoryLockSerializesSymlinkAliases(t *testing.T) {
	realDir := t.TempDir()
	alias := filepath.Join(t.TempDir(), "session-dir-link")
	if err := os.Symlink(realDir, alias); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	assertConcurrentRecords(t, realDir, alias, 96)
}

func assertConcurrentRecords(t *testing.T, firstDir, secondDir string, count int) {
	t.Helper()
	start := make(chan struct{})
	errs := make(chan error, count)
	var wg sync.WaitGroup
	for i := 0; i < count; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			<-start
			dir := firstDir
			if index%2 != 0 {
				dir = secondDir
			}
			content := fmt.Sprintf("expanded prompt %03d", index)
			display := fmt.Sprintf("visible prompt %03d", index)
			if err := Record(dir, filepath.Join(dir, "shared.jsonl"), content, display); err != nil {
				errs <- err
			}
		}(i)
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Errorf("concurrent Record: %v", err)
	}
	if t.Failed() {
		return
	}

	got := Load(firstDir)["shared.jsonl"]
	if len(got) != count {
		t.Fatalf("record count = %d, want %d; records were lost", len(got), count)
	}
	for i := 0; i < count; i++ {
		content := fmt.Sprintf("expanded prompt %03d", i)
		want := fmt.Sprintf("visible prompt %03d", i)
		if display := got[MessageKey(content)]; display != want {
			t.Fatalf("display %d = %q, want %q", i, display, want)
		}
	}
}
