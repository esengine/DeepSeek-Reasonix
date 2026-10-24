package delegation

import (
	"context"
	"encoding/json"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/runtime/agent"
	"reasonix/internal/runtime/checkenv"
	"reasonix/internal/runtime/writeclaim"
	"reasonix/internal/state/instruction"
)

// RunHostChecks uses the original grant and the same registry construction as
// delegated execution. It neither restores disabled tools nor starts a model.
func (t *TaskTool) RunHostChecks(ctx context.Context, spec ProfileExecSpec, checks []instruction.VerifyCheck, sink event.Sink, inputRoots ...string) ([]agent.HostExecutionResult, error) {
	if len(checks) == 0 {
		return nil, nil
	}
	executor, grant, err := t.prepareHostChecks(ctx, spec, checks, sink, inputRoots...)
	if err != nil {
		return nil, err
	}
	defer executor.Close()
	ctx = writeclaim.WithSubagentWriteGrant(ctx, grant)
	results := make([]agent.HostExecutionResult, 0, len(checks))
	for _, check := range checks {
		args, err := json.Marshal(map[string]any{"command": check.Command, "verifies": check.Command})
		if err != nil {
			return results, err
		}
		result := executor.Execute(ctx, provider.ToolCall{Name: "bash", Arguments: string(args)})
		results = append(results, result)
		if !result.VerificationSucceeded() {
			break
		}
	}
	return results, nil
}

func (t *TaskTool) prepareHostChecks(ctx context.Context, spec ProfileExecSpec, checks []instruction.VerifyCheck, sink event.Sink, inputRoots ...string) (*agent.HostExecutor, *writeclaim.WriteGrant, error) {
	release, err := t.admitPreparation(ctx, "host_verification")
	if err != nil {
		return nil, nil, err
	}
	defer release()
	depth, err := t.nextSubagentDepth(ctx)
	if err != nil {
		return nil, nil, err
	}
	grant := writeclaim.NewWriteGrant(spec.Grant.WritePaths)
	reg, err := t.childRegistry(&spec, depth, grant, spec.Worker.Model)
	if err != nil {
		return nil, nil, err
	}
	opts := t.subagentOptions(ctx, 0, nil, 0, depth, "host_verification", nil)
	opts.WriteWorkspaceRoot, opts.ObserveRoot = t.workspaceRoot, t.workspaceRoot
	if len(inputRoots) != 0 && inputRoots[0] != "" {
		opts.ObserveRoot = inputRoots[0]
	}
	opts.ProjectChecks = append([]instruction.VerifyCheck(nil), checks...)
	var environment *checkenv.Spec
	if t.checkEnvironment != nil {
		copy := *t.checkEnvironment
		copy.SourceRoot, copy.Directory = opts.ObserveRoot, t.workspaceRoot
		environment = &copy
	}
	return agent.NewHostExecutor(reg, opts, sink, environment), grant, nil
}

func (t *TaskTool) WithCheckEnvironment(spec *checkenv.Spec) *TaskTool {
	if spec != nil {
		copy := *spec
		t.checkEnvironment = &copy
	}
	return t
}
