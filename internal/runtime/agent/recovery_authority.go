package agent

import (
	"context"
	"encoding/json"
	"slices"

	"reasonix/internal/contract/tool"
	"reasonix/internal/platform/worktree"
)

func (a *Agent) authorizeRecoveryPaths(ctx context.Context, plan *toolCallPlan, paths []string) error {
	for _, path := range paths {
		if !slices.ContainsFunc(plan.declaredPaths, func(approved string) bool { return path == approved }) {
			return worktree.ErrRecoveryChanged
		}
		if a.svc.checkTargetAccess == nil {
			return worktree.ErrApplyRecovery
		}
		args, _ := json.Marshal(map[string]string{"path": path})
		if err := a.svc.checkTargetAccess(ctx, recoveryPathTarget{path: path}, args); err != nil {
			return err
		}
	}
	return nil
}

type recoveryPathTarget struct{ path string }

func (recoveryPathTarget) Name() string             { return "recover_integration" }
func (recoveryPathTarget) Description() string      { return "Recover a workspace path" }
func (recoveryPathTarget) Schema() json.RawMessage  { return json.RawMessage(`{"type":"object"}`) }
func (recoveryPathTarget) ReadOnly() bool           { return false }
func (recoveryPathTarget) WritesNamedPaths() bool   { return true }
func (recoveryPathTarget) RequiresWriteScope() bool { return true }
func (t recoveryPathTarget) WritePaths(json.RawMessage) ([]string, error) {
	return []string{t.path}, nil
}
func (t recoveryPathTarget) ReadPaths(context.Context, json.RawMessage) ([]string, error) {
	return []string{t.path}, nil
}
func (recoveryPathTarget) Execute(context.Context, json.RawMessage) (string, error) {
	return "", tool.Refusal{Code: "isolation.recovery_required", Message: "recovery authorization targets cannot execute"}
}
