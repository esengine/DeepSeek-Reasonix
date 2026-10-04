package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// docRepo is a git repository holding files, uncommitted: repoFiles lists what
// git does not ignore, so no commit is needed for the gate to see them.
func docRepo(t *testing.T, files map[string]string) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not on PATH")
	}
	dir, err := os.MkdirTemp("", "repolint-docs")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	for rel, body := range files {
		path := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	git(t, dir, "init", "--quiet")
	return dir
}

func ruleFindings(findings []Finding, rule string) []Finding {
	var out []Finding
	for _, f := range findings {
		if f.Rule == rule {
			out = append(out, f)
		}
	}
	return out
}

func TestOwnerPatternFollowsCodeOwnersAnchoring(t *testing.T) {
	cases := []struct {
		pattern, path string
		want          bool
	}{
		{"*", "docs/GUIDE.md", true},
		{"/docs/", "docs/GUIDE.md", true},
		{"/docs/", "sub/docs/GUIDE.md", false},
		{"docs/", "docs/GUIDE.md", true},
		{"/docs/STUDIO_*.md", "docs/STUDIO_RELEASE.md", true},
		{"/docs/STUDIO_*.md", "docs/nested/STUDIO_RELEASE.md", false},
		{"*.md", "a/b/c.md", true},
		{"/release-notes/**/*.md", "release-notes/studio/2.0.0.md", true},
		{"/SECURITY.md", "SECURITY.md", true},
		{"/SECURITY.md", "docs/SECURITY.md", false},
	}
	for _, c := range cases {
		if got := ownerPattern(c.pattern).MatchString(c.path); got != c.want {
			t.Errorf("%s vs %s = %v, want %v", c.pattern, c.path, got, c.want)
		}
	}
}

func TestCodeOwnersNeedsALivePathAndTwoPeople(t *testing.T) {
	root := docRepo(t, map[string]string{
		".github/CODEOWNERS": "* @a @b\n/docs/ @a\n/gone.yml @a @b\n/kept.md @a @a\n",
		"docs/x.md":          "x",
		"kept.md":            "x",
	})
	got := ruleFindings(checkCodeOwners(root), ruleCodeOwners)
	lines := map[int]bool{}
	for _, f := range got {
		lines[f.Line] = true
	}
	if len(got) != 3 || !lines[2] || !lines[3] || !lines[4] {
		t.Fatalf("findings = %+v, want the single owner (2), the dead path (3) and the repeated owner (4)", got)
	}
}

func TestADocumentNamesItsOwnersAsCodeOwnersDoes(t *testing.T) {
	header := func(owner, backup string) string {
		return "---\nowner: " + owner + "\nbackup: " + backup + "\nstatus: active\nreviewed: 2026-09-17\n---\n# T\n"
	}
	root := docRepo(t, map[string]string{
		".github/CODEOWNERS":  "* @a @b\n/docs/ @b @a\n",
		"docs/good.md":        header("@b", "@a"),
		"docs/double.md":      header(`"@b"`, `"@a"`),
		"docs/single.md":      header("'@b'", "'@a'"),
		"docs/swapped.md":     header("@a", "@b"),
		"docs/quoted-swap.md": header(`"@a"`, `"@b"`),
		"docs/broken.md":      header(`"@b`, `"@a"`),
		"docs/bare.md":        "# No header\n",
		"top.md":              header("@a", "@b"),
		"README.md":           "# Landing page, owned through CODEOWNERS only\n",
		"REASONIX.md":         "# Read by a model verbatim\n",
	})
	got := ruleFindings(checkDocs(root), ruleDocOwner)
	flagged := map[string]string{}
	for _, f := range got {
		flagged[f.File] = f.Msg
	}
	if len(flagged) != 4 || flagged["docs/swapped.md"] == "" || flagged["docs/quoted-swap.md"] == "" || flagged["docs/broken.md"] == "" || flagged["docs/bare.md"] == "" {
		t.Fatalf("flagged = %v, want swapped owners, malformed quotes and the missing header", flagged)
	}
	if !strings.Contains(flagged["docs/swapped.md"], "CODEOWNERS line 2") {
		t.Fatalf("the finding must point at the rule it disagrees with: %s", flagged["docs/swapped.md"])
	}
}

