package instruction

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveKeepsMostSpecificSourceForExactDuplicate(t *testing.T) {
	root := t.TempDir()
	user := t.TempDir()
	mustWriteInstruction(t, filepath.Join(user, "AGENTS.md"), "Always run tests.")
	mustWriteInstruction(t, filepath.Join(root, "AGENTS.md"), "Always run tests.")

	got := Resolve(ResolveOptions{WorkspaceRoot: root, TargetDir: root, UserDir: user})
	if len(got.Documents) != 1 {
		t.Fatalf("documents = %+v, want one exact instruction body", got.Documents)
	}
	if got.Documents[0].Scope != ScopeProject || got.Documents[0].Path != filepath.Join(root, "AGENTS.md") {
		t.Fatalf("duplicate winner = %+v, want project source", got.Documents[0])
	}
}

func TestResolveDuplicateReplacementPreservesPrecedenceOrder(t *testing.T) {
	root := t.TempDir()
	user := t.TempDir()
	mustWriteInstruction(t, filepath.Join(user, "REASONIX.md"), "duplicate")
	mustWriteInstruction(t, filepath.Join(user, "AGENTS.md"), "unique global")
	mustWriteInstruction(t, filepath.Join(root, "AGENTS.md"), "duplicate")
	mustWriteInstruction(t, filepath.Join(root, "CLAUDE.md"), "unique project")

	got := Resolve(ResolveOptions{WorkspaceRoot: root, TargetDir: root, UserDir: user})
	if len(got.Documents) != 3 {
		t.Fatalf("documents = %+v, want three unique bodies", got.Documents)
	}
	if got.Documents[0].Body != "unique global" || got.Documents[1].Body != "duplicate" || got.Documents[2].Body != "unique project" {
		t.Fatalf("precedence order = %+v", got.Documents)
	}
	for i := range got.Documents {
		if got.Documents[i].Order != i {
			t.Fatalf("document order metadata = %+v", got.Documents)
		}
	}
}

func TestResolveKeepsDistinctConventionFilesInDeterministicOrder(t *testing.T) {
	root := t.TempDir()
	mustWriteInstruction(t, filepath.Join(root, "REASONIX.md"), "Reasonix rule")
	mustWriteInstruction(t, filepath.Join(root, "AGENTS.md"), "Portable rule")
	mustWriteInstruction(t, filepath.Join(root, "CLAUDE.md"), "Claude-compatible rule")

	got := Resolve(ResolveOptions{WorkspaceRoot: root, TargetDir: root})
	if len(got.Documents) != 3 {
		t.Fatalf("documents = %+v, want all three distinct sources", got.Documents)
	}
	for i, name := range []string{"REASONIX.md", "AGENTS.md", "CLAUDE.md"} {
		if filepath.Base(got.Documents[i].Path) != name || got.Documents[i].Order != i {
			t.Fatalf("document %d = %+v, want %s with stable order", i, got.Documents[i], name)
		}
	}
}

func TestResolveAppliesOnlyWorkspaceToTargetAncestorChain(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "repo")
	target := filepath.Join(root, "services", "api")
	sibling := filepath.Join(root, "services", "web")
	for _, dir := range []string{root, target, sibling} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	mustWriteInstruction(t, filepath.Join(parent, "AGENTS.md"), "outside workspace")
	mustWriteInstruction(t, filepath.Join(root, "AGENTS.md"), "root rule")
	mustWriteInstruction(t, filepath.Join(root, "services", "AGENTS.md"), "services rule")
	mustWriteInstruction(t, filepath.Join(target, "AGENTS.md"), "api rule")
	mustWriteInstruction(t, filepath.Join(sibling, "AGENTS.md"), "web rule")

	got := Resolve(ResolveOptions{WorkspaceRoot: root, TargetDir: target})
	joined := documentBodies(got.Documents)
	for _, want := range []string{"root rule", "services rule", "api rule"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("resolved instructions missing %q: %+v", want, got.Documents)
		}
	}
	for _, unwanted := range []string{"outside workspace", "web rule"} {
		if strings.Contains(joined, unwanted) {
			t.Fatalf("resolved instructions included %q outside target chain: %+v", unwanted, got.Documents)
		}
	}
	if got.Documents[0].Scope != ScopeProject || got.Documents[1].Scope != ScopeAncestor || got.Documents[2].Scope != ScopeAncestor {
		t.Fatalf("nested scopes = %+v", got.Documents)
	}
}

