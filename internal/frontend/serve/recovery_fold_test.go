package serve

import (
	"fmt"
	"os"
	"path/filepath"
	"reasonix/internal/state/sessionstore"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/provider"
)

// divergedConflictCopy writes a conflict copy whose tail differs from its
// parent's, so content coverage cannot hide it and only the lineage fold can.
// Each copy forks from the previous one, which is what a live session does: a
// conflict moves it onto the new branch, so the copies form a chain.
func divergedConflictCopy(t *testing.T, parentPath string, turns int, lane string) string {
	t.Helper()
	t.Setenv("REASONIX_WRITER_ID", lane)
	fork := sessionstore.NewSession("sys")
	for i := range turns {
		fork.Add(provider.Message{Role: provider.RoleUser, Content: fmt.Sprintf("why %d", i)})
		fork.Add(provider.Message{Role: provider.RoleAssistant, Content: fmt.Sprintf("answer %s-%d", lane, i)})
	}
	info, err := fork.SaveConflictRecoveryBranch(sessionstore.RecoveryBranchOptions{OriginalPath: parentPath})
	if err != nil {
		t.Fatalf("conflict copy %s: %v", lane, err)
	}
	return info.Path
}

func seedConflictChain(t *testing.T) (root, dir string, chain []string) {
	t.Helper()
	home := testenv.TempDir(t)
	t.Setenv("REASONIX_HOME", home)
	t.Setenv("REASONIX_STATE_HOME", home)
	root = testenv.TempDir(t)
	dir = SessionDirFor(root)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	parentPath := filepath.Join(dir, "20260821-120000-deepseek.jsonl")
	parent := sessionstore.NewSession("sys")
	for i := range 10 {
		parent.Add(provider.Message{Role: provider.RoleUser, Content: fmt.Sprintf("why %d", i)})
		parent.Add(provider.Message{Role: provider.RoleAssistant, Content: fmt.Sprintf("parent %d", i)})
	}
	if err := parent.Save(parentPath); err != nil {
		t.Fatal(err)
	}
	link := parentPath
	for i, turns := range []int{9, 9, 8, 7, 6, 6} {
		next := divergedConflictCopy(t, link, turns, fmt.Sprintf("writer-%d", i))
		chain = append(chain, next)
		link = next
	}
	return root, dir, chain
}

// TestSidebarFoldsAConflictChainWithAReclaimedMiddle is the sidebar the user
// saw: one conversation shown as a staircase of same-titled rows with
// descending turn counts. Folding walked ParentID to group the copies, so every
// middle link GC reclaimed split the chain and added a row — and reclaiming
// covered middles is exactly GC's job.
func TestSidebarFoldsAConflictChainWithAReclaimedMiddle(t *testing.T) {
	root, _, chain := seedConflictChain(t)
	hub := NewHub(HubOptions{})

	rows := hub.workspaceSessions(root, nil)
	if len(rows) != 2 {
		t.Fatalf("intact chain drew %d rows, want the conversation and one folded copy: %s", len(rows), rowSummary(rows))
	}

	// GC reclaims a covered copy from the middle of the chain.
	for _, suffix := range []string{"", ".meta"} {
		if err := os.Remove(chain[2] + suffix); err != nil {
			t.Fatalf("remove middle link: %v", err)
		}
	}
	rows = hub.workspaceSessions(root, nil)
	if len(rows) != 2 {
		t.Fatalf("a reclaimed middle link split the chain into %d rows: %s", len(rows), rowSummary(rows))
	}
	folded := 0
	for _, row := range rows {
		folded += len(row.Copies)
	}
	if folded != 4 {
		t.Errorf("folded %d copies, want the 4 that survive the reclaim: %s", folded, rowSummary(rows))
	}
}

func rowSummary(rows []treeSession) string {
	var out strings.Builder
	for _, row := range rows {
		fmt.Fprintf(&out, "\n  %s turns=%d copies=%d", filepath.Base(row.Path), row.Turns, len(row.Copies))
	}
	return out.String()
}
