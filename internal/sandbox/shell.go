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
	return resolveShell(prefer, path, warn, runtime.GOOS, exec.LookPath, fileExists, windowsBashCandidates(), windowsPowerShellCandidates(), probeBash, isWindowsWSLBash)
}

// resolveShell is ResolveShell with its environment lookups injected — including
// the Git-for-Windows bash candidates, which derive from %ProgramFiles% and so
// are empty off Windows — so the decision table is deterministically testable on
// any host.
func resolveShell(prefer, path string, warn io.Writer, goos string, lookPath func(string) (string, error), exists func(string) bool, winBashCandidates []string, winPowerShellCandidates []string, probe func(string) bool, isWSL func(string) bool) Shell {
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
			for _, p := range winPowerShellCandidates {
				base := strings.ToLower(pathBase(p))
				if base != strings.ToLower(name) && strings.TrimSuffix(base, ".exe") != strings.ToLower(name) {
					continue
				}
				if exists(p) {
					return Shell{Kind: ShellPowerShell, Path: p}, true
				}
			}
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

func pathBase(p string) string {
	if i := strings.LastIndexAny(p, `/\\`); i >= 0 {
		return p[i+1:]
	}
	return p
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

// windowsPowerShellCandidates lists common PowerShell executables that are not
// always present on PATH, especially PowerShell 7's default MSI install path.
func windowsPowerShellCandidates() []string {
	var roots []string
	for _, env := range []string{"ProgramFiles", "ProgramW6432", "ProgramFiles(x86)"} {
		if v := os.Getenv(env); v != "" {
			roots = append(roots, v)
		}
	}
	var out []string
	for _, r := range roots {
		out = append(out, filepath.Join(r, "PowerShell", "7", "pwsh.exe"))
	}
	if v := os.Getenv("SystemRoot"); v != "" {
		out = append(out, filepath.Join(v, "System32", "WindowsPowerShell", "v1.0", "powershell.exe"))
	} else if v := os.Getenv("windir"); v != "" {
		out = append(out, filepath.Join(v, "System32", "WindowsPowerShell", "v1.0", "powershell.exe"))
	}
	return out
}

// normalizeNulRedirect rewrites null-device redirects in command to sink
// ("/dev/null" for bash, "$null" for PowerShell). On PowerShell and
// bash-on-Windows the targets "nul", "null", and "/dev/null" are ordinary
// filenames, so a bare `2>nul` would otherwise create a stray file. #4252.
//
// This is a small quote-aware scanner, not a full shell parser: it skips
// quoted regions (so `echo "2>nul"` is left alone) and rewrites redirect
// operators (`>`, `>>`, `N>`, `&>`, `*>`) whose target is a null-device name.
func normalizeNulRedirect(command, sink string) string {
	var out strings.Builder
	copied := 0
	for i := 0; i < len(command); {
		if command[i] == '\'' || command[i] == '"' {
			i = skipQuoted(command, i)
			continue
		}
		opStart, opEnd, targetEnd, ok := nullRedirectAt(command, i)
		if !ok {
			i++
			continue
		}
		out.WriteString(command[copied:opStart])
		out.WriteString(command[opStart:opEnd])
		out.WriteString(sink)
		copied = targetEnd
		i = targetEnd
	}
	if copied == 0 {
		return command
	}
	out.WriteString(command[copied:])
	return out.String()
}

// skipQuoted advances past a quoted region beginning at the quote byte s[i]
// and returns the index after the closing quote. Double quotes honour bash
// backslash/backtick escapes; single quotes are literal except for `”`.
func skipQuoted(s string, i int) int {
	quote := s[i]
	for i++; i < len(s); i++ {
		if quote == '"' && (s[i] == '\\' || s[i] == '`') && i+1 < len(s) {
			i++
			continue
		}
		if quote == '\'' && i+1 < len(s) && s[i] == '\'' && s[i+1] == '\'' {
			i++
			continue
		}
		if s[i] == quote {
			return i + 1
		}
	}
	return len(s)
}

// nullRedirectAt reports whether a null-device redirect begins at s[i]. If so
// it returns the operator span [opStart, opEnd) and the end of the target
// token; otherwise ok is false.
func nullRedirectAt(s string, i int) (opStart, opEnd, targetEnd int, ok bool) {
	opStart = i
	prefixLen := redirectOpPrefixLen(s, i)
	if prefixLen == noRedirectPrefix {
		return 0, 0, 0, false
	}
	i += prefixLen
	if i >= len(s) || s[i] != '>' {
		return 0, 0, 0, false
	}
	i++
	if i < len(s) && s[i] == '>' {
		i++
	}
	opEnd = i
	for i < len(s) && isShellSpace(s[i]) {
		i++
	}
	target, end, found := redirectTarget(s, i)
	if !found || !isNullDeviceTarget(target) {
		return 0, 0, 0, false
	}
	return opStart, opEnd, end, true
}

const noRedirectPrefix = -1

// redirectOpPrefixLen returns the length of the prefix before the `>` at s[i]
// (0 for `>`/`&>`/`*>`, or the digit count for an fd prefix like `2>`).
// noRedirectPrefix if s[i] starts no recognised prefix.
func redirectOpPrefixLen(s string, i int) int {
	if i >= len(s) {
		return noRedirectPrefix
	}
	switch s[i] {
	case '>', '&', '*':
		return 0
	}
	if s[i] >= '0' && s[i] <= '9' {
		n := 0
		for i+n < len(s) && s[i+n] >= '0' && s[i+n] <= '9' {
			n++
		}
		return n
	}
	return noRedirectPrefix
}

// redirectTarget extracts the redirect target token at s[i], stripping one
// layer of quotes if present, and returns it with the index past the token.
func redirectTarget(s string, i int) (token string, end int, ok bool) {
	if i >= len(s) {
		return "", i, false
	}
	if s[i] == '\'' || s[i] == '"' {
		quote := s[i]
		start := i + 1
		end := skipQuoted(s, i)
		if end <= start || end > len(s) || s[end-1] != quote {
			return "", i, false
		}
		return s[start : end-1], end, true
	}
	start := i
	for i < len(s) && !isRedirectTargetDelimiter(s[i]) {
		i++
	}
	if i == start {
		return "", i, false
	}
	return s[start:i], i, true
}

// isNullDeviceTarget recognizes the null-device spellings the model commonly emits.
func isNullDeviceTarget(token string) bool {
	return strings.EqualFold(token, "nul") ||
		strings.EqualFold(token, "null") ||
		strings.EqualFold(token, "/dev/null")
}

func isRedirectTargetDelimiter(c byte) bool {
	return isShellSpace(c) || strings.ContainsRune(";&|<>)`", rune(c))
}

func isShellSpace(c byte) bool {
	switch c {
	case ' ', '\t', '\r', '\n':
		return true
	default:
		return false
	}
}

// argv builds the exec argv that runs command under this shell.
func (s Shell) argv(command string) []string {
	path := s.Path
	if path == "" {
		path = s.Kind.String()
	}
	if s.Kind == ShellPowerShell {
		return []string{path, "-NoProfile", "-NonInteractive", "-Command", psUTF8Prologue + normalizeNulRedirect(command, "$null")}
	}
	return []string{path, "-c", normalizeNulRedirect(command, "/dev/null")}
}

// SupportsChaining reports whether the shell parses '&&' / '||'. bash does;
// Windows PowerShell 5.1 (powershell.exe) does not — only PowerShell 7+ (pwsh).
func (s Shell) SupportsChaining() bool {
	if s.Kind != ShellPowerShell {
		return true
	}
	base := strings.ToLower(pathBase(s.Path))
	return base == "pwsh" || base == "pwsh.exe"
}