func TestResolveImportsAreProvenancedDeduplicatedAndConfined(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	mustWriteInstruction(t, filepath.Join(root, "shared.md"), "SHARED RULE")
	mustWriteInstruction(t, filepath.Join(root, "a.md"), "A\n@shared.md")
	mustWriteInstruction(t, filepath.Join(root, "b.md"), "B\n@shared.md")
	mustWriteInstruction(t, filepath.Join(outside, "secret.md"), "SECRET")
	if err := os.Symlink(filepath.Join(outside, "secret.md"), filepath.Join(root, "linked.md")); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
	mustWriteInstruction(t, filepath.Join(root, "AGENTS.md"), "@a.md\n@b.md\n@../secret.md\n@linked.md")

	got := Resolve(ResolveOptions{WorkspaceRoot: root, TargetDir: root})
	if len(got.Documents) != 1 {
		t.Fatalf("documents = %+v, want AGENTS.md", got.Documents)
	}
	body := got.Documents[0].Body
	if strings.Count(body, "SHARED RULE") != 1 {
		t.Fatalf("diamond import was not exactly deduplicated:\n%s", body)
	}
	for _, want := range []string{"instruction-import", "a.md", "b.md"} {
		if !strings.Contains(body, want) {
			t.Fatalf("resolved import missing provenance %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "SECRET") {
		t.Fatalf("external import escaped source directory:\n%s", body)
	}
	if len(got.Diagnostics) != 2 {
		t.Fatalf("diagnostics = %+v, want traversal and symlink rejections", got.Diagnostics)
	}
}

func TestResolveUserInstructionsImportTrustedConventionRoots(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	userDir := filepath.Join(home, ".reasonix")
	agentsDir := filepath.Join(home, ".agents")
	root := filepath.Join(home, "repo")
	mustWriteInstruction(t, filepath.Join(agentsDir, "AGENTS.md"), "SHARED USER RULE")
	mustWriteInstruction(t, filepath.Join(userDir, "REASONIX.md"), "@~/.agents/AGENTS.md")
	mustWriteInstruction(t, filepath.Join(root, "AGENTS.md"), "PROJECT RULE")

	got := Resolve(ResolveOptions{WorkspaceRoot: root, TargetDir: root, UserDir: userDir})
	body := documentBodies(got.Documents)
	if !strings.Contains(body, "SHARED USER RULE") {
		t.Fatalf("trusted user convention import missing: %+v", got)
	}
	if strings.Contains(body, home) {
		t.Fatalf("provider-visible import provenance leaked home path:\n%s", body)
	}
	if len(got.Diagnostics) != 0 {
		t.Fatalf("trusted user convention import diagnostics = %+v", got.Diagnostics)
	}
}

func TestResolveProjectInstructionsCannotImportUserConventionRoots(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	root := filepath.Join(home, "repo")
	mustWriteInstruction(t, filepath.Join(home, ".agents", "AGENTS.md"), "PRIVATE USER RULE")
	mustWriteInstruction(t, filepath.Join(root, "AGENTS.md"), "@~/.agents/AGENTS.md")

	got := Resolve(ResolveOptions{WorkspaceRoot: root, TargetDir: root})
	if strings.Contains(documentBodies(got.Documents), "PRIVATE USER RULE") {
		t.Fatalf("project instruction escaped into user convention root: %+v", got)
	}
	if len(got.Diagnostics) != 1 || got.Diagnostics[0].Code != "import_outside_source" {
		t.Fatalf("project external import diagnostics = %+v", got.Diagnostics)
	}
}

func TestResolveUserInstructionsRejectArbitraryHomeAndConventionSymlinkEscape(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	userDir := filepath.Join(home, ".reasonix")
	agentsDir := filepath.Join(home, ".agents")
	mustWriteInstruction(t, filepath.Join(home, "secret.md"), "HOME SECRET")
	if err := os.MkdirAll(agentsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(home, "secret.md"), filepath.Join(agentsDir, "linked.md")); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
	mustWriteInstruction(t, filepath.Join(userDir, "REASONIX.md"), "@~/secret.md\n@~/.agents/linked.md")

	got := Resolve(ResolveOptions{WorkspaceRoot: t.TempDir(), TargetDir: t.TempDir(), UserDir: userDir})
	if strings.Contains(documentBodies(got.Documents), "HOME SECRET") {
		t.Fatalf("arbitrary home content entered instructions: %+v", got)
	}
	codes := map[string]bool{}
	for _, diagnostic := range got.Diagnostics {
		codes[diagnostic.Code] = true
	}
	if !codes["import_outside_source"] || !codes["import_symlink_escape"] {
		t.Fatalf("user import rejection diagnostics = %+v", got.Diagnostics)
	}
}

func TestResolveRejectsDirectInstructionSymlinkOutsideBoundary(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	mustWriteInstruction(t, filepath.Join(outside, "private.md"), "MACHINE-LOCAL SECRET")
	if err := os.Symlink(filepath.Join(outside, "private.md"), filepath.Join(root, "AGENTS.md")); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}

	got := Resolve(ResolveOptions{WorkspaceRoot: root, TargetDir: root})
	if len(got.Documents) != 0 {
		t.Fatalf("documents = %+v, want external symlink excluded", got.Documents)
	}
	if len(got.Diagnostics) != 1 || got.Diagnostics[0].Code != "document_symlink_escape" {
		t.Fatalf("diagnostics = %+v, want document_symlink_escape", got.Diagnostics)
	}
	if strings.Contains(documentBodies(got.Documents), "MACHINE-LOCAL SECRET") {
		t.Fatal("external symlink content entered provider-visible instructions")
	}
}

func TestResolveAllowsDirectInstructionSymlinkWithinBoundary(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "docs", "agent-rules.md")
	mustWriteInstruction(t, target, "Run the focused tests.")
	if err := os.Symlink(target, filepath.Join(root, "AGENTS.md")); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}

	got := Resolve(ResolveOptions{WorkspaceRoot: root, TargetDir: root})
	if len(got.Documents) != 1 || got.Documents[0].Body != "Run the focused tests." {
		t.Fatalf("documents = %+v, want in-boundary symlink loaded", got.Documents)
	}
	if len(got.Diagnostics) != 0 {
		t.Fatalf("diagnostics = %+v, want none", got.Diagnostics)
	}
}

