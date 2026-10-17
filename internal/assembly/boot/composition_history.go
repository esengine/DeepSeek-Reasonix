package boot

import (
	"context"
	"encoding/json"
	"fmt"

	"reasonix/internal/contract/agentcomposition"
	"reasonix/internal/contract/config"
	"reasonix/internal/contract/tool"
	"reasonix/internal/runtime/agent"
	"reasonix/internal/session/control"
	"reasonix/internal/state/sessionstore"
)

// BuildCompositionHistory opens history without discovering or starting any module.
func BuildCompositionHistory(opts Options, cause error) *BuildResult {
	reg := tool.NewRegistry()
	prompt := ""
	if opts.Composition != nil {
		prompt = opts.Composition.SystemPrompt
	}
	sess := sessionstore.NewSession(prompt)
	sess.BindComposition(opts.Composition)
	exec := agent.New(nil, reg, sess, agent.Options{CheckTargetAccess: denyCompositionHistoryAccess}, opts.Sink)
	ctrl := control.New(control.Options{Executor: exec, Runner: exec, Registry: reg, Sink: opts.Sink, WorkspaceRoot: opts.WorkspaceRoot, SessionDir: opts.SessionDir, ModelRef: opts.Model})
	home := opts.Home
	if home == "" {
		home = config.ReasonixHomeDir()
	}
	ctrl.ConfigureCompositions(home, nil)
	ctrl.BlockCompositionExecution(fmt.Errorf("%w: %w", agentcomposition.ErrUnavailable, cause))
	return &BuildResult{Controller: ctrl}
}

func denyCompositionHistoryAccess(context.Context, tool.Tool, json.RawMessage) error {
	return agentcomposition.ErrUnavailable
}
