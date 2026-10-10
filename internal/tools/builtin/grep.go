package builtin

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"golang.org/x/text/transform"

	"reasonix/internal/base/fileutil"
	fileenc "reasonix/internal/base/fileutil/encoding"
	"reasonix/internal/base/proc"
	"reasonix/internal/base/secrets"
	"reasonix/internal/contract/tool"
	"reasonix/internal/safety/sandbox"
	"reasonix/internal/state/sessiontemp"
)

const (
	grepMaxMatches     = 200
	grepDefaultTimeout = 30 * time.Second
	grepMaxTimeout     = 300 * time.Second
)

// grepScope names which tree produced an answer: "not in the tracked files"
// and "not here at all" are different, and a bare "no matches" reads as the
// second while meaning the first.
type grepScope int

const (
	grepScopeTracked grepScope = iota // the ignore rules were in force
	grepScopeIgnored                  // nothing tracked matched; these came from ignored paths
	grepScopeNeither                  // nothing matched in either
)

func formatGrep(ctx context.Context, out []string, truncated bool, to time.Duration, scope grepScope) string {
	timedOut := ctx.Err() == context.DeadlineExceeded
	if len(out) == 0 {
		if timedOut {
			return fmt.Sprintf("%s; timed out after %s — narrow the path/pattern or raise timeout_seconds", tool.NoMatches, to)
		}
		if scope == grepScopeNeither {
			return tool.NoMatches + "; the ignored paths were searched too, so this is absent rather than filtered"
		}
		return tool.NoMatches
	}
	res := strings.Join(out, "\n")
	if scope == grepScopeIgnored {
		res = "(no match in the tracked files; these are from paths the ignore rules exclude — build output, dependencies, generated code)\n" + res
	}
	switch {
	case truncated:
		res += fmt.Sprintf("\n... (truncated at %d matches)", grepMaxMatches)
	case timedOut:
		res += fmt.Sprintf("\n... (timed out after %s; results incomplete — narrow the path/pattern or raise timeout_seconds)", to)
	}
	return res
}

func init() { tool.RegisterBuiltin(grepTool{}) }

// grepTool searches files by regex. workDir, when non-empty, is the directory a
// relative path resolves against (see resolveIn). rg, when non-empty, is a
// ripgrep binary the search delegates to instead of the native Go scanner.
// forbidRoots lists directories the tool may not search inside.
// sb is the OS sandbox spec for the ripgrep subprocess, making forbid-read
// directories invisible to ripgrep instead of checking them in-process.
type grepTool struct {
	netRoots    []string
	workDir     string
	paths       *PathResolver
	rg          string
	forbidRoots []string
	readRoots   []string
	sb          sandbox.Spec
	sessionTemp *sessiontemp.Manager
}

func (grepTool) Name() string { return "grep" }

func (g grepTool) Description() string {
	const scope = " Ignored paths are searched only when nothing else matched; such matches say so, so an empty answer means absent rather than filtered."
	if g.rg != "" {
		return "Search for a regular expression in a file, or recursively under a directory — ripgrep-backed, so it honors .gitignore. Returns matching lines as path:line:text, capped at 200 matches." + scope
	}
	return "Search for a regular expression in a file, or recursively under a directory (skips hidden files and files matched by .gitignore). Returns matching lines as path:line:text, capped at 200 matches." + scope
}

