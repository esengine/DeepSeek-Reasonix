package sandbox

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// psUTF8Prologue forces PowerShell to emit UTF-8 instead of the host's OEM code
// page (e.g. CP936 on a Chinese Windows), so non-ASCII command output and error
// text come back as valid UTF-8 rather than mojibake.
const psUTF8Prologue = "$OutputEncoding=[Console]::OutputEncoding=[System.Text.Encoding]::UTF8;"

// ShellKind is the interpreter a shell command runs under.
type ShellKind int

const (
	ShellBash ShellKind = iota
	ShellPowerShell
)

func (k ShellKind) String() string {
	if k == ShellPowerShell {
		return "powershell"
	}
	return "bash"
}

// Shell is the resolved interpreter the bash tool executes commands with: a kind
// (so callers can adapt prompts) and the executable to invoke.
type Shell struct {
	Kind ShellKind
	Path string
}

// ResolveShell picks the interpreter the shell tool runs commands under. It
// prefers a real bash so the model's POSIX habits work; on Windows, where bash
// is usually absent from PATH, it probes the Git-for-Windows install locations
// and only then falls back to PowerShell so the tool still functions.
func ResolveShell() Shell {
	return resolveShell(runtime.GOOS, exec.LookPath, fileExists, windowsBashCandidates())
}

// resolveShell is ResolveShell with its environment lookups injected — including
// the Git-for-Windows bash candidates, which derive from %ProgramFiles% and so
// are empty off Windows — so the decision table is deterministically testable on
// any host.
func resolveShell(goos string, lookPath func(string) (string, error), exists func(string) bool, winBashCandidates []string) Shell {
	if p, err := lookPath("bash"); err == nil {
		return Shell{Kind: ShellBash, Path: p}
	}
	if goos == "windows" {
		for _, p := range winBashCandidates {
			if exists(p) {
				return Shell{Kind: ShellBash, Path: p}
			}
		}
		for _, name := range []string{"pwsh", "powershell"} {
			if p, err := lookPath(name); err == nil {
				return Shell{Kind: ShellPowerShell, Path: p}
			}
		}
	}
	return Shell{Kind: ShellBash, Path: "bash"}
}

func fileExists(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && !fi.IsDir()
}

// windowsBashCandidates lists the bash.exe paths a Git-for-Windows install
// ships, across the usual program-files roots and a per-user install.
func windowsBashCandidates() []string {
	var roots []string
	for _, env := range []string{"ProgramFiles", "ProgramW6432", "ProgramFiles(x86)"} {
		if v := os.Getenv(env); v != "" {
			roots = append(roots, v)
		}
	}
	if v := os.Getenv("LOCALAPPDATA"); v != "" {
		roots = append(roots, filepath.Join(v, "Programs"))
	}
	var out []string
	for _, r := range roots {
		out = append(out,
			filepath.Join(r, "Git", "bin", "bash.exe"),
			filepath.Join(r, "Git", "usr", "bin", "bash.exe"),
		)
	}
	return out
}

// argv builds the exec argv that runs command under this shell.
func (s Shell) argv(command string) []string {
	path := s.Path
	if path == "" {
		path = s.Kind.String()
	}
	if s.Kind == ShellPowerShell {
		return []string{path, "-NoProfile", "-NonInteractive", "-Command", psUTF8Prologue + command}
	}
	return []string{path, "-c", command}
}

// SupportsChaining reports whether the shell parses '&&' / '||'. bash does;
// Windows PowerShell 5.1 (powershell.exe) does not — only PowerShell 7+ (pwsh).
func (s Shell) SupportsChaining() bool {
	if s.Kind != ShellPowerShell {
		return true
	}
	base := strings.ToLower(s.Path)
	if i := strings.LastIndexAny(base, `/\`); i >= 0 {
		base = base[i+1:] // Windows path; split on either separator off-Windows too
	}
	return base == "pwsh" || base == "pwsh.exe"
}
