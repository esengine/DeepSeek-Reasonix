package agent

import (
	"context"
	"encoding/json"
	"strings"

	"reasonix/internal/contract/provider"
	"reasonix/internal/runtime/checkenv"
	"reasonix/internal/safety/evidence"
	"reasonix/internal/state/instruction"
)

type baselineEvaluation struct {
	evidence evidence.BaselineEvidence
	inputs   evidence.ContentSnapshot
	root     string
}

type baselineEvaluationCache struct {
	epoch   uint64
	results map[string]baselineEvaluation
}

func sameBaselineInput(a, b evidence.ContentSnapshot) bool {
	return a.Complete && b.Complete && a.Scope == b.Scope && a.Digest != "" && a.Digest == b.Digest
}

func cloneCheckEnvironment(spec *checkenv.Spec) *checkenv.Spec {
	if spec == nil {
		return nil
	}
	copy := *spec
	copy.Client.Environment = append([]string(nil), spec.Client.Environment...)
	return &copy
}

func (a *Agent) baselineBackend() string {
	if a.processSandbox.RequiredIntegrity != "" {
		return "controlled_go_snapshot"
	}
	return baselineBackend
}

func (a *Agent) baselineInputRoot() string {
	if a.checkEnvironment != nil {
		return a.checkEnvironment.SourceRoot
	}
	return ""
}

func (a *Agent) runControlledBaseline(ctx context.Context, store *evidence.BaselineStore, criterion evidence.TestCriterion, path string) (evidence.CriterionResult, evidence.ContentSnapshot) {
	if a.checkEnvironment == nil {
		return evidence.CriterionUnavailable, evidence.ContentSnapshot{}
	}
	body, err := store.Open(criterion)
	if err != nil {
		return evidence.CriterionUnavailable, evidence.ContentSnapshot{}
	}
	names := evidence.EvaluableCriterionNames(body)
	if len(names) == 0 {
		return evidence.CriterionUnavailable, evidence.ContentSnapshot{}
	}
	command := "GOENV=off GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off CGO_ENABLED=0 GOCACHE=/tmp/go-build GOMODCACHE=/tmp/go-mod go test -json -count=1 '" + strings.ReplaceAll(packagePathOf(a.writeWorkspaceRoot, path), "'", "'\"'\"'") + "'"
	environment := a.checkEnvironment.WithCaptured(path, body)
	host := NewHostExecutor(a.svc.tools, a.baselineHostOptions(command), a.svc.sink, &environment)
	defer host.Close()
	args, _ := json.Marshal(map[string]string{"command": command, "verifies": command})
	result := host.Execute(ctx, provider.ToolCall{Name: "bash", Arguments: string(args)})
	inputs := result.Inputs
	if ctx.Err() != nil || !result.Executed || result.Blocked || result.RefusalCode != "" {
		return evidence.CriterionUnavailable, evidence.ContentSnapshot{}
	}
	if len(inputs) != 1 || !inputs[0].Completed || !inputs[0].Stable || len(inputs[0].Gaps) != 0 ||
		!inputs[0].HasLaunchIdentity(a.writeWorkspaceRoot) || !checkenv.VerifyCaptured(inputs[0], result.ExecutionID, path, body) {
		return evidence.CriterionUnavailable, evidence.ContentSnapshot{}
	}
	if result.CapturedResult == evidence.CriterionPass && !result.VerificationSucceeded() {
		return evidence.CriterionUnavailable, evidence.ContentSnapshot{}
	}
	ctx, release, err := a.holdHookExecution(ctx, true)
	if err != nil {
		return evidence.CriterionUnavailable, evidence.ContentSnapshot{}
	}
	defer release()
	current := evidence.CaptureContentSnapshot(ctx, a.checkEnvironment.SourceRoot, 50_000)
	if !sameBaselineInput(inputs[0].After, current) {
		return evidence.CriterionUnavailable, evidence.ContentSnapshot{}
	}
	return result.CapturedResult, current
}

func (a *Agent) baselineHostOptions(command string) Options {
	return Options{
		ProcessSandbox: a.processSandbox, Gate: a.svc.gate, CheckTargetAccess: a.svc.checkTargetAccess,
		ReadOnlyExecution: a.role.readOnlyExecution, PlannerMCPExecution: a.role.plannerMCPExecution,
		Hooks: a.svc.hooks, Extensions: a.svc.extensions, RecoveryGate: a.svc.recoveryGate,
		WriteWorkspaceRoot: a.writeWorkspaceRoot, ObserveRoot: a.observeRoot, ExtraWorkspaceRoots: a.extraRoots,
		WorkspaceLease: a.svc.workspaceLease, MutationCoordinator: a.svc.mutations, ExecutionCapacity: a.svc.capacity,
		ProjectChecks: []instruction.VerifyCheck{{Command: command}},
	}
}
