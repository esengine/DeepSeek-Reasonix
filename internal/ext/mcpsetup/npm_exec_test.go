package mcpsetup

import (
	"reflect"
	"testing"
)

func TestNPMExecNameUsesExecutableOperand(t *testing.T) {
	for _, tc := range []struct {
		line string
		name string
	}{
		{"npm exec -- @example/docs-mcp@1.2.0 --port 3456", "docs-mcp"},
		{"npm exec --yes @example/search-mcp@latest", "search-mcp"},
		{"npm --yes exec -- docs-mcp", "docs-mcp"},
		{"npm x -- docs-mcp", "docs-mcp"},
		{"npm exec --package @example/client -- docs-mcp", "docs-mcp"},
		{"npm --package @example/client exec -- docs-mcp", "docs-mcp"},
		{"npm exec --package=@example/client -- docs-mcp", "docs-mcp"},
		{"npm exec --package first --package second docs-mcp", "docs-mcp"},
		{"npm exec --workspace packages/client -- docs-mcp", "docs-mcp"},
		{"npm exec -w packages/client -- docs-mcp", "docs-mcp"},
		{"npm exec --workspace=packages/client -- docs-mcp", "docs-mcp"},
		{"npm exec -p -- docs-mcp", "docs-mcp"},
		{"npm exec -- docs-mcp --package server-value", "docs-mcp"},
		{"npm install docs-mcp", "mcp-server"},
		{"npm run docs-mcp", "mcp-server"},
		{"npm exec --package @example/client", "mcp-server"},
		{"npm exec --call 'docs-mcp --stdio'", "mcp-server"},
		{"npm exec -c 'docs-mcp --stdio'", "mcp-server"},
		{"npm exec --call=docs-mcp", "mcp-server"},
	} {
		t.Run(tc.line, func(t *testing.T) {
			tokens := Tokenize(tc.line)
			entry, err := ParseArgs(append([]string{"--"}, tokens...))
			if err != nil {
				t.Fatal(err)
			}
			if entry.Name != tc.name {
				t.Errorf("name = %q, want %q", entry.Name, tc.name)
			}
			if entry.Command != tokens[0] || !reflect.DeepEqual(entry.Args, tokens[1:]) {
				t.Errorf("argv changed: %+v", entry)
			}
			draft, err := Parse(tc.line)
			if err != nil || len(draft.Entries) != 1 || !reflect.DeepEqual(draft.Entries[0], entry) {
				t.Errorf("pasted preview = %+v, err=%v", draft, err)
			}
		})
	}
	for _, command := range []string{"/usr/bin/npm", "npm.CMD", "npm.EXE", "npm.BAT"} {
		if got := NameFromArgv(command, []string{"exec", "--", "docs-mcp"}); got != "docs-mcp" {
			t.Errorf("%s derived %q", command, got)
		}
	}
	entry, err := ParseArgs([]string{"custom", "--", "npm", "exec", "--", "docs-mcp"})
	if err != nil || entry.Name != "custom" {
		t.Errorf("explicit name = %+v, err=%v", entry, err)
	}
}
