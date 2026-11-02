package boot

import (
	"reasonix/internal/contract/config"
	"reasonix/internal/session/team"
	"reasonix/internal/state/instruction"
	"reasonix/internal/state/memory"
)

// memoryAssembly is the memory surface for one build: the live set the remember
// tools write through, the host checks and sensitive paths its docs declare,
// and the prompt with the memory block folded in.
type memoryAssembly struct {
	set       *memory.Set
	checks    []instruction.VerifyCheck
	sensitive []string
	sysPrompt string
}

// buildMemoryAssembly loads memory and composes it into the prompt. A rebuild
// that reuses discovery skips the read: it also reuses the composed prompt, so
// re-reading the docs would only produce a block nothing goes on to use.
func buildMemoryAssembly(opts Options, cfg *config.Config, root, sysPrompt string) memoryAssembly {
	if opts.ReuseAssembly != nil && shouldReuseDiscovery(opts.PreviousPlan) && opts.ReuseAssembly.Memory != nil {
		return memoryAssembly{
			set:       opts.ReuseAssembly.Memory,
			checks:    opts.ReuseAssembly.ProjectChecks,
			sensitive: opts.ReuseAssembly.ProjectSensitivePaths,
			sysPrompt: sysPrompt,
		}
	}
	set := memory.Load(memory.Options{
		CWD: root, UserDir: opts.roots().MemoryUserDir(),
		PinnedBudgetChars: cfg.Memory.PinnedBudgetChars,
		RecallLimit:       cfg.Memory.RecallLimit,
		RecallMaxChars:    cfg.Memory.RecallMaxChars,
		FactsDir:          agentMemoryDir(opts),
	})
	return memoryAssembly{
		set:       set,
		checks:    instruction.ExtractHostChecks(set.Docs),
		sensitive: instruction.ExtractSensitivePaths(set.Docs),
		sysPrompt: memory.Compose(sysPrompt, set),
	}
}

// agentMemoryDir is where an agent on the user's team keeps its facts: one
// place for its own chat and every group seat, whatever folder each works in.
// Empty for an ordinary session, which keeps its project's.
func agentMemoryDir(opts Options) string {
	if opts.TeamSeat == nil {
		return ""
	}
	_, ref := opts.TeamSeat.Team()
	return team.AgentMemoryDir(opts.roots().TeamDir(), ref.Agent)
}
