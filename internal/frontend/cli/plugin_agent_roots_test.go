package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/ext/pluginpkg"
)

func TestPluginDoctorRejectsUnavailableDeclaredAgentRoots(t *testing.T) {
	for _, failure := range []string{"missing", "file"} {
		t.Run(failure, func(t *testing.T) {
			home, source, workspace := testenv.TempDir(t), testenv.TempDir(t), testenv.TempDir(t)
			t.Setenv("REASONIX_HOME", home)
			t.Setenv("REASONIX_STATE_HOME", home)
			t.Setenv("REASONIX_CACHE_HOME", filepath.Join(home, "cache"))
			t.Chdir(workspace)
			writePluginTestFile(t, filepath.Join(source, pluginpkg.NativeManifest), `{"apiVersion":"reasonix.io/plugin/v2","name":"agent-roots","contributes":{"agents":["agents","profiles"]}}`)
			for _, dir := range []string{"agents", "profiles"} {
				writePluginTestFile(t, filepath.Join(source, dir, "review.md"), "---\ndescription: Review fixture\n---\nReview the files.")
			}
			install := captureStdout(t, func() {
				if rc := pluginCommand([]string{"install", source, "--yes"}); rc != 0 {
					t.Fatalf("install rc=%d", rc)
				}
			})
			var result struct {
				OK     bool   `json:"ok"`
				Status string `json:"status"`
			}
			if err := json.Unmarshal([]byte(install), &result); err != nil || !result.OK || result.Status != "done" {
				t.Fatalf("install=%s err=%v", install, err)
			}
			root := pluginpkg.InstallRoot(home, "agent-roots")
			path := filepath.Join(root, "profiles")
			doctor := func(want int) (string, string) {
				t.Helper()
				var out string
				errOut := captureStderr(t, func() {
					out = captureStdout(t, func() {
						if rc := pluginCommand([]string{"doctor", "agent-roots"}); rc != want {
							t.Errorf("doctor rc=%d, want %d", rc, want)
						}
					})
				})
				return out, errOut
			}
			if out, errOut := doctor(0); !strings.Contains(out, "ok: agent-roots") || errOut != "" {
				t.Fatalf("healthy doctor stdout=%s stderr=%s", out, errOut)
			}
			if err := os.RemoveAll(path); err != nil {
				t.Fatal(err)
			}
			if failure == "file" {
				writePluginTestFile(t, path, "This is no longer a profile directory.")
			}
			for _, op := range []string{"enable", "disable"} {
				captureStdout(t, func() {
					if rc := pluginCommand([]string{op, "agent-roots"}); rc != 0 {
						t.Fatalf("%s rc=%d", op, rc)
					}
				})
				out, errOut := doctor(1)
				if strings.Contains(out, "ok: agent-roots") || !strings.Contains(errOut, "missing agent root: "+path) {
					t.Errorf("%s: doctor stdout=%s stderr=%s", op, out, errOut)
				}
			}
			if failure == "file" {
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
			}
			writePluginTestFile(t, filepath.Join(path, "review.md"), "---\ndescription: Restored fixture\n---\nReview the files.")
			if out, errOut := doctor(0); !strings.Contains(out, "ok: agent-roots") || errOut != "" {
				t.Errorf("restored doctor stdout=%s stderr=%s", out, errOut)
			}
			if _, err := os.Stat(filepath.Join(source, "profiles", "review.md")); err != nil {
				t.Fatalf("installed mutation changed the source: %v", err)
			}
		})
	}
}
