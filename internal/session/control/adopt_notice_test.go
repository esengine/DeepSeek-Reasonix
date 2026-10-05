package control

import (
	"reasonix/internal/state/sessionstore"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/runtime/agent"
)

func coldNoticeController(t *testing.T, sink event.Sink) (*Controller, string) {
	t.Helper()
	dir := testenv.TempDir(t)
	saved := sessionstore.NewSession("sys")
	saved.Add(provider.Message{Role: provider.RoleUser, Content: "task"})
	saved.Add(provider.Message{Role: provider.RoleAssistant, Content: "ok"})
	path := sessionstore.NewSessionPath(dir, "test")
	if err := saved.Save(path); err != nil {
		t.Fatalf("save: %v", err)
	}
	if _, err := sessionstore.EnsureBranchMeta(path); err != nil {
		t.Fatalf("meta: %v", err)
	}
	exec := agent.New(nil, nil, sessionstore.NewSession("sys"), agent.Options{ContextWindow: 1000, RecentKeep: 2, ArchiveDir: dir}, event.Discard)
	c := New(Options{Executor: exec, SessionDir: dir, Label: "test", Sink: sink})
	c.testCacheColdAfter = -1 // force cold
	return c, path
}

func coldNotices(events []event.Event) int {
	n := 0
	for _, e := range events {
		if strings.Contains(e.Text, "provider cache likely expired") {
			n++
		}
	}
	return n
}

// A model or effort switch rebuilds the runtime and re-binds the same session
// through AdoptHistory. That is not a resume: reporting "resumed after 360h
// idle" there blames an idle gap the user never sat through, and it repeats on
// every switch until a turn finally updates the sidecar.
func TestAdoptHistoryDoesNotAnnounceColdResume(t *testing.T) {
	var got []event.Event
	c, path := coldNoticeController(t, event.FuncSink(func(e event.Event) { got = append(got, e) }))
	defer c.Close()

	c.AdoptHistory([]provider.Message{{Role: provider.RoleUser, Content: "task"}}, path)

	if n := coldNotices(got); n != 0 {
		t.Errorf("rebuild emitted %d cold-resume notices, want 0", n)
	}
	if c.executor.CacheState() != agent.CacheStateCold {
		t.Errorf("cache state = %q, want it recorded as cold anyway", c.executor.CacheState())
	}
}

func TestResumeStillAnnouncesColdResume(t *testing.T) {
	var got []event.Event
	c, path := coldNoticeController(t, event.FuncSink(func(e event.Event) { got = append(got, e) }))
	defer c.Close()

	loaded, err := sessionstore.LoadSession(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	c.Resume(loaded, path)

	if n := coldNotices(got); n != 1 {
		t.Errorf("resume emitted %d cold-resume notices, want 1", n)
	}
}
