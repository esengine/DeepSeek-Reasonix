package cli

import (
	"testing"

	"reasonix/internal/config"
	"reasonix/internal/tool"

	_ "reasonix/internal/tool/builtin"
)

func TestACPBuiltinToolsKeepSessionLevelBuiltins(t *testing.T) {
	dir := t.TempDir()
	tools := toolMap(acpBuiltinTools(&config.Config{}, dir, []string{dir}))
	for _, name := range []string{
		"todo_write",
		"complete_step",
		"bash_output",
		"kill_shell",
		"wait",
		"notebook_edit",
	} {
		if tools[name] == nil {
			t.Fatalf("ACP workspace tools missing %q; got %v", name, toolNames(tools))
		}
	}
}

func toolMap(tools []tool.Tool) map[string]tool.Tool {
	out := make(map[string]tool.Tool, len(tools))
	for _, t := range tools {
		out[t.Name()] = t
	}
	return out
}

func toolNames(tools map[string]tool.Tool) []string {
	out := make([]string, 0, len(tools))
	for name := range tools {
		out = append(out, name)
	}
	return out
}
