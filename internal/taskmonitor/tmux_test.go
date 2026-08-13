package taskmonitor

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

type tmuxMock struct {
	calls   [][]string
	err     error
	options map[string]string
}

func (m *tmuxMock) Run(_ context.Context, args ...string) ([]byte, error) {
	m.calls = append(m.calls, append([]string(nil), args...))
	if m.err != nil {
		return nil, m.err
	}
	if len(args) == 5 && args[0] == "set-option" && args[1] == "-t" {
		if m.options == nil {
			m.options = make(map[string]string)
		}
		m.options[tmuxMockSession(args[2])+"\x00"+args[3]] = args[4]
		return nil, nil
	}
	if len(args) == 5 && args[0] == "show-options" && args[1] == "-v" && args[2] == "-t" {
		value, ok := m.options[tmuxMockSession(args[3])+"\x00"+args[4]]
		if !ok {
			return nil, os.ErrNotExist
		}
		return []byte(value + "\n"), nil
	}
	if len(args) == 7 && args[0] == "if-shell" && args[1] == "-t" && args[3] == "-F" {
		session := tmuxMockSession(args[2])
		if m.options[session+"\x00"+tmuxOwnerOption] != "" &&
			strings.Contains(args[4], m.options[session+"\x00"+tmuxOwnerOption]) {
			delete(m.options, session+"\x00"+tmuxOwnerOption)
		}
		return nil, nil
	}
	return nil, nil
}

func tmuxMockSession(target string) string {
	return strings.TrimSuffix(strings.TrimPrefix(target, "="), ":")
}

func seedTmuxTask(t *testing.T, s *InMemoryStore, projectDir string) {
	seedTmuxTaskID(t, s, projectDir, "t1")
}

