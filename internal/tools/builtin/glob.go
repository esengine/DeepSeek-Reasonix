package builtin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/bmatcuk/doublestar/v4"

	"reasonix/internal/base/fileutil"
	"reasonix/internal/base/secrets"
	"reasonix/internal/contract/tool"
)

func init() { tool.RegisterBuiltin(globTool{}) }

// globTool matches files by pattern. workDir, when non-empty, is the directory
// a relative pattern resolves against (see resolveIn). paths resolves
// session-scoped read aliases for external folder refs. forbidRoots lists
// directories the tool may not search inside.
type globTool struct {
	workDir     string
	paths       *PathResolver
	forbidRoots []string
	readRoots   []string
}

func (globTool) Name() string { return "glob" }

func (globTool) Description() string {
	return "Find files matching a glob pattern (e.g. \"*.go\", \"internal/*/*.go\", \"**/*.test.ts\"). Supports shell metacharacters * ? [] and the recursive ** pattern."
}

func (globTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"pattern":{"type":"string","description":"Glob pattern (supports ** for recursive matching)"},"timeout_seconds":{"type":"integer","description":"Walk timeout in seconds (default 30, max 300); partial results are returned when it expires"}},"required":["pattern"]}`)
}

func (globTool) ReadOnly() bool { return true }

func (globTool) Reach() tool.Reach { return tool.ReachLocalRead }

// SnipHint keeps a long head and short tail like grep: the first paths matter
// most, the tail confirms how many more there were.
func (globTool) SnipHint() tool.SnipHint {
	return tool.SnipHint{Head: 80, Tail: 8, HeadChars: 10000, TailChars: 1000}
}

const (
	globMaxResults     = 1000
	globDefaultTimeout = 30 * time.Second
	globMaxTimeout     = 300 * time.Second
)

func (g globTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		Pattern        string `json:"pattern"`
		TimeoutSeconds int    `json:"timeout_seconds"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}
	if p.Pattern == "" {
		return "", fmt.Errorf("pattern is required")
	}
	to := toolTimeout(p.TimeoutSeconds, globDefaultTimeout, globMaxTimeout)
	ctx, cancel := context.WithTimeout(ctx, to)
	defer cancel()
	// Save the original pattern before resolveIn prepends workDir, so the
	// simple-filename recursive-fallback check below works on the raw input
	// — not the already-joined absolute path that always contains separators.
	rawPattern := p.Pattern
	rp := resolveReadablePath(g.workDir, p.Pattern, g.paths)
	p.Pattern = rp.Path
	if err := confineScope(g.readRoots, globBase(p.Pattern)); err != nil {
		return "", err
	}
	p.Pattern = filepath.FromSlash(p.Pattern) // models emit "/" (see Description); WalkDir/Match compare OS-native paths
	displayPattern := rp.DisplayPath

	// If the pattern contains **, use recursive matching via doublestar semantics
	// while retaining Reasonix's cancellation and read-forbid pruning.
	if strings.Contains(p.Pattern, "**") {
		return g.globRecursive(ctx, p.Pattern, displayPattern, rp, to)
	}

	// For patterns without **, try filepath.Glob first. If no matches are
	// found and the pattern is a simple filename (no path separator), retry
	// with a recursive walk (equivalent to "**/<pattern>") so the tool finds
	// files anywhere in the tree — the common case where the model only knows
	// a filename but not its exact location. Uses the raw pattern (before
	// resolveIn) so a workspace root doesn't mask a simple "*.go".
	matches, err := filepath.Glob(p.Pattern)
	if err != nil {
		if rp.External {
			return "", fmt.Errorf("glob %q: %s", displayPattern, rp.ErrorText(err))
		}
		return "", fmt.Errorf("glob %q: %w", displayPattern, err)
	}
	matches = filterForbidMatches(matches, g.forbidRoots, g.readRoots)
	if len(matches) == 0 && !strings.ContainsAny(rawPattern, "/\\") {
		fallback := filepath.Join(g.workDir, "**", rawPattern)
		return g.globRecursive(ctx, fallback, fallback, ResolvedPath{}, to)
	}
	if len(matches) == 0 {
		return tool.NoMatches, nil
	}
	matches = displayGlobMatches(matches, rp)
	if len(matches) > globMaxResults {
		matches = matches[:globMaxResults]
		return strings.Join(matches, "\n") + fmt.Sprintf("\n... (truncated at %d results)", globMaxResults), nil
	}
	return strings.Join(matches, "\n"), nil
}

