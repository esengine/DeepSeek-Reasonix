package workspacestate

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestEnsureWorkspaceResolvedReturnsExistingPhysicalOwner(t *testing.T) {
	realRoot := t.TempDir()
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(realRoot, alias); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	store := NewStore(filepath.Join(t.TempDir(), "workspace-state-v1.json"))
	if err := store.EnsureWorkspace(t.Context(), Workspace{ID: "legacy-project", Root: realRoot, Title: "kept"}); err != nil {
		t.Fatal(err)
	}
	id, err := store.EnsureWorkspaceResolved(t.Context(), Workspace{ID: "new-candidate", Root: alias, Title: "ignored"})
	if err != nil || id != "legacy-project" {
		t.Fatalf("resolved id = %q, err = %v", id, err)
	}
	state, err := store.Load(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Workspaces) != 1 || state.Workspaces[id].Root != realRoot || state.Workspaces[id].Title != "kept" {
		t.Fatalf("state was rewritten: %+v", state.Workspaces)
	}
}

func writeRegistryState(t *testing.T, path string, state State) []byte {
	t.Helper()
	state.Initialized = true
	body, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(body, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	return body
}

// A state written before physical identity could record one directory twice.
// The registry must repair that on contact: refusing leaves every projection
// over the directory ambiguous, which strands both records in the sidebar.
func TestEnsureWorkspaceResolvedRepairsAmbiguousLegacyOwners(t *testing.T) {
	root := t.TempDir()
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(root, alias); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	path := filepath.Join(t.TempDir(), "workspace-state-v1.json")
	state := newState()
	state.WorkspaceIDs = []string{"old-a", "old-b"}
	state.Workspaces["old-a"] = Workspace{ID: "old-a", Root: root, Title: "kept", SessionIDs: []string{"session-a"}}
	state.Workspaces["old-b"] = Workspace{
		ID: "old-b", Root: alias, Visible: true, SessionIDs: []string{"session-b"},
		Organization: &Organization{Groups: []OrganizationGroup{{ID: "group-b", Title: "B", Members: []string{SessionKey("session-b")}}}},
	}
	state.SessionStates = map[string]SessionState{
		"session-a": {Lifecycle: Active}, "session-b": {Lifecycle: Active},
	}
	state.SourceMappings["source-b"] = SourceMapping{SourceKey: "source-b", SessionID: "session-b", WorkspaceID: "old-b", Fingerprint: "fp-b"}
	writeRegistryState(t, path, state)

	store := NewStore(path)
	if id, found, err := ResolveWorkspaceID(state, alias); err != nil || !found || id != "old-a" {
		t.Fatalf("read-only resolution = %q (found=%v): %v", id, found, err)
	}
	index := NewWorkspaceIndex(state)
	for _, candidate := range []string{root, alias} {
		if id, found, err := index.Resolve(candidate); err != nil || !found || id != "old-a" {
			t.Fatalf("indexed resolution for %q = %q (found=%v): %v", candidate, id, found, err)
		}
	}
	id, err := store.EnsureWorkspaceResolved(t.Context(), Workspace{ID: "candidate", Root: root})
	if err != nil || id != "old-a" {
		t.Fatalf("resolved id = %q, err = %v", id, err)
	}
	repaired, err := store.Load(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	owner, ok := repaired.Workspaces["old-a"]
	if !ok || len(repaired.Workspaces) != 1 || !slices.Equal(repaired.WorkspaceIDs, []string{"old-a"}) {
		t.Fatalf("duplicate survived the repair: %+v", repaired.Workspaces)
	}
	if owner.Root != root || owner.Title != "kept" || !owner.Visible {
		t.Fatalf("owner lost its identity or presentation: %+v", owner)
	}
	if !slices.Equal(owner.SessionIDs, []string{"session-a", "session-b"}) {
		t.Fatalf("sessions = %v", owner.SessionIDs)
	}
	if owner.Organization == nil || len(owner.Organization.Groups) != 1 || owner.Organization.Groups[0].ID != "group-b" {
		t.Fatalf("organization was dropped: %+v", owner.Organization)
	}
	if repaired.SourceMappings["source-b"].WorkspaceID != "old-a" {
		t.Fatalf("source mapping still names the absorbed workspace: %+v", repaired.SourceMappings)
	}
}

// The Global folder lives at a fixed directory. A user who also added that
// directory as an ordinary project owns it twice, and Global must survive:
// its ID is addressed across the app and cannot be removed or hidden.
func TestEnsureWorkspaceResolvedKeepsGlobalOverOlderProjectDuplicate(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(t.TempDir(), "workspace-state-v1.json")
	state := newState()
	state.WorkspaceIDs = []string{"project-legacy", GlobalWorkspaceID}
	state.Workspaces["project-legacy"] = Workspace{
		ID: "project-legacy", Root: root, Title: "global-workspace", Visible: true,
		SessionIDs: []string{}, CreatedAt: time.Unix(0, 0).UTC(),
	}
	state.Workspaces[GlobalWorkspaceID] = Workspace{
		ID: GlobalWorkspaceID, Root: root, Title: "Global", Visible: true,
		SessionIDs: []string{}, CreatedAt: time.Unix(1, 0).UTC(),
	}
	writeRegistryState(t, path, state)

	store := NewStore(path)
	id, err := store.EnsureWorkspaceResolved(t.Context(), Workspace{ID: "project-legacy", Root: root, Title: "global-workspace"})
	if err != nil || id != GlobalWorkspaceID {
		t.Fatalf("resolved id = %q, err = %v", id, err)
	}
	repaired, err := store.Load(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(repaired.Workspaces) != 1 || repaired.Workspaces[GlobalWorkspaceID].Title != "Global" {
		t.Fatalf("global did not keep the directory: %+v", repaired.Workspaces)
	}
}

// A path that does not exist resolves through whichever ancestor does, so two
// distinct directories on an unreachable case-sensitive volume can share a key
// on a case-insensitive host. Folding records is not reversible: while the
// directory cannot be verified, a lookup answers but changes nothing.
func TestEnsureWorkspaceResolvedLeavesUnverifiableDirectoriesUnmerged(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "unmounted", "project")
	path := filepath.Join(t.TempDir(), "workspace-state-v1.json")
	state := newState()
	state.WorkspaceIDs = []string{"project-a", "project-b"}
	state.Workspaces["project-a"] = Workspace{ID: "project-a", Root: missing, SessionIDs: []string{"session-a"}}
	state.Workspaces["project-b"] = Workspace{ID: "project-b", Root: missing + string(os.PathSeparator) + ".", SessionIDs: []string{"session-b"}}
	state.SessionStates = map[string]SessionState{
		"session-a": {Lifecycle: Active}, "session-b": {Lifecycle: Active},
	}
	before := writeRegistryState(t, path, state)

	id, err := NewStore(path).EnsureWorkspaceResolved(t.Context(), Workspace{ID: "candidate", Root: missing})
	if err != nil || id != "project-a" {
		t.Fatalf("resolved id = %q, err = %v", id, err)
	}
	after, err := os.ReadFile(path)
	if err != nil || strings.TrimSpace(string(after)) != string(before) {
		t.Fatalf("lookup rewrote records for a directory it could not verify: %v", err)
	}
}

// One record this host cannot resolve must not fail every other workspace.
func TestUnresolvableWorkspaceRootDoesNotBlockOtherWorkspaces(t *testing.T) {
	loop := filepath.Join(t.TempDir(), "loop")
	if err := os.Symlink(loop, loop); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	root := t.TempDir()
	path := filepath.Join(t.TempDir(), "workspace-state-v1.json")
	state := newState()
	state.WorkspaceIDs = []string{"project-broken", "project-ok"}
	state.Workspaces["project-broken"] = Workspace{ID: "project-broken", Root: loop, SessionIDs: []string{}}
	state.Workspaces["project-ok"] = Workspace{ID: "project-ok", Root: root, SessionIDs: []string{}}
	writeRegistryState(t, path, state)

	id, err := NewStore(path).EnsureWorkspaceResolved(t.Context(), Workspace{ID: "candidate", Root: root})
	if err != nil || id != "project-ok" {
		t.Fatalf("resolved id = %q, err = %v", id, err)
	}
}

func TestEnsureWorkspaceResolvedUsesVersionedIDForRealCollision(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "workspace-state-v1.json"))
	if err := store.EnsureWorkspace(t.Context(), Workspace{ID: "project-collision", Root: filepath.Join(t.TempDir(), "one")}); err != nil {
		t.Fatal(err)
	}
	id, err := store.EnsureWorkspaceResolved(t.Context(), Workspace{ID: "project-collision", Root: filepath.Join(t.TempDir(), "two")})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(id, "project-v2-") {
		t.Fatalf("fallback id = %q", id)
	}
}

