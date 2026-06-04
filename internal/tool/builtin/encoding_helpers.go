package builtin

import (
	"fmt"
	"os"
	"strings"

	fileenc "reasonix/internal/fileutil/encoding"
)

// readFileEncoded reads a file and decodes its encoding to UTF-8.
// Returns the decoded content and the detected encoding kind so callers
// can re-encode on write to preserve the original charset.
func readFileEncoded(path string) (content string, enc fileenc.Kind, err error) {
	return readFileEncodedWith(path, "")
}

// readFileEncodedWith reads a file and decodes it to UTF-8. When encName is
// non-empty the encoding is forced (skip auto-detection); otherwise behaves
// like readFileEncoded.
func readFileEncodedWith(path string, encName string) (content string, enc fileenc.Kind, err error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", 0, err
	}
	if forced, ok := fileenc.ParseName(encName); ok {
		return string(fileenc.Decode(b, forced)), forced, nil
	}
	enc, _ = fileenc.Detect(b)
	return string(fileenc.Decode(b, enc)), enc, nil
}

// writeFileEncoded encodes content back to the given encoding and writes it.
func writeFileEncoded(path string, content string, enc fileenc.Kind) error {
	return os.WriteFile(path, fileenc.Encode(content, enc), 0o644)
}

// writeFileEncodedWith writes content to path. When encName is non-empty the
// encoding is forced; otherwise enc (typically from readFileEncoded) is used.
func writeFileEncodedWith(path string, content string, enc fileenc.Kind, encName string) error {
	if forced, ok := fileenc.ParseName(encName); ok {
		return os.WriteFile(path, fileenc.Encode(content, forced), 0o644)
	}
	return writeFileEncoded(path, content, enc)
}

// matchLineEndings adapts an edit's old/new text to a CRLF file when the literal
// old_string isn't present but its CRLF form is. read_file strips '\r' (bufio
// ScanLines), so a model's multi-line old_string arrives LF-only while a
// Windows/CJK source stores '\r\n'; rewriting search and replacement to the
// file's ending fixes the match without rewriting the file's other line endings.
func matchLineEndings(content, old, new string) (string, string) {
	if strings.Contains(content, old) || !strings.Contains(content, "\r\n") {
		return old, new
	}
	toCRLF := func(s string) string {
		return strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\n", "\r\n")
	}
	if strings.Contains(content, toCRLF(old)) {
		return toCRLF(old), toCRLF(new)
	}
	return old, new
}

// diagnoseNotFound builds a helpful error message when old_string is not found in
// content. It locates the nearest line-level match (longest common prefix) and
// reports its line number plus the first differing characters, so the model can
// fix its old_string in one shot instead of blind retries.
func diagnoseNotFound(path, old, content string) error {
	if old == "" {
		return fmt.Errorf("old_string not found in %s", path)
	}
	oldLines := strings.Split(old, "\n")
	contentLines := strings.Split(content, "\n")

	bestLine := -1
	bestScore := 0
	for i, cl := range contentLines {
		score := commonPrefixLen(strings.TrimSpace(cl), strings.TrimSpace(oldLines[0]))
		if score > bestScore {
			bestScore = score
			bestLine = i
		}
	}

	if bestLine < 0 || bestScore < 3 {
		return fmt.Errorf("old_string not found in %s (no close match found)", path)
	}

	// Show the nearest match's line number and the actual content there.
	start := bestLine
	end := start + len(oldLines)
	if end > len(contentLines) {
		end = len(contentLines)
	}
	actual := strings.Join(contentLines[start:end], "\n")

	// Trim both for comparison readability.
	actualTrim := strings.TrimSpace(actual)
	oldTrim := strings.TrimSpace(old)
	if len(actualTrim) > 200 {
		actualTrim = actualTrim[:200] + "…"
	}
	if len(oldTrim) > 200 {
		oldTrim = oldTrim[:200] + "…"
	}

	return fmt.Errorf("old_string not found in %s. Nearest match at line %d:\n  expected: %s\n  actual:   %s",
		path, bestLine+1, quoteLine(oldTrim), quoteLine(actualTrim))
}

// diagnoseNotUnique builds a helpful error message when old_string appears more
// than once, reporting the count and the line numbers of each occurrence so the
// model can add distinguishing context.
func diagnoseNotUnique(path, old, content string) error {
	count := strings.Count(content, old)
	lines := matchLineNumbers(old, content)
	if len(lines) > 8 {
		lines = append(lines[:8], -1) // sentinel for "and more"
	}
	lineStr := formatLineList(lines)
	return fmt.Errorf("old_string is not unique in %s (%d matches at %s); add more surrounding context to disambiguate",
		path, count, lineStr)
}

// matchLineNumbers returns the 1-based line numbers where old appears in content.
// Uses strings.Index for SIMD-optimized substring search instead of a naive
// byte-by-byte scan, giving ~100x speedup on large files.
func matchLineNumbers(old, content string) []int {
	if old == "" {
		return nil
	}
	var lines []int
	lineNo := 1
	offset := 0
	for {
		idx := strings.Index(content[offset:], old)
		if idx < 0 {
			break
		}
		pos := offset + idx
		// Count newlines before this match to get the line number.
		lineNo += strings.Count(content[offset:pos], "\n")
		lines = append(lines, lineNo)
		offset = pos + len(old)
		if offset >= len(content) {
			break
		}
	}
	return lines
}

