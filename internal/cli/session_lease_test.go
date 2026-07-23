package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/control"
	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/store"
)

// holdSessionLease simulates another runtime owning path for the duration of
// the test (the in-process lease registry plus the OS lock behave exactly as a
// foreign holder for acquisition purposes).
func holdSessionLease(t *testing.T, path string) *agent.SessionLease {
	t.Helper()
	lease, err := agent.TryAcquireSessionLease(path)
	if err != nil {
		t.Fatalf("test holder acquire: %v", err)
	}
	t.Cleanup(lease.Release)
	return lease
}

func TestRunResumeRefusedWhenSessionLeaseHeld(t *testing.T) {
	isolateCLIConfigHome(t)

	path := filepath.Join(t.TempDir(), "held-run.jsonl")
	saveTestSession(t, path, "held prompt")
	holdSessionLease(t, path)

	errOut := captureStderr(t, func() {
		if rc := runAgent([]string{"--resume", path, "continue task"}); rc != 1 {
			t.Fatalf("run --resume held rc = %d, want 1", rc)
		}
	})
	if !strings.Contains(errOut, "in use by another Reasonix") {
		t.Fatalf("run --resume held stderr = %q, want holder wording", errOut)
	}
	if !strings.Contains(errOut, "--copy") {
		t.Fatalf("run --resume held stderr = %q, want --copy guidance", errOut)
	}
	if strings.Contains(errOut, path) {
		t.Fatalf("run --resume held stderr leaks the session path: %q", errOut)
	}
}

func TestRunCopyRequiresResumeTarget(t *testing.T) {
	isolateCLIConfigHome(t)

	errOut := captureStderr(t, func() {
		if rc := runAgent([]string{"--copy", "do things"}); rc != 2 {
			t.Fatalf("run --copy without target rc = %d, want 2", rc)
		}
	})
	if !strings.Contains(errOut, "--copy requires --resume or --continue") {
		t.Fatalf("run --copy stderr = %q, want usage error", errOut)
	}
}

// TestRunResumeCopyJSONKeepsStdoutClean guards that --copy under a structured
// output format writes its human notice to stderr, leaving stdout a single valid
// JSON object (the copy notice used to pollute it).
func TestRunResumeCopyJSONKeepsStdoutClean(t *testing.T) {
	isolateCLIConfigHome(t)

	dir := t.TempDir()
	src := filepath.Join(dir, "held-src.jsonl")
	saveTestSession(t, src, "copy me")
	holdSessionLease(t, src)

	var rc int
	var errOut string
	out := captureStdout(t, func() {
		errOut = captureStderr(t, func() {
			rc = runAgent([]string{"--resume", src, "--copy", "--output-format", "json", "continue task"})
		})
	})
	// Setup fails in the isolated home (no provider), so the run ends with a JSON
	// error object — but the copy still happened first.
	if rc != 1 {
		t.Fatalf("rc = %d, want 1 (setup fails in isolated home)", rc)
	}
	if strings.Contains(out, "continuing in a session copy") {
		t.Fatalf("json stdout leaked the copy notice:\n%s", out)
	}
	if !strings.Contains(errOut, "continuing in a session copy: ") {
		t.Fatalf("copy notice should be on stderr, got stderr:\n%s", errOut)
	}
	var obj map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &obj); err != nil {
		t.Fatalf("stdout is not a single JSON object: %v\nstdout:\n%s", err, out)
	}
}

