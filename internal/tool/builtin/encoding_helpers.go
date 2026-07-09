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
	b, err := os.ReadFile(path)
	if err != nil {
		return "", 0, err
	}
	enc, _ = fileenc.Detect(b)
	return string(fileenc.Decode(b, enc)), enc, nil
}

// writeFileEncoded encodes content back to the given encoding and writes it.
func writeFileEncoded(path string, content string, enc fileenc.Kind) error {
	return os.WriteFile(path, fileenc.Encode(content, enc), 0o644)
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
	if strings.Contains(content, toCRLF(old)) {
		return toCRLF(old), toCRLF(new)
	}
	return old, new
}

func toCRLF(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\n", "\r\n")
}

func matchReplacementLineEndings(content, replacement string) string {
	if strings.Contains(content, "\r\n") {
		return toCRLF(replacement)
	}
	return replacement
}

type editApplyResult struct {
	updated string
	applied int
	matches int
	fuzzy   bool
}

type editRange struct {
	start int
	end   int
}

// applyOldStringEdit is the shared edit_file/multi_edit/Preview contract. It
// preserves the exact-match rule first, then falls back to a narrow fuzzy match
// for the mismatches read_file commonly introduces or hides: trailing
// whitespace, tab-vs-spaces indentation, and copied read_file line prefixes.
// Non-replace_all edits still require exactly one match, including fuzzy
// matches.
func applyOldStringEdit(content, oldString, newString string, replaceAll bool) editApplyResult {
	old, newStr := matchLineEndings(content, oldString, newString)
	if replaceAll {
		if count := strings.Count(content, old); count > 0 {
			return editApplyResult{
				updated: strings.ReplaceAll(content, old, newStr),
				applied: count,
				matches: count,
			}
		}
		ranges := fuzzyEditRanges(content, old)
		if len(ranges) == 0 {
			return editApplyResult{updated: content}
		}
		return editApplyResult{
			updated: replaceEditRanges(content, ranges, matchReplacementLineEndings(content, newStr)),
			applied: len(ranges),
			matches: len(ranges),
			fuzzy:   true,
		}
	}

	switch count := strings.Count(content, old); count {
	case 0:
		ranges := fuzzyEditRanges(content, old)
		if len(ranges) != 1 {
			return editApplyResult{updated: content, matches: len(ranges)}
		}
		return editApplyResult{
			updated: replaceEditRanges(content, ranges, matchReplacementLineEndings(content, newStr)),
			applied: 1,
			matches: 1,
			fuzzy:   true,
		}
	case 1:
		return editApplyResult{
			updated: strings.Replace(content, old, newStr, 1),
			applied: 1,
			matches: 1,
		}
	default:
		return editApplyResult{updated: content, matches: count}
	}
}

func oldStringNotFoundError(path, oldString, content string) error {
	hint := oldStringNotFoundHint(oldString, content)
	if line, text, ok := nearestContentLine(oldString, content); ok {
		return fmt.Errorf("old_string not found in %s (nearest line %d: %q).%s", path, line, text, hint)
	}
	return fmt.Errorf("old_string not found in %s.%s", path, hint)
}

func oldStringNotFoundHint(oldString, content string) string {
	base := " Re-read the current file before retrying; if several related edits target the same area, combine the final replacements in one multi_edit call."
	if !strings.Contains(content, "\r\n") {
		return base
	}
	normalizedContent := strings.ReplaceAll(content, "\r\n", "\n")
	normalizedOld := strings.ReplaceAll(oldString, "\r\n", "\n")
	if strings.Contains(normalizedContent, normalizedOld) {
		return " The target file uses CRLF line endings; edit_file/multi_edit normally normalize LF-only old_string for CRLF files, so this is likely stale context. Re-read the current file before retrying."
	}
	return " The target file uses CRLF line endings, but edit_file/multi_edit already tolerate LF-only old_string for CRLF files; check for stale, incomplete, or non-unique context before retrying."
}

func oldStringNotUniqueError(path, oldString, content string, matches int, replaceAllHint bool) error {
	lineHint := oldStringMatchLineSummary(oldString, content, 5)
	if replaceAllHint {
		return fmt.Errorf("old_string is not unique in %s (%d matches)%s; add nearby unique code, not just repeated separator lines, or set replace_all if every match should change", path, matches, lineHint)
	}
	return fmt.Errorf("old_string is not unique in %s (%d matches)%s; add nearby unique code, not just repeated separator lines", path, matches, lineHint)
}

type lineSegment struct {
	raw   string
	start int
	end   int
}

type fuzzyMode struct {
	stripOldReadPrefixes bool
	trimTrailing         bool
	expandTabs           bool
	trimLeading          bool
}

func fuzzyEditRanges(content, old string) []editRange {
	if old == "" || content == "" {
		return nil
	}
	contentLines := splitLineSegments(content)
	oldLines := splitLineSegments(old)
	if len(oldLines) == 0 || len(oldLines) > len(contentLines) {
		return nil
	}

	oldHasReadPrefixes := allLinesHaveReadFilePrefix(oldLines)
	modes := []fuzzyMode{
		{trimTrailing: true},
		{trimTrailing: true, expandTabs: true},
	}
	if oldHasReadPrefixes {
		modes = append(modes,
			fuzzyMode{stripOldReadPrefixes: true, trimTrailing: true},
			fuzzyMode{stripOldReadPrefixes: true, trimTrailing: true, expandTabs: true},
		)
	}

	for _, mode := range modes {
		normOld := make([]string, len(oldLines))
		for i, line := range oldLines {
			normOld[i] = normalizeFuzzyLine(line.raw, lineHasNewline(line.raw), mode, mode.stripOldReadPrefixes)
		}
		var ranges []editRange
		for i := 0; i <= len(contentLines)-len(oldLines); {
			if fuzzyWindowMatches(contentLines[i:i+len(oldLines)], oldLines, normOld, mode) {
				ranges = append(ranges, editRange{
					start: contentLines[i].start,
					end:   fuzzyWindowEnd(contentLines[i+len(oldLines)-1], oldLines[len(oldLines)-1]),
				})
				i += len(oldLines)
				continue
			}
			i++
		}
		if len(ranges) > 0 {
			return ranges
		}
	}
	return nil
}