// formatLineList formats a slice of line numbers for display, with -1 meaning "…".
func formatLineList(lines []int) string {
	parts := make([]string, 0, len(lines))
	for _, l := range lines {
		if l < 0 {
			parts = append(parts, "…")
		} else {
			parts = append(parts, fmt.Sprintf("line %d", l))
		}
	}
	return strings.Join(parts, ", ")
}

// commonPrefixLen returns the number of leading runes two strings share.
func commonPrefixLen(a, b string) int {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	for i := 0; i < n; i++ {
		if a[i] != b[i] {
			return i
		}
	}
	return n
}

// quoteLine wraps a line sample for display in an error message, making
// invisible whitespace visible: tabs render as "→" so the model can tell
// tab-indented code from space-indented code at a glance.
func quoteLine(s string) string {
	s = strings.ReplaceAll(s, "\t", "→")
	if strings.Contains(s, "\n") {
		return "«" + strings.ReplaceAll(s, "\n", "↵") + "»"
	}
	return "`" + s + "`"
}

// editSnippet returns a short context window (±contextLines around the edit)
// so the model can verify the edit landed in the right place. Tabs and other
// invisible whitespace are rendered visibly.
func editSnippet(content, old, newStr string, contextLines int) string {
	if old == "" && newStr == "" {
		return ""
	}
	lines := strings.Split(content, "\n")
	// Find the line where the replacement starts.
	idx := strings.Index(content, newStr)
	if idx < 0 {
		return "" // replacement not in content (shouldn't happen)
	}
	editLine := strings.Count(content[:idx], "\n")

	start := editLine - contextLines
	if start < 0 {
		start = 0
	}
	end := editLine + strings.Count(newStr, "\n") + 1 + contextLines
	if end > len(lines) {
		end = len(lines)
	}

	var sb strings.Builder
	width := len(fmt.Sprintf("%d", end))
	for i := start; i < end; i++ {
		marker := " "
		if i >= editLine && i <= editLine+strings.Count(newStr, "\n") {
			marker = "›"
		}
		line := strings.ReplaceAll(lines[i], "\t", "→")
		fmt.Fprintf(&sb, "%s %*d│%s\n", marker, width, i+1, line)
	}
	return sb.String()
}

// fuzzyFind attempts a whitespace-tolerant match of old in content when exact
// matching fails. It tries two progressive relaxations:
//
//  1. Trim trailing whitespace from every line (models often add or drop
//     spaces at line ends, or the file has trailing spaces the model didn't
//     reproduce).
//  2. Also trim leading whitespace (the model mis-indented the block or the
//     code was extracted from a different indentation context).
//
// When a fuzzy match is found it returns the **actual** text from content
// (with original whitespace preserved) so that strings.Replace substitutes
// the real region without disturbing the file's formatting.
//
// Returns ("", false) if no match is found at any relaxation level.
func fuzzyFind(content, old string) (actualOld string, found bool) {
	if old == "" || content == "" {
		return "", false
	}

	// Split on \n (works for both LF and CRLF — \r stays attached).
	oldLines := strings.Split(old, "\n")
	contentLines := strings.Split(content, "\n")
	nOld := len(oldLines)
	if nOld == 0 || nOld > len(contentLines) {
		return "", false
	}

	// --- Level 1: trim trailing whitespace per line ---
	trimTrail := func(lines []string) []string {
		out := make([]string, len(lines))
		for i, l := range lines {
			out[i] = strings.TrimRight(l, " \t\r")
		}
		return out
	}

	normOld := trimTrail(oldLines)
	for i := 0; i <= len(contentLines)-nOld; i++ {
		window := contentLines[i : i+nOld]
		if linesEqual(trimTrail(window), normOld) {
			return strings.Join(window, "\n"), true
		}
	}

	// --- Level 1.5: normalize tabs to spaces (tab↔space mismatch) ---
	// Catches the common case where the model emits spaces but the file uses
	// tabs (or vice versa), without stripping all indentation like Level 2.
	expandTabs := func(lines []string) []string {
		out := make([]string, len(lines))
		for i, l := range lines {
			out[i] = strings.ReplaceAll(l, "\t", "    ")
		}
		return out
	}

	normOldTab := expandTabs(normOld)
	for i := 0; i <= len(contentLines)-nOld; i++ {
		window := contentLines[i : i+nOld]
		if linesEqual(expandTabs(trimTrail(window)), normOldTab) {
			return strings.Join(window, "\n"), true
		}
	}

	// --- Level 2: also trim leading whitespace (dedent) ---
	trimLead := func(lines []string) []string {
		out := make([]string, len(lines))
		for i, l := range lines {
			out[i] = strings.TrimLeft(l, " \t")
		}
		return out
	}

	normOld2 := trimLead(normOld)
	for i := 0; i <= len(contentLines)-nOld; i++ {
		window := contentLines[i : i+nOld]
		if linesEqual(trimLead(trimTrail(window)), normOld2) {
			return strings.Join(window, "\n"), true
		}
	}

	return "", false
}

// linesEqual compares two line slices element-wise.
func linesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
