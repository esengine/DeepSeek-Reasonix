package agent

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"reasonix/internal/tool"
)

func capsuleForSpec(t *testing.T, spec SubagentSpec) ContextCapsule {
	t.Helper()
	now := time.Unix(0, 0)
	return metaFromSpec("sa_test", SubagentRunning, now, now, spec).Capsule
}

// Children are isolated by construction, and a run nobody fed inherits nothing
// at all. This test states that as a fact so a future change that starts
// inheriting something has to come here, flip the flag, and update the SPEC
// table in the same commit.
func TestContextCapsuleRecordsWhatIsNotInherited(t *testing.T) {
	capsule := capsuleForSpec(t, SubagentSpec{ExecutionID: "exec-test",
		Kind: "task", Name: "task", SystemPrompt: DefaultTaskSystemPrompt,
	})
	if !reflect.DeepEqual(capsule.Inherited, InheritedContext{}) {
		t.Fatalf("Inherited = %+v, want nothing recorded: a child receives no standing instructions, memory, parent conversation, goal, or planner output", capsule.Inherited)
	}
	if capsule.Inherited.HasUpstream() {
		t.Fatal("a run nobody fed must not report an upstream source")
	}
}

// The one thing a child can inherit: a declared dependency's answer. Recording
// it is what keeps the delivery a decision rather than an accident, and it is
// what lets two runs that started from different context be told apart.
func TestContextCapsuleRecordsDeliveredUpstream(t *testing.T) {
	isolated := SubagentSpec{ExecutionID: "exec-test", Kind: "task", Name: "task", SystemPrompt: DefaultTaskSystemPrompt}
	fed := isolated
	fed.UpstreamFrom = []string{"research"}
	other := isolated
	other.UpstreamFrom = []string{"survey"}

	if got := capsuleForSpec(t, fed).Inherited; !reflect.DeepEqual(got, InheritedContext{UpstreamFrom: []string{"research"}}) {
		t.Fatalf("Inherited = %+v, want the delivering dependency named and nothing else", got)
	}
	if capsuleForSpec(t, isolated).Hash() == capsuleForSpec(t, fed).Hash() {
		t.Error("two runs that started from different context must not share a capsule hash")
	}
	// The reason the flag became a list: these two runs read different work and
	// a bool called them identical.
	if capsuleForSpec(t, other).Hash() == capsuleForSpec(t, fed).Hash() {
		t.Error("two runs fed by different dependencies must not share a capsule hash")
	}
}

// Sidecars written while the field was a flag still say that something upstream
// opened the run. Reading one as though it had no upstream would rewrite what
// the record claims; naming a source it never held would be worse.
func TestInheritedContextReadsLegacyUpstreamFlag(t *testing.T) {
	var legacy InheritedContext
	if err := json.Unmarshal([]byte(`{"upstream":true}`), &legacy); err != nil {
		t.Fatal(err)
	}
	if !legacy.HasUpstream() {
		t.Fatal("a legacy record that recorded an upstream must still report one")
	}
	if len(legacy.UpstreamFrom) != 0 {
		t.Fatalf("UpstreamFrom = %v, want no invented source", legacy.UpstreamFrom)
	}

	var none InheritedContext
	if err := json.Unmarshal([]byte(`{"upstream":false}`), &none); err != nil {
		t.Fatal(err)
	}
	if none.HasUpstream() {
		t.Fatal("a legacy record with no upstream must not report one")
	}

	var named InheritedContext
	if err := json.Unmarshal([]byte(`{"upstreamFrom":["research"]}`), &named); err != nil {
		t.Fatal(err)
	}
	if !named.HasUpstream() || named.UpstreamFrom[0] != "research" {
		t.Fatalf("UpstreamFrom = %v, want the named source", named.UpstreamFrom)
	}
}

func TestContextCapsuleNamesTheSystemPromptSource(t *testing.T) {
	for _, tc := range []struct {
		name string
		spec SubagentSpec
		want string
	}{
		{"writer default", SubagentSpec{ExecutionID: "exec-test", Kind: "task", Name: "task", SystemPrompt: DefaultTaskSystemPrompt}, SystemPromptTaskDefault},
		{"read-only default", SubagentSpec{ExecutionID: "exec-test", Kind: "task", Name: "task", SystemPrompt: DefaultReadOnlyTaskSystemPrompt}, SystemPromptReadOnlyDefault},
		{"profile body", SubagentSpec{ExecutionID: "exec-test", Kind: "skill", Name: "reviewer", SystemPrompt: "you review code"}, "profile:reviewer"},
	} {
		if got := capsuleForSpec(t, tc.spec).SystemPromptSource; got != tc.want {
			t.Errorf("%s: SystemPromptSource = %q, want %q", tc.name, got, tc.want)
		}
	}
	// The capsule identifies the prompt without carrying it.
	capsule := capsuleForSpec(t, SubagentSpec{ExecutionID: "exec-test", Kind: "skill", Name: "reviewer", SystemPrompt: "secret review playbook"})
	if capsule.SystemPromptHash == "" {
		t.Fatal("capsule must hash the system prompt")
	}
	if strings.Contains(capsule.Hash(), "secret") {
		t.Fatal("capsule must reference the prompt, never embed it")
	}
}

func TestContextCapsuleHashDiscriminatesRealDifferences(t *testing.T) {
	base := SubagentSpec{ExecutionID: "exec-test", Kind: "task", Name: "task", SystemPrompt: DefaultTaskSystemPrompt, WorkspaceRoot: "/w", Model: "m"}
	first := capsuleForSpec(t, base).Hash()
	if first == "" {
		t.Fatal("capsule hash must be computable")
	}
	if second := capsuleForSpec(t, base).Hash(); second != first {
		t.Fatalf("same context produced different hashes: %s vs %s", first, second)
	}

	for name, mutate := range map[string]func(*SubagentSpec){
		"different prompt":    func(s *SubagentSpec) { s.SystemPrompt = "other" },
		"different model":     func(s *SubagentSpec) { s.Model = "other" },
		"different workspace": func(s *SubagentSpec) { s.WorkspaceRoot = "/other" },
		"resumed transcript":  func(s *SubagentSpec) { s.ResumedFrom = "sa_prior" },
	} {
		changed := base
		mutate(&changed)
		if got := capsuleForSpec(t, changed).Hash(); got == first {
			t.Errorf("%s: hash did not change, so the record cannot explain a behaviour difference", name)
		}
	}
}

// The tool surface is part of what a child saw, so it must move the hash.
func TestContextCapsuleHashCoversToolScope(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(&recordingWriter{name: "write_file", writesPaths: true})
	base := SubagentSpec{ExecutionID: "exec-test", Kind: "task", Name: "task", SystemPrompt: DefaultTaskSystemPrompt}
	withTools := base
	withTools.Registry = reg
	if capsuleForSpec(t, base).Hash() == capsuleForSpec(t, withTools).Hash() {
		t.Fatal("a different tool surface must produce a different capsule hash")
	}
	if scope := capsuleForSpec(t, withTools).ToolScope; len(scope) == 0 {
		t.Fatal("capsule must record the resolved tool scope")
	}
}
