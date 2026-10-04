package mcpsetup

import (
	"reflect"
	"strings"
	"testing"
)

func TestUVServerNameSkipsDependencyValues(t *testing.T) {
	for _, tc := range []struct {
		command string
		args    []string
		name    string
	}{
		{"uvx", []string{"--with", "extra-package", "demo-server"}, "demo-server"},
		{"uvx", []string{"--with=extra-package", "demo-server"}, "demo-server"},
		{"uvx", []string{"-w", "extra-package", "demo-server"}, "demo-server"},
		{"uvx", []string{"-wextra-package", "demo-server"}, "demo-server"},
		{"uvx", []string{"--with-requirements", "./extra requirements.txt", "demo-server"}, "demo-server"},
		{"uvx", []string{"--with-requirements=./extra requirements.txt", "demo-server"}, "demo-server"},
		{"uvx", []string{"--from=main-distribution", "--with", "extra-package", "demo-server"}, "demo-server"},
		{"uvx", []string{"--from", "main-distribution", "--with", "extra-package", "demo-server"}, "demo-server"},
		{"uvx", []string{"--with", "first-package", "-w", "second-package", "--python", "3.12", "demo-server"}, "demo-server"},
		{"uv", []string{"run", "--with", "demo-distribution", "demo-server"}, "demo-server"},
		{"uv", []string{"run", "--with=demo-distribution", "demo-server"}, "demo-server"},
		{"uv", []string{"run", "-w", "demo-distribution", "demo-server"}, "demo-server"},
		{"uv", []string{"run", "--with-requirements", "./requirements.txt", "demo-server"}, "demo-server"},
		{"uv", []string{"run", "--with-requirements=./requirements.txt", "demo-server"}, "demo-server"},
		{"uv", []string{"run", "--with", "demo-distribution", "--with", "extra-package", "demo-server"}, "demo-server"},
		{"uvx", []string{"demo-server", "--with", "client-mode"}, "demo-server"},
		{"uv", []string{"run", "--", "demo-server", "--with", "client-mode"}, "demo-server"},
		{"uvx", []string{"--with", "extra-package"}, "mcp-server"},
		{"uv", []string{"run", "--with-requirements", "./requirements.txt"}, "mcp-server"},
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
			if err != nil || explicit.Name != "manual" || !reflect.DeepEqual(explicit.Args, tc.args) {
				t.Errorf("explicit entry = %+v, err=%v", explicit, err)
			}
		})
	}
	if got := NameFromArgv("npx", []string{"-p", "bootstrap", "demo-server"}); got != "demo-server" {
		t.Errorf("npx control = %q", got)
	}
	if got := NameFromArgv("python3", []string{"-m", "demo_server"}); got != "demo-server" {
		t.Errorf("Python control = %q", got)
	}
}
