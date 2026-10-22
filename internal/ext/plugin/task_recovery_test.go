package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"reasonix/internal/contract/tool"
	"reasonix/internal/tools/jobs"
)

func TestDurableMCPTaskResumesWithoutRepeatingSubmission(t *testing.T) {
	s := &taskServer{t: t, polls: []map[string]any{working(nil)}}
	srv := httptest.NewServer(s)
	defer srv.Close()
	spec := Spec{Name: "m", Type: "http", URL: srv.URL, Authorized: true, WorkspaceRoot: t.TempDir(), ToolTimeouts: map[string]time.Duration{"job": time.Second}}
	host, tools, err := StartAll(t.Context(), []Spec{spec})
	if err != nil {
		t.Fatal(err)
	}
	job := findToolByName(tools, "mcp__m__job")
	m := jobs.NewManager(nil, jobs.WithSessionOwnershipProbe(func(string) bool { return true }))
	path := filepath.Join(t.TempDir(), "session.jsonl")
	m.SetActiveSessionPath("session", path)
	ctx := tool.WithRemoteTaskStarter(t.Context(), func(d tool.RemoteTask, run tool.RemoteTaskRun) (tool.RemoteTaskHandle, error) {
		return m.StartRemoteForSession("session", d, run)
	})
	text, err := job.Execute(ctx, json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "background job mcp_task-1:") {
		t.Fatalf("not a durable host job: %s", text)
	}
	m.Close()
	host.Close()
	if len(s.requests("tasks/cancel")) != 0 {
		t.Fatal("close cancelled accepted remote work")
	}
	s.script(map[string]any{"status": "completed", "result": map[string]any{
		"content":           []map[string]any{{"type": "text", "text": "video ready"}},
		"structuredContent": map[string]any{"media_id": "v", "url": "https://example.test/v.mp4"}, "future": true}})
	host2, _, err := StartAll(t.Context(), []Spec{spec})
	if err != nil {
		t.Fatal(err)
	}
	defer host2.Close()
	m2 := jobs.NewManager(nil, jobs.WithSessionOwnershipProbe(func(string) bool { return true }))
	defer m2.Close()
	m2.SetRemoteTaskResumer(host2.RemoteTaskResumer([]Spec{spec}))
	m2.SetActiveSessionPath("session", path)
	results, _ := m2.WaitForSession(t.Context(), "session", []string{"mcp_task-1"}, jobs.WaitOptions{Timeout: 5 * time.Second})
	if len(results) != 1 || results[0].Status != jobs.Done || !strings.Contains(results[0].Output, "[structuredContent fields: media_id, url]") {
		t.Fatalf("recovered output: %+v", results)
	}
	if calls := len(s.requests("tools/call")); calls != 1 {
		t.Fatalf("submission replayed: %d", calls)
	}
	for _, req := range s.requests("tasks/get") {
		if req.params["taskId"] != "task-1" {
			t.Fatalf("wrong remote handle: %+v", req)
		}
	}
}

func TestRemoteTaskRecoveryRejectsChangedIdentityAndRevocationBeforeNetwork(t *testing.T) {
	base := Spec{Name: "m", Type: "http", URL: "https://example.test/mcp", Authorized: true, Headers: map[string]string{"Authorization": "private-token"}}
	desc := tool.RemoteTask{Server: "m", Tool: "job", TaskID: "task-1", Protocol: modernProtocolVersion, Binding: remoteTaskBinding(base), Deadline: time.Now().Add(time.Hour).UnixMilli()}
	for _, change := range []func(*Spec){
		func(s *Spec) { s.URL = "https://different.invalid/mcp" },
		func(s *Spec) { s.Headers = map[string]string{"Authorization": "different-token"} },
		func(s *Spec) { s.Authorized = false },
		func(s *Spec) { s.DisabledTools = []string{"job"} },
	} {
		current := base
		change(&current)
		host := NewHost()
		_, err := host.RemoteTaskResumer([]Spec{current})(t.Context(), desc, io.Discard)
		host.Close()
		if !errors.Is(err, ErrMCPTaskRecoveryBinding) {
			t.Fatalf("changed identity not refused: %v", err)
		}
	}
	if strings.Contains(desc.Binding, "private-token") {
		t.Fatal("credential leaked into checkpoint identity")
	}
}

func TestDurableMCPTaskUsesCallerCloseCause(t *testing.T) {
	for _, detached := range []bool{false, true} {
		t.Run(map[bool]string{false: "stop", true: "close"}[detached], func(t *testing.T) {
			c := &Client{name: "m"}
			ctx, cancel := context.WithCancel(context.Background())
			ctx = tool.WithRemoteTaskDetach(ctx, func() bool { return detached })
			cancelled := false
			start := func(tool.RemoteTask, tool.RemoteTaskRun) (tool.RemoteTaskHandle, error) {
				return tool.RemoteTaskHandle{ID: "host-1", Wait: func(context.Context) (json.RawMessage, error) { cancel(); return nil, context.Canceled }, Cancel: func() { cancelled = true }}, nil
			}
			_, err := c.awaitDurableTask(ctx, nil, map[string]any{"name": "job"}, mcpTask{TaskID: "task-1"}, start)
			if !errors.Is(err, context.Canceled) || cancelled == detached {
				t.Fatalf("detached=%v, cancel=%v, error=%v", detached, cancelled, err)
			}
		})
	}
}

func TestDurableMCPTaskRetainsToolFailureAndInteractiveInput(t *testing.T) {
	for _, toolError := range []bool{false, true} {
		t.Run(map[bool]string{false: "input", true: "error"}[toolError], func(t *testing.T) {
			s := &taskServer{t: t, polls: []map[string]any{completedWith("finished", toolError)}, created: map[string]any{"status": "input_required", "inputRequests": map[string]any{
				"form": map[string]any{"method": "elicitation/create", "params": map[string]any{"message": "Confirm scene", "requestedSchema": map[string]any{"type": "object", "properties": map[string]any{"scene": map[string]any{"type": "string"}}, "required": []string{"scene"}}}},
			}}}
			job, _ := startTaskTool(t, s, &clockLog{})
			m := jobs.NewManager(nil)
			defer m.Close()
			elicitor := &fakeElicitor{replies: []tool.ElicitReply{{Values: map[string][]string{"scene": {"night"}}}}}
			ctx := tool.WithElicitor(t.Context(), elicitor)
			ctx = tool.WithRemoteTaskStarter(ctx, func(d tool.RemoteTask, run tool.RemoteTaskRun) (tool.RemoteTaskHandle, error) {
				return m.StartRemoteForSession("session", d, run)
			})
			text, err := job.Execute(ctx, json.RawMessage(`{}`))
			if !strings.Contains(text, "finished") || (err != nil) != toolError {
				t.Fatalf("result = %q, %v", text, err)
			}
			elicitor.mu.Lock()
			seen := len(elicitor.seen)
			elicitor.mu.Unlock()
			if seen != 1 || len(s.requests("tasks/update")) != 1 {
				t.Fatal("durable task lost caller elicitation")
			}
		})
	}
}
