package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"testing"

	"reasonix/internal/tool"
)

// meta_capacity_test.go 验证引入 run_mcp 元工具后，provider 请求的 tools 数组
// 容量（条目数 + 序列化字节数）是否按预期下降。
//
// 背景：当前每个 MCP 工具注册为顶层 mcp__<server>__<tool>，全部进入 reg.Schemas()
// 喂给 provider。用户配置 15+ MCP 服务器时，tools 数组膨胀到 50-100+，稀释模型
// 注意力（"AI attention limitations"）。引入 run_mcp 元工具后，MCP 工具不再注册为
// 顶层工具，tools 数组只多一个 run_mcp 条目。
//
// 本测试不改生产代码，只定义"符合预期"的契约：
//   1. 基线（当前行为）：reg.Len() == builtins + Σ(server_tools)
//   2. 单一元工具：reg.Len() == builtins + 1，且字节量显著下降
//   3. 每服务器元工具（中间设计）：reg.Len() == builtins + serverCount
// 三种设计的容量数学在此锁定，实现落地后直接复用断言。

// --- stubs: 不依赖 MCP 子进程，纯容量数学 ---

// stubTool 模拟一个已注册的工具（内置或 MCP 顶层工具）。
type stubTool struct {
	name string
	desc string
	ro   bool
}

func (s stubTool) Name() string                    { return s.name }
func (s stubTool) Description() string             { return s.desc }
func (s stubTool) Schema() json.RawMessage          { return json.RawMessage(`{"type":"object"}`) }
func (s stubTool) Execute(context.Context, json.RawMessage) (string, error) { return "", nil }
func (s stubTool) ReadOnly() bool                  { return s.ro }

// mcpTopLevelTool 模拟当前行为下注册的顶层 MCP 工具 mcp__<server>__<tool>。
func mcpTopLevelTool(server, raw string) stubTool {
	return stubTool{
		name: tool.MCPNamePrefix + server + "__" + raw,
		desc: raw + " from " + server,
	}
}

// metaToolStub 是拟引入的 run_mcp 元工具形状：单一顶层工具，按 (server,tool) 派发。
// MCP 工具不再单独注册——模型通过此工具的描述发现可用的 server_name/tool_name 组合。
//
// Description() 动态拼接"服务器→工具名"映射，避免模型调用时混淆 server_name 与
// tool_name：描述明确标注每个 server_name 下可用的 tool_name 清单，并给出调用格式。
// server_name 用引号标注（%q），与裸逗号分隔的 tool_name 视觉区分，防止模型把工具名
// 当成服务器名传入 server_name 字段。
type metaToolStub struct {
	// serverTools 映射 server_name → 该服务器下可用 tool_name 列表。
	// Description() 按 server_name 排序输出，保证跨运行稳定（不随 map 迭代序抖动），
	// 也让 cache 命中可复现——provider prefix cache 对描述字节序敏感。
	serverTools map[string][]string
}

func (m metaToolStub) Name() string { return "run_mcp" }

func (m metaToolStub) Description() string {
	if len(m.serverTools) == 0 {
		return "Call an MCP tool by server_name and tool_name. No MCP servers are currently available."
	}
	var b strings.Builder
	b.WriteString("Call an MCP tool. Pass server_name and tool_name using the EXACT strings below.\n")
	b.WriteString("server_name -> available tool_name values (server_name is the quoted key; tool_name is one of the comma-separated values after the colon):\n")
	servers := make([]string, 0, len(m.serverTools))
	for s := range m.serverTools {
		servers = append(servers, s)
	}
	sort.Strings(servers)
	for _, s := range servers {
		// 每行: `  "srv0": tool0, tool1, tool2` —— server 带引号标注，工具逗号分隔
		fmt.Fprintf(&b, "  %q: %s\n", s, strings.Join(m.serverTools[s], ", "))
	}
	b.WriteString(`Call with {"server_name": <one of the quoted keys above>, "tool_name": <one of that server's listed tools>, "args": <object>}. Do not swap server_name and tool_name.`)
	return b.String()
}

