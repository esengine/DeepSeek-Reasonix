package agent

import (
	"context"

	"reasonix/internal/navigator"
)

// navigatorBridge adapts the concrete *navigator.Navigator to the agent's
// NavigatorKernel interface. It is the only file in this package that imports
// the navigator package: the run loop and compact path talk to the bridge's
// string-based shape, so the agent stays decoupled from navigator types.
//
// The bridge translates the agent's (verb, args string) tool-call shape into
// navigator.HostAction and maps navigator.Correction back to the
// package-local CorrectionBrief.
type navigatorBridge struct {
	inner *navigator.Navigator
}

// NewNavigatorBridge wraps a navigator kernel for use as the agent's
// NavigatorKernel. A nil inner is not expected from boot (boot only sets the
// option when the navigator is enabled) but is safe: all methods no-op.
func NewNavigatorBridge(inner *navigator.Navigator) NavigatorKernel {
	if inner == nil {
		return nil
	}
	return &navigatorBridge{inner: inner}
}

func (b *navigatorBridge) ImplicitStateDigest() string {
	return b.inner.ImplicitStateDigest()
}

func (b *navigatorBridge) BeginAction(ctx context.Context, verb, args string) error {
	_, err := b.inner.BeginAction(ctx, navigator.HostAction{Verb: verb, Args: args})
	return err
}

func (b *navigatorBridge) EndAction(ctx context.Context, verb, args, output string, toolErr error) (CorrectionBrief, error) {
	corr, err := b.inner.EndAction(ctx,
		navigator.HostAction{Verb: verb, Args: args},
		navigator.HostResult{Output: output, Err: toolErr},
	)
	brief := CorrectionBrief{
		Strategy: correctionStrategyName(corr.Strategy),
		Reason:   corr.Reason,
	}
	for _, f := range corr.Reinject {
		brief.Reinject = append(brief.Reinject, f.Key+": "+f.Value)
	}
	return brief, err
}

func correctionStrategyName(s navigator.CorrectionStrategy) string {
	switch s {
	case navigator.StrategyContinue:
		return "continue"
	case navigator.StrategyReinjectFacts:
		return "reinject_facts"
	case navigator.StrategyRetry:
		return "retry"
	case navigator.StrategyRollback:
		return "rollback"
	case navigator.StrategyAskHost:
		return "ask_host"
	default:
		return "unknown"
	}
}