func (grepTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"pattern":{"type":"string","description":"Regular expression (RE2 syntax)"},"path":{"type":"string","description":"File or directory to search (default \".\")"},"glob":{"type":"string","description":"Only search files matching this glob. A glob with no slash matches the file name at any depth (\"*.go\"); one with a slash matches the path below the search root (\"internal/**/*_test.go\")."},"timeout_seconds":{"type":"integer","description":"Abort and return partial matches after this many seconds (default 30, max 300). Raise it for a large tree; lower it for a quick probe.","minimum":1}},"required":["pattern"]}`)
}

func (grepTool) ReadOnly() bool { return true }

func (grepTool) Reach() tool.Reach { return tool.ReachLocalRead }

// SnipHint keeps a long head of matches and a short tail: the first matches are
// the ones the model usually acts on, the tail just confirms scope.
func (grepTool) SnipHint() tool.SnipHint {
	return tool.SnipHint{Head: 80, Tail: 8, HeadChars: 10000, TailChars: 1000}
}

// ReadTarget resolves the same path Execute will. An unset path searches the
// work dir, which names no single target, so it reports none.
func (g grepTool) ReadTarget(args json.RawMessage) string {
	var p struct {
		Path string `json:"path"`
	}
	if json.Unmarshal(args, &p) != nil || p.Path == "" {
		return ""
	}
	return resolveReadablePath(g.workDir, p.Path, g.paths).Path
}

func (g grepTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		Pattern        string `json:"pattern"`
		Path           string `json:"path"`
		Glob           string `json:"glob"`
		TimeoutSeconds int    `json:"timeout_seconds"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}
	if p.Pattern == "" {
		return "", fmt.Errorf("pattern is required")
	}
	if p.Path == "" {
		p.Path = "."
	}
	p.Glob = strings.TrimSpace(p.Glob)
	if strings.HasPrefix(p.Glob, "!") {
		// A negated glob would re-admit the sensitive-file excludes below it.
		return "", fmt.Errorf("glob must select files, not exclude them")
	}
	rp := resolveReadablePath(g.workDir, p.Path, g.paths)
	p.Path = rp.Path
	if err := rp.refuseNetwork(g.netRoots); err != nil {
		return "", err
	}

	to := toolTimeout(p.TimeoutSeconds, grepDefaultTimeout, grepMaxTimeout)
	ctx, cancel := context.WithTimeout(ctx, to)
	defer cancel()

	if err := confineScope(g.readRoots, p.Path); err != nil {
		return "", err
	}
	info, err := os.Stat(p.Path)
	if err != nil {
		if rp.External {
			return "", fmt.Errorf("grep %s: %s", rp.DisplayPath, rp.ErrorText(err))
		}
		return "", fmt.Errorf("grep %s: %w", rp.DisplayPath, err)
	}
	if confineRead(g.forbidRoots, p.Path) {
		if info.IsDir() {
			return formatGrep(ctx, nil, false, to, grepScopeTracked), nil
		}
		err := &os.PathError{Op: "stat", Path: p.Path, Err: os.ErrNotExist}
		if rp.External {
			return "", fmt.Errorf("grep %s: %s", rp.DisplayPath, rp.ErrorText(err))
		}
		return "", err
	}

	if g.rg != "" {
		out, wrapped, err := g.runRipgrep(ctx, p.Pattern, p.Path, p.Glob, info.IsDir(), to, rp)
		if len(g.forbidRoots) == 0 || wrapped {
			return out, err
		}
		// Without an OS sandbox, ripgrep can walk into forbid-read roots. Fall
		// back to the native scanner, which prunes those roots in-process.
	}

	return g.runNative(ctx, p.Pattern, p.Path, p.Glob, info, to, rp)
}

// grepGlobMatches mirrors ripgrep's rule so both engines answer alike: a glob
// with no slash is matched against the file name at any depth, one with a slash
// against the path relative to the search root.
func grepGlobMatches(glob, root, file string) bool {
	if glob == "" {
		return true
	}
	if !strings.Contains(glob, "/") {
		return fileutil.MatchSlashGlob(filepath.Base(file), glob)
	}
	rel, err := filepath.Rel(root, file)
	if err != nil {
		rel = file
	}
	return fileutil.MatchSlashGlob(rel, glob)
}

