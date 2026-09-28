//go:build darwin || linux

package boot

import (
	"context"
	"encoding/json"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/safety/sandbox"
	"reasonix/internal/session/control"
)

// egressScriptProvider asks for one bash call, then ends the turn.
type egressScriptProvider struct {
	command string
	mu      sync.Mutex
	reqs    []provider.Request
}

func (p *egressScriptProvider) Name() string { return "boot-egress-script" }

func (p *egressScriptProvider) Stream(_ context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	p.mu.Lock()
	p.reqs = append(p.reqs, req)
	p.mu.Unlock()
	ch := make(chan provider.Chunk, 2)
	if hasToolResult(req) {
		ch <- provider.Chunk{Type: provider.ChunkText, Text: "done"}
	} else {
		args, _ := json.Marshal(map[string]string{"command": p.command})
		ch <- provider.Chunk{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{ID: "b1", Name: "bash", Arguments: string(args)}}
	}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}

// With allowed_domains set, bash reaches the network only through the host's
// egress proxy, and a host the list does not name reaches the model as the
// host's refusal rather than as a bare connection error.
func TestEffectEgressRefusalReachesTheModel(t *testing.T) {
	if !sandbox.Available() || !sandbox.EgressSupported() {
		t.Skip("egress confinement not available on this host")
	}
	if _, err := exec.LookPath("curl"); err != nil {
		t.Skip("curl not installed")
	}
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)
	rec := &egressScriptProvider{command: `curl -sS -o /dev/null https://not-listed.example.invalid/; echo "proxy=$HTTPS_PROXY"`}
	provider.Register("boot-egress", func(provider.Config) (provider.Provider, error) { return rec, nil })
	writeFile(t, dir, "reasonix.toml", `
default_model = "test-model"
tool_approval = "yolo"

[agent]
system_prompt = "BASE"

[sandbox]
bash = "enforce"
allowed_domains = ["github.com"]

[codegraph]
enabled = false

[[providers]]
name = "test-model"
kind = "boot-egress"
model = "x"
`)
	approveWorkspace(t, dir)
	ctrl, err := Build(context.Background(), Options{Sink: event.Discard})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if err := ctrl.Run(context.Background(), "fetch it"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	ctrl.Close()

	rec.mu.Lock()
	last := rec.reqs[len(rec.reqs)-1]
	rec.mu.Unlock()
	results := effectToolResults(last)
	if len(results) != 1 {
		t.Fatalf("tool results = %q", results)
	}
	out := results[0]
	for _, want := range []string{
		"proxy=http://",
		"[host] The sandbox refused network egress to: not-listed.example.invalid (blocked-by-allowlist)",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("bash result missing %q:\n%s", want, out)
		}
	}
}

// Where the frontend can ask, a host outside the list reaches the user as a
// network_egress approval, and a refusal reaches the model as theirs.
func TestEffectUnlistedHostIsPutToTheUser(t *testing.T) {
	if !sandbox.Available() || !sandbox.EgressSupported() {
		t.Skip("egress confinement not available on this host")
	}
	if _, err := exec.LookPath("curl"); err != nil {
		t.Skip("curl not installed")
	}
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)
	rec := &egressScriptProvider{command: `curl -sS -o /dev/null https://ask-me.example.invalid/`}
	provider.Register("boot-egress-ask", func(provider.Config) (provider.Provider, error) { return rec, nil })
	writeFile(t, dir, "reasonix.toml", `
default_model = "test-model"
tool_approval = "yolo"

[agent]
system_prompt = "BASE"

[sandbox]
bash = "enforce"
allowed_domains = ["github.com"]

[codegraph]
enabled = false

[[providers]]
name = "test-model"
kind = "boot-egress-ask"
model = "x"
`)
	approveWorkspace(t, dir)
	var ctrlRef atomic.Pointer[control.Controller]
	asked := make(chan event.Approval, 4)
	sink := event.FuncSink(func(e event.Event) {
		if e.Kind != event.ApprovalRequest {
			return
		}
		asked <- e.Approval
		allow := e.Approval.Tool != control.NetworkEgressApprovalTool
		go ctrlRef.Load().Approve(e.Approval.ID, allow, false, false)
	})
	ctrl, err := Build(context.Background(), Options{Sink: sink})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	ctrlRef.Store(ctrl)
	ctrl.EnableInteractiveApproval()
	if err := ctrl.Run(context.Background(), "fetch it"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	ctrl.Close()

	var egressAsks []event.Approval
	for len(asked) > 0 {
		if a := <-asked; a.Tool == control.NetworkEgressApprovalTool {
			egressAsks = append(egressAsks, a)
		}
	}
	if len(egressAsks) != 1 || egressAsks[0].Subject != "ask-me.example.invalid" {
		t.Fatalf("egress approvals = %+v, want one for ask-me.example.invalid", egressAsks)
	}
	rec.mu.Lock()
	last := rec.reqs[len(rec.reqs)-1]
	rec.mu.Unlock()
	results := effectToolResults(last)
	if len(results) != 1 || !strings.Contains(results[0], "ask-me.example.invalid (declined-by-user)") {
		t.Fatalf("bash result = %q, want the user's refusal", results)
	}
}