func TestEnsureWorkspaceAcceptsEquivalentRootWithoutRewritingState(t *testing.T) {
	for _, id := range []string{GlobalWorkspaceID, "project-a"} {
		t.Run(id, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "Workspace")
			path := filepath.Join(t.TempDir(), "workspace-state-v1.json")
			store := NewStore(path)
			if err := store.EnsureWorkspace(t.Context(), Workspace{ID: id, Root: root, Title: "My workspace", Visible: false}); err != nil {
				t.Fatal(err)
			}
			if err := store.AttachSession(t.Context(), "", id, "saved-session", ""); err != nil {
				t.Fatal(err)
			}
			if err := store.ArchiveSession(t.Context(), "saved-session"); err != nil {
				t.Fatal(err)
			}
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			aliases := []string{root + string(os.PathSeparator) + ".", root + string(os.PathSeparator)}
			if runtime.GOOS == "windows" {
				aliases = append(aliases, strings.ToUpper(root), strings.ReplaceAll(root, `\`, "/"))
			}
			for _, alias := range aliases {
				// Reopen the registry just as a later process would. An equivalent
				// spelling must preserve the old root, presentation and lifecycle.
				if err := NewStore(path).EnsureWorkspace(t.Context(), Workspace{ID: id, Root: alias, Title: "Default", Visible: true}); err != nil {
					t.Fatalf("equivalent root %q: %v", alias, err)
				}
				after, err := os.ReadFile(path)
				if err != nil || string(after) != string(before) {
					t.Fatalf("equivalent root rewrote persisted state: %v", err)
				}
			}
		})
	}
}

func TestEnsureWorkspaceRejectsDifferentRootWithoutRewritingState(t *testing.T) {
	root := filepath.Join(t.TempDir(), "Workspace")
	store := NewStore(filepath.Join(t.TempDir(), "workspace-state-v1.json"))
	if err := store.EnsureWorkspace(t.Context(), Workspace{ID: GlobalWorkspaceID, Root: root}); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(store.Path())
	if err != nil {
		t.Fatal(err)
	}
	roots := []string{"", filepath.Join(root, "different")}
	if runtime.GOOS != "windows" && runtime.GOOS != "darwin" {
		roots = append(roots, strings.ToUpper(root))
	}
	for _, other := range roots {
		if err := store.EnsureWorkspace(t.Context(), Workspace{ID: GlobalWorkspaceID, Root: other}); !errors.Is(err, ErrMutationConflict) {
			t.Fatalf("different root %q: %v", other, err)
		}
	}
	after, err := os.ReadFile(store.Path())
	if err != nil || string(after) != string(before) {
		t.Fatalf("conflicting root rewrote persisted state: %v", err)
	}
}
