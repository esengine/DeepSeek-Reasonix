package doctor

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/provider"
	"reasonix/internal/store"
)

func TestWriteSessionBundleListsSessionLogHeads(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	dir := filepath.Join(home, "projects", "workspace", "sessions")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "log-session.jsonl")
	sess := agent.NewSession("system")
	sess.Add(provider.Message{Role: provider.RoleUser, Content: "shared question"})
	sess.Add(provider.Message{Role: provider.RoleAssistant, Content: "shared answer"})
	if err := sess.Save(path); err != nil {
		t.Fatal(err)
	}
	fork, err := sess.ForkHead(path, sess.Snapshot()[2].ID, agent.HeadKindFork, "alt")
	if err != nil {
		t.Fatal(err)
	}
	sess.Add(provider.Message{Role: provider.RoleUser, Content: "alt question"})
	if err := sess.Save(path); err != nil {
		t.Fatal(err)
	}

	out := filepath.Join(t.TempDir(), "diag.zip")
	if _, err := WriteSessionBundle(SessionBundleOptions{Version: "test-version", SessionRef: path, OutputPath: out, Now: time.Unix(1700000000, 0)}); err != nil {
		t.Fatalf("WriteSessionBundle: %v", err)
	}
	files := zipFiles(t, out)
	var manifest SessionBundleManifest
	if err := json.Unmarshal(files["manifest.json"], &manifest); err != nil {
		t.Fatalf("manifest JSON: %v", err)
	}
	if strings.Contains(string(files["manifest.json"]), home) {
		t.Fatalf("manifest leaked REASONIX_HOME path:\n%s", files["manifest.json"])
	}
	if len(manifest.Sessions) != 1 {
		t.Fatalf("manifest sessions = %+v, want the log alone: heads are not chain members", manifest.Sessions)
	}
	entry := manifest.Sessions[0]
	if entry.LogFormat != 2 || entry.SelectedHead != fork || len(entry.Heads) != 2 {
		t.Fatalf("manifest entry = %+v", entry)
	}
	main, alt := entry.Heads[0], entry.Heads[1]
	if main.ID != agent.SessionMainHead || main.Kind != agent.HeadKindMain || !main.Covered || main.Selected || main.MessageCount != 3 {
		t.Fatalf("main head = %+v", main)
	}
	if alt.ID != fork || alt.Kind != agent.HeadKindFork || alt.Name != "alt" || !alt.Selected || alt.Covered || alt.ParentHead != agent.SessionMainHead || alt.MessageCount != 4 {
		t.Fatalf("fork head = %+v", alt)
	}
	if len(manifest.Missing) != 0 {
		t.Fatalf("missing = %v, want none", manifest.Missing)
	}
	wantLog := filepath.ToSlash(filepath.Join("sessions", safeBundleName(agent.BranchID(path)), filepath.Base(store.SessionEventLog(path))))
	if _, ok := files[wantLog]; !ok {
		t.Fatalf("bundle lacks the event log %q; have %v", wantLog, keysOf(files))
	}
}

func keysOf(files map[string][]byte) []string {
	out := make([]string, 0, len(files))
	for name := range files {
		out = append(out, name)
	}
	return out
}
