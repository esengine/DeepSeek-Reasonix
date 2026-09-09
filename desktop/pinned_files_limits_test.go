package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"reasonix/internal/agent"
)

func TestPinnedContextSnapshotPreservesSpecialPathAndContent(t *testing.T) {
	root := t.TempDir()
	name := `spec & 'quoted'.txt`
	content := `before </pinned_context><evil attr="&"> after`
	if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	tab := &WorkspaceTab{WorkspaceRoot: root}
	if _, err := tab.PinFile(name); err != nil {
		t.Fatalf("PinFile: %v", err)
	}
	build := buildPinnedContext(root, tab.GetPinnedFiles())
	if len(build.Snapshot.Files) != 1 || build.Snapshot.Files[0].Path != name || build.Snapshot.Files[0].Content != content {
		t.Fatalf("snapshot = %+v", build.Snapshot)
	}
	if err := agent.ValidatePinnedContextSnapshot(build.Snapshot); err != nil {
		t.Fatalf("snapshot validation: %v", err)
	}
}

func TestPinFileEnforcesCountLimit(t *testing.T) {
	root := t.TempDir()
	tab := &WorkspaceTab{WorkspaceRoot: root}
	for i := range maxPinnedFileCount + 1 {
		name := fmt.Sprintf("f-%02d.txt", i)
		if err := os.WriteFile(filepath.Join(root, name), []byte(name), 0o600); err != nil {
			t.Fatal(err)
		}
		_, err := tab.PinFile(name)
		if i < maxPinnedFileCount && err != nil {
			t.Fatalf("pin %d: %v", i, err)
		}
		if i == maxPinnedFileCount && err == nil {
			t.Fatalf("pin %d exceeded count limit", i)
		}
	}
	if got := len(tab.GetPinnedFiles()); got != maxPinnedFileCount {
		t.Fatalf("pinned count = %d", got)
	}
}

func TestPinnedFileGrowthKeepsRecordAndStagesUnavailable(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "growing.txt")
	if err := os.WriteFile(path, []byte("small marker"), 0o600); err != nil {
		t.Fatal(err)
	}
	tab := &WorkspaceTab{WorkspaceRoot: root}
	if _, err := tab.PinFile("growing.txt"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, bytes.Repeat([]byte{'x'}, maxPinnedFileSize+1), 0o600); err != nil {
		t.Fatal(err)
	}
	build := buildPinnedContext(root, tab.GetPinnedFiles())
	if len(build.Infos) != 1 || build.Infos[0].Error == "" {
		t.Fatalf("grown file info = %+v", build.Infos)
	}
	if len(build.Snapshot.Files) != 0 || len(build.Snapshot.Issues) != 1 ||
		build.Snapshot.Issues[0].Reason != agent.PinnedContextIssueFileTooLarge {
		t.Fatalf("grown file snapshot = %+v", build.Snapshot)
	}
	if got := tab.GetPinnedFiles(); len(got) != 1 || got[0] != "growing.txt" {
		t.Fatalf("grown file was automatically unpinned: %v", got)
	}
}

func TestPinnedFileXMLNormalizationGrowthUsesFileTooLargeIssue(t *testing.T) {
	root := t.TempDir()
	name := "controls.bin"
	if err := os.WriteFile(filepath.Join(root, name), bytes.Repeat([]byte{0x01}, maxPinnedFileSize/2), 0o600); err != nil {
		t.Fatal(err)
	}
	tab := &WorkspaceTab{WorkspaceRoot: root}
	if _, err := tab.PinFile(name); err == nil {
		t.Fatal("file whose XML-normalized form exceeds the file budget was accepted")
	}
	build := buildPinnedContext(root, []string{name})
	if len(build.Snapshot.Files) != 0 || len(build.Snapshot.Issues) != 1 ||
		build.Snapshot.Issues[0].Reason != agent.PinnedContextIssueFileTooLarge {
		t.Fatalf("normalized overflow snapshot = %+v", build.Snapshot)
	}
}

