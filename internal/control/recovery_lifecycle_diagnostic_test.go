package control

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/agent"
	"reasonix/internal/store"
)

func TestRecordRecoveryLifecycleAcceptsOnlyRedactedKnownOutcomes(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "private-session.jsonl")
	if err := agent.SaveBranchMeta(path, agent.BranchMeta{
		ID: agent.BranchID(path), Scope: "project", WorkspaceRoot: "/private/workspace",
		TopicID: "private-topic", TopicTitle: "private title", CustomTitle: "private note",
	}); err != nil {
		t.Fatal(err)
	}

	RecordRecoveryLifecycle(path, "classified_diverged")
	RecordRecoveryLifecycle(path, "classified_diverged")
	RecordRecoveryLifecycle(path, "private user-controlled outcome")

	file, err := os.Open(store.SessionConflictLog(path))
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	lines := []string{}
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if len(lines) != 1 || !strings.Contains(lines[0], `"mode":"catalog"`) ||
		!strings.Contains(lines[0], `"outcome":"classified_diverged"`) {
		t.Fatalf("lifecycle records = %+v", lines)
	}
	for _, secret := range []string{dir, "/private/workspace", "private-topic", "private title", "private note", "user-controlled"} {
		if strings.Contains(lines[0], secret) {
			t.Fatalf("lifecycle diagnostic leaked %q: %s", secret, lines[0])
		}
	}
}
