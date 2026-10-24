package control

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/tool"
	"reasonix/internal/runtime/agent"
	"reasonix/internal/state/memory"
	"reasonix/internal/state/sessionstore"
)

// denyControlFactFile makes one fact file unreadable. The read is verified so a
// root user or a platform where the mode does not deny skips rather than
// asserting a state it did not create.
func denyControlFactFile(t *testing.T, path string) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, info.Mode().Perm()) })
	if _, err := os.ReadFile(path); !errors.Is(err, os.ErrPermission) {
		t.Skip("file permissions do not deny reads on this platform or for this user")
	}
}

// unreadableControlController seeds a readable and an unreadable fact, then
// loads the controller's snapshot after the fault exists, so the snapshot's
// prebuilt recall index carries the unreadable fact.
func unreadableControlController(t *testing.T) (*Controller, *recallAuditSink, string) {
	t.Helper()
	root := testenv.TempDir(t)
	cwd := filepath.Join(root, "project")
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatal(err)
	}
	memOpts := memory.Options{CWD: cwd, UserDir: filepath.Join(root, "user")}
	seed := memory.Load(memOpts)
	if _, err := seed.Store.Save(memory.Memory{
		Name: "good", Title: "Compiler flags", Description: "compiler flags are documented here",
		Body: "compiler flags: use -O2", Type: memory.TypeReference,
	}); err != nil {
		t.Fatal(err)
	}
	locked, err := seed.Store.Save(memory.Memory{
		Name: "locked", Title: "Locked compiler notes", Description: "compiler flags for the locked toolchain",
		Body: "compiler flags for the locked toolchain", Type: memory.TypeReference,
	})
	if err != nil {
		t.Fatal(err)
	}
	denyControlFactFile(t, locked)
	ag := agent.New(nil, tool.NewRegistry(), sessionstore.NewSession("sys"), agent.Options{}, event.Discard)
	sink := &recallAuditSink{Sink: event.Discard}
	c := New(Options{Runner: ag, Executor: ag, Sink: sink, Memory: memory.Load(memOpts)})
	t.Cleanup(c.Close)
	return c, sink, locked
}

// TestComposedTurnRecallsBesideAnUnreadableFact is the end-to-end recall
// control: one unreadable fact must not fail or silence the turn, and the audit
// behind /memory recall must name it as unreadable.
func TestComposedTurnRecallsBesideAnUnreadableFact(t *testing.T) {
	c, sink, locked := unreadableControlController(t)
	prompt := c.Compose("compiler flags")
	if !strings.Contains(prompt, "compiler flags: use -O2") {
		t.Fatalf("the readable fact did not reach the prompt:\n%s", prompt)
	}
	if strings.Contains(prompt, "Background memory could not be read") {
		t.Fatalf("one unreadable fact degraded the whole turn:\n%s", prompt)
	}
	last := c.LastMemoryRecall()
	if last.StoreError != nil {
		t.Fatalf("recall error = %v, want the turn to proceed", last.StoreError)
	}
	if len(last.Hits) != 1 || len(last.Unreadable) != 1 {
		t.Fatalf("recall = %d hits, %d unreadable; want one of each", len(last.Hits), len(last.Unreadable))
	}
	var fact *memory.FactReadError
	if !errors.As(last.Unreadable[0], &fact) || fact.Path != locked {
		t.Fatalf("unreadable = %+v, want the fact at %s", last.Unreadable[0], locked)
	}
	if len(sink.audits) != 1 {
		t.Fatalf("audits = %d, want one per composed turn", len(sink.audits))
	}
	if len(sink.audits[0].Hits) != 1 {
		t.Fatalf("trajectory audit lost the readable hit: %+v", sink.audits[0].Hits)
	}

	audit := MemoryCommandText(c, "/memory recall")
	if !strings.Contains(audit, "unreadable=1") || !strings.Contains(audit, locked) {
		t.Fatalf("/memory recall did not name the unreadable fact:\n%s", audit)
	}
	if strings.Contains(audit, "not found") {
		t.Fatalf("/memory recall reported the fact as not found:\n%s", audit)
	}
}

// TestMemorySummaryNamesTheUnreadableFact is the /memory listing control: the
// readable fact is listed and the unreadable one is named beside it, so the
// panel and the text summary cannot say the store holds only the good fact.
func TestMemorySummaryNamesTheUnreadableFact(t *testing.T) {
	c, _, locked := unreadableControlController(t)
	summary := MemoryCommandText(c, "/memory")
	if !strings.Contains(summary, "good") || !strings.Contains(summary, "compiler flags are documented here") {
		t.Fatalf("the readable fact is not listed:\n%s", summary)
	}
	if !strings.Contains(summary, "unreadable facts") || !strings.Contains(summary, locked) {
		t.Fatalf("/memory did not name the unreadable fact:\n%s", summary)
	}
	if !strings.Contains(summary, "read memory fact") {
		t.Fatalf("/memory did not use the fact read error:\n%s", summary)
	}
}

// TestMemoryCommandsKeepTheNotFoundAnswer is the control for the read-by-name
// path: a genuinely missing fact still answers not found, and an unreadable one
// answers with the store error.
func TestMemoryCommandsKeepTheNotFoundAnswer(t *testing.T) {
	c, _, _ := unreadableControlController(t)
	if text := MemoryCommandText(c, "/memory revisions missing"); !strings.Contains(text, "not found") {
		t.Fatalf("/memory revisions missing = %q, want not found", text)
	}
	if text := MemoryCommandText(c, "/memory revisions locked"); !strings.Contains(text, "read memory fact") || strings.Contains(text, "not found") {
		t.Fatalf("/memory revisions locked = %q, want the fact read error", text)
	}
}
