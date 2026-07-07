package doctor

import (
	"archive/zip"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/store"
)

func TestWriteSessionBundleIncludesRecoveryChain(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)

	dir := filepath.Join(home, "projects", "workspace", "sessions")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	parent := filepath.Join(dir, "parent-session.jsonl")
	recovery := filepath.Join(dir, "parent-session-recovery-deadbeef.jsonl")
	writeFile(t, parent, `{"role":"user","content":"parent"}`+"\n")
	writeFile(t, recovery, `{"role":"user","content":"recovery"}`+"\n")
	if err := agent.SaveBranchMeta(parent, agent.BranchMeta{ID: "parent-session"}); err != nil {
		t.Fatal(err)
	}
	if err := agent.SaveBranchMeta(recovery, agent.BranchMeta{
		ID:             "parent-session-recovery-deadbeef",
		ParentID:       "parent-session",
		Recovered:      true,
		RecoveryReason: "snapshot conflict",
		RecoveryDepth:  1,
	}); err != nil {
		t.Fatal(err)
	}
	writeFile(t, store.SessionEventLog(recovery), `{"type":"replace"}`+"\n")
	writeFile(t, store.SessionEventIndex(recovery), `{"log_size":19}`+"\n")
	writeFile(t, store.SessionConflictLog(recovery), `{"outcome":"forked_recovery_branch"}`+"\n")
	writeFile(t, store.SessionLeaseInfo(recovery), `{"pid":1234}`+"\n")

	out := filepath.Join(t.TempDir(), "diag.zip")
	got, err := WriteSessionBundle(SessionBundleOptions{
		Version:    "test-version",
		SessionRef: store.SessionMeta(recovery),
		OutputPath: out,
		Now:        time.Unix(1700000000, 0),
	})
	if err != nil {
		t.Fatalf("WriteSessionBundle: %v", err)
	}
	if got.Path != out {
		t.Fatalf("bundle path = %q, want %q", got.Path, out)
	}
	if got.SessionPath != recovery {
		t.Fatalf("session path = %q, want %q", got.SessionPath, recovery)
	}

	files := zipFiles(t, out)
	for _, want := range []string{
		"doctor.json",
		"manifest.json",
		"sessions/parent-session-recovery-deadbeef/parent-session-recovery-deadbeef.jsonl",
		"sessions/parent-session-recovery-deadbeef/parent-session-recovery-deadbeef.jsonl.meta",
		"sessions/parent-session-recovery-deadbeef/parent-session-recovery-deadbeef.events.jsonl",
		"sessions/parent-session-recovery-deadbeef/parent-session-recovery-deadbeef.event-index.json",
		"sessions/parent-session-recovery-deadbeef/parent-session-recovery-deadbeef.conflicts.jsonl",
		"sessions/parent-session-recovery-deadbeef/parent-session-recovery-deadbeef.jsonl.lease.json",
		"sessions/parent-session/parent-session.jsonl",
		"sessions/parent-session/parent-session.jsonl.meta",
	} {
		if _, ok := files[want]; !ok {
			t.Fatalf("zip missing %s; entries=%v", want, keys(files))
		}
	}

	var manifest SessionBundleManifest
	if err := json.Unmarshal(files["manifest.json"], &manifest); err != nil {
		t.Fatalf("manifest JSON: %v", err)
	}
	if strings.Contains(string(files["manifest.json"]), home) {
		t.Fatalf("manifest leaked REASONIX_HOME path:\n%s", files["manifest.json"])
	}
	if manifest.Version != "test-version" {
		t.Fatalf("manifest version = %q", manifest.Version)
	}
	if !strings.Contains(manifest.RequestedRef, "<REASONIX_HOME>") {
		t.Fatalf("requested ref = %q, want redacted REASONIX_HOME path", manifest.RequestedRef)
	}
	if len(manifest.Sessions) != 2 {
		t.Fatalf("manifest sessions = %+v, want recovery plus parent", manifest.Sessions)
	}
	if manifest.Sessions[0].BranchID != "parent-session-recovery-deadbeef" || manifest.Sessions[0].ParentID != "parent-session" {
		t.Fatalf("recovery manifest entry = %+v", manifest.Sessions[0])
	}
}

func writeFile(t *testing.T, path, data string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
}

func zipFiles(t *testing.T, path string) map[string][]byte {
	t.Helper()
	zr, err := zip.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	out := map[string][]byte{}
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(rc)
		_ = rc.Close()
		if err != nil {
			t.Fatal(err)
		}
		out[f.Name] = data
	}
	return out
}

func keys(m map[string][]byte) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