// runNative answers in the two phases ripgrep does. A single file is searched
// once: nothing was filtered.
func (g grepTool) runNative(ctx context.Context, pattern, path, glob string, info os.FileInfo, to time.Duration, rp ResolvedPath) (string, error) {
	out, truncated, err := g.nativePass(ctx, pattern, path, glob, info, rp, false)
	if err != nil || len(out) > 0 || ctx.Err() != nil || !info.IsDir() {
		return formatGrep(ctx, out, truncated, to, grepScopeTracked), err
	}
	wide, wideTruncated, wideErr := g.nativePass(ctx, pattern, path, glob, info, rp, true)
	if wideErr != nil || len(wide) == 0 {
		return formatGrep(ctx, nil, false, to, grepScopeNeither), nil
	}
	return formatGrep(ctx, wide, wideTruncated, to, grepScopeIgnored), nil
}

func (g grepTool) nativePass(ctx context.Context, pattern, path, glob string, info os.FileInfo, rp ResolvedPath, wide bool) ([]string, bool, error) {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, false, fmt.Errorf("invalid pattern: %w", err)
	}

	var out []string
	truncated := false

	// Reused across the serial walk so each file doesn't re-allocate ~72 KiB.
	peekBuf := make([]byte, 8*1024)
	scanBuf := make([]byte, 0, 64*1024)

	// searchFile returns io.EOF as a sentinel once the cap is reached.
	searchFile := func(file string) error {
		f, err := os.Open(file)
		if err != nil {
			return nil // skip unreadable files
		}
		defer f.Close()

		// Peek the first 8 KiB to reject binaries cheaply without reading
		// the entire file into memory. Check BOM first (UTF-16 files have
		// 0x00 for ASCII), then NUL.
		n, _ := io.ReadFull(f, peekBuf)
		peek := peekBuf[:n]

		bomKind := fileenc.DetectQuick(peek)
		if bomKind != fileenc.UTF16LE && bomKind != fileenc.UTF16BE && bomKind != fileenc.UTF8BOM {
			if bytes.IndexByte(peek, 0) >= 0 {
				return nil // binary, skip
			}
		}

		// A full peek is only the start of the file and can end inside a
		// character. The rest streams through a decoder so the match cap can
		// stop reading early.
		detect := fileenc.Detect
		if n == len(peekBuf) {
			detect = fileenc.DetectFragment
		}
		enc, _ := detect(peek)

		var src io.Reader
		if enc == fileenc.UTF16LE || enc == fileenc.UTF16BE {
			// UTF-16 needs full-file decode (multi-byte units span the
			// whole stream). These files are rare in grep targets.
			rest, err := io.ReadAll(f)
			if err != nil {
				return nil
			}
			all := append(peek, rest...)
			src = bytes.NewReader(fileenc.Decode(all, enc))
		} else {
			// Non-BOM path: stream through the decoder so the scanner can
			// stop as soon as the cap is reached without buffering the file.
			dec := fileenc.Decoder(enc)
			if dec != nil {
				src = transform.NewReader(io.MultiReader(bytes.NewReader(peek), f), dec)
			} else {
				// UTF-8 or LossyUTF8 — no transformation needed.
				src = io.MultiReader(bytes.NewReader(peek), f)
			}
		}

		sc := bufio.NewScanner(src)
		sc.Buffer(scanBuf, 1024*1024)
		ln := 0
		for sc.Scan() {
			ln++
			line := sc.Text()
			if strings.IndexByte(line, 0) >= 0 {
				return nil // looks binary, skip the file
			}
			if re.MatchString(line) {
				out = append(out, fmt.Sprintf("%s:%d:%s", rp.DisplayFor(file), ln, line))
				if len(out) >= grepMaxMatches {
					truncated = true
					return io.EOF
				}
			}
		}
		return nil
	}

	if info.IsDir() {
		root := path // the walk callback shadows path with each entry
		ig := newWalkIgnorer(path, g.forbidRoots, g.readRoots, wide)
		_ = filepath.WalkDir(path, func(path string, d os.DirEntry, err error) error {
			if ctx.Err() != nil {
				return ctx.Err() // abort promptly on cancel — a huge tree is interruptible
			}
			if err != nil {
				return nil
			}
			if d.IsDir() {
				if ig.skip(path, d) {
					return filepath.SkipDir
				}
				ig.enter(path)
				return nil
			}
			if ig.skip(path, d) || ig.confine.blocked(path, d) || !grepGlobMatches(glob, root, path) {
				return nil
			}
			if errors.Is(searchFile(path), io.EOF) {
				return filepath.SkipAll
			}
			return nil
		})
	} else if !confineRead(g.forbidRoots, path) {
		_ = searchFile(path)
	}

	return out, truncated, nil
}

