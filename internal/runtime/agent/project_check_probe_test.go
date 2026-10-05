package agent

import (
	"context"
	"strings"
	"testing"

	"reasonix/internal/state/sessionstore"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
	"reasonix/internal/safety/evidence"
	"reasonix/internal/state/instruction"
)

type projectCheckProbeSink struct {
	probes []event.ProjectCheckProbe
}

func (s *projectCheckProbeSink) Emit(event.Event) {}

func (s *projectCheckProbeSink) RecordProjectCheckProbe(p event.ProjectCheckProbe) {
	s.probes = append(s.probes, p)
}

func (s *projectCheckProbeSink) last(t *testing.T) event.ProjectCheckProbe {
	t.Helper()
	if len(s.probes) == 0 {
		t.Fatal("no project-check probe recorded")
	}
	return s.probes[len(s.probes)-1]
}

func classOf(p event.ProjectCheckProbe, identity string) string {
	for _, d := range p.Diffs {
		if d.Identity == identity {
			return d.Class
		}
	}
	return ""
}

// rewriteDeclarationRun drives a delivery turn that writes, is blocked owing
// the check the task began under, then rewrites its own declaration and runs
// the replacement instead.
func rewriteDeclarationRun(t *testing.T, began, rewrittenTo, ran string) (*projectCheckProbeSink, error) {
	t.Helper()
	reg := tool.NewRegistry()
	reg.Add(fakeTool{name: "write_file", readOnly: false, writesPaths: true})
	reg.Add(fakeTool{name: "bash", readOnly: false})
	prov := &scriptedProvider{name: "p", turns: [][]provider.Chunk{
		{
			toolCallChunk("c1", "write_file", `{"path":"changed.go","content":"package main"}`),
			{Type: provider.ChunkDone},
		},
		{{Type: provider.ChunkText, Text: "premature"}, {Type: provider.ChunkDone}},
		{
			toolCallChunk("c2", "bash", `{"command":"`+ran+`"}`),
			{Type: provider.ChunkDone},
		},
		{{Type: provider.ChunkText, Text: "done"}, {Type: provider.ChunkDone}},
	}}
	sink := &projectCheckProbeSink{}
	a := New(prov, reg, sessionstore.NewSession(""), Options{
		ProjectChecks: []instruction.VerifyCheck{{Command: began, SourcePath: "AGENTS.md", Line: 3}},
	}, sink)
	ctx := deliveryGoalContext("goal-probe", "edit and finish")

	if err := a.Run(ctx, "edit and finish"); !readinessBlocked(err) {
		t.Fatalf("premature Run err = %v, want FinalReadinessError", err)
	}
	// This models a resume, not a mid-run edit: projectChecks is loaded once at
	// boot, so the divergence needs a process boundary. That the boundary is
	// real is established separately, by the Goal resume test in control.
	a.projectChecks = []instruction.VerifyCheck{{Command: rewrittenTo, SourcePath: "AGENTS.md", Line: 3}}
	return sink, a.Run(ctx, "finish")
}

// A criterion the task began under cannot be retired by the declaration that
// required it changing underneath: the gate still owes it, and the probe names
// the disagreement with the current declaration as baseline preservation.
func TestProjectCheckBaselineSurvivesARewrittenDeclaration(t *testing.T) {
	const began, replacement = "go test ./...", "go test ./internal/foo"
	sink, err := rewriteDeclarationRun(t, began, replacement, replacement)

	if !readinessBlocked(err) {
		t.Fatalf("finalization err = %v, want the baseline criterion to block", err)
	}
	if !strings.Contains(err.Error(), began) {
		t.Fatalf("readiness error %q does not name the baseline criterion %q", err, began)
	}
	probe := sink.last(t)
	if !probe.LegacyBlocked || !probe.CandidateBlocked {
		t.Fatalf("both derivations must owe the baseline criterion: %+v", probe)
	}
	baseline := evidence.VerificationIdentity(began)
	if got := classOf(probe, baseline); got != event.ProjectCheckBaselinePreservation {
		t.Fatalf("class for %q = %q, want %q: %+v", baseline, got, event.ProjectCheckBaselinePreservation, probe)
	}
	if classOf(probe, evidence.VerificationIdentity(replacement)) != "" {
		t.Fatalf("the check that ran is not a disagreement: %+v", probe)
	}
}

// Holding the baseline must not turn into a deadlock: once both the criterion
// the task began under and the one declared now have run, the turn finalizes.
func TestProjectCheckBaselineAndCurrentBothRunFinalizes(t *testing.T) {
	const began, replacement = "go test ./...", "go test ./internal/foo"
	_, err := rewriteDeclarationRun(t, began, replacement, began+" && "+replacement)
	if err != nil {
		t.Fatalf("finalization err = %v, want both criteria satisfied to finalize", err)
	}
}

// Outside a delivery scope the checkpoint is left over from whatever restored
// it, so its baseline is not this turn's to owe.
func TestProjectCheckBaselineIgnoredOutsideItsScope(t *testing.T) {
	const began, replacement = "go test ./...", "go test ./internal/foo"
	reg := tool.NewRegistry()
	reg.Add(fakeTool{name: "write_file", readOnly: false, writesPaths: true})
	reg.Add(fakeTool{name: "bash", readOnly: false})
	prov := &scriptedProvider{name: "p", turns: [][]provider.Chunk{
		{
			toolCallChunk("c1", "write_file", `{"path":"changed.go","content":"package main"}`),
			{Type: provider.ChunkDone},
		},
		{
			toolCallChunk("c2", "bash", `{"command":"`+replacement+`"}`),
			{Type: provider.ChunkDone},
		},
		{{Type: provider.ChunkText, Text: "done"}, {Type: provider.ChunkDone}},
	}}
	a := New(prov, reg, sessionstore.NewSession(""), Options{
		ProjectChecks: []instruction.VerifyCheck{{Command: replacement, SourcePath: "AGENTS.md", Line: 3}},
	}, &projectCheckProbeSink{})
	a.RestoreDeliveryCheckpoint(evidence.DeliveryCheckpoint{ScopeID: "goal-elsewhere", BaselineChecks: []string{began}})
	if err := a.Run(context.Background(), "edit"); err != nil {
		t.Fatalf("unscoped turn err = %v, want a restored goal's baseline not to bind it", err)
	}
}

// The other half: running the baseline does not excuse the criterion the
// project declares now. Both derivations owe it, so it is agreement, not a
// divergence — the probe must not read "candidate is stricter" into it.
func TestProjectCheckProbeAgreesOnTheNewDeclaration(t *testing.T) {
	const began, replacement = "go test ./...", "go test ./internal/foo"
	sink, _ := rewriteDeclarationRun(t, began, replacement, began)

	probe := sink.last(t)
	if !probe.LegacyBlocked || !probe.CandidateBlocked {
		t.Fatalf("both derivations must owe the current declaration: %+v", probe)
	}
	if probe.AgreedMissing != 1 || len(probe.Diffs) != 0 {
		t.Fatalf("agreed = %d diffs = %v, want 1 agreement and no divergence: %+v",
			probe.AgreedMissing, probe.Diffs, probe)
	}
}
