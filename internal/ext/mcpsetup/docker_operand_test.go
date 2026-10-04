package mcpsetup

import (
	"reflect"
	"strings"
	"testing"
)

func TestDockerServerNameUsesImageOperand(t *testing.T) {
	for _, tc := range []struct {
		args []string
		name string
	}{
		{[]string{"run", "--rm", "-i", "mcp/time"}, "time"},
		{[]string{"run", "--rm", "-it", "mcp/fetch:latest"}, "fetch"},
		{[]string{"container", "run", "--rm", "-i", "mcp/time"}, "time"},
		{[]string{"run", "--rm=false", "--interactive=true", "mcp/time:1.2.3"}, "time"},
		{[]string{"run", "-e", "MODE=test", "--env-file", "config.env", "-v", "/data:/data", "mcp/filesystem", "/data"}, "filesystem"},
		{[]string{"run", "--env=MODE=test", "--volume=/data:/data", "mcp/filesystem", "/data"}, "filesystem"},
		{[]string{"run", "-eMODE=test", "-v/data:/data", "mcp/filesystem", "/data"}, "filesystem"},
		{[]string{"run", "--name", "worker", "--network", "none", "--platform", "linux/amd64", "registry.example:5000/tools/time:stable"}, "time"},
		{[]string{"run", "--mount", "type=bind,src=/data,dst=/data", "--entrypoint", "python", "--workdir", "/app", "--user", "1000", "mcp/time", "-m", "server"}, "time"},
		{[]string{"run", "--", "mcp/time@sha256:" + strings.Repeat("0123456789abcdef", 4), "--env", "server-value"}, "time"},
		{[]string{"run", "--pull", "never", "--read-only", "--init", "mcp/time"}, "time"},
		{[]string{"run", "mcp/time", "mcp/fetch"}, "time"},
		{[]string{"run", "--rm", "-i"}, "mcp-server"},
		{[]string{"run", "--env"}, "mcp-server"},
		{[]string{"run", "--unknown", "option-value", "mcp/time"}, "mcp-server"},
		{[]string{"version"}, "mcp-server"},
	} {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			commands := []string{"docker", "/usr/local/bin/docker", "DOCKER.EXE"}
			for _, command := range commands {
				if got := NameFromArgv(command, tc.args); got != tc.name {
					t.Errorf("%s name = %q, want %q", command, got, tc.name)
				}
				entry, err := ParseArgs(append([]string{"--", command}, tc.args...))
				if err != nil || entry.Name != tc.name || entry.Command != command || !reflect.DeepEqual(entry.Args, tc.args) {
					t.Errorf("CLI entry = %+v, err=%v", entry, err)
				}
				explicit, err := ParseArgs(append([]string{"manual", "--", command}, tc.args...))
				if err != nil || explicit.Name != "manual" || !reflect.DeepEqual(explicit.Args, tc.args) {
					t.Errorf("explicit entry = %+v, err=%v", explicit, err)
				}
				draft, err := Parse(command + " " + strings.Join(tc.args, " "))
				if err != nil || len(draft.Entries) != 1 || draft.Entries[0].Name != tc.name || !reflect.DeepEqual(draft.Entries[0].Args, tc.args) {
					t.Errorf("pasted draft = %+v, err=%v", draft, err)
				}
			}
		})
	}
	draft, err := Parse(`{"mcpServers":{"manual":{"command":"docker","args":["run","--rm","-i","mcp/time"]}}}`)
	if err != nil || len(draft.Entries) != 1 || draft.Entries[0].Name != "manual" {
		t.Errorf("explicit JSON name = %+v, err=%v", draft, err)
	}
	if got := NameFromArgv("npx", []string{"-y", "@modelcontextprotocol/server-time@latest"}); got != "server-time" {
		t.Errorf("npx control = %q", got)
	}
	if got := NameFromArgv("node", []string{"--require", "./preload.cjs", "time.js"}); got != "time" {
		t.Errorf("Node control = %q", got)
	}
}
