package memory

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	fileencoding "reasonix/internal/fileutil/encoding"
)

// TestComposeEmptyIsIdentity is the cache-first invariant: with no memory at
// all, Compose must return the base prompt byte-for-byte, so the cached system
// prefix is exactly what it was before memory existed.
func TestComposeEmptyIsIdentity(t *testing.T) {
	base := "You are a helpful coding agent.\nBe concise."
	got := Compose(base, &Set{})
	if got != base {
		t.Fatalf("empty memory changed the prompt:\n base=%q\n got =%q", base, got)
	}
	// A nil-ish set (no docs, blank index) must also be identity.
	if got := Compose(base, &Set{Index: "   \n"}); got != base {
		t.Fatalf("blank index changed the prompt: got %q", got)
	}
}

// TestComposeAppendsAfterBase verifies memory folds in *after* the base prompt,
// so the base stays a valid cache prefix even as memory changes between sessions.
func TestComposeAppendsAfterBase(t *testing.T) {
	base := "BASE PROMPT"
	set := &Set{Docs: []Source{{Path: "/p/REASONIX.md", Scope: ScopeProject, Body: "Use tabs."}}}
	got := Compose(base, set)
	if !strings.HasPrefix(got, base) {
		t.Fatalf("base is not the prefix of the composed prompt:\n%q", got)
	}
	if !strings.Contains(got, "Use tabs.") {
		t.Fatalf("doc body missing from composed prompt:\n%q", got)
	}
}

// TestDiscoverPrecedenceOrder checks user → ancestor → project → local ordering,
// which puts the most specific guidance last.
func TestDiscoverPrecedenceOrder(t *testing.T) {
	root := t.TempDir()
	user := filepath.Join(root, "userconfig")
	proj := filepath.Join(root, "proj")
	mustMkdir(t, user)
	mustMkdir(t, proj)
	// Make proj a git root so discovery stops there.
	mustMkdir(t, filepath.Join(proj, ".git"))

	mustWrite(t, filepath.Join(user, "REASONIX.md"), "USER LEVEL")
	mustWrite(t, filepath.Join(proj, "REASONIX.md"), "PROJECT LEVEL")
	mustWrite(t, filepath.Join(proj, "REASONIX.local.md"), "LOCAL LEVEL")

	set := Load(Options{CWD: proj, UserDir: user})
	if len(set.Docs) != 3 {
		t.Fatalf("want 3 docs, got %d: %+v", len(set.Docs), set.Docs)
	}
	wantScopes := []Scope{ScopeUser, ScopeProject, ScopeLocal}
	for i, s := range wantScopes {
		if set.Docs[i].Scope != s {
			t.Fatalf("doc %d: want scope %q, got %q", i, s, set.Docs[i].Scope)
		}
	}
	// In the composed block, local must appear after project must appear after user.
	block := set.Block()
	iu, ip, il := strings.Index(block, "USER LEVEL"), strings.Index(block, "PROJECT LEVEL"), strings.Index(block, "LOCAL LEVEL")
	if !(iu >= 0 && iu < ip && ip < il) {
		t.Fatalf("precedence order wrong in block: user=%d project=%d local=%d\n%s", iu, ip, il, block)
	}
}

func TestDiscoverDecodesGB18030PrimaryDoc(t *testing.T) {
	proj := t.TempDir()
	mustMkdir(t, filepath.Join(proj, ".git"))
	body := "# 项目约定\n\n始终使用中文回答。"
	if err := os.WriteFile(filepath.Join(proj, "AGENTS.md"), fileencoding.Encode(body, fileencoding.GB18030), 0o644); err != nil {
		t.Fatal(err)
	}

	set := Load(Options{CWD: proj})
	if len(set.Docs) != 1 || !strings.Contains(set.Docs[0].Body, "始终使用中文回答") {
		t.Fatalf("decoded docs = %+v", set.Docs)
	}
}

// TestImportResolution checks "@path" inlining, including a relative import.
func TestImportResolution(t *testing.T) {
	proj := t.TempDir()
	mustMkdir(t, filepath.Join(proj, ".git"))
	mustWrite(t, filepath.Join(proj, "shared.md"), "SHARED CONTENT")
	mustWrite(t, filepath.Join(proj, "REASONIX.md"), "Top line\n@shared.md\nBottom line")

	set := Load(Options{CWD: proj})
	if len(set.Docs) != 1 {
		t.Fatalf("want 1 doc, got %d", len(set.Docs))
	}
	body := set.Docs[0].Body
	if !strings.Contains(body, "SHARED CONTENT") {
		t.Fatalf("import not inlined: %q", body)
	}
	if strings.Contains(body, "@shared.md") {
		t.Fatalf("import directive left in body: %q", body)
	}
}

