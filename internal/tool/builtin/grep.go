package builtin

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"reasonix/internal/tool"
)

const grepMaxMatches = 200

func init() { tool.RegisterBuiltin(grepTool{}) }

// grepTool searches files by regex. workDir, when non-empty, is the directory a
// relative path resolves against (see resolveIn).
type grepTool struct{ workDir string }

func (grepTool) Name() string { return "grep" }

func (grepTool) Description() string {
	return "Search for a regular expression in a file, or recursively under a directory. Returns matching lines as path:line:text, capped at 200 matches."
}

func (grepTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"pattern":{"type":"string","description":"Regular expression (RE2 syntax)"},"path":{"type":"string","description":"File or directory to search (default \".\")"}},"required":["pattern"]}`)
}

func (grepTool) ReadOnly() bool { return true }

func (g grepTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		Pattern string `json:"pattern"`
		Path    string `json:"path"`
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
	re, err := regexp.Compile(p.Pattern)
	if err != nil {
		return "", fmt.Errorf("invalid pattern: %w", err)
	}

	var out []string
	truncated := false

	// searchFile returns io.EOF as a sentinel once the cap is reached.
	searchFile := func(file string) error {
		f, err := os.Open(file)
		if err != nil {
			return nil // skip unreadable files
		}
		defer f.Close()

		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
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
		_ = filepath.WalkDir(p.Path, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() {
				if skipWalkDir(p.Path, path, d.Name()) {
					return filepath.SkipDir
				}
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

	if len(out) == 0 {
		return "(no matches)", nil
	}
	res := strings.Join(out, "\n")
	if truncated {
		res += fmt.Sprintf("\n... (truncated at %d matches)", grepMaxMatches)
	}
	return res, nil
}
