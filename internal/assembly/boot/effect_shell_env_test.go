package boot

import (
	"context"
	"encoding/json"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"

	"reasonix/internal/contract/config"
	"reasonix/internal/contract/provider"
	"reasonix/internal/session/control"
)

// shellEnvProvider asks for one bash command that prints a preset variable.
type shellEnvProvider struct {
	mu    sync.Mutex
	turn  int
	reqs  []provider.Request
	probe string
}

func (p *shellEnvProvider) Name() string { return "boot-shell-env" }

func (p *shellEnvProvider) Stream(_ context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	p.mu.Lock()
	p.reqs = append(p.reqs, req)
	p.turn++
	turn := p.turn
	p.mu.Unlock()
	ch := make(chan provider.Chunk, 2)
	if turn == 1 {
		args, _ := json.Marshal(map[string]string{"command": p.probe})
		ch <- provider.Chunk{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{ID: "call-env", Name: "bash", Arguments: string(args)}}
	} else {
		ch <- provider.Chunk{Type: provider.ChunkText, Text: "done"}
	}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}

var (
	shellEnvRegister sync.Once
	shellEnvMu       sync.Mutex
	shellEnvCurrent  *shellEnvProvider
)

// The user's [tools.shell] env must reach the bash tool through the real Build
// assembly: boot is where the config becomes the sandbox spec, and a wiring gap
// there is invisible to a config-level or tool-level test.
func TestEffectShellEnvReachesTheBashTool(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the probe uses a POSIX shell")
	}
	home := robustTempDir(t)
	dir := robustTempDir(t)
	writeFile(t, home, "config.toml", `
[sandbox]
bash = "off"

[tools.shell.env]
REASONIX_SHELL_ENV_PROBE = "from-config"
`)
	rec := &shellEnvProvider{probe: `printf '%s' "$REASONIX_SHELL_ENV_PROBE"`}
	shellEnvRegister.Do(func() {
		provider.Register("boot-shell-env", func(provider.Config) (provider.Provider, error) {
			shellEnvMu.Lock()
			defer shellEnvMu.Unlock()
			return shellEnvCurrent, nil
		})
	})
	shellEnvMu.Lock()
	shellEnvCurrent = rec
	shellEnvMu.Unlock()
	writeFile(t, dir, "reasonix.toml", `
default_model = "test-model"

[codegraph]
enabled = false

[[providers]]
name = "test-model"
kind = "boot-shell-env"
model = "x"
`)
	_, _ = config.RootsForHome(home).ApproveWorkspacePrograms(dir)
	ctrl, err := Build(context.Background(), Options{Home: home, WorkspaceRoot: dir})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer ctrl.Close()
	ctrl.SetToolApprovalMode(control.ToolApprovalYolo)
	_ = ctrl.Run(context.Background(), "print the preset variable")

	rec.mu.Lock()
	reqs := append([]provider.Request(nil), rec.reqs...)
	rec.mu.Unlock()
	if len(reqs) == 0 {
		t.Fatal("no request reached the provider")
	}
	got := effectToolResults(reqs[len(reqs)-1])
	if len(got) == 0 {
		t.Fatal("no tool result reached the provider")
	}
	if !strings.Contains(got[len(got)-1], "from-config") {
		t.Fatalf("the configured [tools.shell] env did not reach the bash tool: %q", got[len(got)-1])
	}
}

// The egress proxy ports a confined command must keep shut come from both the
// host environment and the preset one: a proxy named only in [tools.shell] env
// would otherwise stay reachable, and overriding a variable must not reopen the
// host's proxy.
func TestClosedLoopbackPortsUnionHostAndPreset(t *testing.T) {
	t.Setenv("HTTP_PROXY", "http://127.0.0.1:3128")
	got := closedLoopbackPorts(map[string]string{"HTTP_PROXY": "http://127.0.0.1:8080"})
	for _, want := range []int{3128, 8080} {
		if !slices.Contains(got, want) {
			t.Fatalf("closed loopback ports = %v, want %d among them", got, want)
		}
	}
}
