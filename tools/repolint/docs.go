// docs.go — a document names who answers for it and states its content as
// rules, steps and tables. docs/DOCS_STANDARD.md is the contract; this file is
// the part of it a machine can hold. Classes are declared by path, never
// inferred from what a file happens to contain.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"unicode"

	"gopkg.in/yaml.v3"
)

type docClass int

const (
	// classDoc carries a metadata header and is held to the prose width.
	classDoc docClass = iota
	// classLanding renders as the repository's front page, where a header
	// table would be the first thing a visitor reads; CODEOWNERS owns it.
	classLanding
	// classMachine is read verbatim by a model, GitHub or a release body, so a
	// header would travel with it; CODEOWNERS owns it.
	classMachine
	classReleaseNote
)

const (
	docProseWidth        = 320
	releaseNoteItemWidth = 200
	proseWeightUnit      = 80
	docMetadataDelimiter = "---"
)

var docClassByPrefix = []struct {
	prefix string
	class  docClass
}{
	{"release-notes/", classReleaseNote},
	{"README.md", classLanding},
	{"README.zh-CN.md", classLanding},
	{"REASONIX.md", classMachine},
	{"AGENTS.md", classMachine},
	{"CLAUDE.md", classMachine},
	{"docs/EXTENSION_PROTOCOL.generated.md", classMachine},
	{".github/", classMachine},
	{".reasonix/", classMachine},
	{"internal/ext/skill/builtincontent/", classMachine},
	{"internal/runtime/guardian/", classMachine},
}

// docSkippedPrefixes are corpora and fixtures: text a benchmark or a test
// reads as data, which no reader is meant to follow.
var docSkippedPrefixes = []string{"benchmarks/"}

// translatedDocs are the onboarding pages kept in Chinese beside the English
// original. Every other document has one language, English.
var translatedDocs = []string{
	"README.zh-CN.md",
	"docs/GUIDE.zh-CN.md",
	"docs/CLI.zh-CN.md",
}

var (
	docStatuses       = []string{"active", "deprecated"}
	releaseNoteGroups = []string{"新增", "变更", "修复", "移除", "升级须知"}
	isoDate           = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
	listItem          = regexp.MustCompile(`^\s*(?:[-*+]|\d+\.)\s+`)
	changeReference   = regexp.MustCompile(`#\d+|\b[0-9a-f]{7,40}\b|https?://`)
)

func classifyDoc(rel string) (docClass, bool) {
	if !strings.HasSuffix(rel, ".md") {
		return 0, false
	}
	for _, p := range docSkippedPrefixes {
		if strings.HasPrefix(rel, p) {
			return 0, false
		}
	}
	for seg := range strings.SplitSeq(rel, "/") {
		if skipDirs[seg] {
			return 0, false
		}
	}
	for _, c := range docClassByPrefix {
		if rel == c.prefix || (strings.HasSuffix(c.prefix, "/") && strings.HasPrefix(rel, c.prefix)) {
			return c.class, true
		}
	}
	return classDoc, true
}

func checkDocs(root string) []Finding {
	files, err := repoFiles(root)
	if err != nil {
		return nil
	}
	rules, rulesErr := loadOwnerRules(root)
	var out []Finding
	for _, rel := range files {
		class, ok := classifyDoc(rel)
		if !ok {
			continue
		}
		data, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			continue
		}
		lines := strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
		if strings.HasSuffix(rel, ".zh-CN.md") && !slices.Contains(translatedDocs, rel) {
			out = append(out, Finding{rel, 1, ruleDocLanguage,
				"only onboarding pages keep a Chinese copy; the English document is the one maintained", 1})
		}
		switch class {
		case classDoc:
			if rulesErr == nil {
				out = append(out, checkDocMetadata(rel, lines, rules)...)
			}
			out = append(out, checkProse(rel, lines, docProseWidth)...)
		case classReleaseNote:
			out = append(out, checkReleaseNote(rel, lines)...)
		default:
			out = append(out, checkProse(rel, lines, docProseWidth)...)
		}
	}
	return out
}

// checkDocMetadata reads the header and holds its owners to CODEOWNERS, so the
// name printed on the page is the name GitHub asks for review.
func checkDocMetadata(rel string, lines []string, rules []ownerRule) []Finding {
	meta, ok := docMetadata(lines)
	if !ok {
		return []Finding{{rel, 1, ruleDocOwner, "no metadata header: start with ---, owner, backup, status, reviewed, ---", 1}}
	}
	var problems []string
	rule, owned := ownersOf(rules, rel)
	switch {
	case !owned || len(rule.owners) < 2:
		problems = append(problems, "no CODEOWNERS rule names a primary and a backup for this path")
	case meta["owner"] != rule.owners[0] || meta["backup"] != rule.owners[1]:
		problems = append(problems, fmt.Sprintf("owner/backup %s/%s differ from CODEOWNERS line %d (%s %s)",
			meta["owner"], meta["backup"], rule.line, rule.owners[0], rule.owners[1]))
	}
	if !slices.Contains(docStatuses, meta["status"]) {
		problems = append(problems, fmt.Sprintf("status %q is not one of %v", meta["status"], docStatuses))
	}
	if !isoDate.MatchString(meta["reviewed"]) {
		problems = append(problems, fmt.Sprintf("reviewed %q is not YYYY-MM-DD", meta["reviewed"]))
	}
	if len(problems) == 0 {
		return nil
	}
	return []Finding{{rel, 1, ruleDocOwner, strings.Join(problems, "; "), len(problems)}}
}

