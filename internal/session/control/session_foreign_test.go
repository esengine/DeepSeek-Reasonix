package control

import (
	"crypto/sha256"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/event"
	"reasonix/internal/runtime/agent"
	"reasonix/internal/state/sessionstore"
	"reasonix/internal/state/store"
)

type foreignNoticeSink struct {
	mu    sync.Mutex
	codes []string
}

func (s *foreignNoticeSink) Emit(e event.Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e.Kind == event.Notice {
		s.codes = append(s.codes, e.Code)
	}
}

// A 1.x log as 1.38.2–1.38.7 write it: a system prompt and one exchange.
const foreignLog = `{"schema_version":2,"type":"log","at":"2026-09-25T13:00:00Z","generation":1}
{"schema_version":2,"type":"message","id":"m1","head":"main","at":"2026-09-25T13:00:00Z","msgs":[{"role":"system","content":"sys"}]}
{"schema_version":2,"type":"message","id":"m2","parent":"m1","head":"main","at":"2026-09-25T13:00:01Z","msgs":[{"role":"user","content":"hello","origin":"user"}]}
{"schema_version":2,"type":"message","id":"m3","parent":"m2","head":"main","at":"2026-09-25T13:00:02Z","msgs":[{"role":"assistant","content":"hi"}]}
`

func sumFile(t *testing.T, path string) [32]byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return sha256.Sum256(b)
}

// Opening a 1.x conversation saves nothing; the turn that continues it starts
// in a session of its own, and 1.x keeps its log exactly as it was.
func TestForeignSessionContinuesInItsOwnSession(t *testing.T) {
	dir := testenv.TempDir(t)
	path := filepath.Join(dir, "20260925-130000.000000000-deepseek-flash.jsonl")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	logPath := store.SessionEventLog(path)
	if err := os.WriteFile(logPath, []byte(foreignLog), 0o600); err != nil {
		t.Fatal(err)
	}
	logSum := sumFile(t, logPath)
	loaded, err := sessionstore.LoadSession(path)
	if err != nil {
		t.Fatal(err)
	}
	sink := &foreignNoticeSink{}
	exec := agent.New(nil, nil, sessionstore.NewSession("sys"), agent.Options{}, sink)
	c := New(Options{Executor: exec, SessionDir: dir, Label: "deepseek-flash", Sink: sink})
	if err := c.Resume(loaded, path); err != nil {
		t.Fatal(err)
	}

	if err := c.Snapshot(); err != nil {
		t.Fatalf("snapshot of an unchanged 1.x session: %v", err)
	}
	if c.SessionPath() != path {
		t.Fatal("opening a 1.x session moved it")
	}

	if err := c.leaveForeignSession(); err != nil {
		t.Fatal(err)
	}
	moved := c.SessionPath()
	if moved == path || filepath.Dir(moved) != dir {
		t.Fatalf("session path = %s, want a new session beside %s", moved, path)
	}
	copied, err := sessionstore.LoadSession(moved)
	if err != nil {
		t.Fatal(err)
	}
	if msgs := copied.Snapshot(); len(msgs) != 3 || msgs[2].Content != "hi" {
		t.Fatalf("continued session holds %+v", msgs)
	}
	if sessionstore.IsForeignSessionLog(moved) {
		t.Fatal("the continued session is still a 1.x log")
	}
	if sumFile(t, logPath) != logSum {
		t.Fatal("the 1.x log changed")
	}
	if got := strings.Join(sink.codes, ","); !strings.Contains(got, event.NoticeCodeSessionContinuedFrom1x) {
		t.Fatalf("notices = %s, want %s", got, event.NoticeCodeSessionContinuedFrom1x)
	}
}

// Dropping the projection of an open 1.x conversation leaves 1.x's own
// sidecar alone: the log it covers never changes under this build.
func TestForeignSessionKeepsTheContextSidecarWhenProjectionDrops(t *testing.T) {
	dir := testenv.TempDir(t)
	path := filepath.Join(dir, "20260925-130000.000000000-deepseek-flash.jsonl")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store.SessionEventLog(path), []byte(foreignLog), 0o600); err != nil {
		t.Fatal(err)
	}
	sidecar := store.SessionContext(path)
	if err := os.WriteFile(sidecar, []byte(`{"schema_version":4,"projection":{"messages":[{"role":"user","content":"summary"}],"covered_count":2,"covered_prefix_hash":"stale"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	sidecarSum := sumFile(t, sidecar)
	loaded, err := sessionstore.LoadSession(path)
	if err != nil {
		t.Fatal(err)
	}
	exec := agent.New(nil, nil, sessionstore.NewSession("sys"), agent.Options{}, &foreignNoticeSink{})
	c := New(Options{Executor: exec, SessionDir: dir, Label: "deepseek-flash", Sink: &foreignNoticeSink{}})
	if err := c.Resume(loaded, path); err != nil {
		t.Fatal(err)
	}
	exec.InvalidateProjection()
	if sumFile(t, sidecar) != sidecarSum {
		t.Fatal("the 1.x context sidecar changed")
	}
}