func (m metaToolStub) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"server_name":{"type":"string"},"tool_name":{"type":"string"},"args":{"type":"object"}},"required":["server_name","tool_name"]}`)
}
func (m metaToolStub) Execute(context.Context, json.RawMessage) (string, error) {
	return "", nil // 真实派发逻辑在实现层
}
func (m metaToolStub) ReadOnly() bool { return false }

// perServerMetaTool 是中间设计：每个服务器一个 mcp__<server> 派发器（非全局单一）。
type perServerMetaTool struct{ server string }

func (p perServerMetaTool) Name() string                    { return tool.MCPNamePrefix + p.server }
func (p perServerMetaTool) Description() string             { return "Dispatch to MCP server " + p.server }
func (p perServerMetaTool) Schema() json.RawMessage         { return json.RawMessage(`{"type":"object","properties":{"tool_name":{"type":"string"},"args":{"type":"object"}},"required":["tool_name"]}`) }
func (p perServerMetaTool) Execute(context.Context, json.RawMessage) (string, error) { return "", nil }
func (p perServerMetaTool) ReadOnly() bool                  { return false }

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

// buildSingleMeta 模拟引入全局 run_mcp 元工具：builtins + 1 个 run_mcp。
// serverTools 映射驱动 Description() 动态拼接，让模型看到每个服务器的工具清单。
func buildSingleMeta(builtinCount, serverCount, toolsPerServer int) *tool.Registry {
	reg := tool.NewRegistry()
	for i := 0; i < builtinCount; i++ {
		reg.Add(stubTool{name: fmt.Sprintf("builtin_%d", i), desc: "builtin"})
	}
	serverTools := make(map[string][]string, serverCount)
	for s := 0; s < serverCount; s++ {
		server := fmt.Sprintf("srv%d", s)
		tools := make([]string, toolsPerServer)
		for k := 0; k < toolsPerServer; k++ {
			tools[k] = fmt.Sprintf("tool%d", k)
		}
		serverTools[server] = tools
	}
	reg.Add(metaToolStub{serverTools: serverTools})
	return reg
}

// buildPerServerMeta 模拟中间设计：builtins + 每服务器一个派发器。
func buildPerServerMeta(builtinCount, serverCount, toolsPerServer int) *tool.Registry {
	reg := tool.NewRegistry()
	for i := 0; i < builtinCount; i++ {
		reg.Add(stubTool{name: fmt.Sprintf("builtin_%d", i), desc: "builtin"})
	}
	for s := 0; s < serverCount; s++ {
		reg.Add(perServerMetaTool{server: fmt.Sprintf("srv%d", s)})
	}
	return reg
}

// TestMetaToolDescriptionListsServerToolMapping 验证 run_mcp 描述动态拼接了
// "服务器→工具名"映射，且 server_name 与 tool_name 在描述中可区分，避免模型调用时
// 把 tool_name 当成 server_name 传入（用户核心关切：调用时混淆）。
func TestMetaToolDescriptionListsServerToolMapping(t *testing.T) {
	serverTools := map[string][]string{
		"srv0": {"echo", "zed"},
		"srv1": {"search", "fetch"},
	}
	desc := metaToolStub{serverTools: serverTools}.Description()

	// 1. 描述必须包含每个 server_name
	for s := range serverTools {
		if !strings.Contains(desc, s) {
			t.Errorf("描述缺少 server_name %q\n描述:\n%s", s, desc)
		}
	}
	// 2. 描述必须包含每个 tool_name
	for _, tools := range serverTools {
		for _, tl := range tools {
			if !strings.Contains(desc, tl) {
				t.Errorf("描述缺少 tool_name %q\n描述:\n%s", tl, desc)
			}
		}
	}
	// 3. 描述必须明确标注 server_name / tool_name 字段语义
	if !strings.Contains(desc, "server_name") || !strings.Contains(desc, "tool_name") {
		t.Errorf("描述未明确标注 server_name/tool_name 字段\n描述:\n%s", desc)
	}
	// 4. server_name 应带引号标注（%q），与裸 tool_name 视觉区分——这是防混淆的关键
	if !strings.Contains(desc, `"srv0"`) || !strings.Contains(desc, `"srv1"`) {
		t.Errorf("描述中 server_name 未带引号标注，易与 tool_name 混淆\n描述:\n%s", desc)
	}
	// 5. 描述必须给出调用格式，明确两个参数位置不可互换
	if !strings.Contains(desc, "Do not swap") {
		t.Errorf("描述缺少「参数不可互换」提示\n描述:\n%s", desc)
	}

	// 6. 空映射不应 panic，且给出明确提示（实现落地后服务器掉线时也会走此路径）
	emptyDesc := metaToolStub{serverTools: map[string][]string{}}.Description()
	if !strings.Contains(emptyDesc, "No MCP servers") {
		t.Errorf("空映射描述应提示无服务器，got:\n%s", emptyDesc)
	}

	// 7. 动态性：不同映射应产出不同描述（非硬编码），且反映输入
	descA := metaToolStub{serverTools: map[string][]string{"alpha": {"x"}}}.Description()
	descB := metaToolStub{serverTools: map[string][]string{"beta": {"y"}}}.Description()
	if descA == descB {
		t.Error("描述非动态：不同映射产出相同描述")
	}
	if !strings.Contains(descA, "alpha") || !strings.Contains(descB, "beta") {
		t.Error("描述未反映输入映射")
	}

	// 8. 稳定性：同一映射多次调用产出相同描述（排序消除 map 迭代序抖动）
	desc2 := metaToolStub{serverTools: serverTools}.Description()
	if desc != desc2 {
		t.Error("描述不稳定：同一映射两次调用产出不同结果（map 迭代序泄漏）")
	}

	t.Logf("run_mcp 描述示例:\n%s", desc)
}

// TestMetaToolCapacityMath 锁定三种注册策略的容量数学。实现落地后此测试直接验证
// "tools 数组容量是否按预期下降"。
func TestMetaToolCapacityMath(t *testing.T) {
	// 模拟用户实际配置：19 内置 + 15 MCP 服务器 × 平均 5 工具 = 94 顶层工具
	const (
		builtinCount  = 19
		serverCount   = 15
		toolsPerServer = 5
	)
	expectedMCP := serverCount * toolsPerServer // 75

	baseline := buildBaseline(builtinCount, serverCount, toolsPerServer)
	singleMeta := buildSingleMeta(builtinCount, serverCount, toolsPerServer)
	perServer := buildPerServerMeta(builtinCount, serverCount, toolsPerServer)

	baseLen := baseline.Len()
	singleLen := singleMeta.Len()
	perServerLen := perServer.Len()

	baseBytes := schemasBytes(t, baseline)
	singleBytes := schemasBytes(t, singleMeta)
	perServerBytes := schemasBytes(t, perServer)

	t.Logf("容量对比 (builtins=%d, servers=%d, tools/server=%d, MCP工具总数=%d):",
		builtinCount, serverCount, toolsPerServer, expectedMCP)
	t.Logf("  基线(当前)     : Len=%d  bytes=%d", baseLen, baseBytes)
	t.Logf("  全局run_mcp    : Len=%d  bytes=%d  (↓ %.1f%%)", singleLen, singleBytes, 100*float64(baseBytes-singleBytes)/float64(baseBytes))
	t.Logf("  每服务器派发器 : Len=%d  bytes=%d  (↓ %.1f%%)", perServerLen, perServerBytes, 100*float64(baseBytes-perServerBytes)/float64(baseBytes))

	// --- 基线契约（当前行为，应通过）---
	if baseLen != builtinCount+expectedMCP {
		t.Errorf("基线 Len=%d, want %d (builtins %d + MCP %d)", baseLen, builtinCount+expectedMCP, builtinCount, expectedMCP)
	}
	// 基线下每个 MCP 工具应是顶层 mcp__<server>__<tool>
	if got := countMCPNames(baseline.Names()); got != expectedMCP {
		t.Errorf("基线顶层 MCP 工具数=%d, want %d", got, expectedMCP)
	}

	// --- 全局 run_mcp 契约（引入元工具后应通过）---
	if singleLen != builtinCount+1 {
		t.Errorf("全局 run_mcp Len=%d, want %d (builtins %d + 1 元工具)", singleLen, builtinCount+1, builtinCount)
	}
	if _, ok := singleMeta.Get("run_mcp"); !ok {
		t.Error("全局 run_mcp 注册表缺少 run_mcp 工具")
	}
	// 顶层 MCP 工具应全部消失
	if got := countMCPNames(singleMeta.Names()); got != 0 {
		t.Errorf("全局 run_mcp 下仍存在 %d 个顶层 mcp__ 工具，应为 0", got)
	}

	// --- 每服务器派发器契约（中间设计）---
	if perServerLen != builtinCount+serverCount {
		t.Errorf("每服务器派发器 Len=%d, want %d (builtins %d + %d 服务器)", perServerLen, builtinCount+serverCount, builtinCount, serverCount)
	}

	// --- 容量下降预期 ---
	// 全局 run_mcp 的 tools 数组必须显著小于基线（MCP 工具越多降幅越大）。
	if singleLen >= baseLen {
		t.Errorf("全局 run_mcp Len=%d 未下降，基线=%d", singleLen, baseLen)
	}
	// 字节量降幅应超过 60%（75 个 MCP schema → 1 个 run_mcp schema）
	if reduction := 100 * float64(baseBytes-singleBytes) / float64(baseBytes); reduction < 60 {
		t.Errorf("全局 run_mcp 字节降幅 %.1f%% < 60%% 预期", reduction)
	}
	// 全局 run_mcp 必须优于每服务器派发器（servers 多时）
	if singleLen >= perServerLen && serverCount > 1 {
		t.Errorf("全局 run_mcp Len=%d 应小于每服务器派发器 Len=%d", singleLen, perServerLen)
	}
}

// TestMetaToolCapacityScalesWithServerCount 验证关键特性：基线随 MCP 服务器数线性
// 膨胀，而 run_mcp 恒为 +1。这是"注意力不随 MCP 服务器数稀释"的数学保证。
func TestMetaToolCapacityScalesWithServerCount(t *testing.T) {
	const builtinCount, toolsPerServer = 19, 5
	for _, serverCount := range []int{1, 5, 15, 30, 50} {
		base := buildBaseline(builtinCount, serverCount, toolsPerServer).Len()
		single := buildSingleMeta(builtinCount, serverCount, toolsPerServer).Len()
		// 基线 = builtins + servers*tools；run_mcp = builtins + 1（与 servers 无关）
		if base != builtinCount+serverCount*toolsPerServer {
			t.Errorf("servers=%d: 基线=%d, want %d", serverCount, base, builtinCount+serverCount*toolsPerServer)
		}
		if single != builtinCount+1 {
			t.Errorf("servers=%d: run_mcp=%d, want %d (恒定)", serverCount, single, builtinCount+1)
		}
		// 服务器越多，run_mcp 的相对优势越大
		if serverCount > 1 && single >= base {
			t.Errorf("servers=%d: run_mcp=%d 未小于基线=%d", serverCount, single, base)
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
