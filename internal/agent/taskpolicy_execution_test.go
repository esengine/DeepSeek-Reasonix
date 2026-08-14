package agent

import (
	"context"
	"encoding/json"
	"strings"
	"sync/atomic"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/evidence"
	"reasonix/internal/provider"
	"reasonix/internal/taskpolicy"
	"reasonix/internal/tool"
)

func TestTaskPolicyUsesStructuredCommandEffects(t *testing.T) {
	var calls int32
	reg := tool.NewRegistry()
	reg.Add(fakeTool{name: "bash", readOnly: false, calls: &calls})
	a := New(&scriptedProvider{name: "p"}, reg, NewSession("sys"), Options{}, event.Discard)
	a.turn.policy = taskpolicy.TaskPolicy{Constraints: taskpolicy.Constraints{ForbidMutation: true}}
	a.turn.policySet = true

	listing := a.executeOne(context.Background(), &a.turn, provider.ToolCall{Name: "bash", Arguments: `{"command":"git branch -a"}`})
	if listing.blocked || listing.errMsg != "" {
		t.Fatalf("branch listing outcome = %+v, want execution", listing)
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("branch listing Execute calls = %d, want 1", got)
	}

	tests := []struct {
		name       string
		command    string
		wantDomain string
		secret     string
	}{
		{name: "tag creation", command: "git tag v1.2.3", wantDomain: "repository metadata", secret: "v1.2.3"},
		{name: "host clock", command: "date --set tomorrow", wantDomain: "host state", secret: "tomorrow"},
		{name: "audit fix", command: "npm audit fix", wantDomain: "workspace content", secret: "fix"},
		{name: "config edit", command: "git config --edit", wantDomain: "repository metadata", secret: "--edit"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args, err := json.Marshal(map[string]string{"command": tt.command})
			if err != nil {
				t.Fatal(err)
			}
			got := a.executeOne(context.Background(), &a.turn, provider.ToolCall{Name: "bash", Arguments: string(args)})
			if !got.blocked || !strings.Contains(got.errMsg, tt.wantDomain) {
				t.Fatalf("command %q outcome = %+v, want %q mutation block", tt.command, got, tt.wantDomain)
			}
			if strings.Contains(got.errMsg, tt.secret) && tt.secret != "--edit" {
				t.Fatalf("policy error leaked command operand %q: %q", tt.secret, got.errMsg)
			}
		})
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("blocked writers reached Execute: calls=%d, want 1", got)
	}
}

func TestTaskPolicyEnforcesVerificationAllowlist(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(fakeTool{name: "bash", readOnly: true})
	a := New(&scriptedProvider{name: "p"}, reg, NewSession("sys"), Options{}, event.Discard)
	a.turn.policy = taskpolicy.Derive(taskpolicy.Input{Raw: "fix it; only run go test ./internal/parser"})
	a.turn.policySet = true

	blocked := a.executeOne(context.Background(), &a.turn, provider.ToolCall{Name: "bash", Arguments: `{"command":"npm test"}`})
	if !blocked.blocked || !strings.Contains(blocked.errMsg, "allowlist") {
		t.Fatalf("npm test outcome = %+v, want allowlist block", blocked)
	}
	allowed := a.executeOne(context.Background(), &a.turn, provider.ToolCall{Name: "bash", Arguments: `{"command":"go test ./internal/parser"}`})
	if allowed.blocked || allowed.errMsg != "" {
		t.Fatalf("allowed go test outcome = %+v", allowed)
	}
}

func TestTaskPolicyBlocksDisallowedExploreSubagent(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(fakeTool{name: "explore", readOnly: true})
	a := New(&scriptedProvider{name: "p"}, reg, NewSession("sys"), Options{}, event.Discard)
	a.turn.policy = taskpolicy.TaskPolicy{AllowExploreSubagent: false}
	a.turn.policySet = true

	got := a.executeOne(context.Background(), &a.turn, provider.ToolCall{Name: "explore", Arguments: `{}`})
	if !got.blocked || !strings.Contains(got.errMsg, "exploration sub-agent") {
		t.Fatalf("explore outcome = %+v, want task-policy block", got)
	}
}

func TestTaskPolicyBlocksExternalActionCommandVariants(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(fakeTool{name: "bash", readOnly: false})
	a := New(&scriptedProvider{name: "p"}, reg, NewSession("sys"), Options{}, event.Discard)
	a.turn.policy = taskpolicy.Derive(taskpolicy.Input{Raw: "fix it, but don't push"})
	a.turn.policySet = true

	for _, command := range []string{
		"git -C ../repo push origin HEAD",
		"npm --workspace pkg publish",
		"kubectl -n production apply -f deploy.yaml",
	} {
		args, err := json.Marshal(map[string]string{"command": command})
		if err != nil {
			t.Fatal(err)
		}
		got := a.executeOne(context.Background(), &a.turn, provider.ToolCall{Name: "bash", Arguments: string(args)})
		if !got.blocked || !strings.Contains(got.errMsg, "external action") {
			t.Fatalf("command %q outcome = %+v, want task-policy block", command, got)
		}
	}
}

func TestTaskPolicyBlocksResolvedExternalCapability(t *testing.T) {
	calls := 0
	target := readOnlyBoundaryTarget{name: "mcp__vercel__deploy_project", readOnly: false, calls: &calls}
	proxy := readOnlyBoundaryProxy{resolved: tool.ResolvedCall{
		ProxyAction: "call", TargetName: target.Name(), Target: target, ReadOnly: false, Args: json.RawMessage(`{}`),
	}}
	reg := tool.NewRegistry()
	reg.Add(proxy)
	a := New(nil, reg, NewSession("sys"), Options{}, event.Discard)
	a.turn.policy = taskpolicy.Derive(taskpolicy.Input{Raw: "prepare the release, but don't deploy"})
	a.turn.policySet = true

	got := a.executeOne(context.Background(), &a.turn, provider.ToolCall{
		ID: "deploy-1", Name: "use_capability", Arguments: `{"action":"call","capability_id":"mcp-tool:vercel/deploy_project"}`,
	})
	if !got.blocked || !strings.Contains(got.errMsg, "external action") {
		t.Fatalf("resolved deploy outcome = %+v, want task-policy block", got)
	}
	if calls != 0 {
		t.Fatalf("resolved deploy Execute calls = %d, want 0", calls)
	}
}

func TestTaskPolicyRequiresPostMutationVerification(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(fakeTool{name: "bash", readOnly: true})
	writer := evidence.Receipt{ToolName: "write_file", Success: true, Write: true, Mutation: true}
	check := evidence.Receipt{ToolName: "bash", Success: true, Command: "go test ./..."}
	a := &Agent{
		task: taskRuntime{ledger: readinessLedger(check, writer)},
		svc:  agentServices{tools: reg},
		turn: turnRuntime{
			policy:    taskpolicy.TaskPolicy{Verification: taskpolicy.VerifyTargeted},
			policySet: true,
		},
	}
	if got := a.finalReadinessCheckFor(); !strings.Contains(got.reason, "verification command") {
		t.Fatalf("readiness = %+v, want post-mutation verification", got)
	}
	a.task.ledger.Record(check)
	if got := a.finalReadinessCheckFor(); got.reason != "" {
		t.Fatalf("readiness after verification = %+v, want ready", got)
	}
}
