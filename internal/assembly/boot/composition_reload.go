package boot

import "reasonix/internal/session/control"

func inheritComposition(old *control.Controller, opts Options) Options {
	if opts.Composition == nil && opts.AgentComposition == "" {
		opts.Composition = old.CompositionBinding()
	}
	if opts.Composition != nil {
		opts.ForceFullRebuild = true
	}
	return opts
}
