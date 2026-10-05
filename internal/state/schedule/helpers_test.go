package schedule

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

var epoch = time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)

func newTestStore(t *testing.T) (*Store, *fakeClock, string) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "schedules")
	clk := &fakeClock{t: epoch}
	st, err := open(dir, clk.Now)
	if err != nil {
		t.Fatal(err)
	}
	return st, clk, dir
}

func reopen(t *testing.T, dir string, clk *fakeClock) *Store {
	t.Helper()
	st, err := open(dir, clk.Now)
	if err != nil {
		t.Fatal(err)
	}
	return st
}

func everyReq() CreateRequest {
	return CreateRequest{
		Trigger:     Trigger{Kind: TriggerEvery, EverySec: 3600},
		Target:      Target{Workspace: filepath.Join(os.TempDir(), "work", "repo")},
		Prompt:      "summarize open TODOs",
		Model:       Model{Provider: "deepseek", Model: "deepseek-chat"},
		Grant:       Grant{Tools: []string{"read_file", "grep"}},
		ConfirmedBy: ConfirmedByHuman,
		ToolCeiling: []string{"read_file", "grep", "glob"},
	}
}

func mustCreate(t *testing.T, st *Store, p Policy, req CreateRequest) Schedule {
	t.Helper()
	sc, err := st.Create(t.Context(), p, req)
	if err != nil {
		t.Fatal(err)
	}
	return sc
}

func slotN(sc Schedule, n int64) time.Time {
	return time.Unix(sc.Trigger.Anchor.Unix()+n*sc.Trigger.EverySec, 0).UTC()
}
