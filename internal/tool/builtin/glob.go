package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"reasonix/internal/tool"
)

func init() { tool.RegisterBuiltin(globTool{}) }

// globTool matches files by pattern. workDir, when non-empty, is the directory
// a relative pattern resolves against (see resolveIn).
type globTool struct{ workDir string }

func (globTool) Name() string { return "glob" }

func (globTool) Description() string {
	return "Find files matching a glob pattern (e.g. \"*.go\", \"internal/*/*.go\", \"**/*.test.ts\"). Supports shell metacharacters * ? [] and the recursive ** pattern."
}

func (globTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"pattern":{"type":"string","description":"Glob pattern (supports ** for recursive matching)"}},"required":["pattern"]}`)
}

func (globTool) ReadOnly() bool { return true }

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
	p.Pattern = resolveIn(g.workDir, p.Pattern)
	p.Pattern = filepath.FromSlash(p.Pattern) // models emit "/" (see Description); WalkDir/Match compare OS-native paths

	// If the pattern contains **, use recursive matching via filepath.WalkDir.
	if strings.Contains(p.Pattern, "**") {
		return globRecursive(ctx, p.Pattern)
	}

	// For patterns without **, try filepath.Glob first. If no matches are
	// found and the pattern is a simple filename (no path separator), retry
	// with a recursive walk (equivalent to "**/<pattern>") so the tool finds
	// files anywhere in the tree — the common case where the model only knows
	// a filename but not its exact location. Uses the raw pattern (before
	// resolveIn) so a workspace root doesn't mask a simple "*.go".
	matches, err := filepath.Glob(p.Pattern)
	if err != nil {
		return "", fmt.Errorf("glob %q: %w", p.Pattern, err)
	}
	if len(matches) == 0 && !strings.ContainsAny(rawPattern, "/\\") {
		return globRecursive(ctx, filepath.Join(g.workDir, "**", rawPattern))
	}
	if len(matches) == 0 {
		return "(no matches)", nil
	}
	if len(matches) > globMaxResults {
		matches = matches[:globMaxResults]
		return strings.Join(matches, "\n") + fmt.Sprintf("\n... (truncated at %d results)", globMaxResults), nil
	}
	return strings.Join(matches, "\n"), nil
}

// globRecursive handles patterns containing ** by walking the filesystem.
// It splits the pattern at ** to get a root prefix and a suffix to match
// against each file path found during the walk. Accepts a context so the
// walk can be interrupted on cancellation.
func globRecursive(ctx context.Context, pattern string) (string, error) {
	// Split on ** to find the root directory and the remaining pattern.
	parts := strings.SplitN(pattern, "**", 2)
	root := parts[0]
	// If root doesn't end with a separator, walk from its parent or "."
	// so we don't miss files at that level.
	if root == "" {
		root = "."
	}
	// Ensure root is a clean directory path.
	root = filepath.Clean(root)

	// Check root exists.
	if info, err := os.Stat(root); err != nil {
		return "", fmt.Errorf("glob %q: %w", pattern, err)
	} else if !info.IsDir() {
		return "(no matches)", nil
	}

	suffix := ""
	if len(parts) > 1 {
		suffix = strings.TrimPrefix(parts[1], string(os.PathSeparator))
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
			if skipWalkDir(root, path, d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		// If there's no suffix, every file matches.
		if suffix == "" {
			matches = append(matches, path)
		} else {
			// Match the path against root + any-subdir + suffix.
			// Try matching the path relative to root against the suffix pattern.
			rel, rerr := filepath.Rel(root, path)
			if rerr != nil {
				return nil
			}
			if matchGlobSuffix(rel, suffix) {
				matches = append(matches, path)
			}
		}
		if len(matches) >= globMaxResults {
			truncated = true
			return filepath.SkipAll
		}
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("glob %q: %w", pattern, err)
	}

	if len(matches) == 0 {
		return "(no matches)", nil
	}
	sort.Strings(matches)
	result := strings.Join(matches, "\n")
	if truncated {
		result += fmt.Sprintf("\n... (truncated at %d results)", globMaxResults)
	}
	return result, nil
}

// matchGlobSuffix checks if path matches the suffix pattern after **.
// It tries matching at each directory level: if the pattern is "*.go",
// it matches "foo.go" and "dir/foo.go". If the pattern is "test/*.go",
// it matches "test/foo.go" and "dir/test/foo.go".
func matchGlobSuffix(path, pattern string) bool {
	// Direct match of the full relative path.
	if matched, _ := filepath.Match(pattern, path); matched {
		return true
	}
	// Try matching at each directory level.
	parts := strings.Split(path, string(os.PathSeparator))
	for i := range parts {
		sub := strings.Join(parts[i:], string(os.PathSeparator))
		if matched, _ := filepath.Match(pattern, sub); matched {
			return true
		}
	}
	// Also try matching just the filename against the pattern (for patterns
	// like "*.go" that should match any .go file at any depth).
	if !strings.Contains(pattern, string(os.PathSeparator)) {
		if matched, _ := filepath.Match(pattern, filepath.Base(path)); matched {
			return true
		}
	}
	return false
}
