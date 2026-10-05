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
	"sync"
	"time"

	"reasonix/internal/base/proc"
	"reasonix/internal/base/secrets"
)

// psUTF8Prologue makes captured output UTF-8 rather than the console code page
// (CP936 on a Chinese Windows). Each assignment is guarded on its own: without a
// console, or under ConstrainedLanguage, the Console one throws, and that must
// neither skip $OutputEncoding nor print an error ahead of every command.
const psUTF8Prologue = "if($ExecutionContext.SessionState.LanguageMode -eq 'FullLanguage'){try{[Console]::OutputEncoding=[Text.UTF8Encoding]::new($false)}catch{};try{$OutputEncoding=[Text.UTF8Encoding]::new($false)}catch{}};"

// psToolFileDefaults is for the agent's shell tool only: Windows PowerShell 5.1
// writes '>' and Out-File as UTF-16LE, so there they default to UTF-8 (with the
// BOM 5.1 always adds). Hooks run the user's own scripts and keep its defaults.
const psToolFileDefaults = "if($PSVersionTable.PSVersion.Major -lt 6){try{$PSDefaultParameterValues['Out-File:Encoding']='utf8'}catch{}};"

// PowerShellUTF8Script prepares a PowerShell script for captured execution, so
// PowerShell's own output and what it pipes to native programs are UTF-8.
func PowerShellUTF8Script(command string) string {
	return psUTF8Prologue + command
}

func powerShellToolScript(command string) string {
	return psUTF8Prologue + psToolFileDefaults + command
}

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

// ResolveShell picks the interpreter the shell tool runs commands under: auto
// favours bash; a forced kind overrides the PATH lookup with path and falls back
// to auto if unusable, so a typo cannot leave the tool broken.
func ResolveShell(prefer, path string, warn io.Writer) Shell {
	return resolveShell(prefer, path, warn, runtime.GOOS, exec.LookPath, fileExists, windowsBashCandidates(), windowsPowerShellCandidates(), probeBash, isWindowsWSLBash, powerShellLaunches)
}

// shellHost holds the lookups shell discovery needs. Resolution and enumeration
// share one, so only a single place knows where an interpreter can hide on an
// OS — and the tests can inject a whole fake host.
type shellHost struct {
	goos     string
	lookPath func(string) (string, error)
	exists   func(string) bool
	winBash  []string
	winPS    []string
	probe    func(string) bool
	isWSL    func(string) bool
	launches func(string) bool // whether a found PowerShell starts; asked only of the one about to win
}

func currentHost() shellHost {
	return shellHost{runtime.GOOS, exec.LookPath, fileExists, windowsBashCandidates(), windowsPowerShellCandidates(), probeBash, isWindowsWSLBash, powerShellLaunches}
}

func (h shellHost) bash() (Shell, bool) {
	if p, err := h.lookPath("bash"); err == nil && !h.isWSL(p) && h.probe(p) {
		return Shell{Kind: ShellBash, Path: p}, true
	}
	for _, p := range h.winBash {
		if h.exists(p) && !h.isWSL(p) && h.probe(p) {
			return Shell{Kind: ShellBash, Path: p}, true
		}
	}
	return Shell{}, false
}

func (h shellHost) powerShell(order []string) (Shell, bool) {
	for _, name := range order {
		for _, p := range h.winPS {
			base := strings.ToLower(pathBase(p))
			if base != strings.ToLower(name) && strings.TrimSuffix(base, ".exe") != strings.ToLower(name) {
				continue
			}
			if h.exists(p) && h.launches(p) {
				return Shell{Kind: ShellPowerShell, Path: p}, true
			}
		}
		if p, err := h.lookPath(name); err == nil && h.launches(p) {
			return Shell{Kind: ShellPowerShell, Path: p}, true
		}
	}
	return Shell{}, false
}

func (h shellHost) auto(warn io.Writer) Shell {
	if sh, ok := h.bash(); ok {
		return sh
	}
	if h.goos == "windows" {
		if sh, ok := h.powerShell([]string{"pwsh", "powershell"}); ok {
			return sh
		}
	}
	// Detection found nothing, so the bare name is all that is left to try. Say
	// so rather than hand the session an interpreter no probe confirmed: a shell
	// the host may not have is the failure this resolver exists to prevent.
	if warn != nil {
		fmt.Fprintf(warn, "warning: [tools.shell] no usable shell was found; falling back to %q, which may fail to start\n", "bash")
	}
	return Shell{Kind: ShellBash, Path: "bash"}
}