func TestRunResumeCopyContinuesInDuplicate(t *testing.T) {
	isolateCLIConfigHome(t)

	dir := t.TempDir()
	src := filepath.Join(dir, "held-src.jsonl")
	saveTestSession(t, src, "copy me")
	srcBytes, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	holdSessionLease(t, src)

	var rc int
	out := captureStdout(t, func() {
		_ = captureStderr(t, func() {
			rc = runAgent([]string{"--resume", src, "--copy", "continue task"})
		})
	})
	// The isolated home has no provider config, so the run itself fails after
	// the copy — but never with the lease refusal, and never touching src.
	if rc != 1 {
		t.Fatalf("run --resume --copy rc = %d, want 1 (setup fails in isolated home)", rc)
	}
	if !strings.Contains(out, "continuing in a session copy: ") {
		t.Fatalf("stdout = %q, want session copy line", out)
	}
	copyPath := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(out), "continuing in a session copy:"))
	if copyPath == "" || copyPath == src {
		t.Fatalf("copy path = %q, want a fresh path", copyPath)
	}

	srcAfter, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	if string(srcAfter) != string(srcBytes) {
		t.Fatalf("source transcript was modified by --copy")
	}
	srcLoaded, err := agent.LoadSession(src)
	if err != nil {
		t.Fatal(err)
	}
	copyLoaded, err := agent.LoadSession(copyPath)
	if err != nil {
		t.Fatalf("load copy: %v", err)
	}
	srcMsgs, copyMsgs := srcLoaded.Snapshot(), copyLoaded.Snapshot()
	if len(copyMsgs) != len(srcMsgs) {
		t.Fatalf("copy has %d messages, source %d", len(copyMsgs), len(srcMsgs))
	}
	for i := range srcMsgs {
		if copyMsgs[i].Role != srcMsgs[i].Role || copyMsgs[i].Content != srcMsgs[i].Content {
			t.Fatalf("copy message %d = %+v, want %+v", i, copyMsgs[i], srcMsgs[i])
		}
	}
	// The run exited: the copy's lease must be released again.
	if _, err := os.Stat(store.SessionLeaseInfo(agent.CanonicalSessionPath(copyPath))); !os.IsNotExist(err) {
		t.Fatalf("copy lease info after exit stat err = %v, want not exist", err)
	}
	lease, err := agent.TryAcquireSessionLease(copyPath)
	if err != nil {
		t.Fatalf("copy lease not released after run: %v", err)
	}
	lease.Release()
}

func TestRunResumeReleasesLeaseOnExit(t *testing.T) {
	isolateCLIConfigHome(t)

	path := filepath.Join(t.TempDir(), "release-run.jsonl")
	saveTestSession(t, path, "resume me")

	// No provider config in the isolated home: the run fails after the lease
	// was taken, and the deferred release must still run.
	_ = captureStderr(t, func() {
		if rc := runAgent([]string{"--resume", path, "continue task"}); rc != 1 {
			t.Fatalf("run --resume rc = %d, want 1 (setup fails in isolated home)", rc)
		}
	})
	if _, err := os.Stat(store.SessionLeaseInfo(agent.CanonicalSessionPath(path))); !os.IsNotExist(err) {
		t.Fatalf("lease info after run exit stat err = %v, want not exist", err)
	}
	lease, err := agent.TryAcquireSessionLease(path)
	if err != nil {
		t.Fatalf("lease not released after run exit: %v", err)
	}
	lease.Release()
}

func TestCopySessionForWritingDuplicatesTranscript(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.jsonl")
	s := agent.NewSession("sys")
	s.Add(provider.Message{Role: provider.RoleUser, Content: "question"})
	s.Add(provider.Message{Role: provider.RoleAssistant, Content: "answer"})
	if err := s.Save(src); err != nil {
		t.Fatal(err)
	}
	if err := agent.SaveBranchMeta(src, agent.BranchMeta{
		CustomTitle:   "My debugging session",
		Model:         "deepseek/deepseek-chat",
		SchemaVersion: agent.BranchMetaCountsVersion,
	}); err != nil {
		t.Fatal(err)
	}
	srcBytes, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}

	copyPath, err := copySessionForWriting(src)
	if err != nil {
		t.Fatalf("copySessionForWriting: %v", err)
	}
	if filepath.Dir(copyPath) != dir {
		t.Fatalf("copy landed in %q, want beside the source in %q", filepath.Dir(copyPath), dir)
	}

	loaded, err := agent.LoadSession(copyPath)
	if err != nil {
		t.Fatalf("load copy: %v", err)
	}
	got := loaded.Snapshot()
	want := s.Snapshot()
	if len(got) != len(want) {
		t.Fatalf("copy has %d messages, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i].Role != want[i].Role || got[i].Content != want[i].Content {
			t.Fatalf("copy message %d = %+v, want %+v", i, got[i], want[i])
		}
	}

	meta, ok, err := agent.LoadBranchMeta(copyPath)
	if err != nil || !ok {
		t.Fatalf("copy branch meta: ok=%v err=%v", ok, err)
	}
	if meta.ParentID != agent.BranchID(src) {
		t.Fatalf("copy ParentID = %q, want %q", meta.ParentID, agent.BranchID(src))
	}
	if meta.CustomTitle != "My debugging session (copy)" {
		t.Fatalf("copy CustomTitle = %q", meta.CustomTitle)
	}
	if meta.Model != "deepseek/deepseek-chat" {
		t.Fatalf("copy Model = %q", meta.Model)
	}

	// The copy starts unowned and without lease/lock sidecars of its own.
	for _, sidecar := range []string{
		store.SessionLeaseInfo(agent.CanonicalSessionPath(copyPath)),
		store.SessionLeaseLock(agent.CanonicalSessionPath(copyPath)),
	} {
		if _, err := os.Stat(sidecar); !os.IsNotExist(err) {
			t.Fatalf("copy has lease sidecar %s (err=%v)", sidecar, err)
		}
	}
	// The source transcript is only read.
	srcAfter, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	if string(srcAfter) != string(srcBytes) {
		t.Fatalf("source transcript was modified by the copy")
	}
}