func seedTmuxTaskID(t *testing.T, s *InMemoryStore, projectDir, taskID string) {
	t.Helper()
	if err := s.UpsertTask(projectDir, TaskSnapshot{SchemaVersion: 1, TaskID: taskID, SessionID: "s1", State: TaskStateRunning, Version: 1, CreatedAt: time.Now(), UpdatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
}

func TestTmuxAdapterAttachIdempotent(t *testing.T) {
	s := NewInMemoryStore()
	root := t.TempDir()
	seedTmuxTask(t, s, root)
	mock := &tmuxMock{}
	a := NewTmuxAdapterWithRunner(s, ".reasonix/tasks", mock)
	first := a.Attach(context.Background(), root, "t1", "demo")
	if first.Error != nil || first.Mapping == nil {
		t.Fatalf("attach failed: %+v", first)
	}
	second := a.Attach(context.Background(), root, "t1", "demo")
	if !second.Idempotent {
		t.Fatalf("expected idempotent attach: %+v", second)
	}
	if len(mock.calls) != 3 { // new-session, set ownership marker, then verify it
		t.Fatalf("unexpected tmux calls: %#v", mock.calls)
	}
}

func TestTmuxAdapterBoundsGeneratedSessionName(t *testing.T) {
	s := NewInMemoryStore()
	root := t.TempDir()
	taskID := strings.Repeat("a", 60)
	seedTmuxTaskID(t, s, root, taskID)
	a := NewTmuxAdapterWithRunner(s, ".reasonix/tasks", &tmuxMock{})
	r := a.Attach(context.Background(), root, taskID, "")
	if r.Error != nil || r.Mapping == nil {
		t.Fatalf("attach failed: %+v", r)
	}
	if got := r.Mapping.Session; len(got) > 64 || !strings.HasPrefix(got, defaultTmuxNamePrefix) {
		t.Fatalf("generated session = %q (len %d)", got, len(got))
	}
	if r.Mapping.Session != defaultTmuxSessionName(taskID) || r.Mapping.Session == defaultTmuxSessionName(taskID+"b") {
		t.Fatalf("generated session is not deterministic and collision-resistant: %q", r.Mapping.Session)
	}
}

func TestTmuxAdapterUnavailableDoesNotChangeTask(t *testing.T) {
	s := NewInMemoryStore()
	root := t.TempDir()
	seedTmuxTask(t, s, root)
	a := NewTmuxAdapterWithRunner(s, ".reasonix/tasks", nil)
	r := a.Attach(context.Background(), root, "t1", "")
	if r.Error == nil || r.Error.Code != ErrTmuxUnavailable {
		t.Fatalf("expected unavailable, got %+v", r)
	}
	snap, _ := s.GetTask(context.Background(), root, "t1")
	if snap == nil || snap.State != TaskStateRunning {
		t.Fatal("tmux operation changed task state")
	}
}

func TestTmuxAdapterRejectsUnsafeNames(t *testing.T) {
	s := NewInMemoryStore()
	seedTmuxTask(t, s, "/p")
	a := NewTmuxAdapterWithRunner(s, ".reasonix/tasks", &tmuxMock{})
	for _, name := range []string{"bad/name", "semi;colon", "dollar$name", "has space"} {
		r := a.Attach(context.Background(), "/p", "t1", name)
		if r.Error == nil || r.Error.Code != ErrTmuxInvalidName {
			t.Fatalf("expected invalid name for %q, got %+v", name, r)
		}
	}
}

func TestTmuxAdapterStaleAndDetach(t *testing.T) {
	s := NewInMemoryStore()
	root := t.TempDir()
	seedTmuxTask(t, s, root)
	mock := &tmuxMock{}
	a := NewTmuxAdapterWithRunner(s, ".reasonix/tasks", mock)
	if r := a.Attach(context.Background(), root, "t1", "demo"); r.Error != nil {
		t.Fatal(r.Error)
	}
	mock.err = os.ErrNotExist
	r := a.Status(context.Background(), root, "t1")
	if r.Mapping == nil || !r.Mapping.Stale {
		t.Fatalf("expected stale mapping: %+v", r)
	}
	mock.err = nil
	if r := a.Detach(context.Background(), root, "t1"); r.Error != nil {
		t.Fatal(r.Error)
	}
	if _, err := os.Stat(filepath.Join(root, ".reasonix/tasks/.tmux/t1.json")); !os.IsNotExist(err) {
		t.Fatalf("mapping was not removed: %v", err)
	}
}

func TestTmuxAdapterRejectsTaskPathTraversal(t *testing.T) {
	a := NewTmuxAdapterWithRunner(NewInMemoryStore(), ".reasonix/tasks", &tmuxMock{})
	r := a.Status(context.Background(), t.TempDir(), "../secret")
	if r.Error == nil {
		t.Fatal("expected invalid task id error")
	}
}

func TestTmuxAdapterAcceptsCleanableProjectDir(t *testing.T) {
	parent := t.TempDir()
	for _, projectDir := range []string{
		filepath.Join(parent, "nested", "..", "project"),
		filepath.Join(parent, "project..archive"),
	} {
		if err := os.MkdirAll(filepath.Clean(projectDir), 0o755); err != nil {
			t.Fatal(err)
		}
		s := NewInMemoryStore()
		seedTmuxTask(t, s, projectDir)
		a := NewTmuxAdapterWithRunner(s, ".reasonix/tasks", &tmuxMock{})
		if result := a.Attach(context.Background(), projectDir, "t1", "demo"); result.Error != nil {
			t.Fatalf("Attach(%q): %+v", projectDir, result.Error)
		}
	}
}

func TestTmuxAdapterRejectsSymlinkMappingDirectory(t *testing.T) {
	project := t.TempDir()
	outside := t.TempDir()
	root := filepath.Join(project, ".reasonix", "tasks")
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, ".tmux")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}

	s := NewInMemoryStore()
	seedTmuxTask(t, s, project)
	attachRunner := &tmuxMock{}
	a := NewTmuxAdapterWithRunner(s, ".reasonix/tasks", attachRunner)
	if result := a.Attach(context.Background(), project, "t1", "demo"); result.Error == nil || result.Error.Code != ErrTmuxMappingFailed {
		t.Fatalf("expected mapping write rejection, got %+v", result)
	}
	outsideMapping := filepath.Join(outside, "t1.json")
	if _, err := os.Stat(outsideMapping); !os.IsNotExist(err) {
		t.Fatalf("mapping escaped through symlink: %v", err)
	}

	mapping := `{"schema_version":1,"task_id":"t1","session":"victim","window":"task","pane":"victim:task.0"}`
	if err := os.WriteFile(outsideMapping, []byte(mapping), 0o600); err != nil {
		t.Fatal(err)
	}
	readRunner := &tmuxMock{}
	a = NewTmuxAdapterWithRunner(s, ".reasonix/tasks", readRunner)
	for _, result := range []TmuxResult{
		a.Status(context.Background(), project, "t1"),
		a.Detach(context.Background(), project, "t1"),
	} {
		if result.Error == nil || result.Error.Code != ErrTmuxMappingFailed {
			t.Fatalf("expected mapping read rejection, got %+v", result)
		}
	}
	if len(readRunner.calls) != 0 {
		t.Fatalf("untrusted mapping triggered tmux calls: %#v", readRunner.calls)
	}
	if _, err := os.Stat(outsideMapping); err != nil {
		t.Fatalf("outside mapping was removed: %v", err)
	}
}

func TestTmuxAdapterRejectsForgedMappingIdentityWithoutTmuxCalls(t *testing.T) {
	project := t.TempDir()
	s := NewInMemoryStore()
	seedTmuxTask(t, s, project)
	mock := &tmuxMock{}
	a := NewTmuxAdapterWithRunner(s, ".reasonix/tasks", mock)
	attached := a.Attach(context.Background(), project, "t1", "demo")
	if attached.Error != nil || attached.Mapping == nil {
		t.Fatalf("attach failed: %+v", attached)
	}

	path := filepath.Join(project, ".reasonix", "tasks", ".tmux", "t1.json")
	forged := *attached.Mapping
	forged.TaskID = "different-task"
	forged.ProjectDir = filepath.Join(project, "different-project")
	forged.Session = "user-owned-session"
	forged.Pane = "user-owned-session:task.0"
	b, err := json.Marshal(forged)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatal(err)
	}

	mock.calls = nil
	if result := a.Status(context.Background(), project, "t1"); result.Error == nil || result.Error.Code != ErrTmuxMappingFailed {
		t.Fatalf("forged status mapping was accepted: %+v", result)
	}
	if len(mock.calls) != 0 {
		t.Fatalf("forged status mapping triggered tmux calls: %#v", mock.calls)
	}
	if result := a.Detach(context.Background(), project, "t1"); result.Error == nil || result.Error.Code != ErrTmuxMappingFailed {
		t.Fatalf("forged detach mapping was accepted: %+v", result)
	}
	if len(mock.calls) != 0 {
		t.Fatalf("forged detach mapping triggered tmux calls: %#v", mock.calls)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("forged mapping was not removed safely: %v", err)
	}
}

