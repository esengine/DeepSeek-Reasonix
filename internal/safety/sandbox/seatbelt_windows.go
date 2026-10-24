//go:build windows

package sandbox

import (
	"os"
	"strings"
)

// Command wraps the shell invocation to run under the write-restricted token
// when spec asks for enforcement and names the SID the roots were granted to;
// otherwise it returns the invocation unwrapped, and a caller in enforce mode
// treats that as refused.
func Command(spec Spec, sh Shell, command string) ([]string, bool) {
	return winWrap(spec, sh.argv(command))
}

// CommandArgs is like Command but accepts the command as raw argv instead of a
// shell command string.
func CommandArgs(spec Spec, args []string) ([]string, bool) {
	return winWrap(spec, args)
}

// winWrap routes argv through this binary's launcher entry.
func winWrap(spec Spec, argv []string) ([]string, bool) {
	if refusesCommand(spec) {
		return nil, false
	}
	if spec.Mode != "enforce" || spec.GrantSID == "" || len(argv) == 0 {
		return argv, false
	}
	self, err := os.Executable()
	if err != nil {
		return argv, false
	}
	env := map[string]string{}
	for _, kv := range SessionTempEnv(spec.SessionTemp, false) {
		k, v, _ := strings.Cut(kv, "=")
		env[k] = v
	}
	l := winLaunch{SID: spec.GrantSID, Env: env}
	if spec.SessionTemp != "" {
		l.Grant = []string{spec.SessionTemp}
	}
	return winLaunchArgv(self, l, argv), true
}

// Available reports that the restricted-token backend exists on Windows. Whether
// a launch is confined is the spec's: it needs GrantSpec to have prepared it.
func Available() bool {
	return true
}

// EgressSupported reports whether this platform can confine egress to the
// egress proxy: Windows has no OS-level bash sandbox.
func EgressSupported() bool { return false }

// EgressNeedsSocket reports whether the proxy must also listen on a Unix socket.
func EgressNeedsSocket() bool { return false }

// bwrapFaultFor names why an enforcing spec cannot be confined: the fault
// GrantSpec recorded, or that nothing prepared it. A spec that asks for nothing has none.
func bwrapFaultFor(spec Spec) error {
	if spec.Mode != "enforce" {
		return nil
	}
	if spec.GrantFault != nil {
		return spec.GrantFault
	}
	if spec.GrantSID == "" {
		return ErrWinNotPrepared
	}
	return nil
}