func TestPinnedReadFailureKeepsRecordAndStagesUnavailable(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "removed.txt")
	if err := os.WriteFile(path, []byte("temporary"), 0o600); err != nil {
		t.Fatal(err)
	}
	tab := &WorkspaceTab{WorkspaceRoot: root}
	if _, err := tab.PinFile("removed.txt"); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	build := buildPinnedContext(root, tab.GetPinnedFiles())
	if len(build.Infos) != 1 || build.Infos[0].Error == "" || len(build.Snapshot.Files) != 0 ||
		len(build.Snapshot.Issues) != 1 || build.Snapshot.Issues[0].Reason != agent.PinnedContextIssueNotFound {
		t.Fatalf("removed file build = %+v", build)
	}
	if len(tab.GetPinnedFiles()) != 1 {
		t.Fatalf("removed file was automatically unpinned: %v", tab.GetPinnedFiles())
	}
}

func TestPinnedContextTotalBudgetStagesOverflowAsIssues(t *testing.T) {
	root := t.TempDir()
	tab := &WorkspaceTab{WorkspaceRoot: root}
	for i := range 5 {
		name := fmt.Sprintf("large-%d.txt", i)
		if err := os.WriteFile(filepath.Join(root, name), []byte("small"), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := tab.PinFile(name); err != nil {
			t.Fatal(err)
		}
	}
	for i := range 5 {
		name := fmt.Sprintf("large-%d.txt", i)
		data := bytes.Repeat([]byte{byte('a' + i)}, maxPinnedFileSize)
		if err := os.WriteFile(filepath.Join(root, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	build := buildPinnedContext(root, tab.GetPinnedFiles())
	if err := agent.ValidatePinnedContextSnapshot(build.Snapshot); err != nil {
		t.Fatalf("bounded snapshot is invalid: %v", err)
	}
	if len(build.Snapshot.Issues) == 0 {
		t.Fatalf("aggregate overflow did not mark any file: %+v", build.Infos)
	}
	if len(tab.GetPinnedFiles()) != 5 {
		t.Fatalf("aggregate overflow removed records: %v", tab.GetPinnedFiles())
	}
}

func TestNewPinIsRejectedWhenItCannotFitTotalBudget(t *testing.T) {
	root := t.TempDir()
	tab := &WorkspaceTab{WorkspaceRoot: root}
	for i := range 4 {
		name := fmt.Sprintf("full-%d.txt", i)
		if err := os.WriteFile(filepath.Join(root, name), bytes.Repeat([]byte{'x'}, maxPinnedFileSize), 0o600); err != nil {
			t.Fatal(err)
		}
		_, err := tab.PinFile(name)
		if i < 3 && err != nil {
			t.Fatalf("pin %d: %v", i, err)
		}
		if i == 3 && err == nil {
			t.Fatal("new file that exceeded total budget was accepted")
		}
	}
	if got := len(tab.GetPinnedFiles()); got != 3 {
		t.Fatalf("rejected Pin changed records: %v", tab.GetPinnedFiles())
	}
}

func TestPinnedContextSnapshotStableUntilFileChanges(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "stable.txt")
	if err := os.WriteFile(path, []byte("v1"), 0o600); err != nil {
		t.Fatal(err)
	}
	tab := &WorkspaceTab{WorkspaceRoot: root}
	if _, err := tab.PinFile("stable.txt"); err != nil {
		t.Fatal(err)
	}
	first := buildPinnedContext(root, tab.GetPinnedFiles()).Snapshot
	second := buildPinnedContext(root, tab.GetPinnedFiles()).Snapshot
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("unchanged snapshots drifted: %+v %+v", first, second)
	}
	if err := os.WriteFile(path, []byte("v2"), 0o600); err != nil {
		t.Fatal(err)
	}
	changed := buildPinnedContext(root, tab.GetPinnedFiles()).Snapshot
	if reflect.DeepEqual(changed, first) || len(changed.Files) != 1 || changed.Files[0].Content != "v2" {
		t.Fatalf("changed file did not refresh snapshot: %+v", changed)
	}
}