func fuzzyWindowMatches(contentWindow, oldLines []lineSegment, normOld []string, mode fuzzyMode) bool {
	for i, contentLine := range contentWindow {
		oldHasNewline := lineHasNewline(oldLines[i].raw)
		if oldHasNewline && !lineHasNewline(contentLine.raw) {
			return false
		}
		got := normalizeFuzzyLine(contentLine.raw, oldHasNewline, mode, false)
		if got != normOld[i] {
			return false
		}
	}
	return true
}

func splitLineSegments(s string) []lineSegment {
	if s == "" {
		return nil
	}
	var lines []lineSegment
	start := 0
	for i, r := range s {
		if r == '\n' {
			end := i + 1
			lines = append(lines, lineSegment{raw: s[start:end], start: start, end: end})
			start = end
		}
	}
	if start < len(s) {
		lines = append(lines, lineSegment{raw: s[start:], start: start, end: len(s)})
	}
	return lines
}

func lineHasNewline(line string) bool {
	return strings.HasSuffix(line, "\n")
}

func fuzzyWindowEnd(contentLast, oldLast lineSegment) int {
	if lineHasNewline(oldLast.raw) || !lineHasNewline(contentLast.raw) {
		return contentLast.end
	}
	end := contentLast.end - 1
	if end > contentLast.start && contentLast.raw[len(contentLast.raw)-2] == '\r' {
		end--
	}
	return end
}

func normalizeFuzzyLine(line string, includeNewline bool, mode fuzzyMode, stripReadPrefix bool) string {
	body := strings.TrimSuffix(line, "\n")
	if stripReadPrefix {
		body, _ = stripReadFileLinePrefix(body)
	}
	if mode.trimTrailing {
		body = strings.TrimRight(body, " \t\r")
	}
	if mode.expandTabs {
		body = strings.ReplaceAll(body, "\t", "    ")
	}
	if mode.trimLeading {
		body = strings.TrimLeft(body, " \t")
	}
	if includeNewline {
		return body + "\n"
	}
	return body
}

func allLinesHaveReadFilePrefix(lines []lineSegment) bool {
	if len(lines) == 0 {
		return false
	}
	for _, line := range lines {
		body := strings.TrimSuffix(line.raw, "\n")
		if _, ok := stripReadFileLinePrefix(body); !ok {
			return false
		}
	}
	return true
}

func stripReadFileLinePrefix(line string) (string, bool) {
	i := 0
	for i < len(line) && (line[i] == ' ' || line[i] == '\t') {
		i++
	}
	j := i
	for j < len(line) && line[j] >= '0' && line[j] <= '9' {
		j++
	}
	if j == i || !strings.HasPrefix(line[j:], "\u2192") {
		return line, false
	}
	return line[j+len("\u2192"):], true
}

func replaceEditRanges(content string, ranges []editRange, replacement string) string {
	updated := content
	for i := len(ranges) - 1; i >= 0; i-- {
		r := ranges[i]
		updated = updated[:r.start] + replacement + updated[r.end:]
	}
	return updated
}

func nearestContentLine(oldString, content string) (int, string, bool) {
	oldLines := splitLineSegments(oldString)
	if len(oldLines) == 0 {
		return 0, "", false
	}
	target := strings.TrimSpace(normalizeFuzzyLine(oldLines[0].raw, false, fuzzyMode{trimTrailing: true, expandTabs: true}, true))
	if target == "" {
		return 0, "", false
	}
	bestLine := 0
	bestScore := 0
	bestText := ""
	for i, line := range splitLineSegments(content) {
		text := strings.TrimSuffix(line.raw, "\n")
		score := commonPrefixLen(strings.TrimSpace(strings.ReplaceAll(text, "\t", "    ")), target)
		if score > bestScore {
			bestLine = i + 1
			bestScore = score
			bestText = text
		}
	}
	if bestScore < 3 {
		return 0, "", false
	}
	return bestLine, bestText, true
}

func oldStringMatchLineSummary(oldString, content string, limit int) string {
	if limit <= 0 {
		return ""
	}
	target := firstNonEmptyLine(oldString)
	if target == "" {
		return ""
	}
	var matches []int
	for i, line := range splitLineSegments(content) {
		text := strings.TrimSuffix(line.raw, "\n")
		text = strings.TrimSuffix(text, "\r")
		if strings.Contains(text, target) {
			matches = append(matches, i+1)
		}
	}
	if len(matches) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("; matching lines include ")
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
	return b.String()
}

func firstNonEmptyLine(s string) string {
	for _, line := range splitLineSegments(s) {
		text := strings.TrimSpace(strings.TrimSuffix(line.raw, "\n"))
		text = strings.TrimSuffix(text, "\r")
		if text != "" {
			return text
		}
	}
	return ""
}

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
