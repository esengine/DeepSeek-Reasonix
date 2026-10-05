package permission

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

type scriptedApprover struct {
	allow bool
	asked int
}

func (a *scriptedApprover) Approve(context.Context, string, string, json.RawMessage) (bool, bool, error) {
	a.asked++
	return a.allow, false, nil
}

type unattendedApprover struct{ scriptedApprover }

func (unattendedApprover) Unattended() bool { return true }

func commandArgs(cmd string) json.RawMessage {
	b, _ := json.Marshal(map[string]string{"command": cmd})
	return b
}

func TestReadOnlyPolicyRefusesEveryWriteWhateverTheRulesAllow(t *testing.T) {
	p := New("allow", []string{"write_file", "Bash", "mcp__srv__put"}, nil, []string{"Bash(rm:*)"})
	p.ReadOnly = true
	approver := &scriptedApprover{allow: true}
	g := NewGate(p, approver)
	ctx := context.Background()
	for _, c := range []struct {
		tool string
		args json.RawMessage
	}{
		{"write_file", json.RawMessage(`{"path":"a.txt","content":"x"}`)},
		{"bash", commandArgs("echo hi > out.txt")},
		{"bash", commandArgs("go test ./...")},
		{"bash", commandArgs("python3 -c 'print(1)'")},
		{"mcp__srv__put", json.RawMessage(`{}`)},
		{"remember", json.RawMessage(`{"name":"x"}`)},
	} {
		v, err := g.Verdict(ctx, c.tool, c.args, false)
		if err != nil || v.Allow || v.Code != RefusalReadOnly {
			t.Errorf("%s %s: verdict %+v err %v, want a read-only refusal", c.tool, c.args, v, err)
		}
	}
	if approver.asked != 0 {
		t.Fatalf("read-only mode asked a person %d times; a refusal is not a question", approver.asked)
	}
	if !g.DeniesWriters() {
		t.Fatal("a read-only gate must say it denies writers")
	}
	for _, cmd := range []string{"git status", "git -C . log -3", "ls -la && cat go.mod", "grep -rn foo ."} {
		if v, _ := g.Verdict(ctx, "bash", commandArgs(cmd), false); !v.Allow {
			t.Errorf("read-only mode refused the read %q: %+v", cmd, v)
		}
	}
	if v, _ := g.Verdict(ctx, "read_file", json.RawMessage(`{"path":"a"}`), true); !v.Allow {
		t.Fatalf("read-only mode refused a declared reader: %+v", v)
	}
	if v, _ := g.Verdict(ctx, "bash", commandArgs("rm -rf build"), false); v.Code != RefusalDenyRule {
		t.Fatalf("a deny rule keeps its own identity in read-only mode, got %+v", v)
	}
}

func TestVerdictNamesWhoRefused(t *testing.T) {
	ctx := context.Background()
	write := json.RawMessage(`{"path":"a.txt","content":"x"}`)
	declined := NewGate(New("ask", nil, nil, nil), &scriptedApprover{})
	if v, _ := declined.Verdict(ctx, "write_file", write, false); v.Allow || v.Code != RefusalDeclined {
		t.Fatalf("a person saying no is a decline, got %+v", v)
	}
	nobody := NewGate(New("ask", nil, nil, nil), &unattendedApprover{})
	if v, _ := nobody.Verdict(ctx, "write_file", write, false); v.Allow || v.Code != RefusalUnattended {
		t.Fatalf("an approver standing in for nobody is unattended, got %+v", v)
	}
	noApprover := NewGate(New("ask", nil, nil, nil), nil)
	if v, _ := noApprover.Verdict(ctx, ExtendWritePaths, json.RawMessage(`{"path":"/x"}`), false); v.Allow || v.Code != RefusalUnattended {
		t.Fatalf("a human-only question with no approver is unattended, got %+v", v)
	}
	coded := NewGate(New("ask", nil, nil, nil), &codedApprover{})
	if v, _ := coded.Verdict(ctx, "write_file", write, false); v.Code != RefusalUntrustedFolder || !IsRefusalCode(v.Code) {
		t.Fatalf("an unattended approver that names its cause carries that code, got %+v", v)
	}
	deny := NewGate(New("allow", nil, nil, []string{"write_file"}), nil)
	if v, _ := deny.Verdict(ctx, "write_file", write, false); v.Code != RefusalDenyRule {
		t.Fatalf("a deny rule is its own identity, got %+v", v)
	}
	for _, code := range []string{RefusalDenyRule, RefusalReadOnly, RefusalDeclined, RefusalUnattended} {
		if !IsRefusalCode(code) {
			t.Errorf("IsRefusalCode(%q) = false", code)
		}
	}
	if IsRefusalCode("sandbox.unavailable") || IsRefusalCode("") {
		t.Fatal("IsRefusalCode claims a code no gate produces")
	}
	allow, reason, _ := declined.Check(ctx, "write_file", write, false)
	if allow || reason == "" {
		t.Fatalf("Check drops the verdict's reason: allow=%v reason=%q", allow, reason)
	}
}

type checkOnly struct{}

func (checkOnly) Check(context.Context, string, json.RawMessage, bool) (bool, string, error) {
	return false, "no", nil
}

// A refusal no rule produced names the posture that produced it, not a deny
// list nobody wrote.
func TestDenialWithoutARuleNamesThePosture(t *testing.T) {
	ctx := context.Background()
	dontAsk := NewGate(New("deny", nil, nil, nil), nil)
	if v, _ := dontAsk.Verdict(ctx, "write_file", json.RawMessage(`{"path":"a"}`), false); v.Code != RefusalUnattended || strings.Contains(v.Reason, "deny list") {
		t.Fatalf("a fallback refusal reads as a deny rule: %+v", v)
	}
	p := New("deny", nil, nil, nil)
	p.ReadOnly = true
	ro := NewGate(p, nil)
	if v, _ := ro.Verdict(ctx, "bash", commandArgs("env"), false); v.Allow || v.Code != RefusalReadOnly {
		t.Fatalf("a read the host cannot confirm is refused as read-only, got %+v", v)
	}
	if v, _ := VerdictOf(ctx, checkOnly{}, "x", nil, false); v.Allow || v.Reason != "no" || v.Code != "" {
		t.Fatalf("a Check-only gate keeps its answer and names no code: %+v", v)
	}
	if _, refused := PostureRefusal(ctx, checkOnly{}, "x", nil, false); refused {
		t.Fatal("a gate with no posture to report is not read-only")
	}
}

type codedApprover struct{ unattendedApprover }

func (codedApprover) RefusalCodeFor(string, string, string, json.RawMessage) string {
	return RefusalUntrustedFolder
}
