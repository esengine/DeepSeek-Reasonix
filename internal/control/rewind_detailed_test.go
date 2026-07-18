package control

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/agent"
	"reasonix/internal/diff"
	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

type detailedRewindFixture struct {
	controller *Controller
	session    *agent.Session
	root       string
	events     *[]event.Event
}

func newDetailedRewindFixture(t *testing.T) detailedRewindFixture {
	t.Helper()
	root := t.TempDir()
	sessionDir := filepath.Join(root, "sessions")
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		t.Fatal(err)
	}
	session := agent.NewSession("system")
	session.Add(provider.Message{Role: provider.RoleUser, Content: "first prompt"})
	session.Add(provider.Message{Role: provider.RoleAssistant, Content: "first answer"})
	session.Add(provider.Message{Role: provider.RoleUser, Content: "second prompt"})
	session.Add(provider.Message{Role: provider.RoleAssistant, Content: "second answer"})
	executor := agent.New(nil, tool.NewRegistry(), session, agent.Options{}, event.Discard)
	events := []event.Event{}
	controller := New(Options{
		Executor:      executor,
		WorkspaceRoot: root,
		SessionDir:    sessionDir,
		SessionPath:   filepath.Join(sessionDir, "session.jsonl"),
		Label:         "rewind-test",
		Sink: event.FuncSink(func(value event.Event) {
			events = append(events, value)
		}),
	})
	return detailedRewindFixture{controller: controller, session: session, root: root, events: &events}
}

func (f detailedRewindFixture) checkpoint(turnBoundary int, changes ...diff.Change) {
	f.controller.checkpoints.begin("checkpoint", turnBoundary)
	for _, change := range changes {
		f.controller.checkpoints.snapshot(change)
	}
}

func assertRewindFailure(t *testing.T, err error, failure RewindFailure, sentinel error) *RewindError {
	t.Helper()
	if err == nil {
		t.Fatalf("RewindDetailed error = nil, want %s", failure)
	}
	if !errors.Is(err, sentinel) {
		t.Fatalf("RewindDetailed error = %v, want errors.Is(%v)", err, sentinel)
	}
	var detailed *RewindError
	if !errors.As(err, &detailed) {
		t.Fatalf("RewindDetailed error type = %T, want *RewindError", err)
	}
	if detailed.Failure != failure {
		t.Fatalf("RewindDetailed failure = %q, want %q", detailed.Failure, failure)
	}
	return detailed
}

func TestRewindDetailedBothPreflightsConversationBeforeCodeWrite(t *testing.T) {
	f := newDetailedRewindFixture(t)
	path := filepath.Join(f.root, "tracked.txt")
	if err := os.WriteFile(path, []byte("current"), 0o644); err != nil {
		t.Fatal(err)
	}
	f.checkpoint(1, diff.Change{Path: "tracked.txt", Kind: diff.Modify, OldText: "original"})

	// Simulate a compaction that made this persisted boundary stale. The code
	// restore remains valid, so this specifically proves both validates the
	// conversation half before RestoreCode can touch the file.
	f.controller.checkpoints.mu.Lock()
	f.controller.checkpoints.bound[0] = f.session.Len() + 10
	f.controller.checkpoints.mu.Unlock()

	result, err := f.controller.RewindDetailed(0, RewindBoth)
	detailed := assertRewindFailure(t, err, RewindFailureScopeUnavailable, ErrRewindScopeUnavailable)
	if detailed.WorkspaceMayHaveChanged || detailed.ConversationMayHaveChanged || detailed.SnapshotRequired {
		t.Fatalf("preflight failure carried partial impact: %+v", detailed)
	}
	if result != (RewindResult{}) {
		t.Fatalf("preflight result = %+v, want zero", result)
	}
	content, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(content) != "current" {
		t.Fatalf("workspace changed before complete preflight: %q", content)
	}
	if got := f.session.Len(); got != 5 {
		t.Fatalf("conversation changed before complete preflight: len=%d", got)
	}
	if got := len(f.controller.Checkpoints()); got != 1 {
		t.Fatalf("checkpoint invalidated on preflight failure: count=%d", got)
	}
}