// runRipgrep searches the tracked tree, and only when that finds nothing goes
// on to the paths the ignore rules exclude. Not a wider default: that pass
// walks the build output, so it is paid only where the cheap answer was empty.
func (g grepTool) runRipgrep(ctx context.Context, pattern, path, glob string, directory bool, to time.Duration, rp ResolvedPath) (string, bool, error) {
	out, truncated, wrapped, err := g.ripgrepPass(ctx, pattern, path, glob, directory, rp, false)
	if err != nil || (len(g.forbidRoots) > 0 && !wrapped) {
		return "", wrapped, err
	}
	if len(out) > 0 || ctx.Err() != nil {
		return formatGrep(ctx, out, truncated, to, grepScopeTracked), wrapped, nil
	}
	wide, wideTruncated, _, wideErr := g.ripgrepPass(ctx, pattern, path, glob, directory, rp, true)
	if wideErr != nil || len(wide) == 0 {
		return formatGrep(ctx, nil, false, to, grepScopeNeither), wrapped, nil
	}
	return formatGrep(ctx, wide, wideTruncated, to, grepScopeIgnored), wrapped, nil
}

// ripgrepPass is one delegation: ripgrep already emits path:line:text and
// honors .gitignore. Output is streamed and capped at grepMaxMatches so a flood
// of hits cannot blow up memory, and the subprocess is wrapped in the OS
// sandbox so forbid-read directories are invisible to it.
func (g grepTool) ripgrepPass(ctx context.Context, pattern, path, glob string, directory bool, rp ResolvedPath, wide bool) ([]string, bool, bool, error) {
	// Build the ripgrep argv and wrap it in the OS sandbox so forbid-read
	// directories are invisible to the ripgrep subprocess.
	args := []string{
		g.rg,
		"--no-heading", "--line-number", "--with-filename", "--color", "never",
		// Ripgrep does not follow links unless told to; stated so that stays true.
		"--no-follow",
	}
	if wide {
		// The ignore rules and nothing else: the VCS store is history rather
		// than a place a build writes, and hidden files stay hidden.
		args = append(args, "--no-ignore-vcs", "--no-ignore-dot", "--no-ignore-exclude", "--glob", "!.git/**")
	}
	if glob != "" {
		// Before the excludes below: ripgrep lets a later glob win, so the
		// denylist must be able to override anything the caller selected.
		args = append(args, "--glob", glob)
	}
	if secrets.ProtectSensitiveFiles() {
		// Mirror sensitiveReadPath for the subprocess: ripgrep cannot call
		// back into confineRead, so the denylist rides along as glob excludes.
		args = append(args,
			"--glob", "!.env",
			"--glob", "!.git-credentials",
			"--glob", "!.netrc",
			"--glob", "!*.pem",
			"--glob", "!*.key",
			"--glob", "!*.p12",
			"--glob", "!*.pfx",
			"--glob", "!.ssh/**",
		)
	}
	target, err := ripgrepTarget(path, directory)
	if err != nil {
		return nil, false, false, ripgrepDisplayError(err, target, path, rp)
	}
	args = append(args, "--regexp", pattern, "--", target)

	var lease *sessiontemp.Lease
	sessionDir := ""
	if m := g.sessionTempManager(ctx); m != nil {
		l, err := m.Acquire()
		if err != nil {
			return nil, false, false, fmt.Errorf("%w: %w", errSessionTemp, err)
		}
		lease = l
		sessionDir = l.Dir()
		defer lease.Release()
	}
	prepared := sandbox.PrepareArgs(g.sb, args, sessionDir)
	argv, wrapped := prepared.Argv, prepared.Wrapped
	if len(g.forbidRoots) > 0 && !wrapped {
		return nil, false, wrapped, nil
	}

	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	// Keep glob anchoring local to this subprocess, never the host process.
	if directory {
		cmd.Dir = target
	}
	cmd.Env = applyEnvOverrides(secrets.ProcessEnv(), prepared.EnvOverrides)
	proc.HideWindow(cmd)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, false, wrapped, err
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return nil, false, wrapped, ripgrepDisplayError(err, target, path, rp)
	}

	var out []string
	truncated := false
	sc := bufio.NewScanner(stdout)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Text()
		if directory {
			line = restoreRipgrepPath(line, target, path)
		}
		out = append(out, displayRipgrepLine(line, rp))
		if len(out) >= grepMaxMatches {
			truncated = true
			break
		}
	}
	if truncated {
		_ = cmd.Process.Kill()
	}
	_, _ = io.Copy(io.Discard, stdout) // drain to EOF so Wait neither blocks nor races the reader
	_ = cmd.Wait()

	if len(out) == 0 && ctx.Err() != context.DeadlineExceeded {
		// ripgrep exits 1 with no output for "no matches"; a real failure (bad
		// pattern, unreadable path) writes a message to stderr.
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return nil, false, wrapped, ripgrepDisplayError(errors.New(msg), target, path, rp)
		}
	}
	return out, truncated, wrapped, nil
}

