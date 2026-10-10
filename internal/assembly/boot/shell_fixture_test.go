package boot

import "reasonix/internal/safety/sandbox"

func pinnedEffectShell() *sandbox.Shell {
	return &sandbox.Shell{Kind: sandbox.ShellBash, Path: "bash"}
}
