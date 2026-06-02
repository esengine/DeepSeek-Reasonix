package control

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/agent/testutil"
	"reasonix/internal/event"
	"reasonix/internal/memory"
	"reasonix/internal/tool"
)

func TestAutoMemorySummarizesAfterIdleAndMergesDailySummary(t *testing.T) {
	userDir := t.TempDir()
	cwd := t.TempDir()
	store := memory.StoreFor(userDir, cwd)
	mem := memory.Load(memory.Options{CWD: cwd, UserDir: userDir})
	prov := testutil.NewMock("fake",
		testutil.Turn{Text: "first answer"},
		testutil.Turn{Text: "summary one"},
		testutil.Turn{Text: "second answer"},
		testutil.Turn{Text: "summary two"},
	)
	exec := agent.New(prov, tool.NewRegistry(), agent.NewSession("sys"), agent.Options{}, event.Discard)
	turnDone := make(chan struct{}, 2)
	c := New(Options{
		Runner:         exec,
		Executor:       exec,
		Memory:         mem,
		AutoMemory:     "on",
		AutoMemoryIdle: time.Millisecond,
		AutoMemoryNow:  func() time.Time { return time.Date(2026, 6, 2, 12, 0, 0, 0, time.UTC) },
		Sink: event.FuncSink(func(e event.Event) {
			if e.Kind == event.TurnDone {
				turnDone <- struct{}{}
			}
		}),
	})
	defer c.Close()

	c.Send("remember this project decision")
	waitForSignal(t, turnDone)
	waitForFileContains(t, store.Path("daily-summary-2026-06-02"), "summary one")

	c.Send("add another decision")
	waitForSignal(t, turnDone)
	waitForFileContains(t, store.Path("daily-summary-2026-06-02"), "summary two")

	requests := prov.Requests()
	if len(requests) != 4 {
		t.Fatalf("provider calls = %d, want turn+summary+turn+summary", len(requests))
	}
	secondSummary := requests[3].Messages[1].Content
	if !strings.Contains(secondSummary, "Existing daily summary:\nsummary one") {
		t.Fatalf("second summary should merge the prior daily memory, got:\n%s", secondSummary)
	}
	if strings.Contains(secondSummary, "remember this project decision") {
		t.Fatalf("second summary should receive only new conversation messages, got:\n%s", secondSummary)
	}
	if !strings.Contains(secondSummary, "add another decision") {
		t.Fatalf("second summary missing the new turn transcript:\n%s", secondSummary)
	}
	idx, err := os.ReadFile(filepath.Join(store.Dir, "MEMORY.md"))
	if err != nil {
		t.Fatalf("read MEMORY.md: %v", err)
	}
	if strings.Count(string(idx), "daily-summary-2026-06-02.md") != 1 {
		t.Fatalf("daily memory should have one index entry, got:\n%s", idx)
	}
}

func waitForSignal(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for turn completion")
	}
}

func waitForFileContains(t *testing.T, path, want string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		b, err := os.ReadFile(path)
		if err == nil && strings.Contains(string(b), want) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s did not contain %q; last err=%v body=%q", path, want, err, string(b))
		}
		time.Sleep(10 * time.Millisecond)
	}
}
