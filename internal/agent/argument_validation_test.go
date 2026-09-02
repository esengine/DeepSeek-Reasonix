package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"reasonix/internal/capability"
	"reasonix/internal/event"
	"reasonix/internal/plugin"
	"reasonix/internal/provider"
	"reasonix/internal/skill"
	"reasonix/internal/tool"
)

func TestRunSkillMissingNestedArgumentsDoesNotStartSubagent(t *testing.T) {
	store := skillStoreWithArchitect(t)
	var started atomic.Int32
	reg := tool.NewRegistry()
	reg.Add(skill.NewRunSkillTool(store, func(context.Context, skill.Skill, string, skill.SubagentRunOptions) (string, error) {
		started.Add(1)
		return "ran", nil
	}))
	proxy := NewUseCapabilityTool(context.Background(), nil, nil, reg, nil, nil, func() capability.Catalog {
		return capability.Catalog{Entries: []capability.Entry{{ID: "skill:team-architect", Kind: capability.KindSkill, Name: "team-architect"}}}
	})
	reg.Add(proxy)
	a := New(nil, reg, NewSession("sys"), Options{}, event.Discard)
	out := a.executeOne(context.Background(), &a.turn, provider.ToolCall{
		ID: "bad-1", Name: "use_capability",
		Arguments: `{"action":"call","capability_id":"skill:team-architect","arguments":{}}`,
	})
	if started.Load() != 0 {
		t.Fatal("subagent started despite missing inner arguments")
	}
	if !strings.Contains(out.output, `"arguments":{"arguments"`) && !strings.Contains(out.output, `"arguments":"specific`) {
		t.Fatalf("missing nested example:\n%s", out.output)
	}
	if strings.Contains(out.output, "remote_dispatched=true") {
		t.Fatal("validation error must not claim remote dispatch")
	}
}

func TestMCPInvalidArgumentsDoNotCallRemote(t *testing.T) {
	t.Setenv("REASONIX_CACHE_HOME", t.TempDir())
	var toolCalls atomic.Int32
	server := requiredQueryMCPServer(t, &toolCalls)
	defer server.Close()
	ctx := context.Background()
	host := plugin.NewHost()
	defer host.Close()
	spec := plugin.Spec{Name: "svc", Type: "http", URL: server.URL, Authorized: true}
	runtime := NewMCPCapabilityRuntime(ctx, host, []plugin.Spec{spec}, tool.NewRegistry(), nil)
	proxy := runtime.NewFrontend(capability.NewLedger(), nil)
	reg := tool.NewRegistry()
	reg.Add(proxy)
	audit := &capability.Audit{}
	a := New(nil, reg, NewSession("sys"), Options{CapabilityAudit: audit}, event.Discard)
	out := a.executeOne(ctx, &a.turn, provider.ToolCall{
		ID: "mcp-bad", Name: "use_capability",
		Arguments: `{"action":"call","capability_id":"mcp-tool:svc/search","arguments":{}}`,
	})
	if toolCalls.Load() != 0 {
		t.Fatalf("tools/call = %d, want 0", toolCalls.Load())
	}
	if !strings.Contains(out.output, "argument validation failed") && !strings.Contains(out.output, "required") {
		t.Fatalf("expected validation failure, got %q", out.output)
	}
	if got := audit.Snapshot().Arguments.RemoteDispatch; got != 0 {
		t.Fatalf("remote dispatch audit = %d, want 0", got)
	}
	valid := a.executeOne(ctx, &a.turn, provider.ToolCall{
		ID: "mcp-good", Name: "use_capability",
		Arguments: `{"action":"call","capability_id":"mcp-tool:svc/search","arguments":{"q":"reasonix"}}`,
	})
	if valid.errMsg != "" {
		t.Fatalf("valid call = %+v", valid)
	}
	if got := audit.Snapshot().Arguments.RemoteDispatch; got != 1 {
		t.Fatalf("remote dispatch audit = %d, want 1", got)
	}
}

func TestUnavailableMCPCallPreservesResolutionReason(t *testing.T) {
	runtime := NewMCPCapabilityRuntime(t.Context(), nil, nil, tool.NewRegistry(), nil)
	proxy := runtime.NewFrontend(capability.NewLedger(), nil)
	reg := tool.NewRegistry()
	reg.Add(proxy)
	a := New(nil, reg, NewSession("sys"), Options{}, event.Discard)
	out := a.executeOne(t.Context(), &a.turn, provider.ToolCall{
		ID: "mcp-missing", Name: "use_capability",
		Arguments: `{"action":"call","capability_id":"mcp-tool:never-configured/ping","arguments":{}}`,
	})
	if !strings.Contains(out.output, `MCP server "never-configured" is not registered in this session`) {
		t.Fatalf("unavailable output = %q", out.output)
	}
	if strings.Contains(out.output, "argument validation failed") {
		t.Fatalf("unavailable resolution was validated as a concrete tool: %q", out.output)
	}
}

