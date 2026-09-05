package boot

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/agent"
	"reasonix/internal/config"
	"reasonix/internal/event"
	"reasonix/internal/execjournal"
	"reasonix/internal/provider"
	"reasonix/internal/skill"
	"reasonix/internal/testenv"
	"reasonix/internal/tool"
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
	if got := facts[0].Meta.ParentToolCallID; got != execution {
		t.Fatalf("child parent = %q, want the execution %q the journal opened", got, execution)
	}
	if got := facts[0].Meta.Kind; got != "skill" {
		t.Fatalf("child kind = %q, want skill", got)
	}
}

// A run the host started is recorded under no parent call, and the case that
// matters is the one where a call context exists anyway: a slash invocation
// carries a synthetic event id so the child's UI can nest under it, and a reader
// that found that id in the store would take it for a call the model made.
func TestAHostStartedSkillIsNotFiledUnderACallNobodyMade(t *testing.T) {
	for _, callID := range []string{"", "slash-skill-1"} {
		t.Run("call="+orNone(callID), func(t *testing.T) {
			runner, ctx, sessionPath, store := skillOwnerFixture(t, callID)
			opts := skill.SubagentRunOptions{HostInitiated: true}
			if _, err := runner.run(ctx, probeSkill(), "do the thing", opts); err != nil {
				t.Fatalf("run: %v", err)
			}
			if entries := execjournal.History(sessionPath); len(entries) != 0 {
				t.Fatalf("journal recorded %d execution(s) for a run no call opened", len(entries))
			}
			facts := ownedChildren(t, store, sessionPath)
			if len(facts) != 1 {
				t.Fatalf("store holds %d children, want the one the skill ran", len(facts))
			}
			if got := facts[0].Meta.ParentToolCallID; got != "" {
				t.Fatalf("child parent = %q, want none for a run no tool call made", got)
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

// The compiled spec carries every fact only the skill layer knows. Each of these
// is a semantic the shared runner cannot recover for itself, so a migration that
// dropped one would change the product while the execution records looked right.
func TestCompiledSkillSpecKeepsWhatOnlyTheSkillLayerKnows(t *testing.T) {
	runner, _, _, _ := skillOwnerFixture(t, "skill-call")
	sk := probeSkill()
	sk.AllowedTools = []string{"read_file"}

	spec, err := runner.compile(context.Background(), sk, "task text", skill.SubagentRunOptions{ContinueFrom: "sa_x"})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if !strings.Contains(spec.Worker.SystemPrompt, "## Workspace") {
		t.Fatalf("system prompt lost the workspace facts:\n%s", spec.Worker.SystemPrompt)
	}
	if !strings.HasPrefix(spec.Worker.SystemPrompt, strings.TrimSpace(sk.Body)) {
		t.Fatal("system prompt no longer starts with the skill body")
	}
	if !spec.Worker.UseProfilePrompt {
		t.Fatal("the body must be used verbatim; a task default would replace it")
	}
	if spec.Grant.WritePaths.Empty() {
		t.Fatal("a writer skill lost its whole-workspace claim and can now race a fleet writer")
	}
	if len(spec.Grant.ProfileTools) == 0 {
		t.Fatal("the skill's allowed-tools ceiling was dropped")
	}
	if spec.Context.ContinueFrom != "sa_x" {
		t.Fatalf("continue_from = %q, want it carried through", spec.Context.ContinueFrom)
	}
	if spec.Sched.MaxSteps != runner.halfSteps() {
		t.Fatalf("max steps = %d, want the skill's half budget %d", spec.Sched.MaxSteps, runner.halfSteps())
	}

	review := probeSkill()
	review.Name = "review"
	reviewSpec, err := runner.compile(context.Background(), review, "task text", skill.SubagentRunOptions{})
	if err != nil {
		t.Fatalf("compile review: %v", err)
	}
	if reviewSpec.Worker.ReviewReport == "" {
		t.Fatal("a reviewer no longer owes a typed verdict")
	}
	if spec.Worker.ReviewReport != "" {
		t.Fatal("an ordinary skill was given a review contract it never had")
	}
}

func probeSkill() skill.Skill {
	return skill.Skill{
		Name: "probe-worker", RunAs: skill.RunSubagent,
		Body: "Answer the task you are given and stop.",
	}
}

// skillOwnerFixture builds the runner against a real store and scheduler. callID
// empty stands for a run the host started with no tool call of its own.
func skillOwnerFixture(t *testing.T, callID string) (*skillSubagents, context.Context, string, *agent.SubagentStore) {
	t.Helper()
	root, sessions := testenv.TempDir(t), testenv.TempDir(t)
	store := agent.NewSubagentStore(filepath.Join(sessions, "subagents"))
	reg := tool.NewRegistry()
	task := agent.NewTaskToolWithOptions(agent.TaskToolOptions{
		Provider: &skillOwnerProvider{}, ParentRegistry: reg, MaxSteps: 8, ContextWindow: 8192,
	}).WithTranscripts(store, root, "base", "high").WithScheduler(agent.NewSubagentScheduler(2, 2))
	runner := &skillSubagents{root: root, cfg: config.Default(), registry: reg, tasks: task, maxSteps: 8}

	ctx := context.Background()
	if callID != "" {
		ctx = agent.WithToolCallContext(ctx, callID, event.Discard, nil, false)
	}
	ctx = agent.WithTurnIdentity(agent.WithParentSession(ctx, "probe"), "turn-1")
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

func ownedChildren(t *testing.T, store *agent.SubagentStore, sessionPath string) []agent.SubagentArtifact {
	t.Helper()
	sessions := filepath.Dir(sessionPath)
	stem := strings.TrimSuffix(filepath.Base(sessionPath), ".jsonl")
	facts, err := agent.ListSubagentsByParent(sessions, stem)
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
