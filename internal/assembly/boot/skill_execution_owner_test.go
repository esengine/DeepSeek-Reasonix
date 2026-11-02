package boot

import (
	"context"
	"path/filepath"
	"reasonix/internal/state/sessionstore"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/config"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
	"reasonix/internal/ext/skill"
	"reasonix/internal/runtime/agent"
	"reasonix/internal/runtime/delegation"
	"reasonix/internal/runtime/writeclaim"
	"reasonix/internal/state/execjournal"
)

// A subagent skill is one delegated execution, so what it leaves behind is what
// every delegation leaves: an opening, a slot grant, a settlement, and a child
// record joined to that execution. This is the gate on the runner keeping an
// execution loop of its own — one that acquired its own slot and saved its own
// terminal would still answer the caller and leave none of this.
func TestASubagentSkillLeavesTheRecordsEveryDelegationLeaves(t *testing.T) {
	runner, ctx, sessionPath, store := skillOwnerFixture(t, "skill-call")

	out, err := runner.run(ctx, probeSkill(), "do the thing", skill.SubagentRunOptions{})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if strings.TrimSpace(out) == "" {
		t.Fatal("the skill answered nothing")
	}

	const execution = "skill-call/sub-1"
	entry, ok := journalEntry(sessionPath, execution)
	if !ok {
		t.Fatalf("no journal entry for %s; the runner reached a provider without recording the execution", execution)
	}
	if !entry.Started() || entry.SettledAt.IsZero() {
		t.Fatalf("journal entry started=%v settled=%v, want a slot grant and a settlement",
			entry.Started(), !entry.SettledAt.IsZero())
	}
	facts := ownedChildren(t, store, sessionPath)
	if len(facts) != 1 {
		t.Fatalf("store holds %d children, want the one the skill ran", len(facts))
	}
	if got := facts[0].Meta.ExecutionID; got != execution {
		t.Fatalf("child execution = %q, want the %q the journal opened", got, execution)
	}
	if got := facts[0].Meta.ParentToolCallID; got != execution {
		t.Fatalf("child parent = %q; a model-delegated run descends from the call it was made from", got)
	}
	if got := facts[0].Meta.Kind; got != "skill" {
		t.Fatalf("child kind = %q, want skill", got)
	}
}

// A run the host started owns its own execution and descends from no call. The
// case that matters is the one where a call context exists anyway: a slash
// invocation carries a synthetic event id so the child's UI can nest under it,
// and a reader finding that id in a durable record would take it for a call the
// model made. So the execution is the host's own, and the lineage stays empty.
func TestAHostStartedSkillIsItsOwnRootAndDescendsFromNoCall(t *testing.T) {
	for _, callID := range []string{"", "slash-skill-1"} {
		t.Run("call="+orNone(callID), func(t *testing.T) {
			runner, ctx, sessionPath, store := skillOwnerFixture(t, callID)
			opts := skill.SubagentRunOptions{HostInitiated: true}
			if _, err := runner.run(ctx, probeSkill(), "do the thing", opts); err != nil {
				t.Fatalf("run: %v", err)
			}
			entries := execjournal.History(sessionPath)
			if len(entries) != 1 {
				t.Fatalf("journal holds %d execution(s), want the one the host started", len(entries))
			}
			execution := entries[0]
			if !strings.HasPrefix(execution.ID, "host-") {
				t.Fatalf("execution %q is not host-owned; it must not borrow a call's identity", execution.ID)
			}
			if execution.ID == callID || execution.Group != "" {
				t.Fatalf("execution %q hangs under %q, want a root of its own", execution.ID, execution.Group)
			}
			if !execution.Started() || execution.SettledAt.IsZero() {
				t.Fatal("a host-started execution owes the same slot grant and settlement as any other")
			}
			facts := ownedChildren(t, store, sessionPath)
			if len(facts) != 1 {
				t.Fatalf("store holds %d children, want the one the skill ran", len(facts))
			}
			if got := facts[0].Meta.ParentToolCallID; got != "" {
				t.Fatalf("child parent = %q, want none for a run no tool call made", got)
			}
			if got := facts[0].Meta.ExecutionID; got != execution.ID {
				t.Fatalf("child execution = %q, want the %q the journal opened", got, execution.ID)
			}
		})
	}
}

func orNone(s string) string {
	if s == "" {
		return "none"
	}
	return s
}

func probeSkill() skill.Skill {
	return skill.Skill{
		Name: "probe-worker", RunAs: skill.RunSubagent,
		Body: "Answer the task you are given and stop.",
	}
}

// skillOwnerFixture builds the runner against a real store and scheduler. callID
// empty stands for a run the host started with no tool call of its own.
func skillOwnerFixture(t *testing.T, callID string) (*skillSubagents, context.Context, string, *delegation.SubagentStore) {
	t.Helper()
	root, sessions := testenv.TempDir(t), testenv.TempDir(t)
	store := delegation.NewSubagentStore(filepath.Join(sessions, "subagents"))
	reg := tool.NewRegistry()
	task := delegation.NewTaskToolWithOptions(delegation.TaskToolOptions{
		Provider: &skillOwnerProvider{}, ParentRegistry: reg, MaxSteps: 8, ContextWindow: 8192,
	}).WithTranscripts(store, root, "base", "high").WithScheduler(writeclaim.NewSubagentScheduler(2, 2))
	runner := &skillSubagents{root: root, cfg: config.Default(), registry: reg, tasks: task, maxSteps: 8}

	ctx := context.Background()
	if callID != "" {
		ctx = agent.WithToolCallContext(ctx, callID, event.Discard, nil, false)
	}
	ctx = delegation.WithTurnIdentity(agent.WithParentSession(ctx, "probe"), "turn-1")
	return runner, ctx, filepath.Join(sessions, "probe.jsonl"), store
}

func journalEntry(sessionPath, id string) (execjournal.Entry, bool) {
	for _, e := range execjournal.History(sessionPath) {
		if e.ID == id {
			return e, true
		}
	}
	return execjournal.Entry{}, false
}

func ownedChildren(t *testing.T, store *delegation.SubagentStore, sessionPath string) []sessionstore.SubagentArtifact {
	t.Helper()
	sessions := filepath.Dir(sessionPath)
	stem := strings.TrimSuffix(filepath.Base(sessionPath), ".jsonl")
	facts, err := sessionstore.ListSubagentsByParent(sessions, stem)
	if err != nil {
		t.Fatalf("list children: %v", err)
	}
	return facts
}

type skillOwnerProvider struct{}

func (*skillOwnerProvider) Name() string { return "skill-owner-test" }

func (*skillOwnerProvider) Stream(context.Context, provider.Request) (<-chan provider.Chunk, error) {
	ch := make(chan provider.Chunk, 2)
	ch <- provider.Chunk{Type: provider.ChunkText, Text: "the skill answered"}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}
