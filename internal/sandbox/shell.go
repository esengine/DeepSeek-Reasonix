package sandbox

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"reasonix/internal/proc"
	"reasonix/internal/secrets"
)

// psUTF8Prologue forces PowerShell to emit UTF-8 instead of the host's OEM code
// page (e.g. CP936 on a Chinese Windows), so non-ASCII command output and error
// text come back as valid UTF-8 rather than mojibake.
const psUTF8Prologue = "$OutputEncoding=[Console]::OutputEncoding=[System.Text.Encoding]::UTF8;"

// PowerShellUTF8Script prepares a PowerShell script for captured execution.
// Setting both encodings keeps PowerShell's own output and native child-process
// output UTF-8 across Windows console code pages.
func PowerShellUTF8Script(command string) string {
	return psUTF8Prologue + command
}

// ShellKind is the interpreter a shell command runs under.
type ShellKind int

const (
	ShellBash ShellKind = iota
	ShellPowerShell
	ShellZsh
	ShellSh
)

func (k ShellKind) String() string {
	names := [...]string{"bash", "powershell", "zsh", "sh"}
	if int(k) >= 0 && int(k) < len(names) {
		return names[k]
	}
	return "bash"
}

// IsPOSIX reports whether this interpreter accepts the POSIX-family command
// path used by the bash tool. zsh and sh are macOS fallbacks, not PowerShell.
func (k ShellKind) IsPOSIX() bool { return k != ShellPowerShell }

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
// Discovery (candidate ordering, probing) is served by the process-wide shell
// inventory snapshot, so repeated calls share one probe pass for 30 seconds.
func ResolveShell(prefer, path string, warn io.Writer) Shell {
	snap := defaultShellInventory.snapshot(runtime.GOOS, prefer, path)
	return resolveShell(prefer, path, warn, snap.goos, snap.lookPath, snap.exists, snap.bashCands, snap.psCands, snap.probe, snap.isWSL)
}