func TestSchemaErrorThirdCallIsBlocked(t *testing.T) {
	store := skillStoreWithArchitect(t)
	reg := tool.NewRegistry()
	reg.Add(skill.NewRunSkillTool(store, func(context.Context, skill.Skill, string, skill.SubagentRunOptions) (string, error) {
		t.Fatal("must not start")
		return "", nil
	}))
	proxy := NewUseCapabilityTool(context.Background(), nil, nil, reg, nil, nil, func() capability.Catalog {
		return capability.Catalog{Entries: []capability.Entry{{ID: "skill:team-architect", Kind: capability.KindSkill, Name: "team-architect"}}}
	})
	reg.Add(proxy)
	a := New(nil, reg, NewSession("sys"), Options{}, event.Discard)
	call := provider.ToolCall{Name: "use_capability", Arguments: `{"action":"call","capability_id":"skill:team-architect","arguments":{}}`}
	var last toolOutcome
	for i := 1; i <= 3; i++ {
		call.ID = "c" + string(rune('0'+i))
		last = a.executeOne(context.Background(), &a.turn, call)
	}
	if !last.blocked || !strings.Contains(last.output, "3 times") {
		t.Fatalf("third call = %+v", last)
	}
}

func TestSuccessfulInspectUnlocksSchemaLoopGuard(t *testing.T) {
	store := skillStoreWithArchitect(t)
	reg := tool.NewRegistry()
	reg.Add(skill.NewRunSkillTool(store, func(context.Context, skill.Skill, string, skill.SubagentRunOptions) (string, error) {
		t.Fatal("must not start")
		return "", nil
	}))
	proxy := NewUseCapabilityTool(context.Background(), nil, nil, reg, nil, nil, func() capability.Catalog {
		return capability.Catalog{Entries: []capability.Entry{{ID: "skill:team-architect", Kind: capability.KindSkill, Name: "team-architect"}}}
	})
	reg.Add(proxy)
	a := New(nil, reg, NewSession("sys"), Options{}, event.Discard)
	bad := provider.ToolCall{Name: "use_capability", Arguments: `{"action":"call","capability_id":"skill:team-architect","arguments":{}}`}
	for i := range 3 {
		bad.ID = fmt.Sprintf("bad-%d", i)
		_ = a.executeOne(context.Background(), &a.turn, bad)
	}
	inspected := a.executeOne(context.Background(), &a.turn, provider.ToolCall{
		ID: "inspect", Name: "use_capability", Arguments: `{"action":"inspect","capability_id":"skill:team-architect"}`,
	})
	if inspected.errMsg != "" || !strings.Contains(inspected.output, "input_schema") {
		t.Fatalf("inspect = %+v", inspected)
	}
	bad.ID = "after-inspect"
	after := a.executeOne(context.Background(), &a.turn, bad)
	if after.blocked || strings.Contains(after.output, "3 times") {
		t.Fatalf("inspect did not unlock schema guard: %+v", after)
	}
}

func TestSchemaErrorIncrementIsAtomic(t *testing.T) {
	var state turnLoopState
	const workers = 64
	var done sync.WaitGroup
	done.Add(workers)
	for range workers {
		go func() {
			defer done.Done()
			state.incrementSchemaError("sig", "skill:x")
		}()
	}
	done.Wait()
	record := state.incrementSchemaError("sig", "skill:x")
	if record.count != workers+1 {
		t.Fatalf("count = %d, want %d", record.count, workers+1)
	}
}

func requiredQueryMCPServer(t *testing.T, toolCalls *atomic.Int32) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			ID     *int   `json:"id"`
			Method string `json:"method"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		if request.ID == nil {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		var result any
		switch request.Method {
		case "initialize":
			result = map[string]any{"protocolVersion": "2024-11-05", "serverInfo": map[string]any{"name": "svc", "version": "1"}}
		case "tools/list":
			result = map[string]any{"tools": []map[string]any{{
				"name": "search", "description": "search",
				"inputSchema": map[string]any{
					"type":       "object",
					"properties": map[string]any{"q": map[string]any{"type": "string"}},
					"required":   []string{"q"},
				},
				"annotations": map[string]any{"readOnlyHint": true},
			}}}
		case "tools/call":
			toolCalls.Add(1)
			result = map[string]any{"content": []map[string]any{{"type": "text", "text": "nope"}}}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": *request.ID, "result": result})
	}))
}