func TestInstructionBlockIsStableAcrossWorkspaceRoots(t *testing.T) {
	resolve := func(base string) string {
		root := filepath.Join(base, "repo")
		target := filepath.Join(root, "services", "api")
		user := filepath.Join(base, "reasonix-home")
		mustWriteInstruction(t, filepath.Join(user, "AGENTS.md"), "Use concise replies.")
		mustWriteInstruction(t, filepath.Join(root, "AGENTS.md"), "Run all tests.")
		mustWriteInstruction(t, filepath.Join(target, "AGENTS.local.md"), "Run API tests first.")
		return Block(Resolve(ResolveOptions{WorkspaceRoot: root, TargetDir: target, UserDir: user}).Documents)
	}

	firstRoot := t.TempDir()
	secondRoot := t.TempDir()
	first := resolve(firstRoot)
	second := resolve(secondRoot)
	if first != second {
		t.Fatalf("provider instruction bytes changed across roots:\nfirst:\n%s\nsecond:\n%s", first, second)
	}
	for _, privateRoot := range []string{firstRoot, secondRoot} {
		if strings.Contains(first, privateRoot) || strings.Contains(second, privateRoot) {
			t.Fatalf("provider instructions exposed machine-local root %q", privateRoot)
		}
	}
	for _, want := range []string{"user/AGENTS.md", "workspace/AGENTS.md", "workspace/services/api/AGENTS.local.md", "applies to workspace/services/api"} {
		if !strings.Contains(first, want) {
			t.Fatalf("provider instructions missing stable label %q:\n%s", want, first)
		}
	}
}

func TestInstructionBlockDerivesWorkspaceRootFromNestedDocument(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "services", "api")
	mustWriteInstruction(t, filepath.Join(target, "AGENTS.md"), "Run API tests first.")

	block := Block(Resolve(ResolveOptions{WorkspaceRoot: root, TargetDir: target}).Documents)
	for _, want := range []string{"workspace/services/api/AGENTS.md", "applies to workspace/services/api"} {
		if !strings.Contains(block, want) {
			t.Fatalf("provider instructions missing nested label %q:\n%s", want, block)
		}
	}
	if strings.Contains(block, root) {
		t.Fatalf("provider instructions exposed machine-local root %q:\n%s", root, block)
	}
}

func TestImportTargetClassification(t *testing.T) {
	for _, tc := range []struct {
		line string
		want bool
	}{
		{"@docs/setup.md", true}, {"@./notes.txt", true}, {"@/abs/path.md", true},
		{"@mention", false}, {"@", false}, {"@a/b and more", false}, {"plain text", false},
	} {
		if _, got := parseImportTarget(tc.line); got != tc.want {
			t.Errorf("parseImportTarget(%q) = %v, want %v", tc.line, got, tc.want)
		}
	}
}

func documentBodies(docs []Document) string {
	var bodies []string
	for _, doc := range docs {
		bodies = append(bodies, doc.Body)
	}
	return strings.Join(bodies, "\n")
}

func mustWriteInstruction(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
