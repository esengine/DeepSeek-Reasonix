package pluginpkg

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// Legacy-format compatibility gates for the Plugin Manifest v1 work. The v1
// manifest adds apiVersion/contributes/runtime, but manifests WITHOUT an
// apiVersion must keep parsing exactly as before (including silently
// ignoring unknown fields), and plugin-packages.json must round-trip without
// dropping or renaming a single field. These tests pin the pre-v1 behavior
// so the v1 parser branch cannot regress it.

// TestLegacyNativeManifestParsesUnchanged pins the pre-v1 native manifest
// contract: every legacy field parses, and unknown fields stay ignored.
func TestLegacyNativeManifestParsesUnchanged(t *testing.T) {
	root := t.TempDir()
	manifest := `{
  "name": "legacy-demo",
  "version": "0.3.1",
  "description": "Legacy manifest without apiVersion",
  "homepage": "https://example.invalid/demo",
  "repository": "https://example.invalid/repo",
  "skills": [{"path": "skills"}, {"path": "more-skills"}],
  "commands": "commands",
  "hooks": {
    "PreToolUse": [
      {"match": "Bash", "command": "./hooks/pre.sh", "args": ["--strict"], "timeout": 5000, "description": "pre hook"}
    ],
    "SessionStart": [
      {"command": "echo start", "shellCommand": true, "async": true}
    ]
  },
  "mcpServers": {
    "demo-server": {
      "type": "stdio",
      "command": "./server",
      "args": ["--serve"],
      "env": {"MODE": "prod"},
      "autoStart": false,
      "description": "demo"
    }
  },
  "futureUnknownField": {"nested": [1, 2, 3]}
}`
	if err := os.WriteFile(filepath.Join(root, NativeManifest), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	pkg, _, err := ParseDir(root)
	if err != nil {
		t.Fatalf("ParseDir legacy manifest: %v", err)
	}
	if pkg.ManifestKind != "reasonix" {
		t.Fatalf("ManifestKind = %q, want reasonix", pkg.ManifestKind)
	}
	m := pkg.Manifest
	if m.Name != "legacy-demo" || m.Version != "0.3.1" || m.Description == "" || m.Homepage == "" || m.Repository == "" {
		t.Fatalf("identity fields drifted: %+v", m)
	}
	// cleanPathList dedupes and sorts; the sorted order is part of the
	// legacy contract (stable discovery output).
	if !reflect.DeepEqual(m.Skills, []string{"more-skills", "skills"}) {
		t.Fatalf("Skills = %#v", m.Skills)
	}
	// The legacy native parser has no top-level "agents" key (that arrives
	// with Manifest v1 contributes.agents); today it is silently ignored.
	if len(m.Agents) != 0 {
		t.Fatalf("Agents = %#v, want empty for a legacy native manifest", m.Agents)
	}
	if !reflect.DeepEqual(m.Commands, []string{"commands"}) {
		t.Fatalf("Commands = %#v", m.Commands)
	}
	pre := m.Hooks["PreToolUse"]
	if len(pre) != 1 || pre[0].Command != "./hooks/pre.sh" || !pre[0].ArgsSet || !reflect.DeepEqual(pre[0].Args, []string{"--strict"}) || pre[0].Timeout != 5000 {
		t.Fatalf("PreToolUse hook drifted: %+v", pre)
	}
	start := m.Hooks["SessionStart"]
	if len(start) != 1 || start[0].Command != "echo start" || !start[0].ShellCommand || !start[0].Async {
		t.Fatalf("SessionStart hook drifted: %+v", start)
	}
	srv := m.MCPServers["demo-server"]
	// Note: an explicit "autoStart": false normalizes to nil on parse — pin
	// the status quo so the v1 branch cannot change legacy semantics.
	if srv.Command != "./server" || srv.AutoStart != nil || srv.Env["MODE"] != "prod" || srv.Type != "stdio" || srv.Description != "demo" {
		t.Fatalf("mcpServers drifted: %+v", srv)
	}
}

// TestLegacyHookArgsPresenceRoundTrip pins the exec/shell presence-bit
// contract: args:[] (exec form, empty argv) must survive a marshal
// round-trip distinct from args being absent (shell form).
func TestLegacyHookArgsPresenceRoundTrip(t *testing.T) {
	for _, tc := range []struct {
		name    string
		json    string
		argsSet bool
	}{
		{"exec form empty args", `{"command":"./a.sh","args":[]}`, true},
		{"exec form with args", `{"command":"./a.sh","args":["x"]}`, true},
		{"shell form no args key", `{"command":"./a.sh"}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var h Hook
			if err := json.Unmarshal([]byte(tc.json), &h); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if h.ArgsSet != tc.argsSet {
				t.Fatalf("ArgsSet = %v, want %v", h.ArgsSet, tc.argsSet)
			}
			out, err := json.Marshal(h)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			var h2 Hook
			if err := json.Unmarshal(out, &h2); err != nil {
				t.Fatalf("re-unmarshal: %v", err)
			}
			if h2.ArgsSet != h.ArgsSet {
				t.Fatalf("ArgsSet did not round-trip: %v -> %v (%s)", h.ArgsSet, h2.ArgsSet, out)
			}
		})
	}
}

// TestLegacyPluginStateRoundTripPreservesFields writes a pre-v1
// plugin-packages.json fixture, loads and re-saves it, and requires both
// the typed values and the raw JSON key set to survive untouched.
func TestLegacyPluginStateRoundTripPreservesFields(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)

	fixture := `{
  "version": 1,
  "plugins": [
    {
      "name": "alpha",
      "source": "https://github.com/example/alpha",
      "root": "plugins/alpha",
      "version": "1.2.0",
      "description": "alpha plugin",
      "manifestKind": "reasonix",
      "enabled": true,
      "commit": "0123456789abcdef0123456789abcdef01234567"
    },
    {
      "name": "beta",
      "source": "/opt/plugins/beta",
      "root": "plugins/beta",
      "version": "0.1.0",
      "description": "",
      "manifestKind": "claude",
      "enabled": false,
      "commit": ""
    }
  ]
}`
	if err := os.WriteFile(StatePath(home), []byte(fixture), 0o644); err != nil {
		t.Fatal(err)
	}

	st, err := LoadState(home)
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	if st.Version != 1 || len(st.Plugins) != 2 {
		t.Fatalf("state drifted: %+v", st)
	}
	if !st.Plugins[0].Enabled || st.Plugins[1].Enabled {
		t.Fatalf("enabled flags drifted: %+v", st.Plugins)
	}
	if st.Plugins[0].Commit == "" || st.Plugins[1].ManifestKind != "claude" {
		t.Fatalf("fields drifted: %+v", st.Plugins)
	}

	if err := SaveState(home, st); err != nil {
		t.Fatalf("SaveState: %v", err)
	}
	reloaded, err := LoadState(home)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if !reflect.DeepEqual(st, reloaded) {
		t.Fatalf("state did not round-trip:\nfirst:  %+v\nsecond: %+v", st, reloaded)
	}

	raw, err := os.ReadFile(StatePath(home))
	if err != nil {
		t.Fatal(err)
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(raw, &top); err != nil {
		t.Fatalf("saved state is not an object: %v", err)
	}
	var plugins []map[string]json.RawMessage
	if err := json.Unmarshal(top["plugins"], &plugins); err != nil {
		t.Fatalf("saved plugins malformed: %v", err)
	}
	// Current schema contract: keys with values always survive a save;
	// empty strings are omitted (omitempty) — this pins the status quo so a
	// v1 change cannot silently start dropping non-empty fields.
	for i, p := range plugins[:1] {
		for _, k := range []string{"name", "source", "root", "version", "description", "manifestKind", "enabled", "commit"} {
			if _, ok := p[k]; !ok {
				t.Fatalf("saved plugin %d lost key %q: %s", i, k, raw)
			}
		}
	}
	for i, p := range plugins[1:] {
		for _, k := range []string{"name", "source", "root", "version", "manifestKind", "enabled"} {
			if _, ok := p[k]; !ok {
				t.Fatalf("saved plugin %d lost key %q: %s", i+1, k, raw)
			}
		}
		if _, ok := p["description"]; ok {
			t.Fatalf("empty description should stay omitted (omitempty contract changed): %s", raw)
		}
		if _, ok := p["commit"]; ok {
			t.Fatalf("empty commit should stay omitted (omitempty contract changed): %s", raw)
		}
	}
}
