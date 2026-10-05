package mcpsetup

import (
	"reflect"
	"strings"
	"testing"
)

func TestUVServerNameSkipsGlobalOptions(t *testing.T) {
	for _, tc := range []struct {
		command string
		args    []string
		name    string
	}{
		{"uv", []string{"--directory", "./project root", "run", "demo-server"}, "demo-server"},
		{"uv", []string{"--directory=./project root", "run", "demo-server"}, "demo-server"},
		{"uv", []string{"run", "--directory", "./project root", "demo-server"}, "demo-server"},
		{"uv", []string{"--project", "./project", "run", "--with", "extra", "demo-server"}, "demo-server"},
		{"uv", []string{"run", "--project", "./project", "demo-server"}, "demo-server"},
		{"uv", []string{"--config-file", "./uv.toml", "run", "demo-server"}, "demo-server"},
		{"uv", []string{"--cache-dir", "./cache", "run", "demo-server"}, "demo-server"},
		{"uv", []string{"--color", "never", "--offline", "run", "demo-server"}, "demo-server"},
		{"uv", []string{"--allow-insecure-host", "localhost", "run", "demo-server"}, "demo-server"},
		{"uv", []string{"--directory", "run", "run", "demo-server"}, "demo-server"},
		{"uv", []string{"run", "--config-file=./uv.toml", "--cache-dir", "./cache", "--color", "never", "demo-server"}, "demo-server"},
		{"uvx", []string{"--directory", "./project root", "demo-server"}, "demo-server"},
		{"uvx", []string{"--project=./project", "--config-file", "./uv.toml", "--from", "distribution", "demo-server"}, "demo-server"},
		{"uvx", []string{"--cache-dir", "./cache", "--color", "never", "demo-server"}, "demo-server"},
		{"uv", []string{"--offline", "--no-config", "run", "--", "demo-server", "--directory", "client-mode"}, "demo-server"},
		{"uvx", []string{"demo-server", "--project", "client-mode"}, "demo-server"},
		{"uv", []string{"--directory", "./project", "run"}, "mcp-server"},
		{"uvx", []string{"--directory", "./project"}, "mcp-server"},
		{"uv", []string{"--directory", "run", "sync"}, "mcp-server"},
		{"uv", []string{"tool", "run", "demo-server"}, "mcp-server"},
		{"npx", []string{"--color", "demo-server"}, "demo-server"},
	} {
		t.Run(tc.command+" "+strings.Join(tc.args, " "), func(t *testing.T) {
			if got := NameFromArgv(tc.command, tc.args); got != tc.name {
				t.Errorf("name = %q, want %q", got, tc.name)
			}
			entry, err := ParseArgs(append([]string{"--", tc.command}, tc.args...))
			if err != nil || entry.Name != tc.name || entry.Command != tc.command || !reflect.DeepEqual(entry.Args, tc.args) {
				t.Errorf("CLI entry = %+v, err=%v", entry, err)
			}
			explicit, err := ParseArgs(append([]string{"manual", "--", tc.command}, tc.args...))
			if err != nil || explicit.Name != "manual" || explicit.Command != tc.command || !reflect.DeepEqual(explicit.Args, tc.args) {
				t.Errorf("explicit entry = %+v, err=%v", explicit, err)
			}
		})
	}
	for _, input := range []string{`uv --directory "./project root" run demo-server`, `uv run --project "./project root" demo-server`, `uvx --config-file ./uv.toml demo-server`} {
		draft, err := Parse(input)
		if err != nil || len(draft.Entries) != 1 || draft.Entries[0].Name != "demo-server" {
			t.Errorf("draft for %q = %+v, err=%v", input, draft, err)
		}
	}
}
