package diff

import (
	"regexp"
	"strconv"
	"strings"
)

var hunkHeader = regexp.MustCompile(`^@@ -(\d+)(?:,(\d+))? \+(\d+)(?:,(\d+))? @@`)

// gitExtendedHeader are the metadata lines git writes between its "diff --git"
// line and the "--- "/"+++ " pair. Each is optional and carries no content.
var gitExtendedHeader = []string{
	"old mode ", "new mode ", "deleted file mode ", "new file mode ",
	"copy from ", "copy to ", "rename from ", "rename to ",
	"similarity index ", "dissimilarity index ", "index ",
}

// logHeaderField prefixes the metadata lines git writes between a "commit …"
// line and the blank line before its indented message.
var logHeaderField = []string{"Author:", "AuthorDate:", "Commit:", "CommitDate:", "Date:", "Merge:"}

// IsUnifiedDiff reports whether text is, in its entirety, a unified diff — the
// shape `git diff` / `diff -u` writes, or `git show` / `git log -p`, whose
// `commit …` header and indented message precede the file sections. Every line
// must belong to that grammar and hunks must match their "@@" counts; one line
// left over rejects it, so prose or a build log after the diff stays prose.
func IsUnifiedDiff(text string) bool {
	p := &diffParser{lines: strings.Split(text, "\n")}
	for p.i < len(p.lines) && strings.TrimSpace(p.lines[p.i]) == "" {
		p.i++
	}
	if p.i >= len(p.lines) {
		return false
	}
	changed := false
	if strings.HasPrefix(p.at(), "commit ") {
		for {
			if !p.logBlock(&changed) {
				return false
			}
			p.skipBlank()
			if p.i >= len(p.lines) {
				return changed
			}
			if !strings.HasPrefix(p.at(), "commit ") {
				return false
			}
		}
	}
	for {
		if !p.file(&changed) {
			return false
		}
		p.skipBlank()
		if p.i >= len(p.lines) {
			return changed
		}
	}
}

type diffParser struct {
	lines []string
	i     int
}

func (p *diffParser) at() string {
	if p.i < len(p.lines) {
		return p.lines[p.i]
	}
	return ""
}

func (p *diffParser) skipBlank() {
	for p.i < len(p.lines) && strings.TrimSpace(p.lines[p.i]) == "" {
		p.i++
	}
}

// logBlock parses one `git show` / `git log -p` entry: the "commit …" line, its
// header fields, the indented message, and the file sections that follow. A
// block with no file section (a merge or empty commit) is legal; the caller
// still requires at least one hunk across the whole text.
func (p *diffParser) logBlock(changed *bool) bool {
	if !strings.HasPrefix(p.at(), "commit ") {
		return false
	}
	p.i++
	for p.i < len(p.lines) && strings.TrimSpace(p.lines[p.i]) != "" {
		if !isLogHeaderField(p.lines[p.i]) {
			return false
		}
		p.i++
	}
	// The message is indented four spaces; blank lines separate its paragraphs
	// and the file sections that follow.
	for p.i < len(p.lines) && (strings.TrimSpace(p.lines[p.i]) == "" || strings.HasPrefix(p.lines[p.i], "    ")) {
		p.i++
	}
	for strings.HasPrefix(p.at(), "diff --git ") {
		if !p.file(changed) {
			return false
		}
	}
	return true
}

// file parses one file section: an optional "diff --git" line, its extended
// headers, the "--- "/"+++ " pair, and one or more hunks.
func (p *diffParser) file(changed *bool) bool {
	if strings.HasPrefix(p.at(), "diff --git ") {
		p.i++
	}
	for p.i < len(p.lines) && isGitExtendedHeader(p.lines[p.i]) {
		p.i++
	}
	if !strings.HasPrefix(p.at(), "--- ") {
		return false
	}
	p.i++
	p.skipNoNewline()
	if !strings.HasPrefix(p.at(), "+++ ") {
		return false
	}
	p.i++
	p.skipNoNewline()
	if !p.hunk(changed) {
		return false
	}
	for strings.HasPrefix(p.at(), "@@ ") {
		if !p.hunk(changed) {
			return false
		}
	}
	return true
}

// hunk parses one "@@" header and exactly the body it declares: oldCount lines
// of " "/"-" and newCount lines of " "/"+". A "\" no-newline marker follows the
// line it applies to and carries no count.
func (p *diffParser) hunk(changed *bool) bool {
	m := hunkHeader.FindStringSubmatch(p.at())
	if m == nil {
		return false
	}
	p.i++
	oldLeft, newLeft := hunkCount(m[2]), hunkCount(m[4])
	for oldLeft > 0 || newLeft > 0 {
		ln := p.at()
		if ln == "" || ln[0] == '\\' {
			return false
		}
		switch ln[0] {
		case ' ':
			if oldLeft <= 0 || newLeft <= 0 {
				return false
			}
			oldLeft--
			newLeft--
		case '-':
			if oldLeft <= 0 {
				return false
			}
			oldLeft--
			*changed = true
		case '+':
			if newLeft <= 0 {
				return false
			}
			newLeft--
			*changed = true
		default:
			return false
		}
		p.i++
		p.skipNoNewline()
	}
	return true
}

func (p *diffParser) skipNoNewline() {
	for strings.HasPrefix(p.at(), `\ `) {
		p.i++
	}
}

func hunkCount(s string) int {
	if s == "" {
		return 1
	}
	n, _ := strconv.Atoi(s)
	return n
}

func isGitExtendedHeader(ln string) bool {
	for _, pre := range gitExtendedHeader {
		if strings.HasPrefix(ln, pre) {
			return true
		}
	}
	return false
}

func isLogHeaderField(ln string) bool {
	for _, pre := range logHeaderField {
		if strings.HasPrefix(ln, pre) {
			return true
		}
	}
	return false
}