// available lists the interpreters this host really has, in the order auto
// detection would take them. Every entry has been probed, so a settings surface
// can offer exactly what will work and put the rest in a "not installed" note
// rather than in a button that fails on the first command.
func (h shellHost) available() []Shell {
	var out []Shell
	seen := map[string]bool{}
	add := func(sh Shell) {
		key := strings.ToLower(sh.Path)
		if key == "" || seen[key] {
			return
		}
		seen[key] = true
		out = append(out, sh)
	}
	if sh, ok := h.bash(); ok {
		add(sh)
	}
	for _, p := range h.winBash {
		if h.exists(p) && !h.isWSL(p) && h.probe(p) {
			add(Shell{Kind: ShellBash, Path: p})
		}
	}
	for _, name := range []string{"pwsh", "powershell"} {
		if sh, ok := h.powerShell([]string{name}); ok {
			add(sh)
		}
	}
	return out
}

// DetectShells lists every interpreter installed on this machine, best first.
func DetectShells() []Shell { return currentHost().available() }

// VerifyShell reports why a chosen interpreter cannot be used. A settings
// surface calls it before persisting, so a typo is refused where it was typed
// instead of failing every later command far from the screen that caused it.
func VerifyShell(prefer, path string) error {
	return verifyShell(prefer, path, fileExists, probeBash, powerShellLaunches)
}

func verifyShell(prefer, path string, exists, probe, launches func(string) bool) error {
	kind := ShellBash
	switch strings.ToLower(strings.TrimSpace(prefer)) {
	case "", "auto", "bash":
	case "powershell", "pwsh":
		kind = ShellPowerShell
	default:
		return fmt.Errorf("shell %q: must be auto, bash, powershell or pwsh", prefer)
	}
	path = strings.TrimSpace(path)
	if path == "" {
		return nil
	}
	if !exists(path) {
		return fmt.Errorf("%s: no such executable", path)
	}
	if kind == ShellBash && !probe(path) {
		return fmt.Errorf("%s: did not run a command", path)
	}
	// A pinned PowerShell is launch-probed too: an existing executable that
	// cannot start is not a usable shell.
	if kind == ShellPowerShell && !launches(path) {
		return fmt.Errorf("%s: did not start", path)
	}
	return nil
}

// resolveShell is ResolveShell with its environment lookups injected, including
// Git-for-Windows candidates whose %ProgramFiles% values are empty off Windows.
// launches is injected too, so any host can test "pwsh will not start, 5.1 will".
func resolveShell(prefer, path string, warn io.Writer, goos string, lookPath func(string) (string, error), exists func(string) bool, winBashCandidates []string, winPowerShellCandidates []string, probe func(string) bool, isWSL func(string) bool, launches func(string) bool) Shell {
	h := shellHost{goos, lookPath, exists, winBashCandidates, winPowerShellCandidates, probe, isWSL, launches}
	switch strings.ToLower(strings.TrimSpace(prefer)) {
	case "", "auto":
		return h.auto(warn)
	case "bash":
		if path != "" && exists(path) && probe(path) {
			return Shell{Kind: ShellBash, Path: path}
		}
		if path != "" {
			warnUnusablePath(warn, path, "bash")
		}
		if sh, ok := h.bash(); ok {
			return sh
		}
		warnMissingShell(warn, prefer)
		return h.auto(warn)
	case "powershell", "pwsh":
		// A pinned path is proven the same way the bash arm proves its own. A Store
		// alias that will not start is exactly the failure this resolver keeps out
		// of a session, so existence alone must not win.
		if path != "" && exists(path) && launches(path) {
			return Shell{Kind: ShellPowerShell, Path: path}
		}
		if path != "" {
			warnUnusablePath(warn, path, "PowerShell")
		}
		order := []string{"pwsh", "powershell"}
		if strings.EqualFold(strings.TrimSpace(prefer), "powershell") {
			order = []string{"powershell", "pwsh"}
		}
		if sh, ok := h.powerShell(order); ok {
			return sh
		}
		warnMissingShell(warn, prefer)
		return h.auto(warn)
	default:
		if warn != nil {
			fmt.Fprintf(warn, "warning: [tools.shell] prefer=%q is not recognised (use auto/bash/powershell); using auto-detection\n", prefer)
		}
		return h.auto(warn)
	}
}

func warnMissingShell(warn io.Writer, prefer string) {
	if warn != nil {
		fmt.Fprintf(warn, "warning: [tools.shell] prefer=%q but that shell was not found; using auto-detection\n", prefer)
	}
}

// warnUnusablePath reports a pinned interpreter that detection will ignore.
// Detection answers in its place so the session still has a shell, while the
// warning tells the user their pin was dropped.
func warnUnusablePath(warn io.Writer, path, what string) {
	if warn != nil {
		fmt.Fprintf(warn, "warning: [tools.shell] path %q is not a usable %s; using auto-detection instead\n", path, what)
	}
}

// isWindowsWSLBash reports whether a resolved bash path is the WSL launcher
// Windows ships under %SystemRoot% (e.g. C:\Windows\System32\bash.exe). With WSL
// installed it runs commands inside the Linux VM — where the Windows workspace is
// a /mnt/<drive> path — so it must never be chosen for a native Windows workspace;
// the only bash.exe Microsoft places under the Windows dir is that launcher.
func isWindowsWSLBash(path string) bool {
	if runtime.GOOS != "windows" {
		return false
	}
	return isWSLLauncherPath(path, systemWindowsDir())
}

