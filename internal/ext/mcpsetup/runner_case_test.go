package mcpsetup

import (
	"reflect"
	"strings"
	"testing"
)

func TestRunnerExtensionCasePreservesDerivedServerName(t *testing.T) {
	for _, tc := range []struct {
		runner string
		args   []string
	}{
		{"npx", []string{"-y", "@example/demo-server@1.0.0"}},
		{"bunx", []string{"-y", "@example/demo-server@1.0.0"}},
		{"uvx", []string{"demo-server"}},
		{"python", []string{"-m", "demo-server"}},
		{"python3", []string{"-m", "demo-server"}},
		{"py", []string{"-m", "demo-server"}},
		{"node", []string{"demo-server.js"}},
		{"uv", []string{"run", "demo-server"}},
	} {
		for _, suffix := range []string{".exe", ".cmd", ".bat", ".EXE", ".CMD", ".BAT", ".Exe", ".Cmd", ".Bat"} {
			command := tc.runner + suffix
			t.Run(command, func(t *testing.T) {
				if got := NameFromArgv(command, tc.args); got != "demo-server" {
					t.Errorf("NameFromArgv = %q, want demo-server", got)
				}
				draft, err := Parse(command + " " + strings.Join(tc.args, " "))
				if err != nil || len(draft.Entries) != 1 {
					t.Fatalf("Parse = %+v, %v", draft, err)
				}
				e := draft.Entries[0]
				if e.Name != "demo-server" || e.Command != command || !reflect.DeepEqual(e.Args, tc.args) {
					t.Errorf("pasted entry = %+v, want unchanged argv and name demo-server", e)
				}
				args := append([]string{"--", command}, tc.args...)
				cli, err := ParseArgs(args)
				if err != nil || !reflect.DeepEqual(cli, e) {
					t.Errorf("CLI entry = %+v, %v; want %+v", cli, err, e)
				}
			})
		}
	}
	for _, command := range []string{"npx.CMD", "uvx.EXE", "python.Bat"} {
		if got := NameFromArgv(command, nil); got != "mcp-server" {
			t.Errorf("runner without operand %q derived %q, want mcp-server", command, got)
		}
	}
	if got := NameFromArgv("custom.CMD", []string{"demo-server"}); got != "custom-cmd" {
		t.Errorf("custom command name = %q, want custom-cmd", got)
	}
	if e, err := ParseArgs([]string{"chosen", "npx.CMD", "-y", "@example/demo-server"}); err != nil || e.Name != "chosen" {
		t.Errorf("explicit server name = %+v, %v", e, err)
	}
}
