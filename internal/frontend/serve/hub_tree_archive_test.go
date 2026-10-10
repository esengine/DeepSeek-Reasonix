package serve

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/provider"
	"reasonix/internal/state/sessionstore"
	"reasonix/internal/state/store"
)

func archiveTreeSession(t *testing.T, srv *httptest.Server, path string, archived bool) {
	t.Helper()
	body, err := json.Marshal(struct {
		Path     string `json:"path"`
		Archived bool   `json:"archived"`
	}{Path: path, Archived: archived})
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.Post(srv.URL+"/tree/sessions/archive", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("POST /tree/sessions/archive = %d, want 204", resp.StatusCode)
	}
}

func seedLegacyRecoveryRow(t *testing.T) (root, dir string, lineage []string) {
	t.Helper()
	home := testenv.TempDir(t)
	t.Setenv("REASONIX_HOME", home)
	t.Setenv("REASONIX_STATE_HOME", home)
	root = testenv.TempDir(t)
	dir = SessionDirFor(root)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}

	legacyPath := filepath.Join(dir, "20260803-140947-legacy.jsonl")
	legacy := sessionstore.NewSession("sys")
	legacy.Add(provider.Message{Role: provider.RoleUser, Content: "legacy prompt"})
	if err := legacy.Save(legacyPath); err != nil {
		t.Fatal(err)
	}
	meta := sessionstore.BranchMeta{
		TopicID:    "topic_legacy",
		TopicTitle: "legacy row",
		ImportedFrom: &sessionstore.ImportSource{
			Line: "legacy desktop recovery snapshot",
		},
	}
	if err := sessionstore.SaveBranchMeta(legacyPath, meta); err != nil {
		t.Fatal(err)
	}

	parent := legacyPath
	for i := range 3 {
		copy := sessionstore.NewSession("sys")
		copy.Add(provider.Message{Role: provider.RoleUser, Content: "legacy prompt"})
		copy.Add(provider.Message{Role: provider.RoleAssistant, Content: string(rune('a' + i))})
		info, err := copy.SaveRecoveryBranch(sessionstore.RecoveryBranchOptions{
			OriginalPath: parent,
			Reason:       "legacy desktop recovery snapshot",
			BranchMeta:   meta,
		})
		if err != nil {
			t.Fatal(err)
		}
		lineage = append(lineage, info.Path)
		parent = info.Path
	}
	if err := store.RemoveSessionArtifacts(legacyPath); err != nil {
		t.Fatal(err)
	}
	return root, dir, lineage
}

func TestArchiveLegacyRecoveryRowArchivesEverySibling(t *testing.T) {
	root, _, lineage := seedLegacyRecoveryRow(t)
	h := NewHub(HubOptions{})
	hubRuntime(t, h, root)
	srv := httptest.NewServer(operatorHandler(h))
	defer srv.Close()

	tree := hubGet[[]treeWorkspace](t, srv, "/tree")
	if len(tree) != 1 || len(tree[0].Sessions) != 1 {
		t.Fatalf("/tree = %+v, want one folded legacy row", tree)
	}
	row := tree[0].Sessions[0]
	if len(row.Copies) != len(lineage)-1 {
		t.Fatalf("legacy row has %d siblings, want %d: %+v", len(row.Copies), len(lineage)-1, row)
	}

	archiveTreeSession(t, srv, row.Path, true)
	for _, path := range lineage {
		meta, ok, err := sessionstore.LoadBranchMeta(path)
		if err != nil || !ok {
			t.Fatalf("load archived sibling %s: ok=%v err=%v", filepath.Base(path), ok, err)
		}
		if !meta.Archived {
			t.Errorf("sibling %s was not archived with its legacy row", filepath.Base(path))
		}
	}

	// Recovery GC may reclaim the archived lead once its content is covered.
	// None of its siblings may then surface as a new active conversation.
	if err := store.RemoveSessionArtifacts(row.Path); err != nil {
		t.Fatal(err)
	}
	tree = hubGet[[]treeWorkspace](t, srv, "/tree")
	active := 0
	for _, ws := range tree {
		for _, session := range ws.Sessions {
			if !session.Archived {
				active++
			}
		}
	}
	if active != 0 {
		t.Fatalf("archiving the legacy row left %d active sibling row(s): %+v", active, tree)
	}
}

func TestArchiveLegacyRecoveryRowIsRepeatableAndReversible(t *testing.T) {
	root, dir, lineage := seedLegacyRecoveryRow(t)
	h := NewHub(HubOptions{})
	hubRuntime(t, h, root)
	srv := httptest.NewServer(operatorHandler(h))
	defer srv.Close()

	tree := hubGet[[]treeWorkspace](t, srv, "/tree")
	if len(tree) != 1 || len(tree[0].Sessions) != 1 {
		t.Fatalf("/tree = %+v, want one folded legacy row", tree)
	}
	row := tree[0].Sessions[0]
	archiveTreeSession(t, srv, row.Path, true)
	archiveTreeSession(t, srv, row.Path, true)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	transcripts := 0
	for _, entry := range entries {
		if !entry.IsDir() && store.IsSessionTranscriptName(entry.Name()) {
			transcripts++
		}
	}
	if got := transcripts; got != len(lineage) {
		t.Fatalf("repeated archive wrote new sessions: %d entries, want %d", got, len(lineage))
	}

	archiveTreeSession(t, srv, row.Path, false)
	for _, path := range lineage {
		meta, ok, err := sessionstore.LoadBranchMeta(path)
		if err != nil || !ok {
			t.Fatalf("load restored sibling %s: ok=%v err=%v", filepath.Base(path), ok, err)
		}
		if meta.Archived {
			t.Errorf("sibling %s stayed archived after unarchive", filepath.Base(path))
		}
	}
	tree = hubGet[[]treeWorkspace](t, srv, "/tree")
	if len(tree) != 1 || len(tree[0].Sessions) != 1 || tree[0].Sessions[0].Archived {
		t.Fatalf("unarchive did not restore one active folded row: %+v", tree)
	}
}

func TestArchiveLegacyRecoveryRowKeepsAnOpenSiblingActive(t *testing.T) {
	root, _, lineage := seedLegacyRecoveryRow(t)
	h := NewHub(HubOptions{})
	rt := hubRuntime(t, h, root)
	openPath := lineage[0]
	rt.Server.Controller().SetSessionPath(openPath)
	srv := httptest.NewServer(operatorHandler(h))
	defer srv.Close()

	tree := hubGet[[]treeWorkspace](t, srv, "/tree")
	if len(tree) != 1 {
		t.Fatalf("/tree = %+v, want one workspace", tree)
	}
	var folded *treeSession
	for i := range tree[0].Sessions {
		if len(tree[0].Sessions[i].Copies) > 0 {
			folded = &tree[0].Sessions[i]
			break
		}
	}
	if folded == nil {
		t.Fatalf("/tree = %+v, want a folded recovery row beside the open sibling", tree)
	}

	archiveTreeSession(t, srv, folded.Path, true)
	meta, ok, err := sessionstore.LoadBranchMeta(openPath)
	if err != nil || !ok {
		t.Fatalf("load open sibling: ok=%v err=%v", ok, err)
	}
	if meta.Archived {
		t.Fatal("archiving a folded recovery row archived the sibling a pane is still writing")
	}

	tree = hubGet[[]treeWorkspace](t, srv, "/tree")
	for _, session := range tree[0].Sessions {
		if session.Path == openPath && session.RuntimeID == rt.ID && !session.Archived {
			return
		}
	}
	t.Fatalf("open sibling did not stay active: %+v", tree)
}
