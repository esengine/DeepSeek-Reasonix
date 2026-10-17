package boot

import (
	"context"
	"fmt"
	"time"

	"reasonix/internal/contract/agentcomposition"
	"reasonix/internal/ext/plugin"
)

func (b *builder) bindCompositionMCPContracts(ctx context.Context) error {
	if b.opts.Composition == nil {
		return nil
	}
	binding := b.opts.Composition
	for _, spec := range append(append([]plugin.Spec{}, b.tools.mcp.configured...), b.tools.mcp.extra...) {
		call, cancel := context.WithTimeout(ctx, 30*time.Second)
		tools, err := b.tools.host.EnsureConnectedWithLifecycle(b.ctx, call, spec, 0)
		cancel()
		if err != nil {
			return fmt.Errorf("%w: MCP %s: %w", agentcomposition.ErrUnavailable, spec.Name, err)
		}
		addServerTools(b.tools.reg, b.tools.host, spec, tools)
		expected := ""
		if binding.Digest != "" {
			for _, r := range binding.Resources {
				if r.Kind == "mcp-contract" && r.ID == spec.Name {
					if err := jsonResource(r, &expected); err != nil {
						return err
					}
				}
			}
			if expected == "" {
				return fmt.Errorf("%w: MCP contract missing", agentcomposition.ErrUnavailable)
			}
		}
		hash, err := b.tools.host.PinCompositionTools(spec.Name, expected)
		if err != nil {
			return fmt.Errorf("%w: %w", agentcomposition.ErrUnavailable, err)
		}
		if binding.Digest == "" {
			if err := keepResource(binding, "mcp-contract", spec.Name, hash); err != nil {
				return err
			}
		}
	}
	return nil
}
