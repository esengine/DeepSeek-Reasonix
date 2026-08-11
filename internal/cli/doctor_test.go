package cli

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"reasonix/internal/config"
)

func TestDoctorCommandPrintsJSON(t *testing.T) {
	out := captureStdout(t, func() {
		if rc := doctorCommand([]string{"--json"}, "test-version"); rc != 0 {
			t.Fatalf("doctorCommand rc = %d, want 0", rc)
		}
	})

	var decoded map[string]any
	if err := json.Unmarshal([]byte(out), &decoded); err != nil {
		t.Fatalf("doctor --json output is not JSON: %v\n%s", err, out)
	}
	if decoded["version"] != "test-version" {
		t.Fatalf("version = %v, want test-version", decoded["version"])
	}
}

func TestRunDispatchesDoctor(t *testing.T) {
	out := captureStdout(t, func() {
		if rc := Run([]string{"doctor"}, "dispatch-version"); rc != 0 {
			t.Fatalf("Run doctor rc = %d, want 0", rc)
		}
	})
	if !strings.Contains(out, "reasonix dispatch-version doctor") {
		t.Fatalf("doctor output missing header:\n%s", out)
	}
}

func TestDoctorSessionsIsReadOnly(t *testing.T) {
	cache := t.TempDir()
	t.Setenv("REASONIX_CACHE_HOME", cache)
	out := captureStdout(t, func() {
		if rc := doctorCommand([]string{"sessions", "--json"}, "test-version"); rc != 0 {
			t.Fatalf("doctor sessions rc = %d, want 0", rc)
		}
	})
	var decoded map[string]any
	if err := json.Unmarshal([]byte(out), &decoded); err != nil {
		t.Fatalf("doctor sessions output: %v\n%s", err, out)
	}
	if _, err := os.Stat(filepath.Join(cache, "session-catalog", "v1.sqlite")); !os.IsNotExist(err) {
		t.Fatalf("doctor sessions created or changed catalog: %v", err)
	}
}

