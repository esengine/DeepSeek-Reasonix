package cli

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"reasonix/internal/pluginpkg"
)

func TestPluginInstallReturnsFailureExitForFailedJSON(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)

	source := filepath.Join(t.TempDir(), "superpowers")
	writePluginTestFile(t, filepath.Join(source, pluginpkg.CodexManifest), `{
	  "name": "superpowers",
	  "version": "6.1.1",
	  "description": "Planning workflows",
	  "skills": "skills"
	}`)
	writePluginTestFile(t, filepath.Join(source, "skills", "using-superpowers", "SKILL.md"), "---\ndescription: Use Superpowers\n---\nUse Superpowers.")

	firstOut := captureStdout(t, func() {
		if rc := pluginCommand([]string{"install", source, "--yes"}); rc != 0 {
			t.Fatalf("first plugin install rc = %d, want 0", rc)
		}
	})
	var first struct {
		OK bool `json:"ok"`
	}
	if err := json.Unmarshal([]byte(firstOut), &first); err != nil {
		t.Fatalf("first output is not JSON: %v\n%s", err, firstOut)
	}
	if !first.OK {
		t.Fatalf("first output ok = false:\n%s", firstOut)
	}

	secondOut := captureStdout(t, func() {
		if rc := pluginCommand([]string{"install", source, "--yes"}); rc != 1 {
			t.Fatalf("duplicate plugin install rc = %d, want 1", rc)
		}
	})
	var second struct {
		OK     bool   `json:"ok"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal([]byte(secondOut), &second); err != nil {
		t.Fatalf("second output is not JSON: %v\n%s", err, secondOut)
	}
	if second.OK || second.Status != "failed" {
		t.Fatalf("duplicate output ok/status = %v/%q, want false/failed\n%s", second.OK, second.Status, secondOut)
	}
}
