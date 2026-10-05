package control

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/config"
)

func TestPermissionRulesReportsRememberedProjectGrants(t *testing.T) {
	home := testenv.TempDir(t)
	t.Setenv("REASONIX_HOME", home)
	workspace := testenv.TempDir(t)
	other := testenv.TempDir(t)
	store := config.NewProjectGrantStore(config.Roots{}.Home())
	if err := store.Update(workspace, func(g config.ProjectGrant) (config.ProjectGrant, error) {
		g.Allow = []string{"Bash(go test:*)"}
		return g, nil
	}); err != nil {
		t.Fatal(err)
	}

	ctrl := New(Options{WorkspaceRoot: workspace})
	defer ctrl.Close()
	rules := ctrl.PermissionRules()
	if rules.RememberedPath != store.Path() || rules.RememberedErrorCode != "" || len(rules.Remembered) != 1 || rules.Remembered[0] != "Bash(go test:*)" {
		t.Fatalf("project rules = %+v", rules)
	}

	otherCtrl := New(Options{WorkspaceRoot: other})
	defer otherCtrl.Close()
	if got := otherCtrl.PermissionRules(); len(got.Remembered) != 0 {
		t.Fatalf("other workspace inherited grants: %+v", got)
	}

	if err := os.WriteFile(store.Path(), []byte("broken JSON"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := ctrl.PermissionRules(); got.RememberedErrorCode != "project_grants.unavailable" || len(got.Remembered) != 0 || got.RememberedPath != filepath.Join(home, "project-grants.json") {
		t.Fatalf("unreadable grant store was hidden or applied: %+v", got)
	}
}

func TestRevokeRememberedProjectRulePreservesOtherWorkspaceAndWriteGrant(t *testing.T) {
	home := testenv.TempDir(t)
	t.Setenv("REASONIX_HOME", home)
	workspace := testenv.TempDir(t)
	other := testenv.TempDir(t)
	store := config.NewProjectGrantStore(config.Roots{}.Home())
	for _, root := range []string{workspace, other} {
		if err := store.Update(root, func(g config.ProjectGrant) (config.ProjectGrant, error) {
			g.Allow = []string{"Bash(go test:*)", "Bash(git status:*)"}
			g.AllowWrite = []string{"/tmp/external"}
			return g, nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	ctrl := New(Options{WorkspaceRoot: workspace})
	defer ctrl.Close()
	ctrl.RestoreSessionAuthorizations(SessionAuthorizations{Grants: []string{"Bash(go test:*)", "Bash(git status:*)"}})
	command := json.RawMessage(`{"command":"go test ./..."}`)
	if !ctrl.approval.preApproved("bash", "go test ./...", command) {
		t.Fatal("session grant did not authorize the command before revocation")
	}
	if err := ctrl.RevokeRememberedProjectRule("Bash(go test:*)"); err != nil {
		t.Fatal(err)
	}
	if ctrl.approval.preApproved("bash", "go test ./...", command) {
		t.Fatal("revoked project rule still authorized by its session grant")
	}
	if !slices.Equal(ctrl.SessionAuthorizations().Grants, []string{"Bash(git status:*)"}) {
		t.Fatalf("other session grants changed: %+v", ctrl.SessionAuthorizations())
	}
	got, err := store.Grant(workspace)
	if err != nil || !slices.Equal(got.Allow, []string{"Bash(git status:*)"}) || !slices.Equal(got.AllowWrite, []string{"/tmp/external"}) {
		t.Fatalf("selected project grant = %+v, %v", got, err)
	}
	otherGrant, err := store.Grant(other)
	if err != nil || len(otherGrant.Allow) != 2 {
		t.Fatalf("other project grant changed: %+v, %v", otherGrant, err)
	}
}
