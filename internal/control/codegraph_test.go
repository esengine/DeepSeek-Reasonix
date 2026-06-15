package control

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"reasonix/internal/codegraph"
	"reasonix/internal/plugin"
)

func TestConnectConfiguredCodegraphSetsShortDaemonIdleTimeout(t *testing.T) {
	isolateControlConfigHome(t)
	dir := t.TempDir()
	t.Chdir(dir)
	launcher := writeControlCodegraphHelper(t, dir)
	envOut := filepath.Join(dir, "codegraph-idle-env")
	t.Setenv("REASONIX_CODEGRAPH_HELPER_ENV_OUT", envOut)
	writeControlFile(t, dir, "reasonix.toml", `
[codegraph]
enabled = true
path = "`+escapeTOMLPath(launcher)+`"
`)

	ctrl := New(Options{Host: plugin.NewHost()})
	defer ctrl.Close()
	if _, err := ctrl.ConnectConfiguredMCPServer("codegraph"); err != nil {
		t.Fatalf("ConnectConfiguredMCPServer(codegraph): %v", err)
	}

	got, err := os.ReadFile(envOut)
	if err != nil {
		t.Fatalf("read codegraph idle timeout env: %v", err)
	}
	if string(got) != codegraph.ReasonixDaemonIdleTimeoutMS {
		t.Fatalf("%s = %q; want %q", codegraph.DaemonIdleTimeoutEnv, got, codegraph.ReasonixDaemonIdleTimeoutMS)
	}
	if !ctrl.DisconnectMCPServer("codegraph") {
		t.Fatal("DisconnectMCPServer(codegraph) returned false")
	}
	time.Sleep(100 * time.Millisecond)
}

func isolateControlConfigHome(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	xdg := filepath.Join(home, ".config")
	appData := filepath.Join(home, "AppData")
	if err := os.MkdirAll(xdg, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(appData, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", xdg)
	t.Setenv("AppData", appData)
}

func writeControlCodegraphHelper(t *testing.T, dir string) string {
	t.Helper()
	path := filepath.Join(dir, "codegraph-helper")
	if runtime.GOOS == "windows" {
		path += ".exe"
	}
	src := filepath.Join(dir, "codegraph-helper.go")
	if err := os.WriteFile(src, []byte(`package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
)

func main() {
	if len(os.Args) >= 3 && os.Args[1] == "init" {
		_ = os.MkdirAll(filepath.Join(os.Args[2], ".codegraph"), 0o755)
		return
	}
	if len(os.Args) >= 2 && os.Args[1] == "serve" {
		if out := os.Getenv("REASONIX_CODEGRAPH_HELPER_ENV_OUT"); out != "" {
			_ = os.WriteFile(out, []byte(os.Getenv("CODEGRAPH_DAEMON_IDLE_TIMEOUT_MS")), 0o644)
		}
	}

	in := bufio.NewReader(os.Stdin)
	for {
		line, err := in.ReadBytes('\n')
		if err != nil {
			return
		}
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}

		var req struct {
			ID     *int            `+"`json:\"id\"`"+`
			Method string          `+"`json:\"method\"`"+`
			Params json.RawMessage `+"`json:\"params\"`"+`
		}
		if err := json.Unmarshal(line, &req); err != nil || req.ID == nil {
			continue
		}

		var result any
		switch req.Method {
		case "initialize":
			result = map[string]any{
				"protocolVersion": "2024-11-05",
				"serverInfo":      map[string]any{"name": "codegraph", "version": "0"},
				"capabilities":    map[string]any{},
			}
		case "tools/list":
			result = map[string]any{"tools": []map[string]any{{
				"name":        "search",
				"description": "Search symbols.",
				"inputSchema": map[string]any{"type": "object"},
			}}}
		}

		resp := map[string]any{"jsonrpc": "2.0", "id": *req.ID, "result": result}
		b, _ := json.Marshal(resp)
		_, _ = os.Stdout.Write(append(b, '\n'))
	}
}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(context.Background(), "go", "build", "-o", path, src)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build codegraph helper: %v\n%s", err, out)
	}
	return path
}

func writeControlFile(t *testing.T, dir, name, body string) {
	t.Helper()
	path := filepath.Join(dir, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func escapeTOMLPath(path string) string {
	escaped := ""
	for _, r := range path {
		if r == '\\' || r == '"' {
			escaped += "\\"
		}
		escaped += string(r)
	}
	return escaped
}
