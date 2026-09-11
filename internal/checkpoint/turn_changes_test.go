package checkpoint

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func turnChangeWrite(t *testing.T, s *Store, path, content string) {
	t.Helper()
	s.CaptureBefore(path, CaptureBeforeOpts{})
	if err := os.WriteFile(filepath.Join(s.root, path), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	s.CaptureAfter(path, CaptureAfterOpts{Seq: 1})
}

func TestTurnChangesNetAndFrozenHistory(t *testing.T) {
	root, dir := t.TempDir(), t.TempDir()
	s := New(dir, root)
	if err := os.WriteFile(filepath.Join(root, "dirty.txt"), []byte("user dirty line\nold\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s.Begin(0, "first", 0)
	turnChangeWrite(t, s, "dirty.txt", "user dirty line\ntemporary\n")
	turnChangeWrite(t, s, "dirty.txt", "user dirty line\nnew\n")
	turnChangeWrite(t, s, "created.txt", "created\n")
	first := s.FreezeTurnChanges(0)
	if first.Coverage != "complete" || len(first.Files) != 2 || first.Added != 2 || first.Removed != 1 {
		t.Fatalf("net result: %+v", first)
	}
	if !strings.Contains(first.Files[0].Patch, "-old") || strings.Contains(first.Files[0].Patch, "-user dirty") {
		t.Fatalf("wrong baseline: %s", first.Files[0].Patch)
	}
	s.Begin(1, "second", 1)
	turnChangeWrite(t, s, "dirty.txt", "future\n")
	s.FreezeTurnChanges(1)
	if got := s.TurnChanges(0); got.Files[0].Patch != first.Files[0].Patch {
		t.Fatalf("history changed: %+v", got)
	}
	first.Files[0].Patch = "caller mutation"
	if got := s.TurnChanges(0); got.Files[0].Patch == first.Files[0].Patch {
		t.Fatal("caller mutated store")
	}
	reloaded := New(dir, root).TurnChanges(0)
	if reloaded.ID == "" || reloaded.Added != 2 || !strings.Contains(reloaded.Files[0].Patch, "+new") {
		t.Fatalf("reload: %+v", reloaded)
	}
	if got := reloaded.Summary(); got.Files[0].Patch != "" || got.Added != reloaded.Added {
		t.Fatal("summary leaked patch or lost facts")
	}
}

func TestTurnChangesNoOpDeletionBinaryAndMode(t *testing.T) {
	for _, tc := range []struct {
		name, before, after   string
		remove, chmod         bool
		count, added, removed int
		binary                bool
	}{
		{name: "restore", before: "same\n", after: "same\n"},
		{name: "delete", before: "one\ntwo\n", remove: true, count: 1, removed: 2},
		{name: "binary", before: "a\x00b", after: "a\x00c", count: 1, binary: true},
		{name: "mode", before: "same\n", after: "same\n", chmod: true, count: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.chmod && runtime.GOOS == "windows" {
				t.Skip("Windows does not preserve POSIX execute mode")
			}
			root := t.TempDir()
			s := New("", root)
			path := filepath.Join(root, "f")
			if err := os.WriteFile(path, []byte(tc.before), 0o644); err != nil {
				t.Fatal(err)
			}
			s.Begin(0, "change", 0)
			turnChangeWrite(t, s, "f", "temporary\n")
			if tc.remove {
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := os.WriteFile(path, []byte(tc.after), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			if tc.chmod {
				if err := os.Chmod(path, 0o755); err != nil {
					t.Fatal(err)
				}
			}
			s.CaptureAfter("f", CaptureAfterOpts{})
			r := s.FreezeTurnChanges(0)
			if r.Coverage != "complete" || len(r.Files) != tc.count || r.Added != tc.added || r.Removed != tc.removed {
				t.Fatalf("result: %+v", r)
			}
			if tc.count > 0 && (r.Files[0].Binary != tc.binary || r.Files[0].ModeOnly != tc.chmod) {
				t.Fatalf("file: %+v", r.Files[0])
			}
		})
	}
}

func TestTurnChangesFailClosed(t *testing.T) {
	for _, cause := range []string{"external", "writer", "gap", "budget", "approximate", "unknown-after"} {
		t.Run(cause, func(t *testing.T) {
			s := New("", t.TempDir())
			s.Begin(0, "change", 0)
			content := "new\n"
			if cause == "budget" {
				content = strings.Repeat("a", TurnChangesBudget+1)
			}
			if cause == "approximate" {
				content = strings.Repeat("line\n", 2100)
			}
			turnChangeWrite(t, s, "f", content)
			switch cause {
			case "external":
				if err := os.WriteFile(filepath.Join(s.root, "f"), []byte("external\n"), 0o644); err != nil {
					t.Fatal(err)
				}
			case "writer":
				if !s.Barrier().TryEnterWrite() {
					t.Fatal("enter write")
				}
				defer s.Barrier().ExitWrite()
			case "gap":
				s.RecordGap(CoverageGap{Reason: GapCaptureFailed})
			case "unknown-after":
				s.cur.Files[0].AfterSHA256 = ""
			}
			r := s.FreezeTurnChanges(0)
			if r.Coverage != "partial" || len(r.Reasons) == 0 {
				t.Fatalf("result: %+v", r)
			}
			if cause != "gap" && (r.Added != 0 || r.Removed != 0) {
				t.Fatalf("unproven tally: %+v", r)
			}
			if cause == "approximate" && (len(r.Files) != 1 || !r.Files[0].Uncounted) {
				t.Fatalf("approximation presented as exact: %+v", r)
			}
		})
	}
}

func TestTurnChangesOldCheckpointAndJSON(t *testing.T) {
	var c Checkpoint
	if err := json.Unmarshal([]byte(`{"turn":0,"files":[]}`), &c); err != nil {
		t.Fatal(err)
	}
	s := New("", t.TempDir())
	s.done = []*Checkpoint{&c}
	old := s.TurnChanges(0)
	if old.Coverage != "unknown" || old.Files == nil || old.Reasons == nil {
		t.Fatalf("old result: %+v", old)
	}
	b, err := json.Marshal(old)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "null") {
		t.Fatalf("null array: %s", b)
	}
}
