package sandbox

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"reasonix/internal/proc"
	"reasonix/internal/secrets"
)

// ResolvePowerShell finds the PowerShell interpreter for the dedicated
// powershell tool. prefer is "pwsh" (default — PowerShell 7+ first) or
// "powershell" (Windows PowerShell 5.1 first); path, when set, is an explicit
// executable that wins over discovery. Unlike ResolveShell there is no bash
// fallback: a host with no PowerShell returns the zero Shell and the caller
// surfaces a clear error, so the tool never silently runs under a different
// interpreter than its name promises. The result (including the not-found
// zero value) is memoized per process so the version probe runs once.
func ResolvePowerShell(prefer, path string, warn io.Writer) Shell {
	key := prefer + "\x00" + path
	pwshResolveMu.Lock()
	sh, ok := pwshResolveCache[key]
	pwshResolveMu.Unlock()
	if ok {
		return sh
	}

	sh = resolvePowerShell(prefer, path, warn, exec.LookPath, fileExists, windowsPowerShellCandidates(), probePowerShellMajor)

	pwshResolveMu.Lock()
	pwshResolveCache[key] = sh
	pwshResolveMu.Unlock()
	return sh
}

var (
	pwshResolveMu    sync.Mutex
	pwshResolveCache = map[string]Shell{}
)

// resolvePowerShell is ResolvePowerShell with its environment lookups injected
// — PATH lookup, file existence, the fixed install candidates (which derive
// from Windows-only env vars and so are empty elsewhere), and the version
// probe — so the decision table is deterministically testable on any host.
// Windows-ness is entirely captured by the candidate list, so no goos
// parameter is needed.
func resolvePowerShell(prefer, path string, warn io.Writer, lookPath func(string) (string, error), exists func(string) bool, candidates []string, probe func(string) int) Shell {
	// An explicit path wins outright: only file existence is validated (the
	// user pointed at it), and the version comes from the install layout or a
	// probe — unknown (0) is acceptable for a user-chosen binary. A missing
	// explicit path warns and falls through to discovery rather than leaving
	// the tool broken.
	if path != "" {
		if exists(path) {
			return Shell{Kind: ShellPowerShell, Path: path, MajorVersion: powerShellMajor(path, probe)}
		}
		if warn != nil {
			fmt.Fprintf(warn, "warning: [tools.powershell] path=%q does not exist; falling back to auto-discovery\n", path)
		}
	}

	// Two search lanes. The pwsh lane trusts the versioned install layout
	// (...\PowerShell\7\pwsh.exe is version-guaranteed — no probe subprocess)
	// and then PATH. The powershell lane probes each candidate for its real
	// major version; a candidate that cannot report one (store stub, wrapper
	// script, broken install) is rejected rather than trusted blindly.
	pwshLane := func() (Shell, bool) {
		for _, p := range candidates {
			if isVersionedPwsh7Install(p) && !isWindowsAppsStub(p) && exists(p) {
				return Shell{Kind: ShellPowerShell, Path: p, MajorVersion: 7}, true
			}
		}
		if p, err := lookPath("pwsh"); err == nil && !isWindowsAppsStub(p) {
			if major := probe(p); major > 0 {
				return Shell{Kind: ShellPowerShell, Path: p, MajorVersion: major}, true
			}
		}
		return Shell{}, false
	}
	powershellLane := func() (Shell, bool) {
		for _, p := range candidates {
			if isVersionedPwsh7Install(p) || isWindowsAppsStub(p) || !exists(p) {
				continue
			}
			if major := probe(p); major > 0 {
				return Shell{Kind: ShellPowerShell, Path: p, MajorVersion: major}, true
			}
		}
		if p, err := lookPath("powershell"); err == nil && !isWindowsAppsStub(p) {
			if major := probe(p); major > 0 {
				return Shell{Kind: ShellPowerShell, Path: p, MajorVersion: major}, true
			}
		}
		return Shell{}, false
	}

	lanes := []func() (Shell, bool){pwshLane, powershellLane}
	switch strings.ToLower(strings.TrimSpace(prefer)) {
	case "", "pwsh":
	case "powershell":
		lanes[0], lanes[1] = lanes[1], lanes[0]
	default:
		if warn != nil {
			fmt.Fprintf(warn, "warning: [tools.powershell] prefer=%q is not recognised (use pwsh/powershell); using the default order\n", prefer)
		}
	}
	for _, lane := range lanes {
		if sh, ok := lane(); ok {
			return sh
		}
	}

	if warn != nil {
		fmt.Fprintln(warn, "warning: no PowerShell interpreter found; install PowerShell 7 (https://aka.ms/powershell) or set [tools.powershell] path")
	}
	return Shell{}
}

// powerShellMajor determines a candidate's major version without spawning a
// process when the install layout guarantees it; otherwise it probes.
func powerShellMajor(path string, probe func(string) int) int {
	if isVersionedPwsh7Install(path) {
		return 7
	}
	return probe(path)
}

// isVersionedPwsh7Install reports whether path ends in ...\PowerShell\7\pwsh.exe
// (any separator style, case-insensitive). The versioned MSI layout guarantees
// PowerShell 7, so callers may skip the version probe for these paths.
func isVersionedPwsh7Install(path string) bool {
	segments := strings.FieldsFunc(strings.ToLower(path), func(r rune) bool {
		return r == '/' || r == '\\'
	})
	if len(segments) < 3 {
		return false
	}
	tail := segments[len(segments)-3:]
	return tail[0] == "powershell" && tail[1] == "7" && tail[2] == "pwsh.exe"
}

// isWindowsAppsStub reports whether path goes through the WindowsApps store
// alias directory. Those executables are 0-byte reparse points that fail to
// spawn (or open the Microsoft Store) unless the app is installed, so
// discovery must skip them in favor of a real install.
func isWindowsAppsStub(path string) bool {
	for _, segment := range strings.FieldsFunc(path, func(r rune) bool {
		return r == '/' || r == '\\'
	}) {
		if strings.EqualFold(segment, "WindowsApps") {
			return true
		}
	}
	return false
}

// probePowerShellMajor runs a candidate just long enough to report its major
// version, returning 0 when the executable cannot be spawned or does not
// answer with a number. The probe is timeout-bounded so a blocking stub can
// never wedge startup, and honors [secrets] filter_subprocess_env like every
// other subprocess Reasonix spawns.
func probePowerShellMajor(path string) int {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, "-NoProfile", "-NonInteractive", "-Command", "$PSVersionTable.PSVersion.Major")
	cmd.Env = secrets.ProcessEnv()
	proc.HideWindow(cmd)
	out, err := cmd.Output()
	if err != nil {
		return 0
	}
	major, err := strconv.Atoi(strings.TrimSpace(string(out)))
	if err != nil {
		return 0
	}
	return major
}
