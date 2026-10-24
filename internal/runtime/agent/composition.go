package agent

import (
	"context"
	"fmt"
	"slices"

	"reasonix/internal/contract/agentcomposition"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
	"reasonix/internal/runtime/persona"
	"reasonix/internal/state/sessionstore"
)

type compositionContextKey struct{}

// CarryChildComposition transfers only the frozen binding, without host capabilities.
func CarryChildComposition(ctx, source context.Context) context.Context {
	b, _ := source.Value(compositionContextKey{}).(*agentcomposition.Binding)
	if b == nil {
		return ctx
	}
	return context.WithValue(ctx, compositionContextKey{}, agentcomposition.Clone(b))
}

func (a *Agent) compositionContext(ctx context.Context) context.Context {
	if b := a.Session().Composition(); b != nil {
		return context.WithValue(ctx, compositionContextKey{}, b)
	}
	return ctx
}

func restrictChildComposition(ctx context.Context, reg *tool.Registry) {
	b, _ := ctx.Value(compositionContextKey{}).(*agentcomposition.Binding)
	if b == nil || reg == nil {
		return
	}
	profile := reg.AllNames()
	selected := func(s agentcomposition.Selection, id string) bool {
		return s.Mode == agentcomposition.Inherit || slices.ContainsFunc(s.Members, func(m agentcomposition.Module) bool { return m.ID == id })
	}
	reg.Restrict(func(t tool.Tool) bool {
		if !slices.Contains(profile, t.Name()) {
			return false
		}
		if m, ok := t.(tool.MCPMetadata); ok {
			return selected(b.Resolved.MCP, m.MCPServerName())
		}
		if e, ok := t.(interface{ ExtensionPackageName() string }); ok {
			return selected(b.Resolved.Extensions, e.ExtensionPackageName())
		}
		return tool.ReachOf(t) == tool.ReachHostControl || selected(b.Resolved.Tools, t.Name())
	})
}

func ChildCompositionPrompt(ctx context.Context, base string) string {
	b, _ := ctx.Value(compositionContextKey{}).(*agentcomposition.Binding)
	if b == nil {
		return base
	}
	p := b.Resolved.Persona
	settings := persona.Settings{Prefix: p.Prefix, Suffix: p.Suffix, IncludeRuntimeContext: p.IncludeRuntimeContext}
	return persona.ApplySuffix(persona.ApplyPrefix(base, settings), settings)
}

// BindChildComposition is called before the first child provider request.
func BindChildComposition(ctx context.Context, sess *sessionstore.Session, decorate bool) error {
	b, _ := ctx.Value(compositionContextKey{}).(*agentcomposition.Binding)
	if b == nil {
		return nil
	}
	if old := sess.Composition(); old != nil {
		if old.Digest != b.Digest {
			return fmt.Errorf("%w: child composition differs from parent", agentcomposition.ErrUnavailable)
		}
		return nil
	}
	if decorate {
		messages := sess.Snapshot()
		for i := range messages {
			if messages[i].Role == provider.RoleSystem {
				messages[i].Content = ChildCompositionPrompt(ctx, messages[i].Content)
				break
			}
		}
		sess.Replace(messages)
	}
	sess.BindComposition(b)
	return nil
}
