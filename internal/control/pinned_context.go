package control

import (
	"context"

	"reasonix/internal/agent"
	"reasonix/internal/provider"
)

type controllerPromptState struct {
	base string
}

// PinnedContextLoader resolves the sidecar-owned desired context for the
// controller's current session immediately before an admitted model turn.
// Implementations perform I/O without holding Controller or Session locks.
type PinnedContextLoader func(context.Context, string) (agent.PinnedContextSnapshot, error)

func newControllerPromptState(base string, executor *agent.Agent) controllerPromptState {
	current := ""
	if executor != nil && executor.Session() != nil {
		messages := executor.Session().Snapshot()
		if len(messages) > 0 && messages[0].Role == provider.RoleSystem {
			current = messages[0].Content
			if base == "" {
				base = current
			}
		}
	}
	state := controllerPromptState{base: base}
	if executor != nil && executor.Session() != nil && state.base != current {
		executor.Session().SetLeadingSystemPromptWithReason(state.base, "legacy_pinned_system_migration")
	}
	return state
}

// ApplyExtensionSystemPrompt replaces only the host/extension-owned base
// prompt. Pinned context is transcript state and is never composed into it.
func (c *Controller) ApplyExtensionSystemPrompt(prompt string) {
	if c == nil || c.executor == nil {
		return
	}
	c.mu.Lock()
	c.prompt.base = prompt
	c.mu.Unlock()
	c.executor.SetSession(agent.NewSession(prompt))
}

func (c *Controller) basePrompt() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.prompt.base
}

// SystemPrompt returns the authoritative host/extension system prompt.
func (c *Controller) SystemPrompt() string {
	if c == nil {
		return ""
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.prompt.base
}

// runModelTurn is the sole controller-to-runner entry point. The controller's
// turn gate is already held, so its session path cannot rotate while the loader
// reads the sidecar. StagePinnedContext only mutates Agent host state; the
// revision is appended atomically with the real user message after
// agent.before_start accepts the turn.
func (c *Controller) runModelTurn(ctx context.Context, input string) error {
	if c == nil || c.runner == nil {
		return nil
	}
	if c.goals.active() {
		ctx = agent.WithContinuationPolicy(ctx, agent.ContinuationExplicitFlow)
	}
	if c.pinnedContextLoader != nil && c.executor != nil {
		snapshot, err := c.pinnedContextLoader(ctx, c.SessionPath())
		if err != nil {
			return err
		}
		if err := c.executor.StagePinnedContext(snapshot); err != nil {
			return err
		}
	}
	return c.runner.Run(ctx, input)
}
