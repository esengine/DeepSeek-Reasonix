package control

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/event"
	"reasonix/internal/guardian"
	"reasonix/internal/permission"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

const dynamicBashCommand = "git status $(touch /tmp/reasonix-dynamic-approval)"

type dynamicApprovalResult struct {
	allow bool
	err   error
}

func requestDynamicBashApproval(c *Controller) <-chan dynamicApprovalResult {
	done := make(chan dynamicApprovalResult, 1)
	go func() {
		allow, _, err := gateApprover{c}.Approve(
			context.Background(),
			"bash",
			dynamicBashCommand,
			json.RawMessage(`{"command":"git status $(touch /tmp/reasonix-dynamic-approval)"}`),
		)
		done <- dynamicApprovalResult{allow: allow, err: err}
	}()
	return done
}

func assertDynamicApprovalPending(t *testing.T, done <-chan dynamicApprovalResult) {
	t.Helper()
	select {
	case got := <-done:
		t.Fatalf("dynamic Bash approval completed without a human decision: %+v", got)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestDynamicBashUsesTheSamePresetDecisionAsOtherCommands(t *testing.T) {
	for _, tt := range []struct {
		name string
		mode string
	}{
		{name: "workspace write", mode: ToolApprovalWorkspaceWrite},
		{name: "full access", mode: ToolApprovalDangerFullAccess},
	} {
		t.Run(tt.name, func(t *testing.T) {
			approvals := make(chan event.Approval, 1)
			c := New(Options{Sink: event.FuncSink(func(e event.Event) {
				if e.Kind == event.ApprovalRequest {
					approvals <- e.Approval
				}
			})})
			c.SetToolApprovalMode(tt.mode)
			args := json.RawMessage(`{"command":"git status $(touch /tmp/reasonix-dynamic-approval)"}`)
			allow, reason, err := c.newInteractiveGate().Check(context.Background(), "bash", args, false)
			if err != nil || !allow || reason != "" {
				t.Fatalf("preset decision = (%v,%q,%v), want allow", allow, reason, err)
			}
			select {
			case approval := <-approvals:
				t.Fatalf("dynamic Bash unexpectedly prompted: %+v", approval)
			default:
			}
		})
	}
}

func TestExactOnlyBashDoesNotPromptInAutoOrApprovedPlan(t *testing.T) {
	commands := []string{
		"REV=HEAD git diff",
		"git status > status.txt",
		"rm *.log",
		"echo $HOME",
	}
	for _, tt := range []struct {
		name  string
		setup func(*Controller)
	}{
		{name: "auto", setup: func(c *Controller) { c.SetToolApprovalMode(ToolApprovalAuto) }},
		{name: "approved plan", setup: func(c *Controller) { c.approval.setPlanAutoApprove(true) }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			approvals := make(chan event.Approval, len(commands))
			c := New(Options{Sink: event.FuncSink(func(e event.Event) {
				if e.Kind == event.ApprovalRequest {
					approvals <- e.Approval
				}
			})})
			tt.setup(c)
			gate := c.newInteractiveGate()
			for _, command := range commands {
				args, err := json.Marshal(map[string]string{"command": command})
				if err != nil {
					t.Fatal(err)
				}
				allow, reason, err := gate.Check(context.Background(), "bash", args, false)
				if err != nil || !allow || reason != "" {
					t.Errorf("%s command %q = (%v,%q,%v), want allow without prompt", tt.name, command, allow, reason, err)
				}
			}
			select {
			case approval := <-approvals:
				t.Fatalf("%s exact-only Bash unexpectedly prompted: %+v", tt.name, approval)
			case <-time.After(50 * time.Millisecond):
			}
		})
	}
}

func TestPermissionModeChangeDoesNotAutoApprovePendingRequest(t *testing.T) {
	approvals := make(chan event.Approval, 1)
	c := New(Options{Sink: event.FuncSink(func(e event.Event) {
		if e.Kind == event.ApprovalRequest {
			approvals <- e.Approval
		}
	})})
	done := requestDynamicBashApproval(c)
	select {
	case <-approvals:
	case <-time.After(30 * time.Second):
		t.Fatal("dynamic Bash approval prompt was not emitted")
	}
	c.SetToolApprovalMode(ToolApprovalWorkspaceWrite)
	assertDynamicApprovalPending(t, done)
	c.SetToolApprovalMode(ToolApprovalDangerFullAccess)
	assertDynamicApprovalPending(t, done)
	c.Approve("1", false, false, false)
	if got := <-done; got.err != nil || got.allow {
		t.Fatalf("explicit denial after mode changes = %+v, want deny", got)
	}
}

func TestDynamicBashExactSessionGrantIsNotPersisted(t *testing.T) {
	approvals := make(chan event.Approval, 2)
	remembered := make(chan string, 1)
	c := New(Options{
		Sink: event.FuncSink(func(e event.Event) {
			if e.Kind == event.ApprovalRequest {
				approvals <- e.Approval
			}
		}),
		OnRemember: func(rule string) RememberResult {
			remembered <- rule
			return RememberResult{Saved: true}
		},
	})

	done := requestDynamicBashApproval(c)
	approval := <-approvals
	c.Approve(approval.ID, true, true, false)
	if got := <-done; got.err != nil || !got.allow {
		t.Fatalf("initial approval = %+v, want allow", got)
	}
	allow, _, err := gateApprover{c}.Approve(context.Background(), "bash", dynamicBashCommand, nil)
	if err != nil || !allow {
		t.Fatalf("exact session grant = (%v,%v), want allow", allow, err)
	}
	select {
	case approval := <-approvals:
		t.Fatalf("exact session grant unexpectedly prompted: %+v", approval)
	case <-time.After(50 * time.Millisecond):
	}
	select {
	case got := <-remembered:
		t.Fatalf("session grant was persisted as %q", got)
	default:
	}

	old := c.SessionAuthorizations()
	fresh := New(Options{})
	fresh.RestoreSessionAuthorizations(old)
	allow, _, err = gateApprover{fresh}.Approve(context.Background(), "bash", dynamicBashCommand, nil)
	if err != nil || !allow {
		t.Fatalf("restored exact session grant = (%v,%v), want allow", allow, err)
	}
}

func TestDynamicBashHookDecisionAppliesWithoutSyntaxSpecialCase(t *testing.T) {
	allowJSON := `{"hookSpecificOutput":{"hookEventName":"PermissionRequest","decision":{"behavior":"allow"}}}`
	c, ids := wildcardClaudePermissionHookController(t, 0, allowJSON)
	done := requestDynamicBashApproval(c)
	if got := <-done; got.err != nil || !got.allow {
		t.Fatalf("hook allow = %+v, want allow", got)
	}
	select {
	case id := <-ids:
		t.Fatalf("hook allow unexpectedly emitted approval %s", id)
	default:
	}

	c, ids = wildcardClaudePermissionHookController(t, 2, "")
	allow, _, err := gateApprover{c}.Approve(context.Background(), "bash", dynamicBashCommand, nil)
	if err != nil || allow {
		t.Fatalf("hook deny = (%v,%v), want deny", allow, err)
	}
	select {
	case id := <-ids:
		t.Fatalf("hook deny unexpectedly emitted approval %s", id)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestDynamicBashSkipsGuardianAllow(t *testing.T) {
	guardianProv := &recordingProvider{
		name:    "guardian",
		streams: [][]provider.Chunk{textTurn(`{"risk_level":"low","user_authorization":"high","outcome":"allow","rationale":"safe"}`)},
	}
	guardianSess := guardian.NewSession(guardianProv, tool.NewRegistry(), guardian.PolicyPrompt(), "guardian-test", 0, nil, event.Discard)
	exec := agent.New(&recordingProvider{name: "executor"}, tool.NewRegistry(), agent.NewSession("sys"), agent.Options{}, event.Discard)
	approvals := make(chan event.Approval, 1)
	c := New(Options{
		Executor: exec,
		Guardian: guardianSess,
		Sink: event.FuncSink(func(e event.Event) {
			if e.Kind == event.ApprovalRequest {
				approvals <- e.Approval
			}
		}),
	})
	done := requestDynamicBashApproval(c)
	approval := <-approvals
	if len(guardianProv.requests) != 0 {
		t.Fatalf("dynamic Bash Guardian reviews = %d, want 0", len(guardianProv.requests))
	}
	c.Approve(approval.ID, true, false, false)
	if got := <-done; got.err != nil || !got.allow {
		t.Fatalf("manual approval = %+v, want allow", got)
	}
}

func TestHeadlessDynamicBashApprovalModes(t *testing.T) {
	args := json.RawMessage(`{"command":"git status $(touch /tmp/reasonix-dynamic-approval)"}`)
	for _, tt := range []struct {
		mode string
		want bool
	}{
		{mode: ToolApprovalAsk},
		{mode: ToolApprovalAuto, want: true},
		{mode: ToolApprovalDontAsk},
		{mode: ToolApprovalYolo, want: true},
	} {
		t.Run(tt.mode, func(t *testing.T) {
			allow, reason, err := BuildHeadlessApprovalGate(permission.New("ask", []string{"Bash(git*)"}, nil, nil), tt.mode).Check(context.Background(), "bash", args, false)
			if err != nil || allow != tt.want {
				t.Fatalf("headless %s = (%v,%q,%v), want allow=%v", tt.mode, allow, reason, err, tt.want)
			}
			if !tt.want && strings.TrimSpace(reason) == "" {
				t.Fatalf("headless %s returned no denial reason", tt.mode)
			}
		})
	}

	exact := permission.New("ask", []string{"Bash=" + dynamicBashCommand}, nil, nil)
	allow, reason, err := BuildHeadlessApprovalGate(exact, ToolApprovalAsk).Check(context.Background(), "bash", args, false)
	if err != nil || !allow || reason != "" {
		t.Fatalf("headless exact literal = (%v,%q,%v), want allow", allow, reason, err)
	}

	optIn := permission.New("ask", nil, nil, nil).WithAllowDynamicBashFallback(true)
	allow, reason, err = BuildHeadlessApprovalGate(optIn, ToolApprovalAuto).Check(context.Background(), "bash", args, false)
	if err != nil || !allow || reason != "" {
		t.Fatalf("headless dynamic fallback opt-in = (%v,%q,%v), want allow", allow, reason, err)
	}
}

func TestHeadlessExactOnlyBashApprovalModes(t *testing.T) {
	args := json.RawMessage(`{"command":"rm *.log"}`)
	for _, tt := range []struct {
		mode string
		want bool
	}{
		{mode: ToolApprovalAsk},
		{mode: ToolApprovalAuto, want: true},
		{mode: ToolApprovalDontAsk},
		{mode: ToolApprovalYolo, want: true},
	} {
		t.Run(tt.mode, func(t *testing.T) {
			allow, _, err := BuildHeadlessApprovalGate(permission.New("ask", []string{"Bash(rm*)"}, nil, nil), tt.mode).Check(context.Background(), "bash", args, false)
			if err != nil || allow != tt.want {
				t.Fatalf("headless %s exact-only Bash = (%v,%v), want allow=%v", tt.mode, allow, err, tt.want)
			}
		})
	}
}
