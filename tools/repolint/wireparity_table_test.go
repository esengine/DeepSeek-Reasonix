package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// treeRoot is the repository root from the package directory a test runs in.
func treeRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	return root
}

// The added half of the registry is the claim in its pull request: each row is
// a page copy that agrees today with the struct its handler writes. A row that
// names nothing, or two rows that name the same reader, would guard a pair the
// tree does not have while the gate reported green.
func TestAddedWireMirrorsResolveAndAreUnique(t *testing.T) {
	root := treeRoot(t)
	tsBodies := map[string]string{}
	headReaders := map[string]bool{}
	for _, m := range mirroredWireTypesHead {
		headReaders[m.tsFile+"."+m.tsType] = true
	}
	seenPair := map[string]bool{}
	seenReader := map[string]bool{}
	for _, m := range mirroredWireTypesTail {
		pair := m.goFile + "." + m.goType + " -> " + m.tsFile + "." + m.tsType
		reader := m.tsFile + "." + m.tsType
		if seenPair[pair] {
			t.Errorf("%s is declared twice", pair)
		}
		seenPair[pair] = true
		// A reader registered once is the point; the same writer may still back
		// two readers (the page keeps a copy per port file that carries it).
		if headReaders[reader] || seenReader[reader] {
			t.Errorf("%s returns a reader the registry already guards", pair)
		}
		seenReader[reader] = true

		// The rows added here register data copies only: a test file, a mock or
		// a fixture would make the guard read a shape the wire never carries.
		if strings.HasSuffix(m.goFile, "_test.go") || strings.HasSuffix(m.tsFile, ".test.ts") ||
			strings.Contains(filepath.Base(m.tsFile), "mock") || strings.Contains(filepath.Base(m.tsFile), "fixture") {
			t.Errorf("%s names a test or fixture file", pair)
			continue
		}

		src, err := parseSource(root, m.goFile)
		if err != nil || src == nil || src.file == nil {
			t.Errorf("%s names a Go file repolint cannot parse", pair)
			continue
		}
		goFields, ok := wireFieldNames(src.file, m.goType)
		if !ok {
			t.Errorf("%s names no Go struct %s in %s", pair, m.goType, m.goFile)
			continue
		}

		body, ok := tsBodies[m.tsFile]
		if !ok {
			raw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(m.tsFile)))
			if err != nil {
				t.Errorf("%s names a TS file that cannot be read: %v", pair, err)
				continue
			}
			body = string(raw)
			tsBodies[m.tsFile] = body
		}
		tsFields, _, found := tsInterfaceFields(body, m.tsType)
		if !found {
			t.Errorf("%s names no TS interface %s in %s", pair, m.tsType, m.tsFile)
			continue
		}

		// Exact today, not close: the row's justification is that the two sides
		// carry the same names, and a set that differs is a row for a follow-up.
		if !slices.Equal(sortedCopy(goFields), sortedCopy(tsFields)) {
			t.Errorf("%s is not exact: Go sends %v, TS reads %v", pair, sortedCopy(goFields), sortedCopy(tsFields))
		}
	}
}

func sortedCopy(names []string) []string {
	out := append([]string(nil), names...)
	slices.Sort(out)
	return out
}

// One newly guarded pair, with a field removed from the page's copy: the guard
// has to name the field, or a reviewer cannot tell a drift from a stale row.
func TestAddedWireMirrorDroppedFieldIsNamed(t *testing.T) {
	root := treeRoot(t)
	const pair = `{"internal/frontend/serve/backup.go", "createBackupRequest", tsBackupFile, "BackupCreateRequest"}`
	var m wireMirror
	for _, candidate := range mirroredWireTypesTail {
		if candidate.goFile == "internal/frontend/serve/backup.go" && candidate.goType == "createBackupRequest" &&
			candidate.tsFile == tsBackupFile && candidate.tsType == "BackupCreateRequest" {
			m = candidate
			break
		}
	}
	if m.tsType == "" {
		t.Fatalf("the registry no longer declares %s", pair)
	}

	src, err := parseSource(root, m.goFile)
	if err != nil {
		t.Fatal(err)
	}
	goFields, ok := wireFieldNames(src.file, m.goType)
	if !ok {
		t.Fatalf("%s does not resolve", m.goType)
	}
	raw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(m.tsFile)))
	if err != nil {
		t.Fatal(err)
	}
	body := string(raw)
	fields, line, found := tsInterfaceFields(body, m.tsType)
	if !found || !slices.Contains(fields, "passphrase") {
		t.Fatalf("%s does not declare passphrase in the tree", m.tsType)
	}
	dropped := strings.Replace(body, "  passphrase: string;\n", "", 1)
	if dropped == body {
		t.Fatal("the test could not find the field to drop")
	}
	got := wireParityFindings([]wireMirror{m}, map[string][]string{m.goFile + "." + m.goType: goFields},
		map[string]string{m.tsFile: dropped})
	if len(got) != 1 {
		t.Fatalf("findings = %+v, want exactly the dropped field", got)
	}
	if !strings.Contains(got[0].Msg, `sends "passphrase"`) || got[0].File != m.tsFile {
		t.Fatalf("finding = %s:%d %s: %s, want it to name passphrase against %s",
			got[0].File, got[0].Line, got[0].Rule, got[0].Msg, m.tsFile)
	}
	t.Logf("dropping passphrase reported: %s:%d %s", got[0].File, line, got[0].Msg)
}
