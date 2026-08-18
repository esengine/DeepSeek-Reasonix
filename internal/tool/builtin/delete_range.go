package builtin

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"reasonix/internal/diff"
	"reasonix/internal/sandbox"
	"reasonix/internal/tool"
)

func init() { tool.RegisterBuiltin(deleteRange{}) }

type deleteRange struct {
	roots   []string
	rootSet *sandbox.WritableRootSet
	guard   SessionDataGuard
	managed ManagedConfigPaths
	workDir string
	overlay FileOverlay
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

func (d deleteRange) DeclareWriteAccess(args json.RawMessage) (tool.WriteAccessDeclaration, error) {
	return declareFilePathWriteAccess(d.workDir, args)
}

func (d deleteRange) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	change, src, err := d.preview(ctx, args)
	if err != nil {
		return "", err
	}
	// preview ran the non-approving boundary check; the actual write needs the
	// full one, which can gate a Reasonix-managed config target on user approval.
	if err := confineWrite(ctx, effectiveWriteRoots(ctx, d.rootSet, d.roots), d.guard, d.managed, change.Path); err != nil {
		return "", err
	}
	// src carries the route and encoding the read came from, so the rewrite
	// returns to the same place and preserves GBK/UTF-16/BOM.
	if err := src.write(ctx, d.overlay, change.Path, change.NewText); err != nil {
		return "", fmt.Errorf("write %s: %w", change.Path, err)
	}
	return change.Diff, nil
}

func (d deleteRange) Preview(ctx context.Context, args json.RawMessage) (diff.Change, error) {
	change, _, err := d.preview(ctx, args)
	return change, err
}

type deleteRangeTarget struct {
	path         string
	inclusive    bool
	source       editSource
	original     string
	lines        []string
	startLine    int
	endLine      int
	deleteStart  int
	deleteEnd    int
	deletesLines bool
}

