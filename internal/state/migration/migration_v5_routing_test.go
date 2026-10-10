package migration

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"reasonix/internal/contract/config"
	"reasonix/internal/contract/event"
)

func hashTree(t *testing.T, root string) string {
	t.Helper()
	h := sha256.New()
	var names []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		names = append(names, rel)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(names)
	for _, rel := range names {
		h.Write([]byte(rel + "\x00"))
		if b, err := os.ReadFile(filepath.Join(root, rel)); err == nil {
			h.Write(b)
		}
	}
	return hex.EncodeToString(h.Sum(nil))
}

func writeDesktopRegistry(t *testing.T, root string, owners map[string][]string) {
	t.Helper()
	workspaces := map[string]any{}
	for ws, ids := range owners {
		workspaces["project-"+filepath.Base(ws)] = map[string]any{"id": "project-" + filepath.Base(ws), "root": ws, "sessionIds": ids}
	}
	b, _ := json.Marshal(map[string]any{"version": 3, "workspaces": workspaces})
	dir := filepath.Join(root, "desktop")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "workspace-state-v1.json"), b, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestExplicitImportKeepsDesktopV5SessionsInTheirOwnWorkspaces(t *testing.T) {
	home := isolateMigrationHome(t)
	root := filepath.Join(home, "Roaming", "reasonix")
	wsA, wsB, gone := filepath.Join(home, "wsA"), filepath.Join(home, "wsB"), filepath.Join(home, "deleted-ws")
	for _, d := range []string{wsA, wsB} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	ids := []string{
		"00000000000000000000000000000001", "00000000000000000000000000000002",
		"00000000000000000000000000000003", "00000000000000000000000000000004",
		"00000000000000000000000000000005",
	}
	for _, id := range ids {
		seedV4Conversation(t, filepath.Join(root, "desktop-sessions-v5", "by-id"), id)
	}
	writeDesktopRegistry(t, root, map[string][]string{wsA: ids[0:2], wsB: ids[2:4], gone: ids[4:5]})
	before := hashTree(t, root)

	picked := wsA
	res := RunLegacySessionImportInto(root, config.ProjectSessionDir(picked), event.Discard)
	if got := totalImported(res.SessionImports); got != 5 || len(res.SessionErrs) != 0 {
		t.Fatalf("imported %d, errs %v; want 5", got, res.SessionErrs)
	}
	if after := hashTree(t, root); after != before {
		t.Fatal("the picked 1.x folder was modified by the import")
	}
	want := map[string][]string{
		config.ProjectSessionDir(wsA): {ids[0], ids[1], ids[4]},
		config.ProjectSessionDir(wsB): {ids[2], ids[3]},
	}
	for dir, owned := range want {
		for _, id := range owned {
			if _, err := os.Stat(filepath.Join(dir, v4Prefix+id+".jsonl")); err != nil {
				t.Errorf("session %s missing from %s", id, dir)
			}
		}
		if entries, _ := filepath.Glob(filepath.Join(dir, v4Prefix+"????????????????????????????????.jsonl")); len(entries) != len(owned) {
			t.Errorf("%s holds %d sessions, want %d", dir, len(entries), len(owned))
		}
	}
}

const v4Prefix = "v4-"
