package main

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"reasonix/desktop/internal/workspacestate"
	"reasonix/internal/session"
)

func TestProjectTreeUnregisteredHistoricalPins(t *testing.T) {
	isolateDesktopUserDirs(t)
	root, other := t.TempDir(), t.TempDir()
	if err := addProject(root, "Legacy"); err != nil {
		t.Fatal(err)
	}
	app := NewApp()
	app.ctx = t.Context()
	t.Cleanup(app.closeSessionServices)
	app.historicalImports.catalog = []historicalCatalogEntry{
		{scope: "project", node: ProjectNode{Key: "source_kept", Root: root, Pinned: true, Source: &SessionSourceRef{HostID: localDesktopHostID, SourceKey: "kept"}}},
		{scope: "project", node: ProjectNode{Key: "source_other", Root: other, Pinned: true, Source: &SessionSourceRef{HostID: localDesktopHostID, SourceKey: "other"}}},
	}
	for range 2 {
		snapshot := app.GetProjectTreeSnapshot()
		if len(snapshot.Projects) != 1 || len(snapshot.Projects[0].Children) != 1 || snapshot.Projects[0].Children[0].Key != "source_kept" {
			t.Fatalf("unregistered project lost its pin or included another project's pin: %+v", snapshot.Projects)
		}
		if _, err := os.Stat(app.workspaceRegistry().Path()); !os.IsNotExist(err) {
			t.Fatalf("snapshot created a registry: %v", err)
		}
	}
}

