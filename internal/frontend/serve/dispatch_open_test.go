package serve

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/config"
	"reasonix/internal/session/control"
)

func openFixture(t *testing.T, userFile string) (*control.Controller, string) {
	t.Helper()
	home := testenv.TempDir(t)
	t.Setenv("REASONIX_HOME", home)
	t.Setenv("HOME", filepath.Join(home, "userhome"))
	t.Setenv("USERPROFILE", filepath.Join(home, "userhome"))
	if err := os.MkdirAll(filepath.Join(home, "userhome"), 0o755); err != nil {
		t.Fatal(err)
	}
	if userFile != "" {
		if err := os.WriteFile(config.UserConfigPath(), []byte(userFile), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	work := testenv.TempDir(t)
	return control.New(control.Options{WorkspaceRoot: work, Posture: control.PostureEvidence{Home: home}}), home
}

func folder(t *testing.T, name string) string {
	t.Helper()
	dir := filepath.Join(testenv.TempDir(t), name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestOpeningProjectsListsAndTrustsExactlyTheApprovedFolders(t *testing.T) {
	c, _ := openFixture(t, "")
	a, b := folder(t, "a"), folder(t, "b")
	got, err := openProjects(context.Background(), c, []string{a, b})
	if err != nil || len(got) != 2 || !got[0].Added || !got[1].Added || got[0].Code != "" {
		t.Fatalf("got %+v err=%v", got, err)
	}
	for _, p := range []string{a, b} {
		if !slices.Contains(Workspaces(), p) {
			t.Errorf("%s is not in the project list: %v", p, Workspaces())
		}
		if trust := c.TrustOfWorkspace(p); trust != config.WorkspaceTrusted {
			t.Errorf("%s trust = %q", p, trust)
		}
	}
	if again, _ := openProjects(context.Background(), c, []string{a}); again[0].Added || again[0].Code != "" {
		t.Fatalf("a folder already listed = %+v, want neither added nor refused", again[0])
	}
}

func TestOpeningProjectsRefusesTheFolderNobodyShouldGrantWholesale(t *testing.T) {
	c, home := openFixture(t, "")
	userHome := filepath.Join(home, "userhome")
	missing := filepath.Join(home, "absent")
	ok := folder(t, "fine")
	got, _ := openProjects(context.Background(), c, []string{userHome, missing, ok})
	if got[0].Code != string(config.RootTooBroad) {
		t.Errorf("home = %+v, want %s", got[0], config.RootTooBroad)
	}
	if got[1].Code != string(config.RootNotDirectory) {
		t.Errorf("missing = %+v, want %s", got[1], config.RootNotDirectory)
	}
	if got[2].Code != "" || !got[2].Added {
		t.Errorf("the ordinary folder beside them = %+v, want it opened", got[2])
	}
	for _, p := range []string{userHome, missing} {
		if slices.Contains(Workspaces(), p) || c.TrustOfWorkspace(p) != config.WorkspaceTrustUndecided {
			t.Errorf("a refused folder %s was listed or trusted", p)
		}
	}
}

func TestOpeningProjectsDoesNotOverrideAFolderTheUserDeclined(t *testing.T) {
	c, _ := openFixture(t, "")
	declined := folder(t, "declined")
	owner := control.New(control.Options{WorkspaceRoot: declined, Posture: control.PostureEvidence{Home: os.Getenv("REASONIX_HOME")}})
	if err := owner.DecideWorkspaceTrust(config.WorkspaceTrustDeclined); err != nil {
		t.Fatal(err)
	}
	got, _ := openProjects(context.Background(), c, []string{declined})
	if got[0].Code != codeOpenUntrusted || got[0].Added || slices.Contains(Workspaces(), declined) {
		t.Fatalf("got %+v list %v", got[0], Workspaces())
	}
}

func TestOpeningProjectsStopsAtTheUsersOwnProjectLimit(t *testing.T) {
	c, _ := openFixture(t, "[desktop]\nmax_workspaces = 1\n")
	a, b := folder(t, "a"), folder(t, "b")
	got, _ := openProjects(context.Background(), c, []string{a, b})
	if !got[0].Added || got[1].Code != codeOpenListFull || slices.Contains(Workspaces(), b) {
		t.Fatalf("got %+v list %v", got, Workspaces())
	}
}
