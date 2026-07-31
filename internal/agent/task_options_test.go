package agent

import (
	"reflect"
	"strings"
	"testing"

	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

func TestNewTaskToolWithOptionsMatchesPositional(t *testing.T) {
	prov := &mockProvider{name: "sub"}
	pricing := &provider.Pricing{Input: 1.5, Output: 2.5}
	reg := tool.NewRegistry()
	gate := &stubGate{}
	resolve := func(model, effort string) (provider.Provider, *provider.Pricing, int, error) {
		return &mockProvider{name: "resolved-" + model}, pricing, 8192, nil
	}

	cases := []struct {
		name string
		opts TaskToolOptions
	}{
		{
			name: "empty-sys-prompt-defaults",
			opts: TaskToolOptions{
				Provider:       prov,
				ParentRegistry: reg,
				MaxSteps:       20,
			},
		},
		{
			name: "zero-value-config",
			opts: TaskToolOptions{
				Provider:       prov,
				ParentRegistry: reg,
			},
		},
		{
			name: "non-empty-gate-and-overrides",
			opts: TaskToolOptions{
				Provider:            prov,
				Pricing:             pricing,
				ParentRegistry:      reg,
				MaxSteps:            12,
				ContextWindow:       64000,
				RecentKeep:          7,
				SoftCompactRatio:    0.55,
				ToolResultSnipRatio: 0.4,
				CompactRatio:        0.8,
				CompactForceRatio:   0.95,
				Temperature:         0.2,
				ArchiveDir:          t.TempDir(),
				SysPrompt:           "custom sub-agent prompt",
				Gate:                gate,
				KeepPolicy:          KeepErrors | KeepUserMarked,
				SubagentModel:       "deepseek-chat",
				SubagentEffort:      "high",
				ResolveProvider:     resolve,
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			legacy := NewTaskTool(
				tc.opts.Provider,
				tc.opts.Pricing,
				tc.opts.ParentRegistry,
				tc.opts.MaxSteps,
				tc.opts.ContextWindow,
				tc.opts.RecentKeep,
				tc.opts.SoftCompactRatio,
				tc.opts.ToolResultSnipRatio,
				tc.opts.CompactRatio,
				tc.opts.CompactForceRatio,
				tc.opts.Temperature,
				tc.opts.ArchiveDir,
				tc.opts.SysPrompt,
				tc.opts.Gate,
				tc.opts.KeepPolicy,
				tc.opts.SubagentModel,
				tc.opts.SubagentEffort,
				tc.opts.ResolveProvider,
			)
			modern := NewTaskToolWithOptions(tc.opts)
			assertTaskToolConfigEqual(t, legacy, modern)
			if !reflect.DeepEqual(legacy.Schema(), modern.Schema()) {
				t.Fatalf("schema mismatch:\nlegacy=%s\nmodern=%s", legacy.Schema(), modern.Schema())
			}
			if legacy.Name() != modern.Name() || legacy.Description() != modern.Description() || legacy.ReadOnly() != modern.ReadOnly() {
				t.Fatalf("tool identity mismatch: name=%q/%q desc-len=%d/%d readOnly=%v/%v",
					legacy.Name(), modern.Name(), len(legacy.Description()), len(modern.Description()), legacy.ReadOnly(), modern.ReadOnly())
			}
		})
	}
}

func TestNewTaskToolWithOptionsEmptySysPromptUsesDefault(t *testing.T) {
	task := NewTaskToolWithOptions(TaskToolOptions{
		Provider:       &mockProvider{name: "sub"},
		ParentRegistry: tool.NewRegistry(),
		MaxSteps:       5,
		SysPrompt:      "",
	})
	if task.sysPrompt != DefaultTaskSystemPrompt {
		t.Fatalf("sysPrompt = %q, want DefaultTaskSystemPrompt", task.sysPrompt)
	}
}

func TestNewTaskToolWithOptionsAndLegacyExecuteEquivalence(t *testing.T) {
	chunks := []provider.Chunk{
		{Type: provider.ChunkText, Text: "options-equivalent-answer"},
		{Type: provider.ChunkDone},
	}
	legacyProv := &mockProvider{name: "sub", chunks: chunks}
	modernProv := &mockProvider{name: "sub", chunks: append([]provider.Chunk(nil), chunks...)}
	reg := tool.NewRegistry()
	sys := "sys-for-equivalence"
	storeDir := t.TempDir()
	workspace := t.TempDir()

	legacy := NewTaskTool(legacyProv, nil, reg, 20, 0, 0, 0, 0, 0, 0, 0.0, "", sys, nil, 0, "", "", nil).
		WithTranscripts(NewSubagentStore(storeDir), workspace, "base-model", "base-effort")
	modern := NewTaskToolWithOptions(TaskToolOptions{
		Provider:       modernProv,
		ParentRegistry: reg,
		MaxSteps:       20,
		SysPrompt:      sys,
	}).WithTranscripts(NewSubagentStore(storeDir), workspace, "base-model", "base-effort")

	legacyOut, legacyErr := legacy.Execute(testTaskContext(), []byte(`{"prompt":"equivalence prompt"}`))
	modernOut, modernErr := modern.Execute(testTaskContext(), []byte(`{"prompt":"equivalence prompt"}`))
	if legacyErr != nil || modernErr != nil {
		t.Fatalf("Execute errors: legacy=%v modern=%v", legacyErr, modernErr)
	}
	// Transcript refs differ by id; compare semantic body and system prompt routing.
	if !strings.Contains(legacyOut, "options-equivalent-answer") || !strings.Contains(modernOut, "options-equivalent-answer") {
		t.Fatalf("final answers missing:\nlegacy=%q\nmodern=%q", legacyOut, modernOut)
	}
	if legacySys := legacyProv.lastReq.Messages[0].Content; legacySys != sys {
		t.Fatalf("legacy system prompt = %q, want %q", legacySys, sys)
	}
	if modernSys := modernProv.lastReq.Messages[0].Content; modernSys != sys {
		t.Fatalf("modern system prompt = %q, want %q", modernSys, sys)
	}
	if !strings.Contains(lastUser(legacyProv.lastReq), "equivalence prompt") ||
		!strings.Contains(lastUser(modernProv.lastReq), "equivalence prompt") {
		t.Fatalf("user prompts not routed:\nlegacy=%q\nmodern=%q", lastUser(legacyProv.lastReq), lastUser(modernProv.lastReq))
	}
}

func TestNewTaskToolWithOptionsProviderResolverAndOverrides(t *testing.T) {
	base := &mockProvider{name: "base"}
	resolved := &mockProvider{name: "resolved-child"}
	pricing := &provider.Pricing{Input: 3}
	var sawModel, sawEffort string
	resolve := func(model, effort string) (provider.Provider, *provider.Pricing, int, error) {
		sawModel, sawEffort = model, effort
		return resolved, pricing, 4096, nil
	}
	task := NewTaskToolWithOptions(TaskToolOptions{
		Provider:        base,
		ParentRegistry:  tool.NewRegistry(),
		MaxSteps:        8,
		SubagentModel:   "child-model",
		SubagentEffort:  "max",
		ResolveProvider: resolve,
	})
	gotProv, gotPrice, gotWin, err := task.resolveSubSessionRuntime("child-model", "max")
	if err != nil {
		t.Fatalf("resolveSubSessionRuntime: %v", err)
	}
	if gotProv != resolved || gotPrice != pricing || gotWin != 4096 {
		t.Fatalf("resolver result = (%v,%v,%d), want resolved pricing/window", gotProv.Name(), gotPrice, gotWin)
	}
	if sawModel != "child-model" || sawEffort != "max" {
		t.Fatalf("resolver args = (%q,%q), want child-model/max", sawModel, sawEffort)
	}
	if task.subagentModel != "child-model" || task.subagentEffort != "max" {
		t.Fatalf("stored overrides = (%q,%q)", task.subagentModel, task.subagentEffort)
	}
}

func assertTaskToolConfigEqual(t *testing.T, a, b *TaskTool) {
	t.Helper()
	if a.prov != b.prov {
		t.Fatalf("prov mismatch")
	}
	if a.pricing != b.pricing {
		t.Fatalf("pricing mismatch")
	}
	if a.parentReg != b.parentReg {
		t.Fatalf("parentReg mismatch")
	}
	if a.maxSteps != b.maxSteps || a.contextWindow != b.contextWindow || a.recentKeep != b.recentKeep {
		t.Fatalf("step/window/keep mismatch: %+v vs %+v",
			[3]int{a.maxSteps, a.contextWindow, a.recentKeep},
			[3]int{b.maxSteps, b.contextWindow, b.recentKeep})
	}
	if a.softCompactRatio != b.softCompactRatio || a.toolResultSnipRatio != b.toolResultSnipRatio ||
		a.compactRatio != b.compactRatio || a.compactForceRatio != b.compactForceRatio || a.temperature != b.temperature {
		t.Fatalf("ratio/temp mismatch")
	}
	if a.archiveDir != b.archiveDir || a.sysPrompt != b.sysPrompt || a.keepPolicy != b.keepPolicy {
		t.Fatalf("archive/sys/keep mismatch: archive=%q/%q sys=%q/%q keep=%v/%v",
			a.archiveDir, b.archiveDir, a.sysPrompt, b.sysPrompt, a.keepPolicy, b.keepPolicy)
	}
	if a.gate != b.gate {
		t.Fatalf("gate mismatch")
	}
	if a.subagentModel != b.subagentModel || a.subagentEffort != b.subagentEffort {
		t.Fatalf("model/effort mismatch: %q/%q vs %q/%q", a.subagentModel, a.subagentEffort, b.subagentModel, b.subagentEffort)
	}
	// Function pointers are compared by identity for the same options value.
	if reflect.ValueOf(a.resolveProvider).Pointer() != reflect.ValueOf(b.resolveProvider).Pointer() {
		t.Fatalf("resolveProvider identity mismatch")
	}
	if a.maxSubagentDepth != b.maxSubagentDepth {
		t.Fatalf("maxSubagentDepth = %d/%d", a.maxSubagentDepth, b.maxSubagentDepth)
	}
}
