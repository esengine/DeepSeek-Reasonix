package sessioncontext

import (
	"strings"
	"testing"
)

func TestBuildParseDeterministicOrderedSnapshot(t *testing.T) {
	sections := Sections{
		Environment:      "## Environment\r\n\r\ngo/linux",
		Workspace:        `Current workspace: "/work"`,
		BackgroundMemory: "fact index",
		SkillsCatalog:    "```\n- review — Review code\n```",
	}
	first := Build(sections)
	second := Build(sections)
	if first.Content == "" || first.Content != second.Content || first.Digest != second.Digest {
		t.Fatalf("Build is not deterministic:\nfirst=%+v\nsecond=%+v", first, second)
	}
	for _, want := range []string{"## Environment", "## Workspace", "## Background memory", "## Skills catalog", "Digest: sha256:"} {
		if !strings.Contains(first.Content, want) {
			t.Fatalf("snapshot missing %q:\n%s", want, first.Content)
		}
	}
	if strings.Index(first.Content, "## Environment") > strings.Index(first.Content, "## Workspace") ||
		strings.Index(first.Content, "## Workspace") > strings.Index(first.Content, "## Background memory") ||
		strings.Index(first.Content, "## Background memory") > strings.Index(first.Content, "## Skills catalog") {
		t.Fatalf("sections out of order:\n%s", first.Content)
	}
	parsed, ok := Parse(first.Content)
	if !ok || parsed.Digest != first.Digest || parsed.Content != first.Content {
		t.Fatalf("Parse(Build) = (%+v, %v), want digest/content round trip", parsed, ok)
	}
	if parsed.Sections != first.Sections {
		t.Fatalf("parsed sections = %+v, want %+v", parsed.Sections, first.Sections)
	}
	diagnostics := SectionDiagnostics(parsed)
	if diagnostics.Environment.Chars != len("go/linux") || len(diagnostics.Environment.Digest) != 64 ||
		diagnostics.Workspace.Chars != len(`Current workspace: "/work"`) {
		t.Fatalf("content-free diagnostics = %+v", diagnostics)
	}
	if strings.Contains(first.Content, "\r") {
		t.Fatalf("snapshot retained non-LF newline: %q", first.Content)
	}
}

func TestBuildEmptyAndDigestChanges(t *testing.T) {
	if got := Build(Sections{}); got != (Snapshot{}) {
		t.Fatalf("Build(empty) = %+v, want zero snapshot", got)
	}
	a := Build(Sections{Workspace: "one"})
	b := Build(Sections{Workspace: "two"})
	if a.Digest == b.Digest {
		t.Fatalf("different sections produced the same digest %q", a.Digest)
	}
	corrupt := strings.Replace(a.Content, "one", "other", 1)
	if _, ok := Parse(corrupt); ok {
		t.Fatal("Parse accepted a digest-invalid snapshot")
	}
	if _, ok := Parse(strings.Replace(a.Content, `version="1"`, `version="2"`, 1)); ok {
		t.Fatal("Parse accepted an unknown version")
	}
}

func TestParsePreservesSectionMarkerInsideValue(t *testing.T) {
	sections := Sections{BackgroundMemory: "fact body\n\n## Skills catalog\n\nthis is still memory"}
	snapshot := Build(sections)
	parsed, ok := Parse(snapshot.Content)
	if !ok || parsed.Sections != sections {
		t.Fatalf("Parse lost a Markdown heading inside a value: ok=%v sections=%+v", ok, parsed.Sections)
	}
}

func TestParseAcceptsLegacyV1Snapshot(t *testing.T) {
	body := preamble + "\n\n## Workspace\n\nlegacy workspace"
	legacy := openTag + "\n" + body + "\n\n" + digestPrefix + digestOf(body) + "\n" + closeTag
	parsed, ok := Parse(legacy)
	if !ok || parsed.Sections.Workspace != "legacy workspace" {
		t.Fatalf("legacy v1 snapshot no longer parses: ok=%v sections=%+v", ok, parsed.Sections)
	}
}

func TestSplitBlocksPreservesBytesAndMarksOnlyValidSnapshots(t *testing.T) {
	snapshot := Build(Sections{Workspace: "workspace"})
	input := "before\n\n" + snapshot.Content + "\n\nafter"
	parts := SplitBlocks(input)
	if len(parts) != 3 || parts[0].SessionContext || !parts[1].SessionContext || parts[2].SessionContext {
		t.Fatalf("SplitBlocks parts = %+v", parts)
	}
	var joined strings.Builder
	for _, part := range parts {
		joined.WriteString(part.Text)
	}
	if joined.String() != input {
		t.Fatalf("SplitBlocks changed bytes:\n got %q\nwant %q", joined.String(), input)
	}
	invalid := strings.Replace(snapshot.Content, "workspace", "tampered", 1)
	parts = SplitBlocks(invalid)
	if len(parts) != 1 || parts[0].SessionContext || parts[0].Text != invalid {
		t.Fatalf("invalid snapshot was split as trusted context: %+v", parts)
	}
}

func TestSplitBlocksFindsFramedSnapshotAfterEmbeddedClosingTag(t *testing.T) {
	snapshot := Build(Sections{Workspace: "literal </session-context> marker"})
	parts := SplitBlocks(snapshot.Content)
	if len(parts) != 1 || !parts[0].SessionContext || parts[0].Text != snapshot.Content {
		t.Fatalf("embedded closing tag broke snapshot framing: %+v", parts)
	}
}
