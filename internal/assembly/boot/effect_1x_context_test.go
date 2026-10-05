package boot

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/state/sessionstore"
	"reasonix/internal/state/store"
)

// testdata/session1x is a schema 2 session a real 1.38.7 build wrote and then
// compacted by hand, cut at a turn boundary, with its workspace path, host name
// and filler shortened; covered_prefix_hash was recomputed by 1.x's own code.
const session1xName = "20260929-043954.562568000-mock-model"

const (
	session1xSummary = "SUMMARY: the user asked mock questions"
	session1xFolded  = "LONG lorem ipsum"
	session1xTail    = "dag187 after compact BASHCALL"
)

func copySession1x(t *testing.T, src, dir string, edit func(name string, b []byte) []byte) (string, map[string][]byte) {
	t.Helper()
	entries, err := os.ReadDir(src)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	files := map[string][]byte{}
	for _, e := range entries {
		b, err := os.ReadFile(filepath.Join(src, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if edit != nil {
			b = edit(e.Name(), b)
		}
		dst := filepath.Join(dir, e.Name())
		if err := os.WriteFile(dst, b, 0o600); err != nil {
			t.Fatal(err)
		}
		files[dst] = b
	}
	return filepath.Join(dir, session1xName+".jsonl"), files
}

func assert1xFilesUntouched(t *testing.T, when string, files map[string][]byte) {
	t.Helper()
	for path, want := range files {
		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("%s: 1.x file %s: %v", when, filepath.Base(path), err)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("%s: 1.x file %s changed", when, filepath.Base(path))
		}
	}
}

// resume1xAndRun opens the copied 1.x session through the real Build stack,
// checks nothing 1.x wrote moved, runs one turn and returns what reached the
// provider on it.
func resume1xAndRun(t *testing.T, kind string, edit func(name string, b []byte) []byte) (string, string) {
	t.Helper()
	src, err := filepath.Abs(filepath.Join("testdata", "session1x"))
	if err != nil {
		t.Fatal(err)
	}
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)
	rec := &effectRecordingProvider{}
	provider.Register(kind, func(provider.Config) (provider.Provider, error) {
		return rec, nil
	})
	writeFile(t, dir, "reasonix.toml", `
default_model = "test-model"

[agent]
system_prompt = "BASE"

[[providers]]
name = "test-model"
kind = "`+kind+`"
model = "x"
`)
	approveWorkspace(t, dir)
	path, files := copySession1x(t, src, filepath.Join(dir, "sessions"), edit)

	ctrl, err := Build(context.Background(), Options{Sink: event.Discard})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer ctrl.Close()
	loaded, err := sessionstore.LoadSession(path)
	if err != nil {
		t.Fatalf("load 1.x session: %v", err)
	}
	if err := ctrl.Resume(loaded, path); err != nil {
		t.Fatalf("Resume: %v", err)
	}
	assert1xFilesUntouched(t, "after open", files)
	if err := ctrl.RunTurn(context.Background(), "carry on"); err != nil {
		t.Fatalf("RunTurn: %v", err)
	}
	assert1xFilesUntouched(t, "after a turn", files)
	if ctrl.SessionPath() == path {
		t.Fatal("the turn was written into the 1.x session")
	}

	reqs := agentRequests(rec.requests())
	if len(reqs) == 0 {
		t.Fatal("no agent request reached the provider boundary")
	}
	var sent strings.Builder
	for _, m := range reqs[0].Messages {
		sent.WriteString(m.Content)
		sent.WriteByte('\n')
	}
	return sent.String(), ctrl.SessionPath()
}

func TestEffect1xCompactionSummaryReachesProvider(t *testing.T) {
	sent, continued := resume1xAndRun(t, "effect-1x-context", nil)
	if !strings.Contains(sent, session1xSummary) {
		t.Fatal("1.x's compaction summary did not reach the provider")
	}
	if strings.Contains(sent, session1xFolded) {
		t.Fatal("history 1.x folded into its summary was sent in full")
	}
	if !strings.Contains(sent, session1xTail) {
		t.Fatal("the turns after 1.x's fold were not sent")
	}
	if _, err := os.Stat(store.SessionContext(continued)); err != nil {
		t.Fatalf("continued session has no context projection of its own: %v", err)
	}
}

func TestEffect1xUnreadableContextSidecarIsLeftInPlace(t *testing.T) {
	future := func(name string, b []byte) []byte {
		if !strings.HasSuffix(name, ".context.json") {
			return b
		}
		return bytes.Replace(b, []byte(`"schema_version": 4`), []byte(`"schema_version": 99`), 1)
	}
	sent, _ := resume1xAndRun(t, "effect-1x-context-future", future)
	if strings.Contains(sent, session1xSummary) {
		t.Fatal("a sidecar of unknown schema was used")
	}
	if !strings.Contains(sent, session1xFolded) {
		t.Fatal("without a usable projection the canonical history must be sent")
	}
}
