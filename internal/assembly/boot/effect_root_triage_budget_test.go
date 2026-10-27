package boot

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/tool"
	"reasonix/internal/runtime/agent"
	"reasonix/internal/session/control"
	"reasonix/internal/state/sessionstore"
)

type triageFixtureRead struct {
	path  string
	reads atomic.Int32
}

func (*triageFixtureRead) Name() string            { return "triage_fixture" }
func (*triageFixtureRead) Description() string     { return "Read the screening fixture." }
func (*triageFixtureRead) ReadOnly() bool          { return true }
func (*triageFixtureRead) Schema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (*triageFixtureRead) Provenance(json.RawMessage) tool.Provenance {
	return tool.Provenance{Kind: tool.ProvenanceWeb, Source: "screening-fixture"}
}
func (p *triageFixtureRead) Execute(context.Context, json.RawMessage) (string, error) {
	b, err := os.ReadFile(p.path)
	if err == nil {
		p.reads.Add(1)
	}
	return string(b), err
}

func TestEffectRootTriageScreenBudget(t *testing.T) {
	for _, tc := range []struct {
		name       string
		limit      int
		missing    bool
		flag       bool
		screenOff  bool
		wantReads  int32
		wantTriage int32
	}{
		{name: "reported", limit: 1500, wantReads: 1, wantTriage: 1},
		{name: "flagged", limit: 1500, flag: true, wantReads: 1, wantTriage: 1},
		{name: "disabled", wantReads: 4, wantTriage: 4},
		{name: "missing", limit: 1500, missing: true, wantReads: 2, wantTriage: 2},
		{name: "screen_disabled", limit: 1500, screenOff: true, wantReads: 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home, root := statedBootHome(t), t.TempDir()
			writeFile(t, root, "fixture.txt", "screening fixture content")
			var normal, triage, notices atomic.Int32
			var sawNotice atomic.Bool
			var mainPrefix atomic.Pointer[string]
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var req struct {
					Model    string            `json:"model"`
					Messages []json.RawMessage `json:"messages"`
					Tools    json.RawMessage   `json:"tools"`
				}
				if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
					t.Error(err)
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				w.Header().Set("Content-Type", "text/event-stream")
				if req.Model == "triage" {
					triage.Add(1)
					if len(req.Messages) != 2 || len(req.Tools) > 0 && string(req.Tools) != "null" {
						t.Errorf("triage request messages/tools=%d/%s", len(req.Messages), req.Tools)
					}
					if !strings.Contains(string(req.Messages[1]), "screening fixture content") {
						t.Error("screen did not receive the actual fixture result")
					}
					answer := "CLEAN"
					if tc.flag {
						answer = "INJECTION"
					}
					_, _ = fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{\"content\":%q},\"finish_reason\":\"stop\"}]}\n\n", answer)
				} else {
					prefix := string(req.Messages[0]) + string(req.Tools)
					if previous := mainPrefix.Swap(&prefix); previous != nil && prefix != *previous {
						t.Error("main serialized prefix changed during the run")
					}
					for _, m := range req.Messages {
						if strings.Contains(string(m), "screening model judged") {
							sawNotice.Store(true)
						}
					}
					if n := normal.Add(1); n <= 4 {
						_, _ = fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"fixture-%d\",\"type\":\"function\",\"function\":{\"name\":\"triage_fixture\",\"arguments\":\"{}\"}}]},\"finish_reason\":\"tool_calls\"}]}\n\n", n)
					} else {
						_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"Finished.\"},\"finish_reason\":\"stop\"}]}\n\n")
					}
				}
				if req.Model != "triage" || !tc.missing {
					_, _ = io.WriteString(w, "data: {\"choices\":[],\"usage\":{\"prompt_tokens\":1000,\"completion_tokens\":100,\"total_tokens\":1100}}\n\n")
				}
				_, _ = io.WriteString(w, "data: [DONE]\n\n")
			}))
			t.Cleanup(srv.Close)
			writeFile(t, root, "reasonix.toml", fmt.Sprintf(`default_model = "worker"
[agent]
system_prompt = "BASE"
triage_model = "triage"
screen_external_content = %t
task_token_budget = %d
[codegraph]
enabled = false
[[providers]]
name = "worker"
kind = "openai"
model = "worker"
context_window = 200000
base_url = %q
api_key_env = "ROOT_TRIAGE_KEY"
[[providers]]
name = "triage"
kind = "openai"
model = "triage"
base_url = %q
api_key_env = "ROOT_TRIAGE_KEY"
`, !tc.screenOff, tc.limit, srv.URL, srv.URL))
			t.Setenv("ROOT_TRIAGE_KEY", "test")
			approveWorkspaceAt(t, home, root)
			sink := event.FuncSink(func(e event.Event) {
				if e.Kind == event.Notice && e.Code == event.NoticeCodeSuspectedInjection {
					notices.Add(1)
				}
				if e.Kind == event.Usage && e.UsageSource == event.UsageSourceInjectionScreen && (e.ModelRef != "triage" || e.Usage.PromptTokens+e.Usage.CompletionTokens != 1100) {
					t.Errorf("triage billing changed: model=%s usage=%+v", e.ModelRef, e.Usage)
				}
			})
			ctrl, err := Build(t.Context(), Options{Home: home, WorkspaceRoot: root, Sink: sink})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(ctrl.Close)
			ctrl.SetSessionPath(sessionstore.NewSessionPath(ctrl.SessionDir(), ctrl.Label()))
			ctrl.SetToolApprovalMode(control.ToolApprovalYolo)
			probe := &triageFixtureRead{path: filepath.Join(root, "fixture.txt")}
			reg := tool.NewRegistry()
			reg.Add(probe)
			ctrl.Executor().SetTools(reg)
			err = ctrl.Run(reportedBudgetEffectContext(t, t.Context()), "read screening fixture")
			if tc.limit == 0 {
				if err != nil {
					t.Fatal(err)
				}
			} else if pause, ok := agent.InspectRunPause(err); !ok || pause.Key != "token" {
				t.Fatalf("pause=%+v err=%v", pause, err)
			}
			if probe.reads.Load() != tc.wantReads || triage.Load() != tc.wantTriage || normal.Load() != tc.wantReads+1 {
				t.Fatalf("read/triage/normal=%d/%d/%d want%d/%d/%d", probe.reads.Load(), triage.Load(), normal.Load(), tc.wantReads, tc.wantTriage, tc.wantReads+1)
			}
			if tc.flag != (notices.Load() == 1) || tc.flag != sawNotice.Load() {
				t.Fatalf("flag=%v notices=%d modelNotice=%v", tc.flag, notices.Load(), sawNotice.Load())
			}
		})
	}
}
