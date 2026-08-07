package control

import (
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/agent"
	"reasonix/internal/event"
	"reasonix/internal/sessiontemp"
)

func TestSessionTempSurvivesHotRebuildStyleRetain(t *testing.T) {
	m := sessiontemp.New()
	old := New(Options{SessionTemp: m, Sink: event.Discard})
	lease, err := old.SessionTemp().Acquire()
	if err != nil {
		t.Fatal(err)
	}
	dir := lease.Dir()
	if err := os.WriteFile(filepath.Join(dir, "keep.txt"), []byte("alive"), 0o600); err != nil {
		t.Fatal(err)
	}
	lease.Release()

	// Hot rebuild: replacement retains the same Manager before old closes.
	replacement := New(Options{SessionTemp: old.SessionTemp(), Sink: event.Discard})
	old.ReleaseResources()

	again, err := replacement.SessionTemp().Acquire()
	if err != nil {
		t.Fatal(err)
	}
	defer again.Release()
	if again.Dir() != dir {
		t.Fatalf("hot rebuild changed generation: got %q want %q", again.Dir(), dir)
	}
	body, err := os.ReadFile(filepath.Join(dir, "keep.txt"))
	if err != nil || string(body) != "alive" {
		t.Fatalf("temp file lost across rebuild: %q %v", body, err)
	}
	replacement.ReleaseResources()
}

func TestNewSessionRotatesSessionTemp(t *testing.T) {
	dir := t.TempDir()
	exec := agent.New(nil, nil, agent.NewSession("sys"), agent.Options{}, event.Discard)
	c := New(Options{
		Executor:   exec,
		SessionDir: dir,
		Label:      "test",
		Sink:       event.Discard,
	})
	defer c.Close()

	lease, err := c.SessionTemp().Acquire()
	if err != nil {
		t.Fatal(err)
	}
	oldDir := lease.Dir()
	if err := os.WriteFile(filepath.Join(oldDir, "old.txt"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	lease.Release()

	if err := c.NewSession(); err != nil {
		t.Fatal(err)
	}
	fresh, err := c.SessionTemp().Acquire()
	if err != nil {
		t.Fatal(err)
	}
	defer fresh.Release()
	if fresh.Dir() == oldDir {
		t.Fatal("NewSession should rotate the private temporary directory")
	}
	if _, err := os.Stat(filepath.Join(fresh.Dir(), "old.txt")); !os.IsNotExist(err) {
		t.Fatalf("new session must not see old temp files: %v", err)
	}
}

func TestSetSessionPathDoesNotRotateSessionTemp(t *testing.T) {
	dir := t.TempDir()
	exec := agent.New(nil, nil, agent.NewSession("sys"), agent.Options{}, event.Discard)
	c := New(Options{
		Executor:   exec,
		SessionDir: dir,
		Label:      "test",
		Sink:       event.Discard,
	})
	defer c.Close()

	lease, err := c.SessionTemp().Acquire()
	if err != nil {
		t.Fatal(err)
	}
	oldDir := lease.Dir()
	lease.Release()

	c.SetSessionPath(filepath.Join(dir, "rebind.jsonl"))
	again, err := c.SessionTemp().Acquire()
	if err != nil {
		t.Fatal(err)
	}
	defer again.Release()
	if again.Dir() != oldDir {
		t.Fatalf("SetSessionPath must not rotate temp: got %q want %q", again.Dir(), oldDir)
	}
}

func TestResumeOtherSessionRotatesSessionTemp(t *testing.T) {
	dir := t.TempDir()
	exec := agent.New(nil, nil, agent.NewSession("sys"), agent.Options{}, event.Discard)
	pathA := agent.NewSessionPath(dir, "a")
	pathB := agent.NewSessionPath(dir, "b")
	c := New(Options{
		Executor:    exec,
		SessionDir:  dir,
		SessionPath: pathA,
		Label:       "test",
		Sink:        event.Discard,
	})
	defer c.Close()

	lease, err := c.SessionTemp().Acquire()
	if err != nil {
		t.Fatal(err)
	}
	oldDir := lease.Dir()
	lease.Release()

	// Same-path resume (rebuild style) keeps generation.
	c.Resume(agent.NewSession("sys"), pathA)
	same, err := c.SessionTemp().Acquire()
	if err != nil {
		t.Fatal(err)
	}
	if same.Dir() != oldDir {
		t.Fatalf("same-path Resume rotated temp: got %q want %q", same.Dir(), oldDir)
	}
	same.Release()

	// Different path (user /resume) rotates.
	c.Resume(agent.NewSession("sys"), pathB)
	other, err := c.SessionTemp().Acquire()
	if err != nil {
		t.Fatal(err)
	}
	defer other.Release()
	if other.Dir() == oldDir {
		t.Fatal("resume of another session should rotate private temp")
	}
}

func TestShouldRotateSessionTempOnResume(t *testing.T) {
	cases := []struct {
		prev, next string
		want       bool
	}{
		{"", "/a", false},
		{"/a", "", false},
		{"/a", "/a", false},
		{"/a", "/b", true},
		{"/a/../b", "/b", false},
	}
	for _, c := range cases {
		if got := shouldRotateSessionTempOnResume(c.prev, c.next); got != c.want {
			t.Errorf("shouldRotate(%q, %q) = %v, want %v", c.prev, c.next, got, c.want)
		}
	}
}

func TestFailedReplacementDoesNotDropOldSessionTemp(t *testing.T) {
	m := sessiontemp.New()
	old := New(Options{SessionTemp: m, Sink: event.Discard})
	lease, err := old.SessionTemp().Acquire()
	if err != nil {
		t.Fatal(err)
	}
	dir := lease.Dir()
	lease.Release()

	// Simulate a failed rebuild: retain then release the replacement only.
	m.Retain()
	m.Release()
	// Old controller still owns the Manager.
	again, err := old.SessionTemp().Acquire()
	if err != nil {
		t.Fatal(err)
	}
	defer again.Release()
	if again.Dir() != dir {
		t.Fatalf("failed rebuild affected old session temp: got %q want %q", again.Dir(), dir)
	}
	old.Close()
}
