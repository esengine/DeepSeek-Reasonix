package control

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/agent"
	"reasonix/internal/store"
)

func TestSnapshotConflictDiagnosticCountsLogicalTopicWithoutLeakingIdentity(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "first.jsonl")
	second := filepath.Join(dir, "second.jsonl")
	for _, path := range []string{first, second} {
		if err := agent.SaveBranchMeta(path, agent.BranchMeta{
			ID: agent.BranchID(path), Scope: "project", WorkspaceRoot: "/private/workspace",
			TopicID: "private-topic-id", TopicTitle: "private topic title", CustomTitle: "private version note",
		}); err != nil {
			t.Fatal(err)
		}
	}
	appendSnapshotConflictDiagnostic(first, "save", "forked_recovery_branch", nil, "", false)
	appendSnapshotConflictDiagnostic(second, "shutdown", "forked_file_lock_recovery", nil, "", false)

	read := func(path string) snapshotConflictDiagnostic {
		t.Helper()
		file, err := os.Open(store.SessionConflictLog(path))
		if err != nil {
			t.Fatal(err)
		}
		defer file.Close()
		scanner := bufio.NewScanner(file)
		if !scanner.Scan() {
			t.Fatalf("missing diagnostic for %s", agent.BranchID(path))
		}
		var record snapshotConflictDiagnostic
		if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
			t.Fatal(err)
		}
		for _, secret := range []string{dir, "/private/workspace", "private-topic-id", "private topic title", "private version note"} {
			if strings.Contains(scanner.Text(), secret) {
				t.Fatalf("diagnostic leaked %q: %s", secret, scanner.Text())
			}
		}
		return record
	}
	firstRecord := read(first)
	secondRecord := read(second)
	if firstRecord.Occurrence != 1 || firstRecord.Repeated {
		t.Fatalf("first occurrence = %+v", firstRecord)
	}
	if secondRecord.Occurrence != 2 || !secondRecord.Repeated {
		t.Fatalf("second occurrence = %+v", secondRecord)
	}
}

func TestSnapshotConflictDiagnosticKeepsFirstRepeatedSamePathRecovery(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "same.jsonl")
	if err := agent.SaveBranchMeta(path, agent.BranchMeta{
		ID: agent.BranchID(path), Scope: "project", WorkspaceRoot: "/private/repeat-workspace",
		TopicID: "private-repeat-topic", TopicTitle: "private repeat title",
	}); err != nil {
		t.Fatal(err)
	}

	appendSnapshotConflictDiagnostic(path, "save", "forked_recovery_branch", nil, "", false)
	appendSnapshotConflictDiagnostic(path, "save", "forked_recovery_branch", nil, "", false)
	appendSnapshotConflictDiagnostic(path, "save", "forked_recovery_branch", nil, "", false)

	file, err := os.Open(store.SessionConflictLog(path))
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	records := []snapshotConflictDiagnostic{}
	for scanner.Scan() {
		for _, secret := range []string{dir, "/private/repeat-workspace", "private-repeat-topic", "private repeat title"} {
			if strings.Contains(scanner.Text(), secret) {
				t.Fatalf("diagnostic leaked %q: %s", secret, scanner.Text())
			}
		}
		var record snapshotConflictDiagnostic
		if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
			t.Fatal(err)
		}
		records = append(records, record)
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if len(records) != 2 {
		t.Fatalf("records = %+v, want first event plus one bounded repeat", records)
	}
	if records[0].Occurrence != 1 || records[0].Repeated || records[1].Occurrence != 2 || !records[1].Repeated {
		t.Fatalf("occurrences = %+v, want 1 then repeated 2", records)
	}
}
