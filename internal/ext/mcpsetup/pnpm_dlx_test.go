package mcpsetup

import (
	"reflect"
	"strings"
	"testing"
)

func TestPNPMDlxServerNameUsesCommandOperand(t *testing.T) {
	for _, tc := range []struct {
		args []string
		name string
	}{
		{[]string{"dlx", "@example/docs-mcp@1.2.0", "--port", "3456"}, "docs-mcp"},
		{[]string{"dlx", "search-mcp@latest"}, "search-mcp"},
		{[]string{"--package", "@example/client", "dlx", "docs-mcp"}, "docs-mcp"},
		{[]string{"--package=@example/client", "dlx", "docs-mcp"}, "docs-mcp"},
		{[]string{"dlx", "--package", "@example/client", "docs-mcp"}, "docs-mcp"},
		{[]string{"--package", "dlx", "--package", "@example/client", "dlx", "docs-mcp"}, "docs-mcp"},
		{[]string{"--reporter", "append-only", "dlx", "--allow-build", "@example/client", "docs-mcp"}, "docs-mcp"},
		{[]string{"--silent", "dlx", "--shell-mode", "docs-mcp"}, "docs-mcp"},
		{[]string{"dlx", "-c", "docs-mcp"}, "docs-mcp"},
		{[]string{"dlx", "--", "docs-mcp", "--package", "literal-argument"}, "docs-mcp"},
		{[]string{"exec", "docs-mcp"}, "mcp-server"},
		{[]string{"dlx"}, "mcp-server"},
		{[]string{"--package"}, "mcp-server"},
	} {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			if got := NameFromArgv("pnpm", tc.args); got != tc.name {
				t.Errorf("name = %q, want %q", got, tc.name)
			}
			entry, err := ParseArgs(append([]string{"--", "pnpm"}, tc.args...))
			if err != nil || entry.Name != tc.name || entry.Command != "pnpm" || !reflect.DeepEqual(entry.Args, tc.args) {
				t.Errorf("CLI entry = %+v, err=%v", entry, err)
			}
			draft, err := Parse("pnpm " + strings.Join(tc.args, " "))
			if err != nil || len(draft.Entries) != 1 || !reflect.DeepEqual(draft.Entries[0], entry) {
				t.Errorf("pasted draft = %+v, err=%v; want %+v", draft, err, entry)
			}
			explicit, err := ParseArgs(append([]string{"manual", "--", "pnpm"}, tc.args...))
			if err != nil || explicit.Name != "manual" || !reflect.DeepEqual(explicit.Args, tc.args) {
				t.Errorf("explicit entry = %+v, err=%v", explicit, err)
			}
		})
	}
	for _, command := range []string{"/usr/local/bin/pnpm", "pnpm.CMD", "pnpm.EXE"} {
		if got := NameFromArgv(command, []string{"dlx", "@example/docs-mcp@1.2.0"}); got != "docs-mcp" {
			t.Errorf("runner %q name = %q", command, got)
		}
	}
	if got := NameFromArgv("custom", []string{"dlx", "docs-mcp"}); got != "custom" {
		t.Errorf("custom command control = %q", got)
	}
}
