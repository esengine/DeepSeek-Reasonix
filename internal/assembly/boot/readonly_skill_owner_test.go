package boot

import (
	"context"
	"encoding/json"
	"slices"
	"testing"

	"reasonix/internal/contract/config"
	"reasonix/internal/contract/provider"
	"reasonix/internal/ext/skill"
	"reasonix/internal/runtime/agent"
	"reasonix/internal/runtime/delegation"
	"reasonix/internal/runtime/writeclaim"
	"reasonix/internal/state/execjournal"
)

type readonlySkillOwnerProvider struct{ requests []provider.Request }

func (*readonlySkillOwnerProvider) Name() string { return "readonly-skill-owner" }
func (p *readonlySkillOwnerProvider) Stream(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	p.requests = append(p.requests, req)
	return (&skillOwnerProvider{}).Stream(ctx, req)
}

func TestSharedReadOnlySkillUsesUnifiedEphemeralExecution(t *testing.T) {
	for _, mode := range []string{"", "shared"} {
		t.Run("mode="+mode, func(t *testing.T) {
			runner, ctx, sessionPath, store := skillOwnerFixture(t, "readonly-skill-call")
			probe := &readonlySkillOwnerProvider{}
			runner.cfg.Agent.Collaboration = mode
			runner.registry.Add(matrixReadTool{})
			runner.provider = probe
			runner.entry = &config.ProviderEntry{ContextWindow: 8192}
			runner.maxDepth = 4
			runner.identity = func(_, _ string) (string, string) { return "base", "high" }
			runner.runOptions = func(context.Context, int, *provider.Pricing, int, int) agent.Options { return agent.Options{} }
			runner.tasks = delegation.NewTaskToolWithOptions(delegation.TaskToolOptions{Provider: probe, ParentRegistry: runner.registry, MaxSteps: 8}).
				WithTranscripts(store, runner.root, "base", "high").WithScheduler(writeclaim.NewSubagentScheduler(2, 2))
			sk := probeSkill()
			sk.AllowedTools = []string{"matrix_read"}
			for range 2 {
				if _, err := runner.runReadOnly(ctx, sk, "inspect the fixture", skill.SubagentRunOptions{}); err != nil {
					t.Fatal(err)
				}
			}
			if entries := execjournal.History(sessionPath); len(entries) != 0 {
				t.Fatal("ephemeral skill wrote lifecycle records")
			}
			if children := ownedChildren(t, store, sessionPath); len(children) != 0 {
				t.Fatal("ephemeral skill persisted a transcript")
			}
			if len(probe.requests) != 2 {
				t.Fatalf("unexpected child calls: %d", len(probe.requests))
			}
			for _, req := range probe.requests {
				completion := slices.ContainsFunc(req.Tools, func(t provider.ToolSchema) bool { return t.Name == "complete_subtask" })
				if completion != (mode != "") {
					t.Fatal("new read-only skill bypassed the unified completion protocol")
				}
				for _, schema := range req.Tools {
					if schema.Name != "matrix_read" && schema.Name != "complete_subtask" {
						t.Fatalf("expanded tool ceiling: %s", schema.Name)
					}
				}
			}
			first, _ := json.Marshal(probe.requests[0].Tools)
			second, _ := json.Marshal(probe.requests[1].Tools)
			if string(first) != string(second) {
				t.Fatal("tool prefix changed between runs")
			}
			if probe.requests[0].Messages[0].Content != probe.requests[1].Messages[0].Content {
				t.Fatal("system prefix changed between runs")
			}
		})
	}
}