func TestRewindDetailedCodePreflightValidatesEveryPathBeforeWrite(t *testing.T) {
	f := newDetailedRewindFixture(t)
	path := filepath.Join(f.root, "first.txt")
	if err := os.WriteFile(path, []byte("current"), 0o644); err != nil {
		t.Fatal(err)
	}
	f.checkpoint(1,
		diff.Change{Path: "first.txt", Kind: diff.Modify, OldText: "original"},
		diff.Change{Path: filepath.Join("..", "escape.txt"), Kind: diff.Modify, OldText: "outside"},
	)

	result, err := f.controller.RewindDetailed(0, RewindCode)
	detailed := assertRewindFailure(t, err, RewindFailureScopeUnavailable, ErrRewindScopeUnavailable)
	if detailed.WorkspaceMayHaveChanged || detailed.ConversationMayHaveChanged || detailed.SnapshotRequired {
		t.Fatalf("code preflight failure carried partial impact: %+v", detailed)
	}
	if result != (RewindResult{}) {
		t.Fatalf("code preflight result = %+v, want zero", result)
	}
	content, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(content) != "current" {
		t.Fatalf("first path was written before later path validation: %q", content)
	}
}

func TestRewindDetailedCodeOnlyRestoresWorkspaceWithoutConversationRewrite(t *testing.T) {
	f := newDetailedRewindFixture(t)
	path := filepath.Join(f.root, "tracked.txt")
	if err := os.WriteFile(path, []byte("current"), 0o644); err != nil {
		t.Fatal(err)
	}
	f.checkpoint(1, diff.Change{Path: "tracked.txt", Kind: diff.Modify, OldText: "original"})
	f.controller.checkpoints.mu.Lock()
	f.controller.checkpoints.bound[0] = f.session.Len() + 10 // irrelevant to code-only
	f.controller.checkpoints.mu.Unlock()

	result, err := f.controller.RewindDetailed(0, RewindCode)
	if err != nil {
		t.Fatalf("RewindDetailed(code): %v", err)
	}
	if !result.WorkspaceChanged || result.ConversationRewritten || !result.SnapshotRequired {
		t.Fatalf("code-only result = %+v", result)
	}
	content, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(content) != "original" {
		t.Fatalf("restored content = %q, want original", content)
	}
	if got := f.session.Len(); got != 5 {
		t.Fatalf("code-only rewrote conversation: len=%d", got)
	}
	if got := len(f.controller.Checkpoints()); got != 1 {
		t.Fatalf("code-only invalidated checkpoint: count=%d", got)
	}
}

func TestRewindDetailedConversationOnlyPersistsAndInvalidatesCheckpoint(t *testing.T) {
	f := newDetailedRewindFixture(t)
	f.checkpoint(1)

	result, err := f.controller.RewindDetailed(0, RewindConversation)
	if err != nil {
		t.Fatalf("RewindDetailed(conversation): %v", err)
	}
	if result.WorkspaceChanged || !result.ConversationRewritten || !result.SnapshotRequired {
		t.Fatalf("conversation-only result = %+v", result)
	}
	if got := f.session.Len(); got != 1 {
		t.Fatalf("conversation len = %d, want boundary 1", got)
	}
	if got := len(f.controller.Checkpoints()); got != 0 {
		t.Fatalf("rewritten checkpoint remained live: count=%d", got)
	}

	_, staleErr := f.controller.RewindDetailed(0, RewindConversation)
	assertRewindFailure(t, staleErr, RewindFailureCheckpointMissing, ErrRewindCheckpointMissing)
}

func TestRewindDetailedRejectsUnavailableCodeButLegacyKeepsNoOpCompatibility(t *testing.T) {
	f := newDetailedRewindFixture(t)
	f.checkpoint(1)

	result, err := f.controller.RewindDetailed(0, RewindCode)
	assertRewindFailure(t, err, RewindFailureScopeUnavailable, ErrRewindScopeUnavailable)
	if result != (RewindResult{}) || f.session.Len() != 5 || len(f.controller.Checkpoints()) != 1 {
		t.Fatalf("strict unavailable code scope mutated state: result=%+v messages=%d checkpoints=%d",
			result, f.session.Len(), len(f.controller.Checkpoints()))
	}
	if legacyErr := f.controller.Rewind(0, RewindCode); legacyErr != nil {
		t.Fatalf("legacy empty code rewind compatibility: %v", legacyErr)
	}
}

func TestRewindDetailedBothOrdersCodeThenConversationAndCompletes(t *testing.T) {
	f := newDetailedRewindFixture(t)
	path := filepath.Join(f.root, "tracked.txt")
	if err := os.WriteFile(path, []byte("current"), 0o644); err != nil {
		t.Fatal(err)
	}
	f.checkpoint(1, diff.Change{Path: "tracked.txt", Kind: diff.Modify, OldText: "original"})

	result, err := f.controller.RewindDetailed(0, RewindBoth)
	if err != nil {
		t.Fatalf("RewindDetailed(both): %v", err)
	}
	if !result.WorkspaceChanged || !result.ConversationRewritten || !result.SnapshotRequired {
		t.Fatalf("both result = %+v", result)
	}
	content, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(content) != "original" || f.session.Len() != 1 {
		t.Fatalf("both final state: file=%q messages=%d", content, f.session.Len())
	}
}

