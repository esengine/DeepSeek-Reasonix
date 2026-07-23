package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHookMachineListRedactsCommandsAndPreservesTrustState(t *testing.T) {
	root := t.TempDir()
	home := t.TempDir()
	projectSettings := filepath.Join(root, ".reasonix", "settings.json")
	if err := os.MkdirAll(filepath.Dir(projectSettings), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(projectSettings, []byte(`{"hooks":{"PreToolUse":[{"match":"bash","command":"printf PRIVATE_COMMAND"}]}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	globalSettings := filepath.Join(home, ".reasonix", "settings.json")
	if err := os.MkdirAll(filepath.Dir(globalSettings), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(globalSettings, []byte(`{"hooks":{"Stop":[{"command":"PRIVATE_GLOBAL_COMMAND"}]}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	if code := runHookCommand([]string{"list", "--json", "--project-root", root, "--home-dir", home}, &out); code != 0 {
		t.Fatalf("hook list exit code = %d, output = %s", code, out.String())
	}
	var response machineHookList
	if err := json.Unmarshal(out.Bytes(), &response); err != nil {
		t.Fatalf("decode hook list: %v", err)
	}
	if len(response.Hooks) != 2 {
		t.Fatalf("hooks = %+v", response.Hooks)
	}
	if response.Hooks[0].Event != "PreToolUse" || response.Hooks[0].Status != "untrusted" {
		t.Errorf("project hook = %+v", response.Hooks[0])
	}
	if response.Hooks[1].Event != "Stop" || response.Hooks[1].Status != "active" {
		t.Errorf("global hook = %+v", response.Hooks[1])
	}
	if strings.Contains(out.String(), "PRIVATE") || strings.Contains(out.String(), root) || strings.Contains(out.String(), home) {
		t.Fatalf("hook output leaked private data: %s", out.String())
	}
}

func TestHookMachineStatusHasStableRedactedSources(t *testing.T) {
	root := t.TempDir()
	home := t.TempDir()
	var out bytes.Buffer
	if code := runHookCommand([]string{"status", "--json", "--project-root", root, "--home-dir", home}, &out); code != 0 {
		t.Fatalf("hook status exit code = %d, output = %s", code, out.String())
	}
	var response machineHookStatus
	if err := json.Unmarshal(out.Bytes(), &response); err != nil {
		t.Fatalf("decode hook status: %v", err)
	}
	if response.SchemaVersion != machineSchemaVersion || response.Command != "hook.status" || response.TrustedProject {
		t.Fatalf("status = %+v", response)
	}
	if len(response.Sources) != 2 {
		t.Fatalf("sources = %+v", response.Sources)
	}
	if response.Sources[0].Scope != "global" || response.Sources[1].Scope != "project" {
		t.Fatalf("sources are not stable: %+v", response.Sources)
	}
}
