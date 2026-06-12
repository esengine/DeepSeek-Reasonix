package builtin

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"golang.org/x/text/transform"

	fileenc "reasonix/internal/fileutil/encoding"
	"reasonix/internal/proc"
	"reasonix/internal/tool"
)

const (
	grepMaxMatches     = 200
	grepDefaultTimeout = 30 * time.Second
	grepMaxTimeout     = 300 * time.Second
)

// grepTimeout clamps a caller-supplied second count to a sane bound; 0 (omitted)
// falls back to the default so a pathological walk can't hang for minutes.
func grepTimeout(sec int) time.Duration {
	switch {
	case sec <= 0:
		return grepDefaultTimeout
	case time.Duration(sec)*time.Second > grepMaxTimeout:
		return grepMaxTimeout
	default:
		return time.Duration(sec) * time.Second
	}
}

func formatGrep(ctx context.Context, out []string, truncated bool, to time.Duration) string {
	timedOut := ctx.Err() == context.DeadlineExceeded
	if len(out) == 0 {
		if timedOut {
			return fmt.Sprintf("(no matches; timed out after %s — narrow the path/pattern or raise timeout_seconds)", to)
		}
		return "(no matches)"
	}
	res := strings.Join(out, "\n")
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
type grepTool struct {
	workDir string
	rg      string
}

func (grepTool) Name() string { return "grep" }

func (g grepTool) Description() string {
	if g.rg != "" {
		return "Search for a regular expression in a file, or recursively under a directory — ripgrep-backed, so it honors .gitignore. Returns matching lines as path:line:text, capped at 200 matches."
	}
	return "Search for a regular expression in a file, or recursively under a directory (skips hidden files and files matched by .gitignore). Returns matching lines as path:line:text, capped at 200 matches."
}

func (grepTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"pattern":{"type":"string","description":"Regular expression (RE2 syntax)"},"path":{"type":"string","description":"File or directory to search (default \".\")"},"timeout_seconds":{"type":"integer","description":"Abort and return partial matches after this many seconds (default 30, max 300). Raise it for a large tree; lower it for a quick probe.","minimum":1}},"required":["pattern"]}`)
}

func (grepTool) ReadOnly() bool { return true }

func (g grepTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		Pattern        string `json:"pattern"`
		Path           string `json:"path"`
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
	p.Path = resolveIn(g.workDir, p.Path)

	to := grepTimeout(p.TimeoutSeconds)
	ctx, cancel := context.WithTimeout(ctx, to)
	defer cancel()

	if g.rg != "" {
		return g.runRipgrep(ctx, p.Pattern, p.Path, to)
	}
	re, err := regexp.Compile(p.Pattern)
	if err != nil {
		return "", fmt.Errorf("invalid pattern: %w", err)
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

		// Detect encoding from the peek alone — sufficient for the
		// UTF-8 vs GB18030 distinction (utf8.Valid on 8 KiB is reliable).
		// Then stream the rest through a decoder so the 200-match cap can
		// stop reading early instead of buffering the entire file.
		enc, _ := fileenc.Detect(peek)

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
			// Non-BOM path: stream. The peek bytes are prepended via
			// io.MultiReader; the remaining bytes flow through a decoder
			// pipe so the scanner can stop as soon as the cap is reached.
			dec := fileenc.Decoder(enc)
			if dec != nil {
				head := append([]byte(nil), peek...) // goroutine can outlive an early return; don't alias the reused buffer
				pr, pw := io.Pipe()
				go func() {
					_, _ = pw.Write(head)
					io.Copy(pw, f) //nolint:errcheck
					pw.Close()
				}()
				src = transform.NewReader(pr, dec)
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
				out = append(out, fmt.Sprintf("%s:%d:%s", file, ln, line))
				if len(out) >= grepMaxMatches {
					truncated = true
					return io.EOF
				}
			}
		}
		return nil
	}

	info, err := os.Stat(p.Path)
	if err != nil {
		return "", fmt.Errorf("grep %s: %w", p.Path, err)
	}

	if info.IsDir() {
		ig := newWalkIgnorer(p.Path)
		_ = filepath.WalkDir(p.Path, func(path string, d os.DirEntry, err error) error {
			if ctx.Err() != nil {
				return ctx.Err() // abort promptly on cancel — a huge tree is interruptible
			}
			if err != nil {
				return nil
			}
			if d.IsDir() {
				if ig.skip(path, d.Name(), true) {
					return filepath.SkipDir
				}
				ig.enter(path)
				return nil
			}
			if ig.skip(path, d.Name(), false) {
				return nil
			}
			if searchFile(path) == io.EOF {
				return filepath.SkipAll
			}
			return nil
		})
	} else {
		_ = searchFile(p.Path)
	}

	return formatGrep(ctx, out, truncated, to), nil
}

// runRipgrep delegates the search to ripgrep, which already emits
// path:line:text with these flags and honors .gitignore. Output is streamed and
// capped at grepMaxMatches so a flood of hits can't blow up memory.
func (g grepTool) runRipgrep(ctx context.Context, pattern, path string, to time.Duration) (string, error) {
	cmd := exec.CommandContext(ctx, g.rg,
		"--no-heading", "--line-number", "--with-filename", "--color", "never",
		"--regexp", pattern, "--", path)
	proc.HideWindow(cmd)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", err
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("ripgrep: %w", err)
	}

	var out []string
	truncated := false
	sc := bufio.NewScanner(stdout)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		out = append(out, sc.Text())
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
			return "", fmt.Errorf("ripgrep: %s", msg)
		}
	}
	return formatGrep(ctx, out, truncated, to), nil
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
// overriding the native instance registered at init.
func ConfineSearch(spec SearchSpec) tool.Tool {
	return grepTool{rg: spec.RgPath}
}
