package agent

import (
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

// Children are isolated by construction. This test states that as a fact so a
// future change that starts inheriting something has to come here, flip the
// flag, and update the SPEC table in the same commit.
func TestContextCapsuleRecordsWhatIsNotInherited(t *testing.T) {
	capsule := capsuleForSpec(t, SubagentSpec{
		Kind: "task", Name: "task", SystemPrompt: DefaultTaskSystemPrompt,
	})
	if capsule.Inherited != (InheritedContext{}) {
		t.Fatalf("Inherited = %+v, want every field false: a child receives no standing instructions, memory, parent conversation, goal, or planner output", capsule.Inherited)
	}
}

func TestContextCapsuleNamesTheSystemPromptSource(t *testing.T) {
	for _, tc := range []struct {
		name string
		spec SubagentSpec
		want string
	}{
		{"writer default", SubagentSpec{Kind: "task", Name: "task", SystemPrompt: DefaultTaskSystemPrompt}, SystemPromptTaskDefault},
		{"read-only default", SubagentSpec{Kind: "task", Name: "task", SystemPrompt: DefaultReadOnlyTaskSystemPrompt}, SystemPromptReadOnlyDefault},
		{"profile body", SubagentSpec{Kind: "skill", Name: "reviewer", SystemPrompt: "you review code"}, "profile:reviewer"},
	} {
		if got := capsuleForSpec(t, tc.spec).SystemPromptSource; got != tc.want {
			t.Errorf("%s: SystemPromptSource = %q, want %q", tc.name, got, tc.want)
		}
	}
	// The capsule identifies the prompt without carrying it.
	capsule := capsuleForSpec(t, SubagentSpec{Kind: "skill", Name: "reviewer", SystemPrompt: "secret review playbook"})
	if capsule.SystemPromptHash == "" {
		t.Fatal("capsule must hash the system prompt")
	}
	if strings.Contains(capsule.Hash(), "secret") {
		t.Fatal("capsule must reference the prompt, never embed it")
	}
}

func TestContextCapsuleHashDiscriminatesRealDifferences(t *testing.T) {
	base := SubagentSpec{Kind: "task", Name: "task", SystemPrompt: DefaultTaskSystemPrompt, WorkspaceRoot: "/w", Model: "m"}
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
	reg.Add(&recordingWriter{name: "write_file"})
	base := SubagentSpec{Kind: "task", Name: "task", SystemPrompt: DefaultTaskSystemPrompt}
	withTools := base
	withTools.Registry = reg
	if capsuleForSpec(t, base).Hash() == capsuleForSpec(t, withTools).Hash() {
		t.Fatal("a different tool surface must produce a different capsule hash")
	}
	if scope := capsuleForSpec(t, withTools).ToolScope; len(scope) == 0 {
		t.Fatal("capsule must record the resolved tool scope")
	}
}
