package mcpsetup

import (
	"reflect"
	"strings"
	"testing"
)

func TestUVXFromFlagFormsDeriveTheSameServerName(t *testing.T) {
	for _, args := range [][]string{
		{"--from", "demo-distribution", "demo-command", "--stdio"},
		{"--from=demo-distribution", "demo-command", "--stdio"},
		{"--from", "demo-distribution==1.2.0", "demo-command", "--stdio"},
		{"--from=demo-distribution==1.2.0", "demo-command", "--stdio"},
		{"--python", "3.12", "--from", "demo-distribution==1.2.0", "demo-command", "--stdio"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			draft, err := Parse("uvx " + strings.Join(args, " "))
			if err != nil || len(draft.Entries) != 1 {
				t.Fatalf("Parse = %+v, %v", draft, err)
			}
			e := draft.Entries[0]
			if e.Name != "demo-command" || e.Command != "uvx" || !reflect.DeepEqual(e.Args, args) {
				t.Errorf("pasted entry = %+v, want command name and original argv", e)
			}
			cli, err := ParseArgs(append([]string{"--", "uvx"}, args...))
			if err != nil || !reflect.DeepEqual(cli, e) {
				t.Errorf("CLI entry = %+v, %v; want %+v", cli, err, e)
			}
		})
	}
	if got := NameFromArgv("uvx", []string{"--from", "demo-distribution"}); got != "mcp-server" {
		t.Errorf("no command operand derived %q, want mcp-server", got)
	}
	if got := NameFromArgv("npx", []string{"--package", "demo-distribution", "demo-command"}); got != "demo-command" {
		t.Errorf("npx package control derived %q", got)
	}
	if e, err := ParseArgs([]string{"chosen", "uvx", "--from", "demo-distribution", "demo-command"}); err != nil || e.Name != "chosen" {
		t.Errorf("explicit name = %+v, %v", e, err)
	}
}
