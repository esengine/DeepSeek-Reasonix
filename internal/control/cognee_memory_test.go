package control

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"reasonix/internal/agent"
	"reasonix/internal/config"
	"reasonix/internal/plugin"
	"reasonix/internal/tool"
)

func TestCogneeRecallConnectsEnabledServerOnDemand(t *testing.T) {
	isolateControlConfigHome(t)
	workspace := t.TempDir()
	var requests atomic.Int32
	var guidanceCalls atomic.Int32
	var proposals []map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params struct {
				Name      string         `json:"name"`
				Arguments map[string]any `json:"arguments"`
			} `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		requests.Add(1)
		if len(req.ID) == 0 || string(req.ID) == "null" {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		var result any = map[string]any{}
		switch req.Method {
		case "initialize":
			result = map[string]any{
				"protocolVersion": "2025-03-26",
				"serverInfo":      map[string]any{"name": "Cognee", "version": "test"},
			}
		case "tools/list":
			result = map[string]any{"tools": []map[string]any{
				{"name": "get_guidance", "inputSchema": map[string]any{"type": "object", "properties": map[string]any{
					"task": map[string]any{"type": "string"},
				}}},
				{"name": "propose_memory", "inputSchema": map[string]any{"type": "object", "properties": map[string]any{
					"content": map[string]any{"type": "string"}, "category": map[string]any{"type": "string"},
					"target_scope": map[string]any{"type": "string", "enum": []string{"private", "global"}}, "source": map[string]any{"type": "string"},
					"memory_id": map[string]any{"type": "string"}, "reason": map[string]any{"type": "string"},
				}}},
			}}
		case "tools/call":
			if req.Params.Name == "get_guidance" {
				if req.Params.Arguments["task"] == "" || req.Params.Arguments["dataset_name"] != nil || req.Params.Arguments["datasets"] != nil || req.Params.Arguments["scope"] != nil || req.Params.Arguments["session_id"] != nil {
					t.Errorf("get_guidance arguments = %#v, want task only and no caller-controlled scope", req.Params.Arguments)
				}
				guidanceCalls.Add(1)
				result = map[string]any{"content": []map[string]any{{"type": "text", "text": "remembered project fact"}}}
			} else if req.Params.Name == "propose_memory" {
				proposals = append(proposals, req.Params.Arguments)
				if req.Params.Arguments["content"] == "" || req.Params.Arguments["category"] != "adaptive_strategy" || req.Params.Arguments["source"] == "" || req.Params.Arguments["target_scope"] == "" || req.Params.Arguments["reason"] == "" {
					t.Errorf("proposal arguments = %#v, want content, supported category, target_scope, source, and reason", req.Params.Arguments)
				}
				result = map[string]any{"content": []map[string]any{{"type": "text", "text": "Proposal created with status=pending"}}}
			} else {
				t.Errorf("tools/call name = %q, want get_guidance or propose_memory", req.Params.Name)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": result})
	}))
	defer server.Close()
	if err := os.WriteFile(filepath.Join(workspace, "reasonix.toml"), []byte(fmt.Sprintf(`
[[plugins]]
name = "cognee"
type = "http"
url = %q
`, server.URL)), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.LoadForRoot(workspace)
	if err != nil || len(cfg.Plugins) != 1 {
		t.Fatalf("load Cognee config = (%+v, %v)", cfg.Plugins, err)
	}
	if err := config.DefaultMCPActivationStore().SetServerEnabled(cfg.Plugins[0], workspace, true); err != nil {
		t.Fatalf("enable Cognee MCP: %v", err)
	}
	host := plugin.NewHost()
	defer host.Close()
	ctrl := newOwnedTestController(t, Options{
		Host:          host,
		Registry:      tool.NewRegistry(),
		PluginCtx:     context.Background(),
		WorkspaceRoot: workspace,
	})
	defer ctrl.Close()
	if host.HasClientForSpec(ctrl.mcpSpec(cfg.Plugins[0])) {
		t.Fatal("Cognee unexpectedly connected before guidance retrieval")
	}

	memory, notice := ctrl.cogneeGuidanceContext(context.Background(), "What project decisions were made?")
	if !strings.Contains(memory, "remembered project fact") || !strings.Contains(notice, "已检索") {
		t.Fatalf("recall = (%q, %q), want remembered context and success notice", memory, notice)
	}
	if got := guidanceCalls.Load(); got != 1 {
		t.Fatalf("guidance calls = %d, want 1 (MCP requests=%d)", got, requests.Load())
	}
	ctrl.sessionDir = filepath.Join(t.TempDir(), "session")
	sessionNotice := ctrl.cogneeProposePrivate(context.Background(), "test-message-id", "Task: remember this safely\nOutcome: durable session result")
	if got := sessionNotice; !strings.Contains(got, "私有记忆提案") || !strings.Contains(got, "待桌面端审核") {
		t.Fatalf("private proposal result = %q", got)
	}
	graphNotice := ctrl.cogneeProposeGlobal(context.Background(), "test-message-id", "Task: remember this safely\nOutcome: durable graph result")
	if got := graphNotice; !strings.Contains(got, "全局记忆提案") || !strings.Contains(got, "待桌面端审核") {
		t.Fatalf("global proposal result = %q", got)
	}
	if len(proposals) != 2 {
		t.Fatalf("proposal calls = %d, want private and global; notices=(%q,%q)", len(proposals), sessionNotice, graphNotice)
	}
	privateArgs, globalArgs := proposals[0], proposals[1]
	if privateArgs["target_scope"] != "private" || privateArgs["memory_id"] != nil || privateArgs["dataset_name"] != nil || privateArgs["session_id"] != nil {
		t.Fatalf("private proposal args = %#v", privateArgs)
	}
	if globalArgs["target_scope"] != "global" || globalArgs["memory_id"] != nil || globalArgs["dataset_name"] != nil || globalArgs["session_id"] != nil {
		t.Fatalf("global proposal args = %#v", globalArgs)
	}
	if strings.Contains(sessionNotice, "已保存") || strings.Contains(graphNotice, "已保存") {
		t.Fatalf("proposal notice claimed data was applied: (%q, %q)", sessionNotice, graphNotice)
	}
}

func TestCogneeRecallDoesNotConnectDisabledProjectServer(t *testing.T) {
	isolateControlConfigHome(t)
	workspace := t.TempDir()
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()
	if err := os.WriteFile(filepath.Join(workspace, "reasonix.toml"), []byte(fmt.Sprintf(`
[[plugins]]
name = "cognee"
type = "http"
url = %q
`, server.URL)), 0o644); err != nil {
		t.Fatal(err)
	}
	ctrl := newOwnedTestController(t, Options{WorkspaceRoot: workspace})
	defer ctrl.Close()
	if _, notice := ctrl.cogneeGuidanceContext(context.Background(), "query"); !strings.Contains(notice, "Cognee MCP 服务未连接或未授权") {
		t.Fatalf("disabled Cognee notice = %q, want a skipped-memory notice", notice)
	}
	if got := requests.Load(); got != 0 {
		t.Fatalf("disabled Cognee server received %d requests, want no network access", got)
	}
}

func TestCogneeRecallReportsAuthenticationFailure(t *testing.T) {
	isolateControlConfigHome(t)
	workspace := t.TempDir()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "missing connector token", http.StatusUnauthorized)
	}))
	defer server.Close()
	if err := os.WriteFile(filepath.Join(workspace, "reasonix.toml"), []byte(fmt.Sprintf(`
[[plugins]]
name = "cognee"
type = "http"
url = %q
`, server.URL)), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.LoadForRoot(workspace)
	if err != nil || len(cfg.Plugins) != 1 {
		t.Fatalf("load Cognee config = (%+v, %v)", cfg.Plugins, err)
	}
	if err := config.DefaultMCPActivationStore().SetServerEnabled(cfg.Plugins[0], workspace, true); err != nil {
		t.Fatalf("enable Cognee MCP: %v", err)
	}
	host := plugin.NewHost()
	defer host.Close()
	ctrl := newOwnedTestController(t, Options{Host: host, WorkspaceRoot: workspace})
	defer ctrl.Close()
	_, notice := ctrl.cogneeGuidanceContext(context.Background(), "query")
	if !strings.Contains(notice, "401") || !strings.Contains(notice, "connector token") {
		t.Fatalf("notice = %q, want authentication status and connector token guidance", notice)
	}
}

func TestValidateCogneeDesignSchemas(t *testing.T) {
	guidance := json.RawMessage(`{"type":"object","properties":{"task":{"type":"string"},"categories":{"type":"array","items":{"type":"string"}},"limit":{"type":"integer","minimum":1,"maximum":100}},"required":["task"]}`)
	got, err := validateCogneeSchema(guidance, "get_guidance")
	if err != nil || got.queryField != "task" {
		t.Fatalf("guidance schema = (%+v, %v), want task input", got, err)
	}
	search, err := validateCogneeSchema(json.RawMessage(`{"type":"object","properties":{"query":{"type":"string"},"categories":{"type":"array","items":{"type":"string"}},"source":{"type":"string"},"since":{"type":"string"},"limit":{"type":"integer","minimum":1,"maximum":100}},"required":["query"]}`), "search_memory")
	if err != nil || search.queryField != "query" {
		t.Fatalf("search schema = (%+v, %v), want query input", search, err)
	}
	if _, err := validateCogneeSchema(json.RawMessage(`{"type":"object","properties":{"query":{"type":"string"}}}`), "get_guidance"); err == nil {
		t.Fatal("get_guidance schema without task was accepted")
	}
	if _, err := validateCogneeSchema(json.RawMessage(`{"type":"object","properties":{"task":{"type":"string"}}}`), "search_memory"); err == nil {
		t.Fatal("search_memory schema without query was accepted")
	}
	proposal := json.RawMessage(`{"type":"object","properties":{"content":{"type":"string"},"category":{"type":"string"},"target_scope":{"type":"string","enum":["private","global"]},"source":{"type":"string"},"memory_id":{"type":"string"},"reason":{"type":"string"}}}`)
	got, err = validateCogneeSchema(proposal, "propose_memory")
	if err != nil || got.contentField != "content" || got.scopeField != "target_scope" {
		t.Fatalf("proposal schema = (%+v, %v), want content and target_scope", got, err)
	}
	unsafe := json.RawMessage(`{"type":"object","properties":{"content":{"type":"string"},"category":{"type":"string"},"target_scope":{"type":"string","enum":["workspace"]},"source":{"type":"string"}}}`)
	if _, err := validateCogneeSchema(unsafe, "propose_memory"); err == nil {
		t.Fatal("proposal schema without authorized private/global scope was accepted")
	}
	nullableScope := json.RawMessage(`{"type":"object","properties":{"content":{"type":"string"},"category":{"type":"string"},"target_scope":{"anyOf":[{"type":"string","enum":["workspace"]},{"type":"null"}]},"source":{"type":"string"}}}`)
	if _, err := validateCogneeSchema(nullableScope, "propose_memory"); err == nil {
		t.Fatal("nullable scope schema without private/global values was accepted")
	}
	for _, legacy := range []string{"recall", "remember"} {
		if _, err := validateCogneeSchema(json.RawMessage(`{"type":"object","properties":{"query":{"type":"string"},"data":{"type":"string"},"dataset_name":{"type":"string"}}}`), legacy); err == nil {
			t.Errorf("legacy tool %q was accepted by the new design adapter", legacy)
		}
	}
}

func TestCogneeTaskRecordRedactsBoundsAndRejectsTrivialOutcome(t *testing.T) {
	record := buildCogneeTaskRecord("Implement memory with api_key=secret-value", "Updated E:\\private\\repo\\internal\\memory.go and verified session recall behavior", `E:\private\repo`, []string{"verification"})
	if record == "" || strings.Contains(record, "secret-value") || strings.Contains(record, `E:\private\repo`) {
		t.Fatalf("Cognee record was empty or unsafe: %q", record)
	}
	if got := buildCogneeTaskRecord("ok", "done", "", nil); got != "" {
		t.Fatalf("trivial record = %q, want empty", got)
	}
	if got := buildCogneeTaskRecord(strings.Repeat("任务", 3000), strings.Repeat("完成", 3000), "", nil); len(got) > maxCogneeRememberBytes {
		t.Fatalf("record bytes = %d, want <= %d", len(got), maxCogneeRememberBytes)
	}
	if got := buildCogneeTaskRecord("Fix workspace issue", "Updated /srv/reasonix/internal/a.go", "/srv/reasonix", []string{"workspace_mutation"}); strings.Contains(got, "/srv/reasonix") {
		t.Fatalf("record contains absolute workspace path: %q", got)
	}
}

func TestCogneeSummaryFailureNoticeIncludesRedactedCause(t *testing.T) {
	notice := cogneeSummaryFailureNotice(fmt.Errorf("provider timeout api_key=secret-value at C:\\Users\\Alice\\repo"))
	if !strings.Contains(notice, "provider timeout") || strings.Contains(notice, "secret-value") || strings.Contains(notice, `C:\Users\Alice\repo`) {
		t.Fatalf("summary failure notice = %q, want a useful redacted cause", notice)
	}
}

func TestCogneeMemoryEligibilitySeparatesSessionAndGraph(t *testing.T) {
	cases := []struct {
		name     string
		snapshot agent.CompletionSnapshot
		record   string
		session  bool
		graph    bool
	}{
		{name: "no todos useful session", record: "useful result", session: true},
		{name: "completed todos and verified graph", snapshot: agent.CompletionSnapshot{TodosWritten: true, TodosComplete: true, HasVerifiedEvidence: true}, record: "useful result", session: true, graph: true},
		{name: "mutation graph without verifier", snapshot: agent.CompletionSnapshot{HasObservedMutation: true}, record: "useful result", session: true, graph: true},
		{name: "incomplete todos block both", snapshot: agent.CompletionSnapshot{TodosWritten: true, TodosComplete: false, HasVerifiedEvidence: true}, record: "useful result"},
		{name: "session only without durable evidence", record: "useful result", session: true},
		{name: "empty record", snapshot: agent.CompletionSnapshot{HasVerifiedEvidence: true}, session: false, graph: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			session, graph := cogneeMemoryEligibility(tc.snapshot, tc.record)
			if session != tc.session || graph != tc.graph {
				t.Fatalf("eligibility = (%v, %v), want (%v, %v)", session, graph, tc.session, tc.graph)
			}
		})
	}
}
