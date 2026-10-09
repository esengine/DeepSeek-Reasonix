package mcpsetup

import (
	"path/filepath"
	"reflect"
	"testing"

	"reasonix/internal/contract/config"
)

func TestParseCopiedMCPAddUsesCLISyntax(t *testing.T) {
	for _, tc := range []struct {
		input string
		want  config.PluginEntry
	}{
		{`reasonix mcp add -- npx -y chrome-devtools-mcp@latest`, config.PluginEntry{Name: "chrome-devtools-mcp", Command: "npx", Args: []string{"-y", "chrome-devtools-mcp@latest"}}},
		{`$ reasonix mcp add docs -- npx -y docs-mcp`, config.PluginEntry{Name: "docs", Command: "npx", Args: []string{"-y", "docs-mcp"}}},
		{`> reasonix mcp add docs npx -y docs-mcp`, config.PluginEntry{Name: "docs", Command: "npx", Args: []string{"-y", "docs-mcp"}}},
		{`% reasonix mcp add docs --env "DOCS_TOKEN=${DOCS_TOKEN}" npx -y docs-mcp`, config.PluginEntry{Name: "docs", Command: "npx", Args: []string{"-y", "docs-mcp"}, Env: map[string]string{"DOCS_TOKEN": "${DOCS_TOKEN}"}}},
		{`reasonix mcp add https://mcp.example.test/endpoint`, config.PluginEntry{Name: "mcp", Type: "http", URL: "https://mcp.example.test/endpoint"}},
		{`reasonix mcp add docs --http https://mcp.example.test/endpoint --header "Authorization=Bearer fixture-token"`, config.PluginEntry{Name: "docs", Type: "http", URL: "https://mcp.example.test/endpoint", Headers: map[string]string{"Authorization": "Bearer fixture-token"}}},
		{`reasonix mcp add docs --sse=https://mcp.example.test/sse`, config.PluginEntry{Name: "docs", Type: "sse", URL: "https://mcp.example.test/sse"}},
		{`npx -y docs-mcp`, config.PluginEntry{Name: "docs-mcp", Command: "npx", Args: []string{"-y", "docs-mcp"}}},
		{`reasonix --stdio`, config.PluginEntry{Name: "reasonix", Command: "reasonix", Args: []string{"--stdio"}}},
	} {
		t.Run(tc.input, func(t *testing.T) {
			draft, err := Parse(tc.input)
			if err != nil {
				t.Fatal(err)
			}
			if len(draft.Entries) != 1 || !reflect.DeepEqual(draft.Entries[0], tc.want) {
				t.Fatalf("entries=%+v, want %+v", draft.Entries, tc.want)
			}
			if tc.want.Command != "" && !hasKind(draft.Risks, "shell") {
				t.Error("stdio command missing from risk disclosure")
			}
			if tc.want.URL != "" && (!hasKind(draft.Risks, "unknown-host") || hasKind(draft.Risks, "shell")) {
				t.Errorf("remote server risks=%+v", draft.Risks)
			}
			if tc.want.Headers != nil && !hasKind(draft.Risks, "secret") {
				t.Error("literal header missing from risk disclosure")
			}
		})
	}
}

func TestParseCopiedMCPAddRejectsIncompleteArguments(t *testing.T) {
	for _, input := range []string{
		"reasonix mcp add", "reasonix mcp add --", "reasonix mcp add docs",
		"reasonix mcp add docs --http", "reasonix mcp add docs --unknown value",
	} {
		t.Run(input, func(t *testing.T) {
			if _, err := Parse(input); err == nil {
				t.Fatal("incomplete MCP add accepted as a server command")
			}
		})
	}
}

func TestParseCopiedMCPAddRecognizesLaunchers(t *testing.T) {
	for _, launcher := range []string{"reasonix", "reasonix.exe", "REASONIX.EXE", filepath.Join(".", "bin", "reasonix"), filepath.Join("My Apps", "reasonix.exe")} {
		t.Run(launcher, func(t *testing.T) {
			prefix := `"` + launcher + `" mcp add `
			want := config.PluginEntry{Name: "docs", Command: "node", Args: []string{"docs_server.js", "--mode", "fixture"}, Env: map[string]string{"MODE": "fixture"}}
			draft, err := Parse(prefix + `docs --env MODE=fixture node docs_server.js --mode fixture`)
			if err != nil || len(draft.Entries) != 1 || !reflect.DeepEqual(draft.Entries[0], want) {
				t.Fatalf("stdio entries=%+v, err=%v, want %+v", draft.Entries, err, want)
			}
			want = config.PluginEntry{Name: "docs", Type: "http", URL: "https://mcp.example.test/endpoint", Headers: map[string]string{"Authorization": "Bearer fixture-token"}}
			draft, err = Parse(prefix + `docs --http https://mcp.example.test/endpoint --header "Authorization=Bearer fixture-token"`)
			if err != nil || len(draft.Entries) != 1 || !reflect.DeepEqual(draft.Entries[0], want) {
				t.Fatalf("remote entries=%+v, err=%v, want %+v", draft.Entries, err, want)
			}
			if hasKind(draft.Risks, "shell") || !hasKind(draft.Risks, "unknown-host") || !hasKind(draft.Risks, "secret") {
				t.Errorf("remote risks=%+v", draft.Risks)
			}
			for _, arguments := range []string{"", "--", "docs --http"} {
				if _, err := Parse(prefix + arguments); err == nil {
					t.Errorf("incomplete %q accepted", prefix+arguments)
				}
			}
			draft, err = Parse(`"` + launcher + `" --stdio`)
			if err != nil || len(draft.Entries) != 1 || draft.Entries[0].Command != launcher || !reflect.DeepEqual(draft.Entries[0].Args, []string{"--stdio"}) {
				t.Fatalf("bare executable entries=%+v, err=%v", draft.Entries, err)
			}
		})
	}
	draft, err := Parse(`reasonix-helper.exe mcp add -- node docs_server.js`)
	if err != nil || len(draft.Entries) != 1 || draft.Entries[0].Command != "reasonix-helper.exe" || !reflect.DeepEqual(draft.Entries[0].Args, []string{"mcp", "add", "--", "node", "docs_server.js"}) {
		t.Fatalf("other executable entries=%+v, err=%v", draft.Entries, err)
	}
}