// isWSLLauncherPath judges by file identity, not spelling. A Store app-execution
// alias is a reparse point Go reports as irregular; the launchers Windows ships are
// the bash.exe in System32 and SysWOW64, and os.Stat follows every link to them, so
// short names, junctions, symlinks and hard links all compare equal.
func isWSLLauncherPath(path, winDir string) bool {
	if path == "" {
		return false
	}
	if fi, err := os.Lstat(path); err == nil && fi.Mode()&os.ModeIrregular != 0 {
		return true
	}
	fi, err := os.Stat(path)
	if err != nil || winDir == "" {
		return false
	}
	for _, dir := range []string{"System32", "SysWOW64"} {
		if ref, err := os.Stat(filepath.Join(winDir, dir, "bash.exe")); err == nil && os.SameFile(fi, ref) {
			return true
		}
	}
	return false
}

// Windows ships a bash.exe launcher stub in %SystemRoot% that opens the WSL
// install prompt instead of running anything, so confirm bash actually works
// before trusting it. Timeout-bounded in case the stub blocks on that prompt.
func probeBash(path string) bool {
	if runtime.GOOS != "windows" {
		return true
	}
	return probeBashMemo(path, runBashProbe)
}

func runBashProbe(path string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, "-c", "true")
	cmd.Env = secrets.ProcessEnv()
	proc.HideWindow(cmd)
	return cmd.Run() == nil
}

// bashProbeIdentity is the file a successful probe vouched for. The same path
// with the same size and mtime is the same executable, so it is not launched
// again; a failure is never kept, because a timeout may be transient.
type bashProbeIdentity struct {
	path    string
	size    int64
	modTime int64
}

// provenBash lives for the process and holds successes only.
var provenBash sync.Map

func probeBashMemo(path string, run func(string) bool) bool {
	fi, err := os.Stat(path)
	if err != nil {
		return run(path)
	}
	id := bashProbeIdentity{path: path, size: fi.Size(), modTime: fi.ModTime().UnixNano()}
	if _, ok := provenBash.Load(id); ok {
		return true
	}
	if !run(path) {
		return false
	}
	provenBash.Store(id, struct{}{})
	return true
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
	return bashCandidates(os.Getenv, exec.LookPath)
}

// bashCandidates adds to the standard roots every absolute PATH directory and the
// Git tree the git.exe on PATH sits in: exec.LookPath sees only the first
// bash.exe (usually the WSL launcher). Relative entries would resolve against the
// workspace, and duplicates would be probed twice, so both are dropped.
func bashCandidates(getenv func(string) string, lookPath func(string) (string, error)) []string {
	var roots []string
	for _, env := range []string{"ProgramFiles", "ProgramW6432", "ProgramFiles(x86)"} {
		if v := getenv(env); v != "" {
			roots = append(roots, filepath.Join(v, "Git"))
		}
	}
	if v := getenv("LOCALAPPDATA"); v != "" {
		roots = append(roots, filepath.Join(v, "Programs", "Git"))
	}
	if g, err := lookPath("git"); err == nil && filepath.IsAbs(g) {
		// git.exe is in <root>\cmd or <root>\bin, or one level deeper in
		// <root>\mingw64\bin.
		dir := filepath.Dir(g)
		roots = append(roots, filepath.Dir(dir), filepath.Dir(filepath.Dir(dir)))
	}
	var out []string
	seen := map[string]bool{}
	add := func(p string) {
		key := strings.ToLower(filepath.Clean(p))
		if !seen[key] {
			seen[key] = true
			out = append(out, p)
		}
	}
	for _, r := range roots {
		add(filepath.Join(r, "bin", "bash.exe"))
		add(filepath.Join(r, "usr", "bin", "bash.exe"))
	}
	for _, d := range filepath.SplitList(getenv("PATH")) {
		if filepath.IsAbs(d) {
			add(filepath.Join(d, "bash.exe"))
		}
	}
	return out
}

// windowsPowerShellCandidates lists common PowerShell executables that are not
// always present on PATH: PowerShell 7's MSI path, then the Store's execution alias.
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
	if v := os.Getenv("LOCALAPPDATA"); v != "" {
		out = append(out, filepath.Join(v, "Microsoft", "WindowsApps", "pwsh.exe"))
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
			// PowerShell's escape, and only inside its double quotes: single-quoted
			// text is literal in both shells, so a backtick there ends nothing.
			if c == '`' && quote == '"' && i < len(command) {
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
		return []string{path, "-NoProfile", "-NonInteractive", "-Command", powerShellToolScript(normalizeNullRedirects(command, "$null"))}
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
