package evidence

import (
	"path/filepath"
	"slices"
	"testing"
)

func TestNamedPathsReadsLocationsOutOfDelegationProse(t *testing.T) {
	text := "Look at `src/parser.go:181-207` and @internal/agent, " +
		"then compare against src/parser.go. See https://example.com/docs.go, i.e. the CRLF path."
	got := NamedPaths(text)
	want := []string{
		filepath.FromSlash("internal/agent"),
		filepath.FromSlash("src/parser.go"),
	}
	if !slices.Equal(got, want) {
		t.Fatalf("NamedPaths = %q, want %q", got, want)
	}
}

// Prose that merely discusses the problem is not a location handed over: if
// it were, every blind delegation would report a non-zero hint count.
func TestNamedPathsIgnoresOrdinaryProse(t *testing.T) {
	for _, text := range []string{
		"Determine the root cause of the failing parser test.",
		"The offset is wrong, i.e. it double-counts.",
		"Check the scanner, e.g. its newline handling.",
		"Run the suite and report what fails.",
	} {
		if got := NamedPaths(text); len(got) != 0 {
			t.Errorf("NamedPaths(%q) = %q, want none", text, got)
		}
	}
}

// A delegation writes workspace-relative prose while a receipt records the
// absolute path the tool received, so origin cannot be decided by equality.
func TestUnderNamedPathMatchesAbsoluteReceiptAgainstRelativeProse(t *testing.T) {
	named := NamedPaths("start from src/parser.go")
	abs := filepath.Join(t.TempDir(), "src", "parser.go")
	if !UnderNamedPath(named, abs) {
		t.Fatalf("UnderNamedPath(%q, %q) = false, want true", named, abs)
	}
}

func TestUnderNamedPathCoversFilesBeneathADirectoryHint(t *testing.T) {
	named := NamedPaths("the bug is somewhere in internal/agent")
	root := t.TempDir()
	inside := filepath.Join(root, "internal", "agent", "task.go")
	outside := filepath.Join(root, "internal", "evidence", "task.go")
	if !UnderNamedPath(named, inside) {
		t.Errorf("UnderNamedPath(%q, %q) = false, want true", named, inside)
	}
	if UnderNamedPath(named, outside) {
		t.Errorf("UnderNamedPath(%q, %q) = true, want false", named, outside)
	}
}

// Substring matching would credit the parent for a file it never named.
func TestUnderNamedPathMatchesWholeSegmentsOnly(t *testing.T) {
	named := NamedPaths("see parser.go")
	if UnderNamedPath(named, filepath.FromSlash("src/myparser.go")) {
		t.Fatal("UnderNamedPath matched myparser.go against parser.go")
	}
}

// Narrowing the search is what delegating costs; naming the file is handing
// over the answer. Reported as one number, a parent that said "look in pkg/"
// would be indistinguishable from one that said "the bug is in pkg/romeo.go".
func TestSplitNamedPathsSeparatesScopeFromNamedFiles(t *testing.T) {
	scope, files := SplitNamedPaths(NamedPaths("search pkg/ and internal/agent; the bug is in pkg/romeo.py"))
	wantScope := []string{filepath.FromSlash("internal/agent"), "pkg"}
	wantFiles := []string{filepath.FromSlash("pkg/romeo.py")}
	if !slices.Equal(scope, wantScope) {
		t.Errorf("scope = %q, want %q", scope, wantScope)
	}
	if !slices.Equal(files, wantFiles) {
		t.Errorf("files = %q, want %q", files, wantFiles)
	}
}

// A child sent to a directory still had to work out which file in it mattered,
// so a scope hint must not zero out the credit for finding one.
func TestClassifyEvidenceOriginCreditsWorkInsideAScopeHint(t *testing.T) {
	root := t.TempDir()
	looked := []string{
		filepath.Join(root, "pkg", "alpha.py"),
		filepath.Join(root, "pkg", "romeo.py"),
	}

	var scoped DelegationAudit
	scoped.ClassifyEvidenceOrigin("Search pkg/ for the module that gets it wrong.", looked)
	if scoped.ParentScopeHints != 1 || scoped.ParentNamedFiles != 0 {
		t.Fatalf("scoped = %+v, want 1 scope hint and no named file", scoped)
	}
	if scoped.DiscoveredPaths != 2 {
		t.Errorf("a scope hint erased the child's credit: %d/2", scoped.DiscoveredPaths)
	}

	var told DelegationAudit
	told.ClassifyEvidenceOrigin("The bug is in pkg/romeo.py.", looked)
	if told.ParentNamedFiles != 1 || told.DiscoveredPaths != 1 {
		t.Errorf("told = %+v, want 1 named file and 1 of 2 discovered", told)
	}
}

func TestClassifyEvidenceOriginSplitsBlindFromSeededDelegation(t *testing.T) {
	root := t.TempDir()
	looked := []string{
		filepath.Join(root, "src", "parser.go"),
		filepath.Join(root, "src", "scanner.go"),
		filepath.Join(root, "tests", "test_parser.py"),
	}

	var blind DelegationAudit
	blind.ClassifyEvidenceOrigin("Diagnose the failing parser test.", looked)
	if blind.ParentNamedFiles != 0 || blind.DiscoveredPaths != 3 || blind.EvidencePaths != 3 {
		t.Fatalf("blind = %+v, want 0 named and 3/3 discovered", blind)
	}

	var seeded DelegationAudit
	seeded.ClassifyEvidenceOrigin("I suspect src/parser.go double-normalizes CRLF.", looked)
	if seeded.ParentNamedFiles != 1 {
		t.Fatalf("seeded.ParentNamedFiles = %d, want 1", seeded.ParentNamedFiles)
	}
	if seeded.DiscoveredPaths != 2 || seeded.EvidencePaths != 3 {
		t.Fatalf("seeded = %+v, want 2 of 3 discovered", seeded)
	}
}

func TestEvidencePathsCoversReadsAndSkipsFailedReceipts(t *testing.T) {
	summary := ChildEvidenceSummary{Receipts: []Receipt{
		{ToolName: "read", Success: true, Read: true, Paths: []string{"src/parser.go"}},
		{ToolName: "read", Success: true, Read: true, Paths: []string{"src/parser.go"}},
		{ToolName: "edit", Success: true, Write: true, Mutation: true, Paths: []string{"src/scanner.go"}},
		{ToolName: "read", Success: false, Read: true, Paths: []string{"src/never.go"}},
	}}
	want := []string{"src/parser.go", "src/scanner.go"}
	if got := summary.EvidencePaths(); !slices.Equal(got, want) {
		t.Fatalf("EvidencePaths = %q, want %q", got, want)
	}
}
