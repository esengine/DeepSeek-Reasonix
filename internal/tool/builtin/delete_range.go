package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"reasonix/internal/diff"
	"reasonix/internal/tool"
)

func init() { tool.RegisterBuiltin(deleteRange{}) }

type deleteRange struct {
	roots   []string
	workDir string
}

func (deleteRange) Name() string { return "delete_range" }

func (deleteRange) Description() string {
	return "Delete a contiguous text range from a file using exact start/end text anchors. Each anchor must match exactly one line. Returns unified diff on success. Use for large deletions — smaller changes should use edit_file."
}

func (deleteRange) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type":"object",
		"properties":{
			"path":{"type":"string","description":"File path"},
			"start_anchor":{"type":"string","description":"Exact text of the first line to delete (must be unique in the file)"},
			"end_anchor":{"type":"string","description":"Exact text of the last line to delete (must be unique in the file)"},
			"inclusive":{"type":"boolean","description":"Whether to include the anchor lines in the deletion (default true)"}
		},
		"required":["path","start_anchor","end_anchor"]
	}`)
}

func (deleteRange) ReadOnly() bool { return false }

func (d deleteRange) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	change, err := d.preview(args)
	if err != nil {
		return "", err
	}
	// Re-detect the file's encoding so the rewrite preserves it (GBK/UTF-16/BOM)
	// rather than forcing UTF-8 and corrupting a non-UTF-8 file.
	_, enc, err := readFileEncoded(change.Path)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", change.Path, err)
	}
	if err := writeFileEncoded(change.Path, change.NewText, enc); err != nil {
		return "", fmt.Errorf("write %s: %w", change.Path, err)
	}
	return change.Diff, nil
}

func (d deleteRange) Preview(args json.RawMessage) (diff.Change, error) {
	return d.preview(args)
}

func (d deleteRange) preview(args json.RawMessage) (diff.Change, error) {
	var p struct {
		Path        string `json:"path"`
		StartAnchor string `json:"start_anchor"`
		EndAnchor   string `json:"end_anchor"`
		Inclusive   *bool  `json:"inclusive"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return diff.Change{}, fmt.Errorf("invalid args: %w", err)
	}
	if p.Path == "" {
		return diff.Change{}, fmt.Errorf("path is required")
	}
	if p.StartAnchor == "" {
		return diff.Change{}, fmt.Errorf("start_anchor is required")
	}
	if p.EndAnchor == "" {
		return diff.Change{}, fmt.Errorf("end_anchor is required")
	}

	inclusive := true
	if p.Inclusive != nil {
		inclusive = *p.Inclusive
	}

	p.Path = resolveIn(d.workDir, p.Path)
	if err := confine(d.roots, p.Path); err != nil {
		return diff.Change{}, err
	}

	original, _, err := readFileEncoded(p.Path)
	if err != nil {
		return diff.Change{}, fmt.Errorf("read %s: %w", p.Path, err)
	}

	// Detect line ending style so we can preserve it on write.
	lineSep := "\n"
	if strings.Contains(original, "\r\n") {
		lineSep = "\r\n"
	}

	// Strip \r for matching (split on \n after removing \r).
	lines := strings.Split(strings.ReplaceAll(original, "\r", ""), "\n")
	startLine := findUniqueLine(lines, p.StartAnchor)
	if startLine == -2 {
		return diff.Change{}, fmt.Errorf("start_anchor is not unique in %s; add more surrounding context", p.Path)
	}
	if startLine == -1 {
		return diff.Change{}, fmt.Errorf("start_anchor not found in %s", p.Path)
	}
	endLine := findUniqueLine(lines, p.EndAnchor)
	if endLine == -2 {
		return diff.Change{}, fmt.Errorf("end_anchor is not unique in %s; add more surrounding context", p.Path)
	}
	if endLine == -1 {
		return diff.Change{}, fmt.Errorf("end_anchor not found in %s", p.Path)
	}
	if startLine > endLine {
		return diff.Change{}, fmt.Errorf("start_anchor appears after end_anchor (lines %d and %d)", startLine+1, endLine+1)
	}

	// Build new content
	var keep []string
	if inclusive {
		keep = append(keep, lines[:startLine]...)
		keep = append(keep, lines[endLine+1:]...)
	} else {
		// Same line for both anchors: the kept prefix and suffix would overlap at
		// that line and duplicate it. There is nothing strictly between a line and
		// itself, so the exclusive deletion is contradictory — reject it.
		if startLine == endLine {
			return diff.Change{}, fmt.Errorf("start_anchor and end_anchor match the same line in %s; with inclusive=false there is nothing between them to delete", p.Path)
		}
		keep = append(keep, lines[:startLine+1]...)
		keep = append(keep, lines[endLine:]...)
	}
	newContent := strings.Join(keep, lineSep)
	// Preserve trailing newline if original had one.
	if newContent != "" && strings.HasSuffix(original, lineSep) && !strings.HasSuffix(newContent, lineSep) {
		newContent += lineSep
	}

	return diff.Build(p.Path, original, newContent, diff.Modify), nil
}

// findUniqueLine returns the index of the line that equals target.
// Returns -1 if not found, -2 if found on multiple lines.
func findUniqueLine(lines []string, target string) int {
	idx := -1
	for i, l := range lines {
		if l == target {
			if idx >= 0 {
				return -2
			}
			idx = i
		}
	}
	return idx
}