func TestSessionsReindexRebuildsOnlyCatalog(t *testing.T) {
	cache := t.TempDir()
	home := t.TempDir()
	sessions := filepath.Join(home, "sessions")
	t.Setenv("REASONIX_CACHE_HOME", cache)
	t.Setenv("REASONIX_HOME", home)
	if err := os.MkdirAll(sessions, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(sessions, "keep.jsonl")
	body := []byte(`{"role":"user","content":"keep me"}` + "\n")
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}
	out := captureStdout(t, func() {
		if rc := sessionsCommand([]string{"reindex", "--dir", sessions, "--json"}); rc != 0 {
			t.Fatalf("sessions reindex rc = %d, want 0", rc)
		}
	})
	if !json.Valid([]byte(out)) {
		t.Fatalf("sessions reindex output is not JSON: %s", out)
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != string(body) {
		t.Fatalf("authoritative transcript changed: err=%v got=%q", err, got)
	}
}

func TestDefaultSessionCatalogTargetsIncludeDesktopProjects(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	projectRoot := filepath.Join(t.TempDir(), "project")
	projects := `{"projects":[{"root":` + strconv.Quote(projectRoot) + `}]}`
	if err := os.WriteFile(filepath.Join(home, "desktop-projects.json"), []byte(projects), 0o600); err != nil {
		t.Fatal(err)
	}
	targets := defaultSessionCatalogTargets()
	want := map[string]string{
		filepath.Clean(filepath.Join(home, "sessions")):                                   "global",
		filepath.Clean(config.ProjectSessionDir(filepath.Join(home, "global-workspace"))): "global",
		filepath.Clean(config.ProjectSessionDir(projectRoot)):                             "project",
	}
	if len(targets) != len(want) {
		t.Fatalf("targets = %#v, want %d entries", targets, len(want))
	}
	for _, target := range targets {
		if scope, ok := want[target.Path]; !ok || target.Scope != scope {
			t.Fatalf("unexpected target %#v (want %#v)", target, want)
		}
		if target.Scope == "project" && target.WorkspaceRoot != projectRoot {
			t.Fatalf("project target lost workspace root: %#v", target)
		}
	}
}

func TestDoctorSessionCommandWritesBundle(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	sessionDir := filepath.Join(home, "sessions")
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sessionDir, "abc.jsonl"), []byte(`{"role":"user","content":"hi"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	outPath := filepath.Join(t.TempDir(), "abc-diag.zip")
	out := captureStdout(t, func() {
		if rc := doctorCommand([]string{"session", "abc", "--zip", "--out", outPath}, "test-version"); rc != 0 {
			t.Fatalf("doctor session rc = %d, want 0", rc)
		}
	})
	if strings.TrimSpace(out) != outPath {
		t.Fatalf("doctor session output = %q, want %q", strings.TrimSpace(out), outPath)
	}
	if info, err := os.Stat(outPath); err != nil || info.Size() == 0 {
		t.Fatalf("bundle stat = %v, %v", info, err)
	}
}

func TestDoctorQualityCommandPrintsPublicSafeJSON(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	sessionDir := filepath.Join(home, "sessions")
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(sessionDir, "abc.jsonl")
	const secret = "quality-command-secret"
	if err := os.WriteFile(path, []byte(`{"role":"user","content":"`+secret+`"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := captureStdout(t, func() {
		if rc := doctorCommand([]string{"quality", "abc", "--json"}, "test-version"); rc != 0 {
			t.Fatalf("doctor quality rc = %d, want 0", rc)
		}
	})
	var decoded map[string]any
	if err := json.Unmarshal([]byte(out), &decoded); err != nil {
		t.Fatalf("doctor quality output is not JSON: %v\n%s", err, out)
	}
	if decoded["version"] != "test-version" {
		t.Fatalf("version = %v", decoded["version"])
	}
	if strings.Contains(out, secret) || strings.Contains(out, home) || strings.Contains(out, "abc.jsonl") {
		t.Fatalf("doctor quality leaked session data:\n%s", out)
	}
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	defer func() { os.Stdout = old }()

	fn()
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestDoctorSessionCommandOutEqualsForm(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	sessionDir := filepath.Join(home, "sessions")
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sessionDir, "abc.jsonl"), []byte(`{"role":"user","content":"hi"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	outPath := filepath.Join(t.TempDir(), "abc-diag.zip")
	out := captureStdout(t, func() {
		if rc := doctorCommand([]string{"session", "abc", "--out=" + outPath}, "test-version"); rc != 0 {
			t.Fatalf("doctor session rc = %d, want 0", rc)
		}
	})
	if strings.TrimSpace(out) != outPath {
		t.Fatalf("doctor session output = %q, want %q", strings.TrimSpace(out), outPath)
	}
	if rc := doctorCommand([]string{"session", "abc", "--out="}, "test-version"); rc != 2 {
		t.Fatalf("empty --out= rc = %d, want usage error 2", rc)
	}
}

func TestDoctorRedactSessionsCommand(t *testing.T) {
	dir := t.TempDir()
	const secret = "sensitive-value-that-must-not-leak"
	path := filepath.Join(dir, "abc.jsonl")
	if err := os.WriteFile(path, []byte(`{"role":"tool","content":"DEEPSEEK_API_KEY=`+secret+`"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out := captureStdout(t, func() {
		if rc := doctorCommand([]string{"redact-sessions", "--dir", dir}, "test-version"); rc != 0 {
			t.Fatalf("doctor redact-sessions rc = %d, want 0", rc)
		}
	})
	if !strings.Contains(out, "session secret cleanup redacted 1/1 files") {
		t.Fatalf("unexpected output:\n%s", out)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), secret) {
		t.Fatalf("doctor redact-sessions leaked secret:\n%s", data)
	}
}
