package boot

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"reasonix/internal/provider"
)

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

			req, entries := captureTokenProfileSurface(t, tc.tokenMode)
			wantNames := defaultFullBootToolNames()
			if tc.tokenMode == TokenModeEconomy {
				wantNames = economyBootToolNames()
			}
			if got := toolSchemaNames(req.Tools); !reflect.DeepEqual(got, wantNames) {
				t.Fatalf("%s provider-visible tool surface changed\ngot  %v\nwant %v", tc.name, got, wantNames)
			}
			if len(entries) != len(req.Tools) {
				t.Fatalf("contract entries = %d, provider tools = %d\ncontract=%v\nprovider=%v", len(entries), len(req.Tools), contractEntryNames(entries), toolSchemaNames(req.Tools))
			}
			for i, e := range entries {
				s := req.Tools[i]
				if e.Name != s.Name {
					t.Fatalf("tool[%d] name = %q, want %q\ncontract=%v\nprovider=%v", i, e.Name, s.Name, contractEntryNames(entries), toolSchemaNames(req.Tools))
				}
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
				"bash":                false,
				"read_file":           true,
				"connect_tool_source": tc.tokenMode == TokenModeEconomy,
			} {
				got, ok := readOnly[name]
				if !ok {
					if name == "connect_tool_source" && tc.tokenMode != TokenModeEconomy {
						continue
					}
					t.Fatalf("contract missing %s; tools=%v", name, contractEntryNames(entries))
				}
				if got != want {
					t.Fatalf("%s ReadOnly = %v, want %v", name, got, want)
				}
			}
		})
	}
}