// chatLeaseFixture builds a TUI over a temp session dir with two saved
// sessions: older (the active one) and newer (the /resume 1 target).
func chatLeaseFixture(t *testing.T) (m chatTUI, active, target string) {
	t.Helper()
	dir := t.TempDir()
	active = filepath.Join(dir, "a-active.jsonl")
	target = filepath.Join(dir, "b-target.jsonl")
	saveTestSession(t, active, "active session")
	saveTestSession(t, target, "target session")
	pinNewer(t, active, target)

	exec := agent.New(nil, nil, agent.NewSession("sys"), agent.Options{}, event.Discard)
	m = newTestChatTUI()
	m.width = 80
	m.ctrl = control.New(control.Options{Executor: exec, SessionDir: dir, SessionPath: active, Label: "test"})
	m.leases = control.NewSessionLeaseKeeper()
	t.Cleanup(m.leases.Release)
	if err := m.leases.Rebind(active); err != nil {
		t.Fatalf("seed lease on active: %v", err)
	}
	return m, active, target
}

// pinNewer gives newer a strictly later mtime than older so ListSessions
// ordering is deterministic across filesystems with coarse mtimes.
func pinNewer(t *testing.T, older, newer string) {
	t.Helper()
	info, err := os.Stat(newer)
	if err != nil {
		t.Fatal(err)
	}
	old := info.ModTime().Add(-2 * time.Second)
	if err := os.Chtimes(older, old, old); err != nil {
		t.Fatal(err)
	}
}

func TestChatResumeCommandRefusedWhenLeaseHeld(t *testing.T) {
	m, active, target := chatLeaseFixture(t)
	holdSessionLease(t, target)

	m.runResumeCommand("/resume 1")

	out := strings.Join(m.transcript, "\n")
	if !strings.Contains(out, "in use by another Reasonix") {
		t.Fatalf("refusal notice missing from transcript:\n%s", out)
	}
	if got := m.ctrl.SessionPath(); got != active {
		t.Fatalf("session path after refused /resume = %q, want %q", got, active)
	}
	if got, want := m.leases.HeldPath(), agent.CanonicalSessionPath(active); got != want {
		t.Fatalf("lease after refused /resume = %q, want %q", got, want)
	}
}

func TestChatResumeCommandMovesLease(t *testing.T) {
	m, active, target := chatLeaseFixture(t)

	m.runResumeCommand("/resume 1")

	if got := m.ctrl.SessionPath(); got != target {
		t.Fatalf("session path after /resume = %q, want %q", got, target)
	}
	if got, want := m.leases.HeldPath(), agent.CanonicalSessionPath(target); got != want {
		t.Fatalf("lease after /resume = %q, want %q", got, want)
	}
	// The lease on the session we left must be free again.
	lease, err := agent.TryAcquireSessionLease(active)
	if err != nil {
		t.Fatalf("old session lease not released by /resume: %v", err)
	}
	lease.Release()
}

func TestChatNewSessionTakesFreshLease(t *testing.T) {
	m, active, _ := chatLeaseFixture(t)

	if cmd := m.runSlashCommand("/new"); cmd != nil {
		t.Fatal("/new should not return a tea.Cmd")
	}

	fresh := m.ctrl.SessionPath()
	if fresh == active {
		t.Fatalf("/new did not rotate the session path")
	}
	if got, want := m.leases.HeldPath(), agent.CanonicalSessionPath(fresh); got != want {
		t.Fatalf("lease after /new = %q, want %q", got, want)
	}
	lease, err := agent.TryAcquireSessionLease(active)
	if err != nil {
		t.Fatalf("old session lease not released by /new: %v", err)
	}
	lease.Release()
}
