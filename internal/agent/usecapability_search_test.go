package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"reasonix/internal/capability"
	"reasonix/internal/plugin"
	"reasonix/internal/skill"
	"reasonix/internal/tool"
)

func TestSearchCapabilitiesRanksDeterministicallyAndStaysLocal(t *testing.T) {
	t.Setenv("REASONIX_CACHE_HOME", t.TempDir())
	spec := plugin.Spec{Name: "teamcity", Type: "stdio", Command: "tc", Authorized: true}
	cached := make([]plugin.CachedTool, 100)
	var schemaBytes int
	for i := range cached {
		schema := json.RawMessage(fmt.Sprintf(`{"type":"object","properties":{"q%03d":{"type":"string","description":"query field %03d %s"}}}`, i, i, strings.Repeat("x", 800)))
		schemaBytes += len(schema)
		cached[i] = plugin.CachedTool{
			Name:        fmt.Sprintf("tool_%03d", i),
			Description: fmt.Sprintf("TeamCity catalog entry %03d", i),
			Schema:      schema,
			ReadOnly:    true,
		}
	}
	if schemaBytes < 80<<10 {
		t.Fatalf("fixture schema bytes = %d, want ~88KB class", schemaBytes)
	}
	if err := plugin.SaveCachedSchema(spec.Name, plugin.CachedSchema{CacheKey: plugin.SchemaCacheKey(spec), Tools: cached}); err != nil {
		t.Fatal(err)
	}
	plugin.ResetProtocolMetricsForTest()
	host := plugin.NewHost()
	defer host.Close()
	reg := tool.NewRegistry()
	catalog := func() capability.Catalog {
		entries := make([]capability.Entry, 0, len(cached)+1)
		entries = append(entries, capability.Entry{ID: "mcp-server:teamcity", Kind: capability.KindMCPServer, Name: "teamcity", Source: "teamcity", ConnectName: "teamcity"})
		for _, ct := range cached {
			entries = append(entries, capability.Entry{
				ID: "mcp-tool:teamcity/" + ct.Name, Kind: capability.KindMCPTool, Name: ct.Name,
				Source: "teamcity", Description: ct.Description, ReadOnly: true,
			})
		}
		return capability.Catalog{Entries: entries}
	}
	runtime := NewMCPCapabilityRuntime(context.Background(), host, []plugin.Spec{spec}, reg, catalog)
	proxy := runtime.NewFrontend(nil, nil)

	out, err := proxy.Execute(context.Background(), json.RawMessage(`{"action":"search","query":"tool_007","limit":5}`))
	if err != nil {
		t.Fatal(err)
	}
	if plugin.ToolsListCount() != 0 {
		t.Fatalf("search issued remote tools/list")
	}
	var payload struct {
		Results []struct {
			CapabilityID string `json:"capability_id"`
		} `json:"results"`
	}
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("decode search: %v\n%s", err, out)
	}
	if len(payload.Results) == 0 || len(payload.Results) > 5 {
		t.Fatalf("results = %d, want 1..5", len(payload.Results))
	}
	if payload.Results[0].CapabilityID != "mcp-tool:teamcity/tool_007" {
		t.Fatalf("top result = %s", payload.Results[0].CapabilityID)
	}

	inspect, err := proxy.Execute(context.Background(), json.RawMessage(`{"action":"inspect","capability_id":"mcp-server:teamcity"}`))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(inspect, `"input_schema"`) && strings.Count(inspect, "query field") > 3 {
		t.Fatalf("server inspect included full schemas:\n%s", inspect)
	}
	if plugin.ToolsListCount() != 0 {
		t.Fatalf("inspect issued remote tools/list")
	}
}

func TestNewSessionSearchDoesNotRelistSharedHost(t *testing.T) {
	t.Setenv("REASONIX_CACHE_HOME", t.TempDir())
	var calls atomic.Int32
	server := readonlyMCPServer(t, "shared", &calls)
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	host := plugin.NewHost()
	defer host.Close()
	spec := plugin.Spec{Name: "shared", Type: "http", URL: server.URL, Authorized: true}
	plugin.ResetProtocolMetricsForTest()
	if _, err := host.Add(ctx, spec); err != nil {
		t.Fatalf("connect: %v", err)
	}
	listed := plugin.ToolsListCount()
	if listed == 0 {
		t.Fatal("expected initial tools/list")
	}
	runtime := NewMCPCapabilityRuntime(ctx, host, []plugin.Spec{spec}, tool.NewRegistry(), func() capability.Catalog {
		return capability.Catalog{Entries: []capability.Entry{{
			ID: "mcp-server:shared", Kind: capability.KindMCPServer, Name: "shared", Source: "shared", ConnectName: "shared",
		}, {
			ID: "mcp-tool:shared/search", Kind: capability.KindMCPTool, Name: "search", Source: "shared", ReadOnly: true,
		}}}
	})
	first := runtime.NewFrontend(nil, nil)
	second := runtime.NewFrontend(nil, nil)
	if _, err := first.Execute(ctx, json.RawMessage(`{"action":"search","query":"search"}`)); err != nil {
		t.Fatalf("first session search: %v", err)
	}
	if _, err := second.Execute(ctx, json.RawMessage(`{"action":"inspect","capability_id":"mcp-server:shared"}`)); err != nil {
		t.Fatalf("second session inspect: %v", err)
	}
	if plugin.ToolsListCount() != listed {
		t.Fatalf("new session re-listed tools: before=%d after=%d", listed, plugin.ToolsListCount())
	}
}

func TestInspectSkillReturnsNestedArgumentsContract(t *testing.T) {
	store := skillStoreWithArchitect(t)
	reg := tool.NewRegistry()
	reg.Add(mustSkillRunTool(t, store))
	proxy := NewUseCapabilityTool(context.Background(), nil, nil, reg, nil, nil, func() capability.Catalog {
		return capability.Catalog{Entries: []capability.Entry{{
			ID: "skill:team-architect", Kind: capability.KindSkill, Name: "team-architect",
		}}}
	})
	out, err := proxy.Execute(context.Background(), json.RawMessage(`{"action":"inspect","capability_id":"skill:team-architect"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `"arguments"`) || !strings.Contains(out, `"call_example"`) {
		t.Fatalf("inspect missing nested contract:\n%s", out)
	}
}

func skillStoreWithArchitect(t *testing.T) *skill.Store {
	t.Helper()
	store := skill.New(skill.Options{HomeDir: t.TempDir(), DisableBuiltins: true})
	content := skill.RenderSkillFile(skill.SkillFileOptions{
		Name: "team-architect", Description: "architecture review", Body: "review architecture",
		RunAs: skill.RunSubagent,
	})
	if _, err := store.CreateWithContent("team-architect", skill.ScopeGlobal, content); err != nil {
		t.Fatal(err)
	}
	return store
}

func mustSkillRunTool(t *testing.T, store *skill.Store) tool.Tool {
	t.Helper()
	return skill.NewRunSkillTool(store, func(context.Context, skill.Skill, string, skill.SubagentRunOptions) (string, error) {
		t.Fatal("subagent must not start during inspect/validation")
		return "", nil
	})
}
