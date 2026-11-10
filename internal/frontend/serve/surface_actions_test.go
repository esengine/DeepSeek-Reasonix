package serve

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/config"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/surfacespec"
	"reasonix/internal/contract/tool"
	"reasonix/internal/runtime/agent"
	"reasonix/internal/session/control"
	"reasonix/internal/state/sessionstore"
	"strings"
	"testing"
)

func TestSurfaceActionHTTPClosedPayloadAndIdentity(t *testing.T) {
	const spec = `{"spec_version":1,"body":[{"kind":"field","key":"name","label":"Name","field":{"type":"string","required":true}},{"kind":"button","label":"Continue","actionId":"continue"}]}`
	sess := sessionstore.NewSession("system")
	sess.Add(provider.Message{Role: provider.RoleUser, Content: "task"})
	sess.Add(provider.Message{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: "call", Name: surfacespec.ToolName, Arguments: spec}}})
	sess.Add(provider.Message{Role: provider.RoleTool, ToolCallID: "call", Content: `{"nonblocking":true}`})
	registry := tool.NewRegistry()
	registry.Add(agent.NewRenderSurfaceTool())
	exec := agent.New(nil, registry, sess, agent.Options{}, event.Discard)
	ctrl := control.New(control.Options{Runner: exec, Executor: exec, Registry: registry, Sink: event.Discard, SessionPath: filepath.Join(testenv.TempDir(t), "session.jsonl")})
	defer ctrl.Close()
	if err := ctrl.SetInboxPaused(true); err != nil {
		t.Fatal(err)
	}
	server := New(ctrl, NewBroadcaster(), config.ServeConfig{})
	parsed, _ := surfacespec.Parse([]byte(spec))
	if err := ctrl.PublishNativeSurface("call", parsed.ID()); err != nil {
		t.Fatal(err)
	}
	state, err := ctrl.SurfaceActionState("call")
	if err != nil {
		t.Fatal(err)
	}
	state.Action.ActionID = "continue"
	state.Action.RequestID = "request"
	state.Action.Values = json.RawMessage(`{"name":"Name"}`)
	req := control.SurfaceActionRequest{Action: state.Action, Delivery: "queue"}
	body, _ := json.Marshal(req)
	for _, test := range []struct {
		body   string
		status int
	}{
		{strings.Replace(string(body), `"continue"`, `"missing"`, 1), http.StatusBadRequest},
		{strings.Replace(string(body), `"delivery":"queue"`, `"delivery":"approve"`, 1), http.StatusBadRequest},
		{strings.TrimSuffix(string(body), "}") + `,"approve":true}`, http.StatusBadRequest},
		{string(body), http.StatusAccepted}, {string(body), http.StatusAccepted},
	} {
		recorder := httptest.NewRecorder()
		server.surfaceAction(recorder, httptest.NewRequest(http.MethodPost, "/surfaces/action", strings.NewReader(test.body)))
		if recorder.Code != test.status {
			t.Fatalf("%d != %d: %s", recorder.Code, test.status, recorder.Body.String())
		}
	}
	if len(ctrl.InboxSnapshot().Items) != 1 {
		t.Fatal("duplicate action was enqueued")
	}
	for _, m := range historyMessages(ctrl.History()) {
		if m.Content == "" {
			continue
		}
		if strings.Contains(m.Content, `"status":"accepted"`) {
			t.Fatal("receipt leaked into conversation")
		}
	}
}
