package boot

import (
	"encoding/json"
	"strings"
	"testing"

	"reasonix/internal/contract/provider"
)

// unifiedBootToolNames is the provider-visible surface shared by every Agent
// role setting under identical configuration (core tools + host-control tools).
// stableBootToolNames is the segment every turn carries, in the order it is
// sent. It may not move with the turn's context: it is the cache prefix each
// request shares with the last.
func stableBootToolNames() []string {
	return []string{
		"ask",
		"await_user",
		"bash",
		"browser_act",
		"browser_open",
		"browser_read",
		"compress",
		"context_budget",
		"edit_file",
		"read_file",
		"recall",
		"todo_write",
		"use_capability",
		"write_file",
	}
}

// contextualBootToolNames may come and go with what the turn carries, and only
// ever after the stable segment, so an absence is a byte prefix of a presence.
func contextualBootToolNames() []string {
	return []string{"bash_output", "complete_step", "kill_shell", "update_goal", "wait"}
}

// assertSegmentedSurface holds the shape both cache and contract depend on: the
// stable segment identical and first, the rest drawn from the contextual set in
// its own order, and nothing else.
func assertSegmentedSurface(t *testing.T, label string, got []string) {
	t.Helper()
	stable := stableBootToolNames()
	if len(got) < len(stable) {
		t.Fatalf("%s surface is shorter than the stable segment\ngot  %v\nstable %v", label, got, stable)
	}
	for i, want := range stable {
		if got[i] != want {
			t.Fatalf("%s stable segment moved at %d\ngot  %v\nwant %v first", label, i, got, stable)
		}
	}
	allowed := map[string]int{}
	for i, name := range contextualBootToolNames() {
		allowed[name] = i
	}
	last := -1
	for _, name := range got[len(stable):] {
		at, ok := allowed[name]
		if !ok {
			t.Fatalf("%s carries %q after the stable segment, which is neither stable nor contextual\ngot %v", label, name, got)
		}
		if at <= last {
			t.Fatalf("%s contextual segment is out of order at %q\ngot %v", label, name, got)
		}
		last = at
	}
}

func TestBootToolContractMatchesProviderVisibleSurface(t *testing.T) {
	for _, tc := range []struct {
		name      string
		tokenMode string
	}{
		{name: "default", tokenMode: ""},
		{name: "economy", tokenMode: TokenModeEconomy},
	} {
		t.Run(tc.name, func(t *testing.T) {
			isolateConfigHome(t)
			dir := robustTempDir(t)
			t.Chdir(dir)
			writeFile(t, dir, "reasonix.toml", `
default_model = "test-model"

[agent]
system_prompt = "BASE"

[[providers]]
name = "test-model"
kind = "boot-token-profile-test"
model = "x"
`)
			approveWorkspace(t, dir)

			req, entries := captureTokenProfileSurface(t, tc.tokenMode)
			assertSegmentedSurface(t, tc.name, toolSchemaNames(req.Tools))
			byName := make(map[string]int, len(entries))
			for i, e := range entries {
				byName[e.Name] = i
			}
			for _, s := range req.Tools {
				at, ok := byName[s.Name]
				if !ok {
					t.Fatalf("provider carries %q with no contract entry\ncontract=%v\nprovider=%v", s.Name, contractEntryNames(entries), toolSchemaNames(req.Tools))
				}
				e := entries[at]
				if e.Description != strings.TrimSpace(s.Description) {
					t.Fatalf("%s description drift\ncontract=%q\nprovider=%q", e.Name, e.Description, s.Description)
				}
				if !json.Valid(e.Schema) {
					t.Fatalf("%s contract schema is invalid JSON: %s", e.Name, e.Schema)
				}
				if got := string(provider.CanonicalizeSchema(e.Schema)); got != string(e.Schema) {
					t.Fatalf("%s contract schema is not canonical", e.Name)
				}
				if string(e.Schema) != string(s.Parameters) {
					t.Fatalf("%s schema drift\ncontract=%s\nprovider=%s", e.Name, e.Schema, s.Parameters)
				}
			}
			readOnly := map[string]bool{}
			for _, e := range entries {
				readOnly[e.Name] = e.ReadOnly
			}
			for name, want := range map[string]bool{
				"bash":           false,
				"read_file":      true,
				"use_capability": true,
			} {
				got, ok := readOnly[name]
				if !ok {
					t.Fatalf("contract missing %s; tools=%v", name, contractEntryNames(entries))
				}
				if got != want {
					t.Fatalf("%s ReadOnly = %v, want %v", name, got, want)
				}
			}
			if _, ok := readOnly["connect_tool_source"]; ok {
				t.Fatalf("connect_tool_source must not appear on the provider-visible surface")
			}
		})
	}
}