// resolveShell is ResolveShell with its environment lookups injected — including
// the Git-for-Windows bash candidates, which derive from %ProgramFiles% and so
// are empty off Windows — so the decision table is deterministically testable on
// any host.
func resolveShell(prefer, path string, warn io.Writer, goos string, lookPath func(string) (string, error), exists func(string) bool, winBashCandidates []string, winPowerShellCandidates []string, probe func(string) bool, isWSL func(string) bool) Shell {
	findPOSIX := func(name string, kind ShellKind) (Shell, bool) {
		if p, err := lookPath(name); err == nil && !isWSL(p) && probe(p) {
			return Shell{Kind: kind, Path: p}, true
		}
		if goos != "windows" {
			for _, p := range []string{"/bin/" + name, "/usr/bin/" + name} {
				if exists(p) && probe(p) {
					return Shell{Kind: kind, Path: p}, true
				}
			}
		}
		return Shell{}, false
	}
	findBash := func() (Shell, bool) {
		if sh, ok := findPOSIX("bash", ShellBash); ok {
			return sh, true
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
	auto := func() Shell { return autoDetectedShell(goos, findBash, findPOSIX, findPowerShell) }

	switch strings.ToLower(strings.TrimSpace(prefer)) {
	case "", "auto":
		return autoShellWithConfiguredPath(goos, path, exists, probe, isWSL, auto)
	case "bash":
		path = configuredShellPath(goos, ShellBash, path, exists, isWSL)
		if path != "" && exists(path) && probe(path) {
			return Shell{Kind: ShellBash, Path: path}
		}
		if sh, ok := findBash(); ok {
			return sh
		}
		warnMissingShell(warn, prefer)
		return auto()
	case "powershell", "pwsh":
		path = configuredShellPath(goos, ShellPowerShell, path, exists, isWSL)
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

// autoShellWithConfiguredPath gives a compatible explicit Windows Bash path
// priority over PATH while keeping resolveShell's decision table compact. Auto
// accepts only well-known Bash names; arbitrary wrappers remain an explicit
// opt-in through prefer="bash".
func autoShellWithConfiguredPath(goos, path string, exists, probe, isWSL func(string) bool, fallback func() Shell) Shell {
	if goos == "windows" {
		base := strings.TrimSuffix(strings.ToLower(pathBase(strings.TrimSpace(path))), ".exe")
		if base == "bash" || base == "git-bash" {
			configured := configuredShellPath(goos, ShellBash, path, exists, isWSL)
			if configured != "" && exists(configured) && probe(configured) {
				return Shell{Kind: ShellBash, Path: configured}
			}
		}
	}
	return fallback()
}

func autoDetectedShell(goos string, findBash func() (Shell, bool), findPOSIX func(string, ShellKind) (Shell, bool), findPowerShell func([]string) (Shell, bool)) Shell {
	if sh, ok := findBash(); ok {
		return sh
	}
	if goos == "darwin" {
		for _, fallback := range []struct {
			name string
			kind ShellKind
		}{{"zsh", ShellZsh}, {"sh", ShellSh}} {
			if sh, ok := findPOSIX(fallback.name, fallback.kind); ok {
				return sh
			}
		}
	}
	if goos == "windows" {
		if sh, ok := findPowerShell([]string{"pwsh", "powershell"}); ok {
			return sh
		}
	}
	return Shell{Kind: ShellBash, Path: "bash"}
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
	cmd := proc.CommandContext(ctx, path, "-c", "true")
	cmd.Env = secrets.ProcessEnv()
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

func pathDir(p string) string {
	if i := strings.LastIndexAny(p, `/\`); i >= 0 {
		return p[:i]
	}
	return "."
}

// ConfiguredShellPathForPreference returns a configured executable only when
// it is compatible with the forced interpreter. The path remains persisted
// even when rejected here, so changing preferences never destroys the user's
// custom setting while runtime consumers avoid launching it with the wrong
// argv contract.
func ConfiguredShellPathForPreference(prefer, path string) string {
	var kind ShellKind
	switch strings.ToLower(strings.TrimSpace(prefer)) {
	case "bash":
		kind = ShellBash
	case "powershell", "pwsh":
		kind = ShellPowerShell
	default:
		return ""
	}
	return configuredShellPath(runtime.GOOS, kind, path, fileExists, isWindowsWSLBash)
}

// configuredShellPath is the shared safety boundary for every consumer of
// [tools.shell].path. Known cross-kind executables are ignored instead of being
// relabeled, while unknown names remain available for intentional wrappers.
func configuredShellPath(goos string, kind ShellKind, path string, exists func(string) bool, isWSL func(string) bool) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if kind == ShellBash && goos == "windows" {
		path = sanitizeWindowsBashPath(path, exists)
		if isWSL != nil && isWSL(path) {
			return ""
		}
	}
	base := strings.TrimSuffix(strings.ToLower(pathBase(path)), ".exe")
	switch kind {
	case ShellBash:
		if base == "git-bash" || base == "powershell" || base == "pwsh" || base == "zsh" || base == "sh" {
			return ""
		}
	case ShellPowerShell:
		if base == "bash" || base == "git-bash" || base == "zsh" || base == "sh" {
			return ""
		}
	}
	return path
}

func sanitizeWindowsBashPath(path string, exists func(string) bool) string {
	if path == "" {
		return path
	}
	base := strings.ToLower(pathBase(path))
	if base == "git-bash.exe" || base == "git-bash" {
		dir := pathDir(path)
		sep := "/"
		if strings.Contains(path, `\`) {
			sep = `\`
		}
		parent := pathDir(dir)
		for _, sub := range []string{
			dir + sep + "bin" + sep + "bash.exe",
			dir + sep + "usr" + sep + "bin" + sep + "bash.exe",
			parent + sep + "bin" + sep + "bash.exe",
			parent + sep + "usr" + sep + "bin" + sep + "bash.exe",
		} {
			if exists(sub) {
				return sub
			}
		}
	}
	return path
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

// normalizeNullRedirects rewrites null-device redirect aliases to sink
// ("/dev/null" for bash, "$null" for PowerShell), so permission-approved
// null-sink commands discard output under the resolved shell. It handles
// cmd.exe-style `nul`, PowerShell `$null`, and POSIX `/dev/null` while avoiding
// quoted/escaped text.
func normalizeNullRedirects(command, sink string) string {
	var (
		out   strings.Builder
		quote byte
	)
	write := func(c byte) {
		out.WriteByte(c)
	}
	for i := 0; i < len(command); {
		c := command[i]
		if quote != 0 {
			write(c)
			i++
			if c == '\\' && quote == '"' && i < len(command) {
				write(command[i])
				i++
				continue
			}
			if c == '`' && i < len(command) {
				write(command[i])
				i++
				continue
			}
			if c == quote {
				quote = 0
			}
			continue
		}
		switch c {
		case '\'', '"':
			quote = c
			write(c)
			i++
		case '\\', '`':
			write(c)
			i++
			if i < len(command) {
				write(command[i])
				i++
			}
		default:
			if replacement, next, ok := consumeNullRedirect(command, i, sink); ok {
				out.WriteString(replacement)
				i = next
				continue
			}
			write(c)
			i++
		}
	}
	return out.String()
}

func consumeNullRedirect(s string, start int, sink string) (string, int, bool) {
	i := start
	if i >= len(s) {
		return "", start, false
	}
	if s[i] == '&' {
		i++
		if i < len(s) && s[i] == '>' {
			i++
			if i < len(s) && s[i] == '>' {
				i++
			}
		} else {
			return "", start, false
		}
	} else {
		for i < len(s) && s[i] >= '0' && s[i] <= '9' {
			i++
		}
		if i >= len(s) || s[i] != '>' {
			return "", start, false
		}
		i++
		if i < len(s) && s[i] == '>' {
			i++
		}
	}
	opEnd := i
	for i < len(s) && (s[i] == ' ' || s[i] == '\t') {
		i++
	}
	next, ok := consumeNullSink(s, i)
	if !ok {
		return "", start, false
	}
	return s[start:opEnd] + sink, next, true
}

func consumeNullSink(s string, i int) (int, bool) {
	for _, sink := range []string{"/dev/null", "$null", "nul"} {
		if i+len(sink) > len(s) {
			continue
		}
		got := s[i : i+len(sink)]
		if sink == "/dev/null" {
			if got != sink {
				continue
			}
		} else if !strings.EqualFold(got, sink) {
			continue
		}
		next := i + len(sink)
		if next < len(s) && !isNullRedirectWordEnd(s[next]) {
			return next, false
		}
		return next, true
	}
	return i, false
}

func isNullRedirectWordEnd(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r' || strings.ContainsRune(";&|<>)]", rune(c))
}

// argv builds the exec argv that runs command under this shell.
func (s Shell) argv(command string) []string {
	path := s.Path
	if path == "" {
		path = s.Kind.String()
	}
	if s.Kind == ShellPowerShell {
		return []string{path, "-NoProfile", "-NonInteractive", "-Command", PowerShellUTF8Script(normalizeNullRedirects(command, "$null"))}
	}
	return []string{path, "-c", normalizeNullRedirects(command, "/dev/null")}
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
