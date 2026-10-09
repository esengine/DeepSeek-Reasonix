package mcpsetup

import (
	"reflect"
	"testing"
)

func TestUVXVersionPinDoesNotChangeDerivedServerName(t *testing.T) {
	for _, spec := range []string{"demo-server", "demo-server@1.2.0", "demo-server==1.2.0", "demo-server==2.0.0", "demo-server==1.2.0.post1", "demo-server==1.2.0+local"} {
		t.Run(spec, func(t *testing.T) {
			args := []string{"--python", "3.12", spec, "--stdio"}
			draft, err := Parse("uvx --python 3.12 " + spec + " --stdio")
			if err != nil || len(draft.Entries) != 1 {
				t.Fatalf("Parse = %+v, %v", draft, err)
			}
			e := draft.Entries[0]
			if e.Name != "demo-server" || e.Command != "uvx" || !reflect.DeepEqual(e.Args, args) {
				t.Errorf("pasted version pin = %+v, want package name and unchanged argv", e)
			}
			cli, err := ParseArgs(append([]string{"--", "uvx"}, args...))
			if err != nil || !reflect.DeepEqual(cli, e) {
				t.Errorf("CLI entry = %+v, %v; want %+v", cli, err, e)
			}
			if got := NameFromArgv("uvx", args); got != "demo-server" {
				t.Errorf("NameFromArgv = %q, want demo-server", got)
			}
		})
	}
	if got := NameFromArgv("custom", []string{"demo-server==1.2.0"}); got != "custom" {
		t.Errorf("custom command name = %q", got)
	}
	if e, err := ParseArgs([]string{"chosen", "uvx", "demo-server==1.2.0"}); err != nil || e.Name != "chosen" {
		t.Errorf("explicit server name = %+v, %v", e, err)
	}
	if got := NameFromArgv("uvx", nil); got != "mcp-server" {
		t.Errorf("uvx without an operand = %q", got)
	}
}
