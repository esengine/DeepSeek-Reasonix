package mcpsetup

import (
	"reflect"
	"testing"
)

func TestLauncherNamesAcrossGlobalOptionsAndUnknownFlags(t *testing.T) {
	for _, tc := range []struct {
		line string
		name string
	}{
		{"docker run -p 8080:80 img", "img"},
		{"docker run localhost:5000/img:tag", "img"},
		{"docker run --cap-add SYS_ADMIN img", "mcp-server"},
		{"docker run img@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", "img"},
		{"node --experimental-vm-modules server.js", "server"},
		{"node --max-old-space-size=4096 server.js", "server"},
		{"npx --unknown option-value pkg", "option-value"},
		{"npx -Z pkg", "pkg"},
		{"bunx --unknown=option-value pkg", "pkg"},
		{"python3 --unknown option-value server.py", "option-value"},
		{"python3 --unknown -m server_module", "server-module"},
		{"node --unknown option-value server.js", "option-value"},
		{"uv --unknown option-value run pkg", "mcp-server"},
		{"uvx --unknown option-value pkg", "option-value"},
		{"npx -c 'pkg --stdio' client-mode", "mcp-server"},
		{"python3 -Imreasonix_probe", "reasonix-probe"},
		{"docker run -p8080:80 --publish=9000:90 registry.example:5000/tools/server:stable", "server"},
		{"docker --context work run --rm -it mcp/time", "time"},
		{"docker -H unix:///tmp/fixture.sock container run --rm mcp/time", "time"},
		{"podman run --rm -i -p 8080:80 mcp/time", "time"},
		{"nerdctl --namespace work run --rm -i mcp/fetch:latest", "fetch"},
		{"pnpm --dir x dlx pkg", "pkg"},
		{"pnpm -C x --reporter append-only dlx --package dep pkg@latest", "pkg"},
		{"npm --prefix x --cache cache exec --package dep -- pkg@latest", "pkg"},
		{`"C:\Program Files\nodejs\pnpm.CMD" --dir x dlx pkg`, "pkg"},
		{`C:\nodejs\npm.CMD exec -- pkg`, "pkg"},
		{`C:\Go\bin\go.EXE run .\cmd\server --stdio`, "server"},
		{`C:\Docker\docker.EXE run --rm -i mcp/time`, "time"},
		{"go run .", "mcp-server"},
		{"go run -unknown option-value ./cmd/server", "mcp-server"},
		{"pnpm --unknown option-value dlx pkg", "mcp-server"},
		{"npm exec --unknown option-value pkg", "mcp-server"},
		{"docker run --unknown=option-value img", "mcp-server"},
		{"podman run --unknown option-value img", "mcp-server"},
		{"nerdctl run --unknown option-value img", "mcp-server"},
		{"docker run -p", "mcp-server"},
		{"pnpm --dir x dlx", "mcp-server"},
		{"npm exec --call 'pkg --stdio'", "mcp-server"},
		{"go run - ./cmd/server", "mcp-server"},
		{"docker run - mcp/time", "mcp-server"},
		{"docker run -- img --unknown server-value", "img"},
	} {
		t.Run(tc.line, func(t *testing.T) {
			argv := Tokenize(tc.line)
			entry, err := ParseArgs(append([]string{"--"}, argv...))
			if err != nil || entry.Name != tc.name || entry.Command != argv[0] || !reflect.DeepEqual(entry.Args, argv[1:]) {
				t.Errorf("CLI entry = %+v, err=%v, want %q with original argv", entry, err, tc.name)
			}
			draft, err := Parse(tc.line)
			if err != nil || len(draft.Entries) != 1 || !reflect.DeepEqual(draft.Entries[0], entry) {
				t.Errorf("draft = %+v, err=%v", draft, err)
			}
		})
	}
}