func (d deleteRange) resolveTarget(ctx context.Context, args json.RawMessage) (deleteRangeTarget, error) {
	var p struct {
		Path        string `json:"path"`
		StartAnchor string `json:"start_anchor"`
		EndAnchor   string `json:"end_anchor"`
		Inclusive   *bool  `json:"inclusive"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return deleteRangeTarget{}, fmt.Errorf("invalid args: %w", err)
	}
	if p.Path == "" {
		return deleteRangeTarget{}, fmt.Errorf("path is required")
	}
	if p.StartAnchor == "" {
		return deleteRangeTarget{}, fmt.Errorf("start_anchor is required")
	}
	if p.EndAnchor == "" {
		return deleteRangeTarget{}, fmt.Errorf("end_anchor is required")
	}

	inclusive := true
	if p.Inclusive != nil {
		inclusive = *p.Inclusive
	}

	p.Path = resolveIn(d.workDir, p.Path)
	if err := confinePreview(effectiveWriteRoots(ctx, d.rootSet, d.roots), d.guard, d.managed, p.Path); err != nil {
		return deleteRangeTarget{}, err
	}

	src, err := readEditSource(ctx, d.overlay, p.Path)
	if err != nil {
		return deleteRangeTarget{}, fmt.Errorf("read %s: %w", p.Path, err)
	}
	original := src.content

	// Strip \r for matching (split on \n after removing \r).
	lines := strings.Split(strings.ReplaceAll(original, "\r", ""), "\n")
	startLine := findUniqueLine(lines, p.StartAnchor)
	if startLine == -2 {
		return deleteRangeTarget{}, fmt.Errorf("start_anchor is not unique in %s%s; add nearby unique code, not just repeated separator lines", p.Path, lineMatchSummary(lines, p.StartAnchor, 5))
	}
	if startLine == -1 {
		return deleteRangeTarget{}, fmt.Errorf("start_anchor not found in %s", p.Path)
	}
	endLine := findUniqueLine(lines, p.EndAnchor)
	if endLine == -2 {
		return deleteRangeTarget{}, fmt.Errorf("end_anchor is not unique in %s%s; add nearby unique code, not just repeated separator lines", p.Path, lineMatchSummary(lines, p.EndAnchor, 5))
	}
	if endLine == -1 {
		return deleteRangeTarget{}, fmt.Errorf("end_anchor not found in %s", p.Path)
	}
	if startLine > endLine {
		return deleteRangeTarget{}, fmt.Errorf("start_anchor appears after end_anchor (lines %d and %d)", startLine+1, endLine+1)
	}
	deleteStart, deleteEnd, deletesLines, err := deletionLineInterval(startLine, endLine, inclusive, p.Path)
	if err != nil {
		return deleteRangeTarget{}, err
	}
	if deletesLines && shouldValidateBraceCompleteDeletion(p.Path) {
		if err := validateBraceCompleteDeletion(lines, deleteStart, deleteEnd, p.Path); err != nil {
			return deleteRangeTarget{}, err
		}
	}
	return deleteRangeTarget{
		path: p.Path, inclusive: inclusive, source: src, original: original,
		lines: lines, startLine: startLine, endLine: endLine,
		deleteStart: deleteStart, deleteEnd: deleteEnd, deletesLines: deletesLines,
	}, nil
}

func (d deleteRange) preview(ctx context.Context, args json.RawMessage) (diff.Change, editSource, error) {
	target, err := d.resolveTarget(ctx, args)
	if err != nil {
		return diff.Change{}, editSource{}, err
	}
	lineSep := "\n"
	if strings.Contains(target.original, "\r\n") {
		lineSep = "\r\n"
	}

	// Build new content
	var keep []string
	if target.inclusive {
		keep = append(keep, target.lines[:target.startLine]...)
		keep = append(keep, target.lines[target.endLine+1:]...)
	} else {
		// Same line for both anchors: the kept prefix and suffix would overlap at
		// that line and duplicate it. There is nothing strictly between a line and
		// itself, so the exclusive deletion is contradictory — reject it.
		if target.startLine == target.endLine {
			return diff.Change{}, editSource{}, fmt.Errorf("start_anchor and end_anchor match the same line in %s; with inclusive=false there is nothing between them to delete", target.path)
		}
		keep = append(keep, target.lines[:target.startLine+1]...)
		keep = append(keep, target.lines[target.endLine:]...)
	}
	newContent := strings.Join(keep, lineSep)
	// Preserve trailing newline if original had one.
	if newContent != "" && strings.HasSuffix(target.original, lineSep) && !strings.HasSuffix(newContent, lineSep) {
		newContent += lineSep
	}

	return diff.Build(target.path, target.original, newContent, diff.Modify), target.source, nil
}

// ResolveAnchoredTextTarget exposes the same validated target used by Preview
// and Execute, without adding anything to the provider-visible tool contract.
func (d deleteRange) ResolveAnchoredTextTarget(ctx context.Context, args json.RawMessage) (tool.AnchoredTextTargetInfo, error) {
	target, err := d.resolveTarget(ctx, args)
	if err != nil {
		return tool.AnchoredTextTargetInfo{}, err
	}
	hashes := make([]string, 0, target.endLine-target.startLine+1)
	for _, line := range target.lines[target.startLine : target.endLine+1] {
		sum := sha256.Sum256([]byte(line))
		hashes = append(hashes, hex.EncodeToString(sum[:]))
	}
	return tool.AnchoredTextTargetInfo{
		Path: target.path, Inclusive: target.inclusive,
		StartLine: target.startLine + 1, EndLine: target.endLine + 1,
		LineHashes: hashes,
	}, nil
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

func deletionLineInterval(startLine, endLine int, inclusive bool, path string) (int, int, bool, error) {
	if inclusive {
		return startLine, endLine, true, nil
	}
	if startLine == endLine {
		return 0, 0, false, fmt.Errorf("start_anchor and end_anchor match the same line in %s; with inclusive=false there is nothing between them to delete", path)
	}
	start, end := startLine+1, endLine-1
	if start > end {
		return start, end, false, nil
	}
	return start, end, true, nil
}

func shouldValidateBraceCompleteDeletion(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".c", ".cc", ".cjs", ".cpp", ".cs", ".css", ".cxx",
		".go", ".h", ".hh", ".hpp", ".htm", ".html",
		".java", ".js", ".json", ".jsonc", ".jsx", ".kt", ".kts",
		".less", ".mjs", ".php", ".rs", ".sass", ".scss", ".svelte",
		".swift", ".ts", ".tsx", ".vue":
		return true
	default:
		return false
	}
}

func validateBraceCompleteDeletion(lines []string, deleteStart, deleteEnd int, path string) error {
	for _, pair := range bracePairsByLine(lines) {
		if pair.openLine < 0 || pair.closeLine < 0 {
			continue
		}
		openDeleted := lineInRange(pair.openLine, deleteStart, deleteEnd)
		closeDeleted := lineInRange(pair.closeLine, deleteStart, deleteEnd)
		switch {
		case openDeleted && !closeDeleted:
			if pair.openLine == deleteEnd {
				return fmt.Errorf("end_anchor in %s appears to open a code block at line %d; delete_range would delete that header but leave its closing line %d outside the range. Use an end_anchor on the block's closing line, or use edit_file/multi_edit with the full exact block", path, pair.openLine+1, pair.closeLine+1)
			}
			return fmt.Errorf("delete_range in %s would cut a code block: opening brace at line %d is deleted but its closing brace at line %d is kept. Choose anchors that include the whole block, or use edit_file/multi_edit with the full exact block", path, pair.openLine+1, pair.closeLine+1)
		case !openDeleted && closeDeleted:
			return fmt.Errorf("delete_range in %s would cut a code block: closing brace at line %d is deleted but its opening brace at line %d is kept. Choose anchors that include the whole block, or use edit_file/multi_edit with the full exact block", path, pair.closeLine+1, pair.openLine+1)
		}
	}
	return nil
}

func lineInRange(line, start, end int) bool {
	return line >= start && line <= end
}

type bracePair struct {
	openLine  int
	closeLine int
}

func bracePairsByLine(lines []string) []bracePair {
	var pairs []bracePair
	var stack []int
	inBlockComment := false
	var quote byte
	escaped := false

	for lineNo, line := range lines {
		for i := 0; i < len(line); i++ {
			c := line[i]
			if inBlockComment {
				if c == '*' && i+1 < len(line) && line[i+1] == '/' {
					inBlockComment = false
					i++
				}
				continue
			}
			if quote != 0 {
				if escaped {
					escaped = false
					continue
				}
				if c == '\\' {
					escaped = true
					continue
				}
				if c == quote {
					quote = 0
				}
				continue
			}
			if c == '/' && i+1 < len(line) {
				switch line[i+1] {
				case '/':
					i = len(line)
					continue
				case '*':
					inBlockComment = true
					i++
					continue
				}
			}
			switch c {
			case '\'', '"', '`':
				quote = c
			case '{':
				stack = append(stack, lineNo)
			case '}':
				if len(stack) == 0 {
					pairs = append(pairs, bracePair{openLine: -1, closeLine: lineNo})
					continue
				}
				openLine := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				pairs = append(pairs, bracePair{openLine: openLine, closeLine: lineNo})
			}
		}
		if quote == '\'' || quote == '"' {
			quote = 0
			escaped = false
		}
	}
	for _, openLine := range stack {
		pairs = append(pairs, bracePair{openLine: openLine, closeLine: -1})
	}
	return pairs
}

func lineMatchSummary(lines []string, target string, limit int) string {
	var matches []int
	for i, line := range lines {
		if line == target {
			matches = append(matches, i+1)
		}
	}
	if len(matches) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString(" (matching lines include ")
	for i, line := range matches {
		if i >= limit {
			b.WriteString(", ...")
			break
		}
		if i > 0 {
			b.WriteString(", ")
		}
		fmt.Fprint(&b, line)
	}
	b.WriteString(")")
	return b.String()
}
