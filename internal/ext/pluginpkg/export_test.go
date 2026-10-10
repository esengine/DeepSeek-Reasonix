package pluginpkg

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
)

func writeExportFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func exportedEntries(t *testing.T, archive []byte) map[string]string {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		var buf bytes.Buffer
		if _, err := buf.ReadFrom(rc); err != nil {
			t.Fatal(err)
		}
		rc.Close()
		out[f.Name] = buf.String()
	}
	return out
}

func TestExportStripsCredentialsAndNamesThemPerServer(t *testing.T) {
	root := testenv.TempDir(t)
	writeExportFile(t, filepath.Join(root, NativeManifest), `{
  "apiVersion": "reasonix.io/plugin/v2",
  "name": "demo",
  "mcpServers": {
    "docs": {"command": "docs", "env": {"TOKEN": "sk-live-123"}},
    "wiki": {"url": "https://wiki.example", "headers": {"Authorization": "Bearer abc"}}
  },
  "runtime": {"command": "bin/rt", "env": {"SEAT": "${SEAT_ID}"}}
}`)
	writeExportFile(t, filepath.Join(root, ".git", "config"), "[remote]\n url = https://x:tok@example\n")
	writeExportFile(t, filepath.Join(root, "skills", "greet", "fixture.json"), `{"env":{"KEEP":"me"}}`)

	archive, required, err := Export("demo", root)
	if err != nil {
		t.Fatal(err)
	}
	entries := exportedEntries(t, archive)

	if _, ok := entries["demo/.git/config"]; ok {
		t.Fatal("export packed .git, whose remote URLs carry tokens")
	}
	// Only configuration is rewritten; a skill's own data file is content.
	if got := entries["demo/skills/greet/fixture.json"]; got != `{"env":{"KEEP":"me"}}` {
		t.Fatalf("fixture.json = %q, want it byte-for-byte", got)
	}

	manifest := entries["demo/"+NativeManifest]
	for _, literal := range []string{"sk-live-123", "Bearer abc"} {
		if strings.Contains(manifest, literal) {
			t.Fatalf("exported manifest still carries %q:\n%s", literal, manifest)
		}
	}
	var doc struct {
		MCPServers map[string]struct {
			Env     map[string]string `json:"env"`
			Headers map[string]string `json:"headers"`
		} `json:"mcpServers"`
	}
	if err := json.Unmarshal([]byte(manifest), &doc); err != nil {
		t.Fatal(err)
	}
	// Two servers that both authenticate must not collapse onto one variable.
	if got := doc.MCPServers["docs"].Env["TOKEN"]; got != "${DOCS_TOKEN}" {
		t.Fatalf("docs env TOKEN = %q", got)
	}
	if got := doc.MCPServers["wiki"].Headers["Authorization"]; got != "${WIKI_AUTHORIZATION}" {
		t.Fatalf("wiki Authorization = %q", got)
	}
	want := []string{"DOCS_TOKEN", "SEAT_ID", "WIKI_AUTHORIZATION"}
	if strings.Join(required, ",") != strings.Join(want, ",") {
		t.Fatalf("required = %v, want %v", required, want)
	}
}

// A reference was never a literal, so it survives unchanged — but whoever
// installs the package still has to provide it.
func TestStripCredentialsKeepsExistingReferences(t *testing.T) {
	out, names, err := StripCredentials([]byte(`{"mcpServers":{"a":{"env":{"K":"${EXISTING}"}}}}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "${EXISTING}") {
		t.Fatalf("rewrote an existing reference: %s", out)
	}
	if len(names) != 1 || names[0] != "EXISTING" {
		t.Fatalf("required = %v", names)
	}
}

func TestExportRefusesInvalidName(t *testing.T) {
	if _, _, err := Export("../etc", testenv.TempDir(t)); err == nil {
		t.Fatal("Export accepted a path-shaped name")
	}
}

func TestExportPreservesRegularFilePermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows does not store POSIX execute bits")
	}
	for _, mode := range []os.FileMode{0o755, 0o744, 0o644, 0o600, 0o674} {
		t.Run(mode.String(), func(t *testing.T) {
			root := testenv.TempDir(t)
			file := filepath.Join(root, "content")
			writeExportFile(t, file, "package content")
			if err := os.Chmod(file, mode); err != nil {
				t.Fatal(err)
			}
			archive, _, err := Export("demo", root)
			if err != nil {
				t.Fatal(err)
			}
			zr, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
			if err != nil {
				t.Fatal(err)
			}
			if len(zr.File) != 1 || zr.File[0].Name != "demo/content" {
				t.Fatalf("exported files = %+v", zr.File)
			}
			if got := zr.File[0].Mode(); got != mode {
				t.Fatalf("exported mode = %v, want %v", got, mode)
			}
		})
	}
}