func TestTmuxAdapterDoesNotKillSessionWithWrongOwnerToken(t *testing.T) {
	project := t.TempDir()
	s := NewInMemoryStore()
	seedTmuxTask(t, s, project)
	mock := &tmuxMock{}
	a := NewTmuxAdapterWithRunner(s, ".reasonix/tasks", mock)
	attached := a.Attach(context.Background(), project, "t1", "demo")
	if attached.Error != nil || attached.Mapping == nil {
		t.Fatalf("attach failed: %+v", attached)
	}
	mock.options["demo\x00"+tmuxOwnerOption] = strings.Repeat("0", tmuxOwnerTokenBytes*2)
	mock.calls = nil

	result := a.Detach(context.Background(), project, "t1")
	if result.Error != nil || result.Mapping == nil || !result.Mapping.Stale {
		t.Fatalf("wrong-owner detach result: %+v", result)
	}
	for _, call := range mock.calls {
		if len(call) > 0 && (call[0] == "kill-session" || call[0] == "if-shell") {
			t.Fatalf("wrong owner token killed tmux session: %#v", mock.calls)
		}
	}
}

func TestTmuxAdapterDetachUsesAtomicOwnedKill(t *testing.T) {
	project := t.TempDir()
	s := NewInMemoryStore()
	seedTmuxTask(t, s, project)
	mock := &tmuxMock{}
	a := NewTmuxAdapterWithRunner(s, ".reasonix/tasks", mock)
	attached := a.Attach(context.Background(), project, "t1", "demo")
	if attached.Error != nil || attached.Mapping == nil {
		t.Fatalf("attach failed: %+v", attached)
	}
	mock.calls = nil

	result := a.Detach(context.Background(), project, "t1")
	if result.Error != nil {
		t.Fatalf("detach failed: %+v", result)
	}
	foundAtomicKill := false
	for _, call := range mock.calls {
		if len(call) > 0 && call[0] == "kill-session" {
			t.Fatalf("detach used a standalone kill-session: %#v", mock.calls)
		}
		if len(call) > 0 && call[0] == "if-shell" {
			foundAtomicKill = true
		}
	}
	if !foundAtomicKill {
		t.Fatalf("detach did not issue an ownership-checked atomic kill: %#v", mock.calls)
	}
}

func TestTmuxAdapterReplacesLegacyMappingWithoutTrustingIt(t *testing.T) {
	project := t.TempDir()
	s := NewInMemoryStore()
	seedTmuxTask(t, s, project)
	path := filepath.Join(project, ".reasonix", "tasks", ".tmux", "t1.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	legacy, err := json.Marshal(TmuxMapping{SchemaVersion: 1, TaskID: "t1", ProjectDir: project, Session: "legacy", Window: "task", Pane: "legacy:task.0"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, legacy, 0o600); err != nil {
		t.Fatal(err)
	}
	mock := &tmuxMock{}
	a := NewTmuxAdapterWithRunner(s, ".reasonix/tasks", mock)

	result := a.Attach(context.Background(), project, "t1", "fresh")
	if result.Error != nil || result.Mapping == nil || result.Mapping.OwnerToken == "" {
		t.Fatalf("legacy mapping replacement failed: %+v", result)
	}
	for _, call := range mock.calls {
		if len(call) > 2 && call[0] == "show-options" && call[3] == "legacy" {
			t.Fatalf("legacy mapping was trusted: %#v", mock.calls)
		}
	}
}

func TestTmuxAdapterWritesPrivateMappingFiles(t *testing.T) {
	project := t.TempDir()
	s := NewInMemoryStore()
	seedTmuxTask(t, s, project)
	a := NewTmuxAdapterWithRunner(s, ".reasonix/tasks", &tmuxMock{})
	if result := a.Attach(context.Background(), project, "t1", "demo"); result.Error != nil {
		t.Fatal(result.Error)
	}
	if runtime.GOOS == "windows" {
		return // Windows does not expose POSIX permission bits through os.FileMode.
	}

	for path, want := range map[string]os.FileMode{
		filepath.Join(project, ".reasonix", "tasks"):                     0o700,
		filepath.Join(project, ".reasonix", "tasks", ".tmux"):            0o700,
		filepath.Join(project, ".reasonix", "tasks", ".tmux", "t1.json"): 0o600,
	} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if got := info.Mode().Perm(); got != want {
			t.Errorf("%s mode = %o, want %o", path, got, want)
		}
	}
}
