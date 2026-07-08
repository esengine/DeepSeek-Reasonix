package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/bmatcuk/doublestar/v4"

	"reasonix/internal/fileutil"
	"reasonix/internal/secrets"
	"reasonix/internal/tool"
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
}

func (globTool) Name() string { return "glob" }

func (globTool) Description() string {
	return "Find files matching a glob pattern (e.g. \"*.go\", \"internal/*/*.go\", \"**/*.test.ts\"). Supports shell metacharacters * ? [] and the recursive ** pattern."
}

func (globTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"pattern":{"type":"string","description":"Glob pattern (supports ** for recursive matching)"}},"required":["pattern"]}`)
}

func (globTool) ReadOnly() bool { return true }

// SnipHint keeps a long head and short tail like grep: the first paths matter
// most, the tail confirms how many more there were.
func (globTool) SnipHint() tool.SnipHint {
	return tool.SnipHint{Head: 80, Tail: 8, HeadChars: 10000, TailChars: 1000}
}

const globMaxResults = 1000

func (g globTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		Pattern string `json:"pattern"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}
	if p.Pattern == "" {
		return "", fmt.Errorf("pattern is required")
	}
	// Save the original pattern before resolveIn prepends workDir, so the
	// simple-filename recursive-fallback check below works on the raw input
	// — not the already-joined absolute path that always contains separators.
	rawPattern := p.Pattern
	rp := resolveReadablePath(g.workDir, p.Pattern, g.paths)
	p.Pattern = rp.Path
	p.Pattern = filepath.FromSlash(p.Pattern) // models emit "/" (see Description); WalkDir/Match compare OS-native paths
	displayPattern := rp.DisplayPath

	// If the pattern contains **, use recursive matching via doublestar semantics
	// while retaining Reasonix's cancellation and read-forbid pruning.
	if strings.Contains(p.Pattern, "**") {
		return g.globRecursive(ctx, p.Pattern, displayPattern, rp)
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
	matches = filterForbidMatches(matches, g.forbidRoots)
	if len(matches) == 0 && !strings.ContainsAny(rawPattern, "/\\") {
		fallback := filepath.Join(g.workDir, "**", rawPattern)
		return g.globRecursive(ctx, fallback, fallback, ResolvedPath{})
	}
	if len(matches) == 0 {
		return "(no matches)", nil
	}
	matches = displayGlobMatches(matches, rp)
	if len(matches) > globMaxResults {
		matches = matches[:globMaxResults]
		return strings.Join(matches, "\n") + fmt.Sprintf("\n... (truncated at %d results)", globMaxResults), nil
	}
	return strings.Join(matches, "\n"), nil
}

func filterForbidMatches(matches, forbidRoots []string) []string {
	if len(matches) == 0 || (len(forbidRoots) == 0 && !secrets.ProtectSensitiveFiles()) {
		return matches
	}
	out := matches[:0]
	for _, match := range matches {
		if !confineRead(forbidRoots, match) {
			out = append(out, match)
		}
	}
	return out
}

// globRecursive handles patterns containing ** by walking the stable non-meta
// prefix and matching relative paths with doublestar. Accepts a context so the
// walk can be interrupted on cancellation.
func (g globTool) globRecursive(ctx context.Context, pattern, displayPattern string, rp ResolvedPath) (string, error) {
	rootSlash, relPattern := doublestar.SplitPattern(filepath.ToSlash(filepath.Clean(pattern)))
	root := filepath.FromSlash(rootSlash)
	if relPattern == "" {
		relPattern = "**"
	}

	// Check root exists.
	if info, err := os.Stat(root); err != nil {
		if rp.External {
			return "", fmt.Errorf("glob %q: %s", displayPattern, rp.ErrorText(err))
		}
		return "", fmt.Errorf("glob %q: %w", displayPattern, err)
	} else if !info.IsDir() {
		return "(no matches)", nil
	}

	var matches []string
	truncated := false

	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if ctx.Err() != nil {
			return ctx.Err() // abort promptly on cancel — a huge tree is interruptible
		}
		if err != nil {
			return nil // skip unreadable entries
		}
		if d.IsDir() {
			if skipWalkDir(root, path, d.Name()) || skipForbidDir(path, g.forbidRoots) {
				return filepath.SkipDir
			}
			return nil
		}
		if confineRead(g.forbidRoots, path) {
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
	if err != nil {
		if rp.External {
			return "", fmt.Errorf("glob %q: %s", displayPattern, rp.ErrorText(err))
		}
		return "", fmt.Errorf("glob %q: %w", displayPattern, err)
	}

	if len(matches) == 0 {
		return "(no matches)", nil
	}
	sort.Strings(matches)
	matches = displayGlobMatches(matches, rp)
	result := strings.Join(matches, "\n")
	if truncated {
		result += fmt.Sprintf("\n... (truncated at %d results)", globMaxResults)
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
