package control

import (
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/config"
)

// Two folders named alike are the common case in a picker, and the whole path
// is unreadable at a glance — so only the collision pays, and only as much as
// it takes to tell them apart.
func TestDescribeScopesLabelsOnlyWhatCollides(t *testing.T) {
	base := testenv.TempDir(t)
	alone := mkdirs(t, base, "solo")
	one := mkdirs(t, base, "work", "api", "frontend")
	two := mkdirs(t, base, "side", "shop", "frontend")

	scopes := DescribeScopes([]string{alone, one, two}, "")
	byRoot := map[string]CapabilityScope{}
	for _, scope := range scopes {
		byRoot[scope.Root] = scope
	}
	if got := byRoot[alone].Label; got != "" {
		t.Fatalf("unique name got label %q, want none", got)
	}
	a, b := byRoot[one].Label, byRoot[two].Label
	if a == "" || b == "" || a == b {
		t.Fatalf("colliding labels = %q and %q, want two distinct non-empty labels", a, b)
	}
	if a != "api/frontend" || b != "shop/frontend" {
		t.Fatalf("labels = %q and %q, want the shortest distinguishing tails", a, b)
	}
}

// Worktrees of one repository are one project. Listing every tree separately is
// exactly the crowding the picker exists to avoid.
func TestDescribeScopesCollapsesWorktreesOfOneRepository(t *testing.T) {
	base := testenv.TempDir(t)
	repo := mkdirs(t, base, "repo")
	gitDir := mkdirs(t, repo, ".git")
	mkdirs(t, gitDir, "worktrees", "studio")
	tree := mkdirs(t, base, "studio")
	if err := os.WriteFile(filepath.Join(tree, ".git"),
		[]byte("gitdir: "+filepath.Join(gitDir, "worktrees", "studio")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	scopes := DescribeScopes([]string{repo, tree}, repo)
	if len(scopes) != 1 {
		t.Fatalf("DescribeScopes = %d entries, want the repository once", len(scopes))
	}
	if !scopes[0].Current || scopes[0].Root != repo {
		t.Fatalf("kept %+v, want the current root to survive the collapse", scopes[0])
	}
}

// Managing another project must not need the session to move there.
func TestInspectProjectReadsAnotherFolderWithoutRepointing(t *testing.T) {
	t.Setenv("REASONIX_HOME", testenv.TempDir(t))
	other := testenv.TempDir(t)
	mustWriteFile(t, filepath.Join(other, ".mcp.json"),
		`{"mcpServers":{"codegraph":{"command":"codegraph","args":["serve"]}}}`)

	before := InspectProject(other)
	if len(before.Servers) != 1 || before.Servers[0].Entry.Name != "codegraph" {
		t.Fatalf("servers = %+v, want the folder's own declaration", before.Servers)
	}
	// The folder's .mcp.json arrived with whatever produced that folder, so the
	// server waits for an answer rather than starting on its own.
	if before.Servers[0].Enabled || !before.Servers[0].Pending {
		t.Fatalf("a freshly declared repo server = %+v, want off and awaiting a decision", before.Servers[0])
	}

	store := config.DefaultActivationStore()
	if err := store.SetServerEnabled(before.Servers[0].Entry, other, config.ActivationProject, true); err != nil {
		t.Fatalf("approve: %v", err)
	}
	if approved := InspectProject(other); !approved.Servers[0].Enabled || approved.Servers[0].Pending {
		t.Fatalf("after approval = %+v, want on and settled", approved.Servers[0])
	}
	if err := store.SetServerEnabled(before.Servers[0].Entry, other, config.ActivationProject, false); err != nil {
		t.Fatalf("SetServerEnabled: %v", err)
	}
	after := InspectProject(other)
	if after.Servers[0].Enabled || !after.Servers[0].LocalOverride {
		t.Fatalf("after switching off: %+v, want off and marked as this project's own", after.Servers[0])
	}
	if after.Scope.Overrides != 1 {
		t.Fatalf("scope.Overrides = %d, want 1", after.Scope.Overrides)
	}
}

func mkdirs(t *testing.T, parts ...string) string {
	t.Helper()
	path := filepath.Join(parts...)
	if err := os.MkdirAll(path, 0o700); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
	return path
}

func mustWriteFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
