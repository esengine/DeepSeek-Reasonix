//go:build windows

package sandbox

import (
	"context"
	"os"
	"os/exec"
	"sync"

	"reasonix/internal/base/proc"
	"reasonix/internal/base/secrets"
)

// BashConfine is what ShellDiscovery.Confine asks of a sandboxed session: whether a
// bash starts under the restricted token. Git for Windows' bash does not: it
// exits at start with "couldn't create signal pipe, Win32 error 5". It is nil
// unless mode enforces, and true when no token can be made at all, since enforce
// mode then refuses every command by itself.
func BashConfine(mode, stateDir string) func(string) bool {
	if mode != "enforce" {
		return nil
	}
	var once sync.Once
	var spec Spec
	var ready bool
	var memo sync.Map
	return func(path string) bool {
		once.Do(func() {
			book, err := windowsBook(stateDir)
			if err != nil || probeToken(book.SID()) != nil {
				return
			}
			spec, ready = Spec{Mode: "enforce", GrantSID: book.SID()}, true
		})
		if !ready {
			return true
		}
		fi, err := os.Stat(path)
		if err != nil {
			return false
		}
		id := bashProbeIdentity{path: path, size: fi.Size(), modTime: fi.ModTime().UnixNano()}
		if v, ok := memo.Load(id); ok {
			return v.(bool)
		}
		argv, wrapped := winWrap(spec, Shell{Kind: ShellBash, Path: path}.argv("true"))
		ok := !wrapped || startsConfined(argv)
		memo.Store(id, ok)
		return ok
	}
}

func startsConfined(argv []string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), bashProbeTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.WaitDelay = probeWaitDelay
	cmd.Env = secrets.ProcessEnv()
	proc.HideWindow(cmd)
	return cmd.Run() == nil
}