func TestImportResolutionRejectsEscapes(t *testing.T) {
	proj := t.TempDir()
	mustMkdir(t, filepath.Join(proj, ".git"))
	outside := t.TempDir()
	mustWrite(t, filepath.Join(outside, "secret.md"), "SECRET")
	mustWrite(t, filepath.Join(proj, "REASONIX.md"), "Top\n@/abs/path.md\n@~/secret.md\n@../secret.md\nBottom")

	set := Load(Options{CWD: proj})
	if len(set.Docs) != 1 {
		t.Fatalf("want 1 doc, got %d", len(set.Docs))
	}
	body := set.Docs[0].Body
	if strings.Contains(body, "SECRET") {
		t.Fatalf("unsafe import was inlined: %q", body)
	}
	for _, directive := range []string{"@/abs/path.md", "@~/secret.md", "@../secret.md"} {
		if !strings.Contains(body, directive) {
			t.Fatalf("unsafe directive %q should be left visible, body: %q", directive, body)
		}
	}
}

func TestImportResolutionRejectsSymlinkEscape(t *testing.T) {
	proj := t.TempDir()
	mustMkdir(t, filepath.Join(proj, ".git"))
	outside := t.TempDir()
	mustWrite(t, filepath.Join(outside, "secret.md"), "SECRET")
	if err := os.Symlink(filepath.Join(outside, "secret.md"), filepath.Join(proj, "linked.md")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	mustWrite(t, filepath.Join(proj, "REASONIX.md"), "Top\n@linked.md\nBottom")

	set := Load(Options{CWD: proj})
	if len(set.Docs) != 1 {
		t.Fatalf("want 1 doc, got %d", len(set.Docs))
	}
	body := set.Docs[0].Body
	if strings.Contains(body, "SECRET") || !strings.Contains(body, "@linked.md") {
		t.Fatalf("symlink escape should not be inlined, body: %q", body)
	}
}

// TestImportCycleDoesNotHang verifies cycle detection terminates.
func TestImportCycleDoesNotHang(t *testing.T) {
	proj := t.TempDir()
	mustMkdir(t, filepath.Join(proj, ".git"))
	mustWrite(t, filepath.Join(proj, "a.md"), "A\n@b.md")
	mustWrite(t, filepath.Join(proj, "b.md"), "B\n@a.md")
	mustWrite(t, filepath.Join(proj, "REASONIX.md"), "@a.md")

	set := Load(Options{CWD: proj}) // must return, not loop forever
	body := set.Docs[0].Body
	if !strings.Contains(body, "A") || !strings.Contains(body, "B") {
		t.Fatalf("cycle import dropped content: %q", body)
	}
}

// TestImportTargetClassification guards the "@mention vs @import" heuristic.
func TestImportTargetClassification(t *testing.T) {
	cases := []struct {
		line string
		want bool
	}{
		{"@docs/setup.md", true},
		{"@./notes.txt", true},
		{"@/abs/path.md", true},
		{"@mention", false},      // prose-y, no separator/dot
		{"@", false},             // bare
		{"@a/b and more", false}, // not the only token
		{"plain text", false},
	}
	for _, c := range cases {
		if _, got := importTarget(c.line); got != c.want {
			t.Errorf("importTarget(%q) = %v, want %v", c.line, got, c.want)
		}
	}
}

func mustMkdir(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
}

func mustWrite(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestImportDiamondAndCycle(t *testing.T) {
	proj := t.TempDir()
	mustMkdir(t, filepath.Join(proj, ".git"))

	mustWrite(t, filepath.Join(proj, "shared.md"), "SHARED CONTENT")
	mustWrite(t, filepath.Join(proj, "a.md"), "A\n@shared.md")
	mustWrite(t, filepath.Join(proj, "b.md"), "B\n@shared.md")
	mustWrite(t, filepath.Join(proj, "REASONIX.md"), "@a.md\n@b.md")

	set := Load(Options{CWD: proj})
	if len(set.Docs) != 1 {
		t.Fatalf("want 1 doc, got %d", len(set.Docs))
	}
	body := set.Docs[0].Body

	count := strings.Count(body, "SHARED CONTENT")
	if count != 2 {
		t.Errorf("expected 'SHARED CONTENT' to appear twice, got %d times. Body:\n%s", count, body)
	}
	if strings.Contains(body, "skipped: import cycle") {
		t.Errorf("body contains incorrect import cycle message:\n%s", body)
	}

	projCycle := t.TempDir()
	mustMkdir(t, filepath.Join(projCycle, ".git"))
	mustWrite(t, filepath.Join(projCycle, "cycle1.md"), "CYCLE1\n@cycle2.md")
	mustWrite(t, filepath.Join(projCycle, "cycle2.md"), "CYCLE2\n@cycle1.md")
	mustWrite(t, filepath.Join(projCycle, "REASONIX.md"), "@cycle1.md")

	setCycle := Load(Options{CWD: projCycle})
	if len(setCycle.Docs) != 1 {
		t.Fatalf("want 1 doc, got %d", len(setCycle.Docs))
	}
	bodyCycle := setCycle.Docs[0].Body
	if !strings.Contains(bodyCycle, "skipped: import cycle") {
		t.Errorf("expected import cycle to be detected and reported. Body:\n%s", bodyCycle)
	}
}
