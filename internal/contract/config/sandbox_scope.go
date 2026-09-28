package config

import (
	"maps"
	"slices"

	"github.com/BurntSushi/toml"
)

// heldUserGlobals is the user's sandbox grants and shell env, held across the
// project merge. The slices are copies: decoding the project file writes into
// the arrays the user's decode left behind, so a shared one would read back the
// repo's list.
type heldUserGlobals struct {
	hostAuthorities, allowedDomains, deniedDomains []string
	shellEnv                                       map[string]string
}

func holdUserGlobals(s SandboxConfig, shellEnv map[string]string) heldUserGlobals {
	return heldUserGlobals{
		hostAuthorities: slices.Clone(s.HostAuthorities),
		allowedDomains:  slices.Clone(s.AllowedDomains),
		deniedDomains:   slices.Clone(s.DeniedDomains),
		shellEnv:        maps.Clone(shellEnv),
	}
}

// restore puts back what a repo may not change. A cloned repo must not hand
// itself the container daemon socket, nor inject variables into every command
// the agent runs. An egress allow list only narrows open network, so a repo may
// set one the user has not, but never replace the user's; its denials only add
// to the user's.
func (h heldUserGlobals) restore(cfg *Config, projectMeta toml.MetaData) {
	s := &cfg.Sandbox
	s.HostAuthorities = h.hostAuthorities
	if len(h.allowedDomains) > 0 {
		s.AllowedDomains = h.allowedDomains
	}
	denied := h.deniedDomains
	for _, d := range s.DeniedDomains {
		if !slices.Contains(denied, d) {
			denied = append(denied, d)
		}
	}
	s.DeniedDomains = denied
	cfg.Tools.Shell.Env = h.shellEnv
	warnShellEnvProblems(cfg, projectMeta)
}