func (g grepTool) sessionTempManager(ctx context.Context) *sessiontemp.Manager {
	if m := sessiontemp.FromContext(ctx); m != nil {
		return m
	}
	return g.sessionTemp
}

func displayRipgrepLine(line string, rp ResolvedPath) string {
	if !rp.External || !strings.HasPrefix(line, rp.Root) {
		return line
	}
	for i := len(rp.Root); i < len(line); i++ {
		if line[i] != ':' || i+1 >= len(line) || line[i+1] < '0' || line[i+1] > '9' {
			continue
		}
		j := i + 1
		for j < len(line) && line[j] >= '0' && line[j] <= '9' {
			j++
		}
		if j >= len(line) || line[j] != ':' {
			continue
		}
		return rp.DisplayFor(line[:i]) + line[i:]
	}
	return line
}

// SearchSpec configures the grep tool's engine. A non-empty RgPath makes grep
// delegate to that ripgrep binary; empty uses the native Go scanner.
type SearchSpec struct {
	RgPath string
}

// ResolveSearch picks the grep engine from config. "native" forces the Go
// scanner; "rg" requires ripgrep (warns and falls back to native if absent);
// "auto"/"" uses ripgrep when found, else native. rgPath overrides the PATH
// lookup. warn (may be nil) receives the fall-back notice for engine="rg".
func ResolveSearch(engine, rgPath string, warn io.Writer) SearchSpec {
	find := func() string {
		if rgPath != "" {
			if fi, err := os.Stat(rgPath); err == nil && !fi.IsDir() {
				return rgPath
			}
			return ""
		}
		if p, err := exec.LookPath("rg"); err == nil {
			return p
		}
		return ""
	}
	switch strings.ToLower(strings.TrimSpace(engine)) {
	case "native":
		return SearchSpec{}
	case "rg":
		if p := find(); p != "" {
			return SearchSpec{RgPath: p}
		}
		if warn != nil {
			fmt.Fprintln(warn, `warning: [tools.search] engine="rg" but ripgrep (rg) was not found; using the native search engine`)
		}
		return SearchSpec{}
	default: // "auto", ""
		return SearchSpec{RgPath: find()}
	}
}

// ConfineSearch returns the grep built-in bound to a resolved search engine,
// os sandbox spec for the ripgrep subprocess, and forbid-read roots for the
// native scanner, overriding the native instance registered at init.
// Session-private temporary directories are bound via BindSessionTemp or
// Workspace.SessionTemp.
func ConfineSearch(spec SearchSpec, sb sandbox.Spec, forbidRoots []string) tool.Tool {
	return grepTool{rg: spec.RgPath, sb: sb, forbidRoots: forbidRoots}
}