func TestRewindDetailedCodeFailureAfterFirstWriteReturnsPartialImpact(t *testing.T) {
	f := newDetailedRewindFixture(t)
	validPath := filepath.Join(f.root, "first.txt")
	if err := os.WriteFile(validPath, []byte("current"), 0o644); err != nil {
		t.Fatal(err)
	}
	blockedParent := filepath.Join(f.root, "blocked")
	if err := os.WriteFile(blockedParent, []byte("not a directory"), 0o644); err != nil {
		t.Fatal(err)
	}
	f.checkpoint(1,
		diff.Change{Path: "first.txt", Kind: diff.Modify, OldText: "original"},
		diff.Change{Path: filepath.Join("blocked", "second.txt"), Kind: diff.Modify, OldText: "old second"},
	)

	result, err := f.controller.RewindDetailed(0, RewindCode)
	detailed := assertRewindFailure(t, err, RewindFailurePartial, ErrRewindPartial)
	if !detailed.WorkspaceMayHaveChanged || detailed.ConversationMayHaveChanged || !detailed.SnapshotRequired {
		t.Fatalf("partial impact = %+v", detailed)
	}
	if !result.WorkspaceChanged || result.ConversationRewritten || !result.SnapshotRequired {
		t.Fatalf("partial confirmed result = %+v", result)
	}
	content, readErr := os.ReadFile(validPath)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(content) != "original" {
		t.Fatalf("first restore did not land before later failure: %q", content)
	}
	if got := f.session.Len(); got != 5 {
		t.Fatalf("conversation changed after code failure: len=%d", got)
	}
}

func TestRewindDetailedSnapshotRewriteFailureIsNotSwallowed(t *testing.T) {
	tests := []struct {
		name   string
		legacy bool
	}{
		{name: "detailed"},
		{name: "legacy wrapper", legacy: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			f := newDetailedRewindFixture(t)
			f.checkpoint(3)
			blockedParent := filepath.Join(f.root, "session-parent-is-a-file")
			if err := os.WriteFile(blockedParent, []byte("block mkdir"), 0o644); err != nil {
				t.Fatal(err)
			}
			f.controller.mu.Lock()
			f.controller.sessionPath = filepath.Join(blockedParent, "session.jsonl")
			f.controller.mu.Unlock()

			var result RewindResult
			var err error
			if test.legacy {
				err = f.controller.Rewind(0, RewindConversation)
			} else {
				result, err = f.controller.RewindDetailed(0, RewindConversation)
			}
			detailed := assertRewindFailure(t, err, RewindFailurePartial, ErrRewindPartial)
			if detailed.WorkspaceMayHaveChanged || !detailed.ConversationMayHaveChanged || !detailed.SnapshotRequired {
				t.Fatalf("snapshot partial impact = %+v", detailed)
			}
			if !test.legacy && (result.WorkspaceChanged || !result.ConversationRewritten || !result.SnapshotRequired) {
				t.Fatalf("snapshot partial confirmed result = %+v", result)
			}
			if f.session.Len() != 3 || len(f.controller.Checkpoints()) != 0 {
				t.Fatalf("in-memory rewrite was hidden: messages=%d checkpoints=%d", f.session.Len(), len(f.controller.Checkpoints()))
			}
			for _, emitted := range *f.events {
				if emitted.Kind == event.Notice && emitted.Level == event.LevelInfo && strings.Contains(emitted.Text, "rewound conversation") {
					t.Fatalf("false success notice after persistence failure: %q", emitted.Text)
				}
			}
			if test.legacy {
				warned := false
				for _, emitted := range *f.events {
					warned = warned || (emitted.Kind == event.Notice && emitted.Level == event.LevelWarn)
				}
				if !warned {
					t.Fatal("legacy Rewind did not emit its compatibility warning")
				}
			}
		})
	}
}

func TestRewindDetailedRejectsInvalidScopeWithoutMutation(t *testing.T) {
	f := newDetailedRewindFixture(t)
	f.checkpoint(1)
	result, err := f.controller.RewindDetailed(0, RewindScope(99))
	assertRewindFailure(t, err, RewindFailureInvalidScope, ErrRewindInvalidScope)
	if result != (RewindResult{}) || f.session.Len() != 5 || len(f.controller.Checkpoints()) != 1 {
		t.Fatalf("invalid scope mutated state: result=%+v messages=%d checkpoints=%d", result, f.session.Len(), len(f.controller.Checkpoints()))
	}
}
