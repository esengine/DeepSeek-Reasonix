package agent

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
	"reasonix/internal/ext/extension"
	"reasonix/internal/ext/extension/dispatch"
	"reasonix/internal/ext/extension/protocol"
	"reasonix/internal/runtime/taskpolicy"
	"reasonix/internal/safety/evidence"
	"reasonix/internal/state/sessionstore"
)

type namedFileWriter struct{ root string }

func (namedFileWriter) Name() string            { return "write_file" }
func (namedFileWriter) Description() string     { return "" }
func (namedFileWriter) Schema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (namedFileWriter) ReadOnly() bool          { return false }
func (namedFileWriter) WritesNamedPaths() bool  { return true }
func (w namedFileWriter) Execute(_ context.Context, args json.RawMessage) (string, error) {
	var p struct{ Path, Content string }
	if err := json.Unmarshal(args, &p); err != nil {
		return "", err
	}
	return "wrote", os.WriteFile(filepath.Join(w.root, p.Path), []byte(p.Content), 0o600)
}

func TestToolAfterRewriteCannotHideWrittenCodeFromProseWaiver(t *testing.T) {
	for _, mode := range []string{"block", "sidecar error"} {
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			for name, body := range map[string]string{"Program.cs": "class P {}\n", "README.md": "readme\n"} {
				if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			client := &fakeDispatchClient{interceptFn: func(ev protocol.InterceptEvent, payload json.RawMessage) (protocol.InterceptResult, error) {
				if ev == protocol.EventToolAfter && strings.Contains(string(payload), "Program.cs") {
					if mode == "block" {
						return blockWith("lint rejected the write"), nil
					}
					return protocol.InterceptResult{}, errors.New("sidecar timeout")
				}
				return protocol.InterceptResult{Decision: protocol.DecisionContinue}, nil
			}}
			d := newExtDispatcher(client, true, nil, extension.PointToolAfter)
			reg := tool.NewRegistry()
			reg.Add(namedFileWriter{root: root})
			reg.Add(fakeTool{name: "bash"})
			a := New(nil, reg, sessionstore.NewSession(""), Options{Extensions: d}, event.Discard)
			a.observeRoot = root
			a.writeWorkspaceRoot = root
			a.executeOne(context.Background(), &a.turn, provider.ToolCall{ID: "1", Name: "write_file", Arguments: `{"path":"Program.cs","content":"class Q {}\n"}`})
			a.executeOne(context.Background(), &a.turn, provider.ToolCall{ID: "2", Name: "write_file", Arguments: `{"path":"README.md","content":"note\n"}`})
			a.turn.policySet = true
			a.turn.policy = taskpolicy.TaskPolicy{Verification: taskpolicy.VerifyTargeted}
			src, _ := os.ReadFile(filepath.Join(root, "Program.cs"))
			check := a.finalReadinessCheckFor()
			if string(src) != "class Q {}\n" {
				t.Fatalf("Program.cs = %q, the write did not land", src)
			}
			if check.missingVerification != 1 || !strings.Contains(check.reason, "Program.cs") {
				t.Errorf("missing verification = %d, reason %q; want the written code to keep the check owed", check.missingVerification, check.reason)
			}
		})
	}
}

func TestToolAfterRewriteAfterPassingCheckKeepsCheckOwed(t *testing.T) {
	root := t.TempDir()
	for name, body := range map[string]string{"Program.cs": "class P {}\n", "README.md": "readme\n"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	client := &fakeDispatchClient{interceptFn: func(ev protocol.InterceptEvent, payload json.RawMessage) (protocol.InterceptResult, error) {
		if ev == protocol.EventToolAfter && strings.Contains(string(payload), "class R") {
			return blockWith("lint rejected the write"), nil
		}
		return protocol.InterceptResult{Decision: protocol.DecisionContinue}, nil
	}}
	reg := tool.NewRegistry()
	reg.Add(namedFileWriter{root: root})
	reg.Add(fakeTool{name: "bash"})
	a := New(nil, reg, sessionstore.NewSession(""), Options{Extensions: newExtDispatcher(client, true, nil, extension.PointToolAfter)}, event.Discard)
	a.observeRoot, a.writeWorkspaceRoot = root, root
	a.executeOne(context.Background(), &a.turn, provider.ToolCall{ID: "1", Name: "write_file", Arguments: `{"path":"Program.cs","content":"class Q {}\n"}`})
	zero := 0
	a.task.ledger.Record(evidence.Receipt{ToolName: "bash", Success: true, Command: "dotnet test", ExitCode: &zero, Verification: evidence.VerificationPassed})
	a.executeOne(context.Background(), &a.turn, provider.ToolCall{ID: "2", Name: "write_file", Arguments: `{"path":"Program.cs","content":"class R {}\n"}`})
	a.executeOne(context.Background(), &a.turn, provider.ToolCall{ID: "3", Name: "write_file", Arguments: `{"path":"README.md","content":"note\n"}`})
	a.turn.policySet = true
	a.turn.policy = taskpolicy.TaskPolicy{Verification: taskpolicy.VerifyTargeted}
	if src, _ := os.ReadFile(filepath.Join(root, "Program.cs")); string(src) != "class R {}\n" {
		t.Fatalf("Program.cs = %q, the write did not land", src)
	}
	if check := a.finalReadinessCheckFor(); check.missingVerification != 1 || !strings.Contains(check.reason, "Program.cs") {
		t.Errorf("missing verification = %d, reason %q; want the code written after the check to keep it owed", check.missingVerification, check.reason)
	}
}

func TestSidecarOnToolCallsIsAnUnseenWriter(t *testing.T) {
	sidecar := extension.Contribution{Kind: extension.KindInterceptor, ID: "x", Source: extension.ContributionSource{Scope: extension.ScopePlugin, PluginID: "p"}}
	for _, tc := range []struct {
		point extension.InterceptorPoint
		want  bool
	}{
		{extension.PointToolBefore, true},
		{extension.PointToolAfter, true},
		{extension.PointPermissionDecision, true},
		{extension.PointInputReceive, false},
	} {
		t.Run(string(tc.point), func(t *testing.T) {
			d := dispatch.New(map[extension.InterceptorPoint][]extension.Contribution{tc.point: {sidecar}}, nil,
				func(string) dispatch.Client { return nil }, nil, dispatch.Options{})
			a := New(nil, tool.NewRegistry(), sessionstore.NewSession(""), Options{Extensions: d}, event.Discard)
			if got := a.unseenToolWriter(); got != tc.want {
				t.Errorf("unseen writer = %v, want %v", got, tc.want)
			}
			if got := a.checkContract().UnseenWriter(); got != tc.want {
				t.Errorf("contract unseen writer = %v, want %v", got, tc.want)
			}
		})
	}
}