func filterForbidMatches(matches, forbidRoots, scope []string) []string {
	if len(matches) == 0 || (len(forbidRoots) == 0 && len(scope) == 0 && !secrets.ProtectSensitiveFiles() && !secrets.ProtectCredentialFiles()) {
		return matches
	}
	out := matches[:0]
	for _, match := range matches {
		if !readOutsideScope(scope, match) && !confineRead(forbidRoots, match) {
			out = append(out, match)
		}
	}
	return out
}

// globWalkRoot splits a pattern into the tree to walk and the pattern to match
// relative paths against. It repairs the one root doublestar hands back that
// does not name what it looks like: a pattern rooted at a drive splits to the
// bare volume, and "D:" is that drive's *current directory*, not its root — so
// the walk started elsewhere and every match came back drive-relative.
func globWalkRoot(pattern string) (root, rel string) {
	rootSlash, rel := doublestar.SplitPattern(filepath.ToSlash(filepath.Clean(pattern)))
	root = filepath.FromSlash(rootSlash)
	if vol := filepath.VolumeName(root); vol != "" && vol == root {
		root += string(filepath.Separator)
	}
	if rel == "" {
		rel = "**"
	}
	return root, rel
}

// globRecursive handles patterns containing ** by walking the stable non-meta
// prefix and matching relative paths with doublestar. Accepts a context so the
// walk can be interrupted on cancellation, and to so an expired deadline can be
// reported as incomplete results rather than as a failure.
func (g globTool) globRecursive(ctx context.Context, pattern, displayPattern string, rp ResolvedPath, to time.Duration) (string, error) {
	root, relPattern := globWalkRoot(pattern)

	// Check root exists.
	if info, err := os.Stat(root); err != nil {
		if rp.External {
			return "", fmt.Errorf("glob %q: %s", displayPattern, rp.ErrorText(err))
		}
		return "", fmt.Errorf("glob %q: %w", displayPattern, err)
	} else if !info.IsDir() {
		return tool.NoMatches, nil
	}

	if err := confineScope(g.readRoots, root); err != nil {
		return "", err
	}
	var matches []string
	truncated := false
	confine := newWalkConfine(g.forbidRoots, g.readRoots, root)

	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if ctx.Err() != nil {
			return ctx.Err() // abort promptly on cancel/deadline — a huge tree is interruptible
		}
		if err != nil {
			return nil // skip unreadable entries
		}
		if d.IsDir() {
			if skipWalkDir(root, path, d.Name()) || confine.blocked(path, d) {
				return filepath.SkipDir
			}
			return nil
		}
		if confine.blocked(path, d) {
			return nil
		}
		rel, rerr := filepath.Rel(root, path)
		if rerr != nil {
			return nil
		}
		if matchGlobPattern(filepath.ToSlash(rel), relPattern) {
			matches = append(matches, path)
		}
		if len(matches) >= globMaxResults {
			truncated = true
			return filepath.SkipAll
		}
		return nil
	})
	timedOut := errors.Is(err, context.DeadlineExceeded)
	if err != nil && !timedOut {
		if rp.External {
			return "", fmt.Errorf("glob %q: %s", displayPattern, rp.ErrorText(err))
		}
		return "", fmt.Errorf("glob %q: %w", displayPattern, err)
	}

	if len(matches) == 0 {
		if timedOut {
			return fmt.Sprintf("%s; timed out after %s — narrow the pattern or raise timeout_seconds", tool.NoMatches, to), nil
		}
		return tool.NoMatches, nil
	}
	slices.Sort(matches)
	matches = displayGlobMatches(matches, rp)
	result := strings.Join(matches, "\n")
	switch {
	case truncated:
		result += fmt.Sprintf("\n... (truncated at %d results)", globMaxResults)
	case timedOut:
		result += fmt.Sprintf("\n... (timed out after %s; results incomplete — narrow the pattern or raise timeout_seconds)", to)
	}
	return result, nil
}

func displayGlobMatches(matches []string, rp ResolvedPath) []string {
	if !rp.External {
		return matches
	}
	out := make([]string, len(matches))
	for i, m := range matches {
		out[i] = rp.DisplayFor(m)
	}
	return out
}

func matchGlobPattern(path, pattern string) bool {
	return fileutil.MatchSlashGlob(path, pattern)
}

// globBase is the literal directory a pattern is anchored at: everything before
// its first metacharacter, cut back to a path separator.
func globBase(pattern string) string {
	i := strings.IndexAny(pattern, "*?[{")
	if i < 0 {
		return pattern
	}
	base := pattern[:i]
	if j := strings.LastIndexAny(base, "/\\"); j >= 0 {
		return base[:j+1]
	}
	return "."
}
