package hook

import (
	"reasonix/internal/safety/sandbox"
	"reasonix/internal/state/sessiontemp"
	"strings"
)

// RuntimeOptions carries resolved host dependencies into Hook execution.
// It is runtime-only and never changes persisted Hook configuration.
type RuntimeOptions struct {
	BashPath    string
	Sandbox     *sandbox.Spec
	SessionTemp *sessiontemp.Manager
}

// RuntimeOptionsForShell carries an explicitly configured Bash path into Hook
// execution while leaving other interpreter preferences independent. A path
// that needs launcher arguments (wsl.exe) is the agent's shell tool alone:
// hooks are the user's own scripts, written for the host's shell, so they keep
// their own discovery.
func RuntimeOptionsForShell(prefer, path string, launcherArgs ...string) RuntimeOptions {
	if !strings.EqualFold(strings.TrimSpace(prefer), "bash") || len(launcherArgs) > 0 {
		return RuntimeOptions{}
	}
	return RuntimeOptions{BashPath: strings.TrimSpace(path)}
}
