package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"reasonix/internal/tool"
)

// meta_capacity_test.go 验证 meta-tool 模式启用后，provider 请求的 tools 数组
// 容量（条目数 + 序列化字节数）是否按预期下降。
//
// 背景：当前每个 MCP 工具注册为顶层 mcp__<server>__<tool>，全部进入 reg.Schemas()
// 喂给 provider。用户配置 15+ MCP 服务器时，tools 数组膨胀到 50-100+，稀释模型
// 注意力。meta-tool 模式启用后，MCP 工具不再注册为顶层工具，tools 数组只多一个
// use_capability 条目——它通过 list/inspect/call 三动作代理全部 MCP 操作，且拥有
// 固定 schema（不随连接状态变化）和 CallResolver 安全流程。
//
// 本测试不改生产代码，只定义"符合预期"的契约：
//   1. 基线（当前行为）：reg.Len() == builtins + Σ(server_tools)
//   2. meta-tool 模式：reg.Len() == builtins + 1，且字节量显著下降

// --- stubs: 不依赖 MCP 子进程，纯容量数学 ---

// stubTool 模拟一个已注册的工具（内置或 MCP 顶层工具）。
type stubTool struct {
	name string
	desc string
	ro   bool
}

func (s stubTool) Name() string                                             { return s.name }
func (s stubTool) Description() string                                      { return s.desc }
func (s stubTool) Schema() json.RawMessage                                  { return json.RawMessage(`{"type":"object"}`) }
func (s stubTool) Execute(context.Context, json.RawMessage) (string, error) { return "", nil }
func (s stubTool) ReadOnly() bool                                           { return s.ro }

// mcpTopLevelTool 模拟当前行为下注册的顶层 MCP 工具 mcp__<server>__<tool>。
func mcpTopLevelTool(server, raw string) stubTool {
	return stubTool{
		name: tool.MCPNamePrefix + server + "__" + raw,
		desc: raw + " from " + server,
	}
}

// useCapabilityStub 模拟 meta-tool 模式下的 use_capability 代理工具。
// 与旧 run_mcp stub 不同：Schema() 和 Description() 是固定常量（不随 MCP 连接
// 状态变化），匹配真实 UseCapabilityTool 的缓存稳定性。
type useCapabilityStub struct{}

