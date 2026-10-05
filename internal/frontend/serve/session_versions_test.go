package serve

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/config"
	"reasonix/internal/contract/provider"
	"reasonix/internal/session/control"
	"reasonix/internal/state/sessionstore"
)

// seedVersionedSession writes a conversation and one version a rewind cut
// from it, and returns the workspace, the conversation and the version.
func seedVersionedSession(t *testing.T) (root, parentPath, version string) {
	t.Helper()
	home := testenv.TempDir(t)
	t.Setenv("REASONIX_HOME", home)
	t.Setenv("REASONIX_STATE_HOME", home)
	root = testenv.TempDir(t)
	dir := SessionDirFor(root)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	parentPath = filepath.Join(dir, "20260924-120000-deepseek.jsonl")
	parent := sessionstore.NewSession("sys")
	var cut []provider.Message
	for i := range 3 {
		parent.Add(provider.Message{Role: provider.RoleUser, Content: fmt.Sprintf("ask %d", i)})
		parent.Add(provider.Message{Role: provider.RoleAssistant, Content: fmt.Sprintf("answer %d", i)})
	}
	cut = append(cut, parent.Snapshot()...)
	cut = append(cut, provider.Message{Role: provider.RoleUser, Content: "the prompt later edited"},
		provider.Message{Role: provider.RoleAssistant, Content: "the reply it replaced"})
	if err := parent.Save(parentPath); err != nil {
		t.Fatal(err)
	}
	version, err := sessionstore.SaveSupersededVersion(parentPath, cut)
	if err != nil {
		t.Fatal(err)
	}
	return root, parentPath, version
}

// The sidebar shows one row for the conversation, with the version under it.
func TestSidebarHangsVersionsUnderTheirConversation(t *testing.T) {
	root, parentPath, version := seedVersionedSession(t)
	rows := NewHub(HubOptions{}).workspaceSessions(root, map[string]string{})
	if len(rows) != 1 || rows[0].Path != parentPath {
		t.Fatalf("rows = %+v, want only the conversation", rows)
	}
	if len(rows[0].Versions) != 1 || rows[0].Versions[0].Path != version || rows[0].Versions[0].Turns != 4 {
		t.Fatalf("versions = %+v, want the one cut version with its 4 turns", rows[0].Versions)
	}
}

// A version nobody lists must not outlive its conversation.
func TestRemovingAConversationRemovesItsVersions(t *testing.T) {
	root, parentPath, version := seedVersionedSession(t)
	if err := removeSessionFiles(SessionDirFor(root), parentPath); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(version); !os.IsNotExist(err) {
		t.Fatalf("the version outlived its conversation: %v", err)
	}
}

// paneOn adopts a pane working in root that holds the session at path.
func paneOn(t *testing.T, h *Hub, root, path string) {
	t.Helper()
	bc := NewBroadcaster()
	ctrl := control.New(control.Options{Sink: bc, SessionDir: SessionDirFor(root), WorkspaceRoot: root})
	ctrl.SetSessionPath(path)
	rt, err := h.Adopt(New(ctrl, bc, config.ServeConfig{}), bc)
	if err != nil {
		t.Fatalf("adopt: %v", err)
	}
	t.Cleanup(func() { _ = h.Close(rt.ID) })
}

func countPath(rows []treeSession, path string) int {
	n := 0
	for _, row := range rows {
		if row.Path == path {
			n++
		}
		for _, v := range row.Versions {
			if v.Path == path {
				n++
			}
		}
	}
	return n
}

// A version a pane has open is one row of its own, not also a version under its
// conversation (the session lease keeps a second pane off it).
func TestSidebarListsAnOpenVersionOnce(t *testing.T) {
	root, parentPath, version := seedVersionedSession(t)
	h := NewHub(HubOptions{})
	paneOn(t, h, root, version)
	rows := h.workspaceSessions(root, h.openSessions())
	if len(rows) != 2 || countPath(rows, version) != 1 || countPath(rows, parentPath) != 1 {
		t.Fatalf("rows = %+v, want the conversation and the open version once each", rows)
	}
	for _, row := range rows {
		if len(row.Versions) != 0 {
			t.Fatalf("versions = %+v, want none", row.Versions)
		}
		if row.Path == version && row.RuntimeID == "" {
			t.Fatal("the open version lost its pane")
		}
	}
}

// A pane in another folder gives the version no row here, so it stays under its
// conversation instead of vanishing from the tree.
func TestSidebarKeepsAVersionOpenedFromAnotherRoot(t *testing.T) {
	root, parentPath, version := seedVersionedSession(t)
	h := NewHub(HubOptions{})
	paneOn(t, h, testenv.TempDir(t), version)
	rows := h.workspaceSessions(root, h.openSessions())
	if len(rows) != 1 || rows[0].Path != parentPath || countPath(rows, version) != 1 || len(rows[0].Versions) != 1 {
		t.Fatalf("rows = %+v, want the version under its conversation", rows)
	}
}
