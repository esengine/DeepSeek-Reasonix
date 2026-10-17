package cli

import (
	"reasonix/internal/contract/agentcomposition"
	"reasonix/internal/state/sessionstore"
)

func compositionCompletionFlag() cliCompletionFlag {
	return completionFlag("--agent-composition", cliCompletionStaticValue, "default", "builtin:general", "builtin:minimal")
}

func withComposition(opts cliBuildOverrides, name string, s *sessionstore.Session) cliBuildOverrides {
	opts.AgentComposition = freshCompositionName(name, s)
	opts.Composition = resumedComposition(s)
	return opts
}

func freshCompositionName(name string, s *sessionstore.Session) string {
	if s != nil {
		return ""
	}
	return name
}
func resumedComposition(s *sessionstore.Session) *agentcomposition.Binding {
	if s == nil {
		return nil
	}
	return s.Composition()
}