func docMetadata(lines []string) (map[string]string, bool) {
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != docMetadataDelimiter {
		return nil, false
	}
	meta := map[string]string{}
	for _, line := range lines[1:] {
		if strings.TrimSpace(line) == docMetadataDelimiter {
			return meta, true
		}
		if key, value, ok := strings.Cut(line, ":"); ok {
			value = strings.TrimSpace(value)
			if strings.HasPrefix(value, `"`) || strings.HasPrefix(value, "'") {
				var scalar string
				if err := yaml.Unmarshal([]byte(value), &scalar); err == nil {
					value = scalar
				}
			}
			meta[strings.TrimSpace(key)] = value
		}
	}
	return nil, false
}

// proseBlock is one paragraph or one list item with its continuation lines.
type proseBlock struct {
	line  int
	item  bool
	width int
	text  string
}

func proseBlocks(lines []string) []proseBlock {
	var out []proseBlock
	var cur *proseBlock
	flush := func() {
		if cur != nil {
			out = append(out, *cur)
			cur = nil
		}
	}
	inFence, inMeta := false, false
	for i, raw := range lines {
		t := strings.TrimSpace(raw)
		switch {
		case i == 0 && t == docMetadataDelimiter:
			inMeta = true
			continue
		case inMeta:
			inMeta = t != docMetadataDelimiter
			continue
		case strings.HasPrefix(t, "```"):
			inFence = !inFence
			flush()
			continue
		case inFence:
			continue
		case t == "" || t == ">" || strings.HasPrefix(t, "#") || strings.HasPrefix(t, "|") || strings.HasPrefix(t, "<"):
			flush()
			continue
		}
		if loc := listItem.FindStringIndex(raw); loc != nil {
			flush()
			cur = &proseBlock{line: i + 1, item: true, text: raw[loc[1]:]}
			continue
		}
		if cur == nil {
			cur = &proseBlock{line: i + 1}
		}
		cur.text += " " + strings.TrimLeft(t, "> ")
	}
	flush()
	for i := range out {
		out[i].text = strings.TrimSpace(out[i].text)
		out[i].width = displayWidth(out[i].text)
	}
	return out
}

// displayWidth counts a CJK character as two columns: the same budget in runes
// would let a Chinese paragraph run twice as long as an English one.
func displayWidth(s string) int {
	n := 0
	for _, r := range s {
		if unicode.In(r, unicode.Han, unicode.Hiragana, unicode.Katakana, unicode.Hangul) || (r >= 0x3000 && r <= 0x303f) || (r >= 0xff00 && r <= 0xffef) {
			n += 2
		} else {
			n++
		}
	}
	return n
}

func checkProse(rel string, lines []string, limit int) []Finding {
	var out []Finding
	for _, b := range proseBlocks(lines) {
		if b.width <= limit {
			continue
		}
		out = append(out, Finding{rel, b.line, ruleDocProse,
			fmt.Sprintf("%s is %d columns, over %d: split it into rules, steps or a table", blockKind(b), b.width, limit),
			(b.width - limit + proseWeightUnit - 1) / proseWeightUnit})
	}
	return out
}

func blockKind(b proseBlock) string {
	if b.item {
		return "list item"
	}
	return "paragraph"
}

// checkReleaseNote holds a version's notes to the published shape: one summary
// line, changes grouped under the fixed headings, one short referenced line per
// change. The reasoning behind a change lives in its commit.
func checkReleaseNote(rel string, lines []string) []Finding {
	var out []Finding
	paragraphs := 0
	for i, raw := range lines {
		t := strings.TrimSpace(raw)
		switch {
		case strings.HasPrefix(t, "## "):
			if !slices.Contains(releaseNoteGroups, strings.TrimSpace(t[3:])) {
				out = append(out, Finding{rel, i + 1, ruleReleaseNote,
					fmt.Sprintf("heading %q is not one of %v", t, releaseNoteGroups), 1})
			}
		case strings.HasPrefix(t, "#"):
			out = append(out, Finding{rel, i + 1, ruleReleaseNote, "only ## group headings; a change is one list item, not a section", 1})
		}
	}
	for _, b := range proseBlocks(lines) {
		switch {
		case !b.item:
			paragraphs++
			if paragraphs > 1 {
				out = append(out, Finding{rel, b.line, ruleReleaseNote, "only the first line may be a summary paragraph; state changes as list items", 1})
			}
		case !changeReference.MatchString(b.text):
			out = append(out, Finding{rel, b.line, ruleReleaseNote, "a change names its issue, pull request or commit", 1})
		}
		limit := docProseWidth
		if b.item {
			limit = releaseNoteItemWidth
		}
		if b.width > limit {
			out = append(out, Finding{rel, b.line, ruleDocProse,
				fmt.Sprintf("%s is %d columns, over %d", blockKind(b), b.width, limit),
				(b.width - limit + proseWeightUnit - 1) / proseWeightUnit})
		}
	}
	return out
}