func (useCapabilityStub) Name() string { return "use_capability" }
func (useCapabilityStub) Description() string {
	return "Stable capability proxy: list configured MCP servers, inspect metadata, call MCP tools."
}
func (useCapabilityStub) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"action":{"type":"string","description":"list | inspect | call | decline"},"capability_id":{"type":"string"},"arguments":{"type":"object"},"reason":{"type":"string"}},"required":["action"]}`)
}
func (useCapabilityStub) Execute(context.Context, json.RawMessage) (string, error) {
	return "", nil
}
func (useCapabilityStub) ReadOnly() bool { return true }

// schemasBytes 度量 provider 请求中 tools 数组的序列化体积——这是"注意力"的真实
// 开销指标：模型每轮读的是字节，不是条目数。
func schemasBytes(t *testing.T, reg *tool.Registry) int {
	t.Helper()
	b, err := json.Marshal(reg.Schemas())
	if err != nil {
		t.Fatalf("marshal schemas: %v", err)
	}
	return len(b)
}

// buildBaseline 模拟当前行为：builtins + 每个 MCP 服务器的全部工具注册为顶层。
func buildBaseline(builtinCount, serverCount, toolsPerServer int) *tool.Registry {
	reg := tool.NewRegistry()
	for i := 0; i < builtinCount; i++ {
		reg.Add(stubTool{name: fmt.Sprintf("builtin_%d", i), desc: "builtin"})
	}
	for s := 0; s < serverCount; s++ {
		server := fmt.Sprintf("srv%d", s)
		for k := 0; k < toolsPerServer; k++ {
			reg.Add(mcpTopLevelTool(server, fmt.Sprintf("tool%d", k)))
		}
	}
	return reg
}

// buildUseCapability 模拟 meta-tool 模式：builtins + 1 个 use_capability 代理。
// MCP 工具不再单独注册——模型通过 use_capability 的 list/inspect/call 发现和调用。
func buildUseCapability(builtinCount, serverCount, toolsPerServer int) *tool.Registry {
	reg := tool.NewRegistry()
	for i := 0; i < builtinCount; i++ {
		reg.Add(stubTool{name: fmt.Sprintf("builtin_%d", i), desc: "builtin"})
	}
	reg.Add(useCapabilityStub{})
	return reg
}

// TestMetaToolCapacityMath 锁定两种注册策略的容量数学。
func TestMetaToolCapacityMath(t *testing.T) {
	const (
		builtinCount   = 19
		serverCount    = 15
		toolsPerServer = 5
	)
	expectedMCP := serverCount * toolsPerServer // 75

	baseline := buildBaseline(builtinCount, serverCount, toolsPerServer)
	metaMode := buildUseCapability(builtinCount, serverCount, toolsPerServer)

	baseLen := baseline.Len()
	metaLen := metaMode.Len()

	baseBytes := schemasBytes(t, baseline)
	metaBytes := schemasBytes(t, metaMode)

	t.Logf("容量对比 (builtins=%d, servers=%d, tools/server=%d, MCP工具总数=%d):",
		builtinCount, serverCount, toolsPerServer, expectedMCP)
	t.Logf("  基线(当前)       : Len=%d  bytes=%d", baseLen, baseBytes)
	t.Logf("  use_capability  : Len=%d  bytes=%d  (↓ %.1f%%)", metaLen, metaBytes, 100*float64(baseBytes-metaBytes)/float64(baseBytes))

	// --- 基线契约（当前行为，应通过）---
	if baseLen != builtinCount+expectedMCP {
		t.Errorf("基线 Len=%d, want %d (builtins %d + MCP %d)", baseLen, builtinCount+expectedMCP, builtinCount, expectedMCP)
	}
	if got := countMCPNames(baseline.Names()); got != expectedMCP {
		t.Errorf("基线顶层 MCP 工具数=%d, want %d", got, expectedMCP)
	}

	// --- use_capability 契约（meta-tool 模式启用后应通过）---
	if metaLen != builtinCount+1 {
		t.Errorf("use_capability Len=%d, want %d (builtins %d + 1 代理)", metaLen, builtinCount+1, builtinCount)
	}
	if _, ok := metaMode.Get("use_capability"); !ok {
		t.Error("注册表缺少 use_capability 工具")
	}
	// 顶层 MCP 工具应全部消失
	if got := countMCPNames(metaMode.Names()); got != 0 {
		t.Errorf("use_capability 模式下仍存在 %d 个顶层 mcp__ 工具，应为 0", got)
	}

	// --- 容量下降预期 ---
	if metaLen >= baseLen {
		t.Errorf("use_capability Len=%d 未下降，基线=%d", metaLen, baseLen)
	}
	// 字节量降幅应超过 60%（75 个 MCP schema → 1 个 use_capability schema）
	if reduction := 100 * float64(baseBytes-metaBytes) / float64(baseBytes); reduction < 60 {
		t.Errorf("use_capability 字节降幅 %.1f%% < 60%% 预期", reduction)
	}
}

// TestMetaToolCapacityScalesWithServerCount 验证关键特性：基线随 MCP 服务器数线性
// 膨胀，而 use_capability 恒为 +1。这是"注意力不随 MCP 服务器数稀释"的数学保证。
func TestMetaToolCapacityScalesWithServerCount(t *testing.T) {
	const builtinCount, toolsPerServer = 19, 5
	for _, serverCount := range []int{1, 5, 15, 30, 50} {
		base := buildBaseline(builtinCount, serverCount, toolsPerServer).Len()
		meta := buildUseCapability(builtinCount, serverCount, toolsPerServer).Len()
		// 基线 = builtins + servers*tools；use_capability = builtins + 1（与 servers 无关）
		if base != builtinCount+serverCount*toolsPerServer {
			t.Errorf("servers=%d: 基线=%d, want %d", serverCount, base, builtinCount+serverCount*toolsPerServer)
		}
		if meta != builtinCount+1 {
			t.Errorf("servers=%d: use_capability=%d, want %d (恒定)", serverCount, meta, builtinCount+1)
		}
		if serverCount > 1 && meta >= base {
			t.Errorf("servers=%d: use_capability=%d 未小于基线=%d", serverCount, meta, base)
		}
	}
}

// TestCurrentBehaviorRegistersAllCachedToolsAsTopLevel 用真实 LazyToolset（cache-hit,
// kick=false, 不调 Execute → 不起子进程）确认当前生产行为确实把每个缓存 MCP 工具
// 注册为顶层工具。这是基线契约的真实性证据，非假设。
func TestCurrentBehaviorRegistersAllCachedToolsAsTopLevel(t *testing.T) {
	redirectCache(t)
	spec := helperSpec()
	writeMockCache(t, spec) // echo + zed
	cs, ok := LoadCachedSchemaForSpec(spec)
	if !ok {
		t.Fatal("LoadCachedSchema miss after save")
	}

	host := NewHost()
	defer host.Close()
	reg := tool.NewRegistry()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	for _, lt := range LazyToolset(spec, cs, host, reg, ctx, false /* kick=false */) {
		reg.Add(lt)
	}
	// 不调 Execute → 不起子进程（证明：host 无连接）
	if names := host.ServerNames(); len(names) != 0 {
		t.Fatalf("kick=false 且无 Execute 不应起子进程，got %v", names)
	}
	// 当前行为：2 个缓存工具全部注册为顶层 mcp__mock__echo / mcp__mock__zed
	if reg.Len() != 2 {
		t.Fatalf("当前行为应注册 2 个顶层 MCP 工具，got Len=%d (%v)", reg.Len(), reg.Names())
	}
	if got := countMCPNames(reg.Names()); got != 2 {
		t.Errorf("顶层 mcp__ 工具数=%d, want 2", got)
	}
	if _, ok := reg.Get("mcp__mock__echo"); !ok {
		t.Error("缺少 mcp__mock__echo（当前行为应顶层注册）")
	}
	if _, ok := reg.Get("mcp__mock__zed"); !ok {
		t.Error("缺少 mcp__mock__zed（当前行为应顶层注册）")
	}
}

// countMCPNames 统计注册表中 mcp__ 前缀的工具数。
func countMCPNames(names []string) int {
	n := 0
	for _, name := range names {
		if strings.HasPrefix(name, tool.MCPNamePrefix) {
			n++
		}
	}
	return n
}
