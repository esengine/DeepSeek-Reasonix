package sandbox

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"reasonix/internal/proc"
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

// ResolveShell picks the interpreter the shell tool runs commands under. With
// prefer "auto"/"" it favours a real bash so the model's POSIX habits work and
// only falls back to PowerShell on Windows when bash is absent. prefer "bash" or
// "powershell"/"pwsh" forces that interpreter (path overrides the PATH lookup),
// warning to warn and falling back to auto-detection if the forced one is
// missing — so a typo or an uninstalled shell can never leave the tool broken.
func ResolveShell(prefer, path string, warn io.Writer) Shell {
	return resolveShell(prefer, path, warn, runtime.GOOS, exec.LookPath, fileExists, windowsBashCandidates(), probeBash, isWindowsWSLBash)
}

// resolveShell is ResolveShell with its environment lookups injected — including
// the Git-for-Windows bash candidates, which derive from %ProgramFiles% and so
// are empty off Windows — so the decision table is deterministically testable on
// any host.
func resolveShell(prefer, path string, warn io.Writer, goos string, lookPath func(string) (string, error), exists func(string) bool, winBashCandidates []string, probe func(string) bool, isWSL func(string) bool) Shell {
	findBash := func() (Shell, bool) {
		if p, err := lookPath("bash"); err == nil && !isWSL(p) && probe(p) {
			return Shell{Kind: ShellBash, Path: p}, true
		}
		for _, p := range winBashCandidates {
			if exists(p) && probe(p) {
				return Shell{Kind: ShellBash, Path: p}, true
			}
		}
		return Shell{}, false
	}
	findPowerShell := func(order []string) (Shell, bool) {
		for _, name := range order {
			if p, err := lookPath(name); err == nil {
				return Shell{Kind: ShellPowerShell, Path: p}, true
			}
		}
		return Shell{}, false
	}
	auto := func() Shell {
		if sh, ok := findBash(); ok {
			return sh
		}
		if goos == "windows" {
			if sh, ok := findPowerShell([]string{"pwsh", "powershell"}); ok {
				return sh
			}
		}
		return Shell{Kind: ShellBash, Path: "bash"}
	}

	switch strings.ToLower(strings.TrimSpace(prefer)) {
	case "", "auto":
		return auto()
	case "bash":
		if path != "" && exists(path) && probe(path) {
			return Shell{Kind: ShellBash, Path: path}
		}
		if sh, ok := findBash(); ok {
			return sh
		}
		warnMissingShell(warn, prefer)
		return auto()
	case "powershell", "pwsh":
		if path != "" && exists(path) {
			return Shell{Kind: ShellPowerShell, Path: path}
		}
		order := []string{"pwsh", "powershell"}
		if strings.EqualFold(strings.TrimSpace(prefer), "powershell") {
			order = []string{"powershell", "pwsh"}
		}
		if sh, ok := findPowerShell(order); ok {
			return sh
		}
		warnMissingShell(warn, prefer)
		return auto()
	default:
		if warn != nil {
			fmt.Fprintf(warn, "warning: [tools.shell] prefer=%q is not recognised (use auto/bash/powershell); using auto-detection\n", prefer)
		}
		return auto()
	}
}

func warnMissingShell(warn io.Writer, prefer string) {
	if warn != nil {
		fmt.Fprintf(warn, "warning: [tools.shell] prefer=%q but that shell was not found; using auto-detection\n", prefer)
	}
}

// isWindowsWSLBash reports whether a resolved bash path is the WSL launcher
// Windows ships under %SystemRoot% (e.g. C:\Windows\System32\bash.exe). With WSL
// installed it runs commands inside the Linux VM — where the Windows workspace is
// a /mnt/<drive> path — so it must never be chosen for a native Windows workspace;
// the only bash.exe Microsoft places under the Windows dir is that launcher.
func isWindowsWSLBash(path string) bool {
	if runtime.GOOS != "windows" || path == "" {
		return false
	}
	win := os.Getenv("SystemRoot")
	if win == "" {
		win = os.Getenv("windir")
	}
	if win == "" {
		return false
	}
	p := strings.ToLower(filepath.Clean(path))
	root := strings.ToLower(filepath.Clean(win)) + string(filepath.Separator)
	return strings.HasPrefix(p, root)
}

// Windows ships a bash.exe launcher stub in %SystemRoot% that opens the WSL
// install prompt instead of running anything, so confirm bash actually works
// before trusting it. Timeout-bounded in case the stub blocks on that prompt.
func probeBash(path string) bool {
	if runtime.GOOS != "windows" {
		return true
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, "-c", "true")
	proc.HideWindow(cmd)
	return cmd.Run() == nil
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