func TestProjectedShellOrganizationPreservesAdoptedAliasOrder(t *testing.T) {
	app, root, refs := canonicalOrganizationFixture(t, "a", "b")
	state, err := app.workspaceRegistry().LoadProjection(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	workspaceID := desktopWorkspaceID("project", root)
	workspace := state.Workspaces[workspaceID]
	workspace.Organization = &workspacestate.Organization{Order: []string{}, Imported: map[string]bool{}}
	state.SourceMappings["adopted"] = workspacestate.SourceMapping{WorkspaceID: workspaceID, SessionID: "b", SourceKey: "adopted", Path: filepath.Join(root, "old.jsonl")}
	legacy := desktopProject{ManualSessionOrder: true, SessionOrder: []string{"source\x00local\x00adopted", workspacestate.SessionKey("a")}}
	org := projectedShellOrganization(workspace, state, nil, legacy)
	want := []string{workspacestate.SessionKey("b"), workspacestate.SessionKey("a")}
	if !reflect.DeepEqual(org.Order, want) {
		t.Fatalf("alias order=%v want=%v", org.Order, want)
	}
	if workspace.Organization.MigrationVersion != 0 || len(workspace.Organization.Imported) != 0 || len(workspace.Organization.Order) != 0 {
		t.Fatal("projection mutated the caller's organization")
	}
	// The same identity must sort the canonical row, never its retired source.
	nodes := []ProjectNode{{Session: &session.SessionRef{HostID: refs["a"].HostID, SessionID: "a"}}, {Session: &session.SessionRef{HostID: refs["b"].HostID, SessionID: "b"}}}
	got := filterWorkspaceSessionNodes(ProjectTopicPageRequest{}, org, state, workspaceID, nodes)
	if len(got) != 2 || got[0].SortOrder != 1 || got[1].SortOrder != 0 {
		t.Fatalf("canonical ranks=%+v", got)
	}
}

func TestProjectTreeUnmigratedManualOrderMatchesExpandedPage(t *testing.T) {
	for _, mode := range []string{"session", "topic"} {
		t.Run(mode, func(t *testing.T) {
			app, root, _ := canonicalOrganizationFixture(t, "a", "b", "c")
			if err := updateProjectsFile(func(f *desktopProjectFile) (bool, error) {
				p := &f.Projects[projectIndexByRoot(f.Projects, root)]
				if mode == "topic" {
					p.Topics, p.ManualTopicOrder = []string{"c", "a", "b"}, true
				} else {
					p.SessionOrder = []string{workspacestate.SessionKey("c"), workspacestate.SessionKey("a"), workspacestate.SessionKey("b")}
					p.ManualSessionOrder = true
				}
				return true, nil
			}); err != nil {
				t.Fatal(err)
			}
			pinned := true
			if err := app.workspaceRegistry().UpdatePresentation(t.Context(), []string{"a", "b", "c"}, nil, &pinned); err != nil {
				t.Fatal(err)
			}
			before, err := os.ReadFile(app.workspaceRegistry().Path())
			if err != nil {
				t.Fatal(err)
			}
			keys := func(nodes []ProjectNode) []string {
				result := []string{}
				for _, node := range nodes {
					result = append(result, projectNodeSessionKey(node))
				}
				return result
			}
			want := []string{workspacestate.SessionKey("c"), workspacestate.SessionKey("a"), workspacestate.SessionKey("b")}
			for range 2 {
				snapshot := app.GetProjectTreeSnapshot()
				if len(snapshot.Projects) != 1 || !reflect.DeepEqual(keys(snapshot.Projects[0].Children), want) {
					t.Fatalf("cold snapshot order: %+v", snapshot.Projects)
				}
			}
			after, err := os.ReadFile(app.workspaceRegistry().Path())
			if err != nil || !bytes.Equal(before, after) {
				t.Fatalf("snapshot persisted migration: %v", err)
			}
			page, err := app.ListProjectTopics(ProjectTopicPageRequest{Scope: "project", WorkspaceRoot: root, Limit: 50})
			if err != nil || !reflect.DeepEqual(keys(page.Items), want) {
				t.Fatalf("expanded page order=%v err=%v", keys(page.Items), err)
			}
			// Once migrated, durable user choices override legacy preferences.
			want = []string{workspacestate.SessionKey("b"), workspacestate.SessionKey("a"), workspacestate.SessionKey("c")}
			if err := app.ReorderSessions("project", root, want); err != nil {
				t.Fatal(err)
			}
			if got := keys(app.GetProjectTreeSnapshot().Projects[0].Children); !reflect.DeepEqual(got, want) {
				t.Fatalf("persisted order=%v want=%v", got, want)
			}
		})
	}
}

func TestHistoricalProjectionUnregisteredPathAlias(t *testing.T) {
	isolateDesktopUserDirs(t)
	root := t.TempDir()
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(root, alias); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	app := NewApp()
	app.ctx = t.Context()
	app.historicalImports.catalog = []historicalCatalogEntry{{scope: "project", node: ProjectNode{Root: alias, Source: &SessionSourceRef{SourceKey: "alias"}}}}
	state, err := app.workspaceRegistry().LoadProjection(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if got := app.historicalCanonicalTopics("project", root, state); len(got) != 1 {
		t.Fatalf("unregistered alias lost: %+v", got)
	}
}

func TestProjectTreeHistoricalManualOrderBeforeRegistration(t *testing.T) {
	for _, scope := range []string{"project", "global"} {
		t.Run(scope, func(t *testing.T) {
			isolateDesktopUserDirs(t)
			root := t.TempDir()
			if scope == "project" {
				if err := addProject(root, "Legacy"); err != nil {
					t.Fatal(err)
				}
			}
			app := NewApp()
			app.ctx = t.Context()
			t.Cleanup(app.closeSessionServices)
			for _, id := range []string{"a", "b"} {
				path := filepath.Join(root, id+".jsonl")
				app.historicalImports.catalog = append(app.historicalImports.catalog, historicalCatalogEntry{scope: scope, node: ProjectNode{Root: root, Key: "source_" + id, TopicID: id, SessionPath: path, Pinned: true, Source: &SessionSourceRef{HostID: localDesktopHostID, SourceKey: id, Path: path}}})
			}
			want := []string{"source\x00local\x00b", "source\x00local\x00a"}
			if err := updateProjectsFile(func(f *desktopProjectFile) (bool, error) {
				if scope == "global" {
					f.GlobalSessionOrder, f.GlobalManualSessionOrder = want, true
				} else {
					f.Projects[0].SessionOrder, f.Projects[0].ManualSessionOrder = want, true
				}
				return true, nil
			}); err != nil {
				t.Fatal(err)
			}
			for range 2 {
				snapshot := app.GetProjectTreeSnapshot()
				if len(snapshot.Projects) != 1 {
					t.Fatalf("projects=%+v", snapshot.Projects)
				}
				got := []string{}
				for _, node := range snapshot.Projects[0].Children {
					got = append(got, projectNodeSessionKey(node))
				}
				if !reflect.DeepEqual(got, want) {
					t.Fatalf("historical order=%v want=%v", got, want)
				}
			}
			if _, err := os.Stat(app.workspaceRegistry().Path()); !os.IsNotExist(err) {
				t.Fatalf("read created registry: %v", err)
			}
		})
	}
}