func TestStatusAndReviewDateAreChecked(t *testing.T) {
	rules := []ownerRule{{line: 1, pattern: "*", owners: []string{"@a", "@b"}, match: ownerPattern("*")}}
	lines := strings.Split("---\nowner: @a\nbackup: @b\nstatus: draft\nreviewed: last week\n---\n", "\n")
	got := checkDocMetadata("x.md", lines, rules)
	if len(got) != 1 || got[0].Weight != 2 {
		t.Fatalf("findings = %+v, want one finding weighing both problems", got)
	}
}

func TestProseWidthCountsChineseAsTwoColumns(t *testing.T) {
	if got := displayWidth("ab中文，"); got != 8 {
		t.Fatalf("width = %d, want 8", got)
	}
	long := strings.Repeat("字", 170)
	lines := []string{"---", "owner: @a", "---", "# Title", "", long, "", "- short item", "```", long, "```", "| " + long + " |"}
	got := checkProse("x.md", lines, docProseWidth)
	if len(got) != 1 || got[0].Line != 6 {
		t.Fatalf("findings = %+v, want only the paragraph: fences, tables and the header are not prose", got)
	}
	if got[0].Weight != 1 {
		t.Fatalf("weight = %d, want the 20-column excess rounded up to one unit", got[0].Weight)
	}
}

func TestAWrappedParagraphIsOneBlock(t *testing.T) {
	line := strings.Repeat("word ", 20)
	blocks := proseBlocks([]string{line, line, line, line, "", "- item", "  continued " + line})
	if len(blocks) != 2 || blocks[0].width != 399 || !blocks[1].item || !strings.Contains(blocks[1].text, "continued") {
		t.Fatalf("blocks = %+v, want one wrapped paragraph and one item with its continuation", blocks)
	}
}

func TestQuotedParagraphsAreSeparateProseBlocks(t *testing.T) {
	line := "> " + strings.Repeat("word ", 20)
	lines := []string{line, line, ">", line, line, "> ", "> " + strings.Repeat("word ", 70)}
	blocks := proseBlocks(lines)
	if len(blocks) != 3 || blocks[0].width != 199 || blocks[1].width != 199 || blocks[2].line != 7 {
		t.Fatalf("blocks = %+v, want three quoted paragraphs with wrapped lines kept together", blocks)
	}
	got := checkProse("x.md", lines, docProseWidth)
	if len(got) != 1 || got[0].Line != 7 {
		t.Fatalf("findings = %+v, want only the final overlong quoted paragraph", got)
	}
}

func TestOnlyOnboardingPagesKeepAChineseCopy(t *testing.T) {
	root := docRepo(t, map[string]string{
		".github/CODEOWNERS":    "* @a @b\n",
		"README.zh-CN.md":       "# 入门\n",
		"docs/GUIDE.zh-CN.md":   "---\nowner: @a\nbackup: @b\nstatus: active\nreviewed: 2026-09-17\n---\n",
		"docs/SPEC.zh-CN.md":    "---\nowner: @a\nbackup: @b\nstatus: active\nreviewed: 2026-09-17\n---\n",
		"benchmarks/x.zh-CN.md": "corpus",
	})
	got := ruleFindings(checkDocs(root), ruleDocLanguage)
	if len(got) != 1 || got[0].File != "docs/SPEC.zh-CN.md" {
		t.Fatalf("findings = %+v, want only the non-onboarding translation", got)
	}
}

func TestAReleaseNoteIsGroupedReferencedAndShort(t *testing.T) {
	note := strings.Join([]string{
		"一句话概括本版。",
		"",
		"## 修复",
		"- 读图模型不再说看不到图 (44150b0aa)",
		"- 没有引用的改动",
		"- " + strings.Repeat("长", 110) + " #10435",
		"",
		"### 细节",
		"",
		"第二段散文。",
		"",
		"## 亮点",
	}, "\n")
	got := checkReleaseNote("release-notes/studio/9.9.9.md", strings.Split(note, "\n"))
	byRule := map[string]int{}
	for _, f := range got {
		byRule[f.Rule]++
	}
	if byRule[ruleReleaseNote] != 4 || byRule[ruleDocProse] != 1 {
		t.Fatalf("findings = %+v, want sub-heading, unknown group, unreferenced item, second paragraph, and one long item", got)
	}
}

func TestClassifyDocSkipsMigrationBackup(t *testing.T) {
	if _, ok := classifyDoc(".migration-backup/legacy-fact.md"); ok {
		t.Fatal("a store-owned memory backup is not an authored doc")
	}
}
