import type { GraphEdge, GraphNode, WireEvent } from "./wire";

// The scripted turn MockPort replays. It lives apart from the port because it is
// content, not transport: every card the UI can draw needs a beat here, or that
// rendering path is never seen during development.
export interface Beat {
  wait: number;
  ev: WireEvent;
}

// contextTokens is derived, not written into each beat: the kernel computes it
// from the same two strings, so hand-authored numbers here would drift the
// moment a fixture's output is edited.
const tool = (name: string, over: Partial<WireEvent["tool"] & object> = {}): WireEvent["tool"] => {
  const t = { id: name + "-" + Math.floor(performance.now()), name, readOnly: true, ...over };
  return { ...t, contextTokens: t.contextTokens ?? estimateTokens(t.args) + estimateTokens(t.output) };
};

// Mirrors internal/tokencount.Text: Latin near four bytes per token, CJK near one.
function estimateTokens(s?: string): number {
  let narrow = 0;
  let wide = 0;
  for (const ch of s ?? "") (ch.codePointAt(0)! < 128 ? narrow++ : wide++);
  return Math.ceil(narrow / 4) + wide;
}

const usage = (hit: number, miss: number, out: number, source = "executor"): WireEvent => ({
  kind: "usage",
  usage: {
    promptTokens: hit + miss,
    completionTokens: out,
    totalTokens: hit + miss + out,
    cacheHitTokens: hit,
    cacheMissTokens: miss,
    sessionCacheHitTokens: hit,
    sessionCacheMissTokens: miss,
    source,
    currency: "¥",
  },
});

// The run graph the kernel publishes alongside the stream. Timestamps are
// fabricated off one base rather than read from a clock: the timeline draws
// offsets from the earliest node, so what matters is the spacing, and a fixture
// that moved with the wall clock could never be compared against itself.
const T0 = 1_756_000_000_000;
const node = (id: string, over: Partial<GraphNode> = {}): GraphNode => ({ id, kind: "worker", ...over });
const graph = (nodes: GraphNode[], edges: GraphEdge[] = []): WireEvent => ({ kind: "graph_delta", graph: { nodes, edges } });

// What the kernel says a turn is about: the authored turn its message opens and
// the index that message takes. A mock session is a system message and one
// user/assistant pair per turn — the number matters only in that the checkpoint
// list and the turn's own event derive it the same way, or mock mode's rewind
// entries name nothing.
export const mockMsgIndex = (turn: number) => turn * 2 - 1;
export const mockTurnStart = (turn: number) => ({ authoredTurn: turn, msgIndex: mockMsgIndex(turn) });

export const SCRIPT: Beat[] = [
  { wait: 300, ev: { kind: "turn_started" } },
  {
    wait: 900,
    ev: {
      kind: "reasoning",
      text: "401 有两种可能：key 真的失效，或者网关瞬时态。两种处置完全相反，先别改代码。",
    },
  },
  {
    wait: 700,
    ev: {
      kind: "text",
      text: "先翻历史 —— #3146 和 #4106 都记着这是网关瞬时态，我们从不删 key。",
    },
  },
  // The kernel seals a finished assistant message with its own event. Without
  // it the card never leaves "思考中…", which is a state the real stream never
  // holds once the answer has arrived.
  { wait: 100, ev: { kind: "message" } },
  { wait: 100, ev: usage(2100, 180, 40) },
  { wait: 500, ev: { kind: "tool_dispatch", tool: tool("todo_write", { id: "td1" }) } },
  {
    wait: 600,
    ev: {
      kind: "tool_result",
      // Todos ride the arguments; the output is a receipt. Putting them in the
      // output left both the card and the rail with no plan to draw.
      tool: tool("todo_write", {
        id: "td1",
        args: JSON.stringify({
          todos: [
            // Long enough to wrap in the 296px rail: a one-line step never shows
            // whether the strike-through covers every line it should.
            { content: "翻 #3146 / #4106 的历史结论，确认当年是按网关瞬时态结的案", status: "completed" },
            { content: "定位 provider 侧重试路径", status: "completed" },
            { content: "用真实 curl 做 A/B 复现", status: "in_progress" },
            { content: "确认是否网关瞬时态", status: "pending" },
            { content: "给修法，不动 key 存储", status: "pending" },
          ],
        }),
        output: "Todos updated: 5 total",
      }),
    },
  },
  // Three reads in a row: the spec collapses a run into one manifest, so the
  // fixture has to produce a run. Outputs carry read_file's real line numbering.
  ...[
    ["internal/provider/retry.go", 203],
    ["internal/config/credentials.go", 88],
    ["internal/agent/agent.go", 412],
  ].flatMap(([path, n], i): Beat[] => [
    {
      wait: 400,
      ev: { kind: "tool_dispatch", tool: tool("read_file", { id: `rd${i}`, args: JSON.stringify({ path }) }) },
    },
    {
      wait: 500,
      ev: {
        kind: "tool_result",
        tool: tool("read_file", {
          id: `rd${i}`,
          args: JSON.stringify({ path }),
          output: Array.from({ length: Number(n) }, (_, k) => `${String(k + 1).padStart(3)}→// ${path}:${k + 1}`).join("\n"),
          durationMs: 210 + i * 40,
        }),
      },
    },
  ]),
  { wait: 100, ev: usage(12400, 0, 120) },
  // ls: a directory is "name/" with no size, a file is "name<TAB>bytes".
  {
    wait: 500,
    ev: {
      kind: "tool_result",
      tool: tool("ls", {
        id: "ls1",
        args: JSON.stringify({ path: "internal/provider" }),
        output: ["openai/", "anthropic/", "retry.go\t8420", "retry_test.go\t11304", "provider.go\t5177"].join("\n"),
        durationMs: 60,
      }),
    },
  },
  // grep: "path:line:text" exactly as internal/tool/builtin/grep.go formats it,
  // trailing truncation note included.
  {
    wait: 500,
    ev: {
      kind: "tool_result",
      tool: tool("grep", {
        id: "gr1",
        args: JSON.stringify({ pattern: "forget\\(provider\\)" }),
        output: [
          "internal/config/credentials.go:205:\ts.forget(provider)",
          "internal/provider/retry.go:88:\t\t\tstore.forget(provider)",
          "internal/agent/agent.go:1841:\t// 瞬时 401 不该走 forget(provider)",
          "... (truncated at 3 matches)",
        ].join("\n"),
        durationMs: 140,
      }),
    },
  },
  // An external service answering, and a named skill running as a subagent. Both
  // render differently from a built-in call — the server badge and the nested
  // trace — and neither had a fixture, so neither was ever seen in dev.
  {
    wait: 450,
    ev: {
      kind: "tool_result",
      tool: tool("use_capability", {
        id: "mcp1",
        resolvedName: "mcp__time__get_current_time",
        args: JSON.stringify({ timezone: "Asia/Shanghai" }),
        output: "2026-08-13T14:22:07+08:00",
        durationMs: 120,
      }),
    },
  },
  // A search the provider ran itself. Its result arrives as the listing the
  // kernel hands the model, and that listing used to land in the answer text —
  // forty lines of links that pushed the actual answer off the screen.
  {
    wait: 500,
    ev: {
      kind: "tool_result",
      tool: tool("web_search", {
        id: "ws1",
        args: JSON.stringify({ query: "mimo gateway intermittent 401 invalid api key" }),
        output: [
          "",
          "- **Gateway returns 401 sporadically under burst — token bucket race**",
          "  <https://github.com/xiaomimimo/MiMo/issues/218>",
          "- **MiMo API — 错误码与重试建议**",
          "  <https://platform.xiaomi.com/docs/mimo/errors>",
          "- **为什么不该在收到 401 时清除用户 key**",
          "  <https://reasonix.io/blog/never-delete-keys>",
          "",
        ].join("\n"),
        durationMs: 1200,
      }),
    },
  },
  // Only thirteen tools are provider-visible; everything else — web_fetch, task,
  // grep — is reached through use_capability. A card that reads the proxy as
  // "MCP" therefore mislabels most of the toolset, and the fixture had no
  // builtin-through-the-proxy beat to catch it.
  {
    wait: 450,
    ev: {
      kind: "tool_result",
      tool: tool("use_capability", {
        id: "wf1",
        resolvedName: "web_fetch",
        args: JSON.stringify({ url: "platform.xiaomi.com/docs/mimo/errors" }),
        output: "401 在网关层可能为瞬时态。建议对 401 做一次退避重试后再判定为凭据失效。",
        durationMs: 640,
      }),
    },
  },
  {
    wait: 200,
    ev: graph([
      node("plan1", { label: "读历史与现场", profile: "planner", model: "deepseek-v4", state: "completed",
        startedAt: T0, endedAt: T0 + 12_000 }),
    ]),
  },
  {
    wait: 400,
    ev: {
      kind: "tool_dispatch",
      tool: tool("use_capability", { id: "tk1", resolvedName: "task", profile: { name: "security-review" } }),
    },
  },
  {
    wait: 100,
    ev: graph(
      [node("tk1", { label: "只读地过一遍安全面", profile: "security-review", model: "deepseek-v4-pro",
        grant: "read", state: "running", startedAt: T0 + 12_400 })],
      [{ from: "plan1", to: "tk1", kind: "depends" }],
    ),
  },
  {
    wait: 500,
    ev: {
      kind: "tool_result",
      tool: tool("grep", {
        id: "tk1-a", parentId: "tk1",
        args: JSON.stringify({ pattern: "api_key" }),
        output: "internal/config/credentials.go:41:\tKey string `toml:\"api_key\"`",
        durationMs: 90,
      }),
    },
  },
  // A delegate writing its own todo list. It must not reach the rail: this beat
  // is the regression guard for the plan flipping to a subagent's steps mid-run.
  {
    wait: 300,
    ev: {
      kind: "tool_result",
      tool: tool("todo_write", {
        id: "tk1-b",
        parentId: "tk1",
        args: JSON.stringify({
          todos: [
            { content: "列出所有读 key 的位置", status: "completed" },
            { content: "确认没有新增写路径", status: "in_progress" },
          ],
        }),
        output: "Todos updated: 2 total",
      }),
    },
  },
  {
    wait: 600,
    ev: {
      kind: "tool_result",
      tool: tool("use_capability", {
        id: "tk1",
        resolvedName: "task",
        profile: { name: "security-review" },
        args: JSON.stringify({ description: "只读地过一遍安全面" }),
        output: "没有新增的密钥读写路径；退避补丁不触碰凭据存储。",
        durationMs: 8400,
      }),
    },
  },
  {
    wait: 100,
    ev: graph([
      node("tk1", { state: "completed", endedAt: T0 + 20_800, ref: "sessions/2026-08-25-security.jsonl" }),
      node("fleet1", { kind: "group", label: "复核这一轮改动", state: "running", startedAt: T0 + 21_000 }),
      node("w-retry", { parentId: "fleet1", label: "retry.go 退避面", profile: "review", grant: "read",
        state: "completed", startedAt: T0 + 21_100, endedAt: T0 + 27_400 }),
      node("w-cred", { parentId: "fleet1", label: "credentials.go 读写面", profile: "review", grant: "read",
        state: "running", startedAt: T0 + 21_100 }),
      node("w-tests", { parentId: "fleet1", label: "测试是否覆盖 401", profile: "review", grant: "read",
        state: "pending" }),
      node("w-lint", { parentId: "fleet1", label: "风格与命名", profile: "review", state: "adopted",
        ref: "sessions/2026-08-24-lint.jsonl" }),
    ], [
      { from: "fleet1", to: "w-retry", kind: "spawn" },
      { from: "fleet1", to: "w-cred", kind: "spawn" },
      { from: "fleet1", to: "w-tests", kind: "spawn" },
      { from: "fleet1", to: "w-lint", kind: "spawn" },
      { from: "w-retry", to: "w-tests", kind: "depends" },
      { from: "w-cred", to: "w-tests", kind: "depends" },
      { from: "w-retry", to: "w-tests", kind: "context" },
    ]),
  },
  {
    wait: 400,
    ev: {
      kind: "tool_dispatch",
      tool: tool("use_capability", { id: "fx1", resolvedName: "task", readOnly: false, profile: { name: "patch" } }),
    },
  },
  {
    wait: 100,
    ev: graph(
      [node("fx1", { label: "落地退避补丁", profile: "patch", model: "deepseek-v4-pro", grant: "write",
        state: "running", startedAt: T0 + 28_000 })],
      [{ from: "tk1", to: "fx1", kind: "context" }],
    ),
  },
  {
    wait: 500,
    ev: {
      kind: "tool_result",
      tool: tool("edit_file", {
        id: "fx1-a", parentId: "fx1", readOnly: false,
        args: JSON.stringify({ path: "internal/provider/retry.go" }),
        added: 9, removed: 2,
        diff: "@@ -61,2 +61,9 @@\n+\tif code == 401 && attempt < max {",
      }),
    },
  },
  {
    wait: 300,
    ev: {
      kind: "tool_result",
      tool: tool("edit_file", {
        id: "fx1-b", parentId: "fx1", readOnly: false,
        args: JSON.stringify({ path: "internal/provider/openai/streaming/chunk_decoder.go" }),
        added: 4, removed: 0,
        diff: "@@ -22,0 +22,4 @@\n+\t// 401 在这里也要走退避",
      }),
    },
  },
  {
    wait: 400,
    ev: {
      kind: "tool_result",
      tool: tool("use_capability", {
        id: "fx1", resolvedName: "task", readOnly: false, profile: { name: "patch" },
        args: JSON.stringify({ description: "把 401 退避补上" }),
        output: "retry.go 加了一次 401 退避，解码器那处跟上；没碰凭据存储。",
        durationMs: 5200,
      }),
    },
  },
  { wait: 100, ev: graph([node("fx1", { state: "completed", endedAt: T0 + 33_200 })]) },
  // The agent teaching itself something, which no other tool call does — and the
  // card for it had no fixture, so it was never seen in dev.
  {
    wait: 500,
    ev: {
      kind: "tool_result",
      tool: tool("remember", {
        id: "rm1",
        readOnly: false,
        args: JSON.stringify({
          name: "gateway-401-transient",
          title: "网关的 401 是瞬时态",
          description: "退避重试即可，不要删 key",
          scope: "project",
          activation: "relevant",
          body: "curl A/B 证实：同一个 key 同一分钟，401 只落在无退避的一组。",
        }),
        output: "Saved memory id=mem_01 revision=1 (project background) as gateway-401-transient",
        durationMs: 40,
      }),
    },
  },
  {
    wait: 400,
    ev: { kind: "notice", level: "warn", text: "工作树里有未提交的改动，退避补丁会叠在上面。" },
  },
  {
    wait: 600,
    ev: {
      kind: "guardian_assessment",
      guardian: {
        id: "g1",
        tool: "bash",
        subject: "curl A/B · 200 次并发",
        outcome: "放行",
        risk_level: "medium",
        rationale: "对外发压是可观测的副作用，不是纯读。范围收在单 key、单分钟、无写操作。",
      },
    },
  },
  {
    wait: 800,
    ev: {
      kind: "tool_result",
      tool: tool("bash", {
        readOnly: false,
        args: "curl A/B · 200 次并发 · 同一个 key",
        output: [
          "$ for i in $(seq 200); do curl -s -o /dev/null -w '%{http_code}\\n' \"$EP\"; done | sort | uniq -c",
          "",
          "A 组（无退避）",
          "    7 401",
          "  193 200",
          "",
          "B 组（200ms 退避后重试）",
          "    0 401",
          "  200 200",
          "",
          "同一个 key，同一分钟。401 只落在 A 组。",
        ].join("\n"),
        durationMs: 64000,
        execution: { kind: "shell", shell: "git-bash", platform: "windows", state: "completed", exitCode: 0 },
      }),
    },
  },
  { wait: 100, ev: usage(6100, 400, 180, "subagent") },
  // The fold names the bound that sent it. Without the pair the card falls
  // back to "a threshold was reached", which is the reading this fixture is
  // here to stop anyone shipping again.
  { wait: 400, ev: { kind: "compaction_started", compaction: { trigger: "auto", boundary: "economic", triggerTokens: 160_000 } } },
  {
    wait: 900,
    ev: {
      kind: "compaction_done",
      compaction: {
        trigger: "auto",
        boundary: "economic",
        triggerTokens: 160_000,
        messages: 34,
        summary: "401 已证实是网关瞬时态（curl A/B，A 组 7 次 B 组 0 次）。处置是退避重试，不删 key。",
        // A fold that dropped one of its own changes: the case the card exists
        // for, since the summary above reads just as finished either way.
        sourceTokens: 128_400,
        projectionTokens: 31_200,
        coverageRequired: 12,
        coverageMissing: 2,
      },
    },
  },
  // A write's payload streams in as arguments before any of it can be parsed.
  // Without these beats the card jumps from blank to a finished diff, which is
  // exactly the "did it hang?" the counter exists to answer.
  ...[420, 1180, 2360, 3910].map((argChars) => ({
    wait: 260,
    ev: {
      kind: "tool_dispatch" as const,
      tool: tool("edit_file", { id: "ed1", readOnly: false, partial: true, argChars }),
    },
  })),
  {
    wait: 600,
    ev: {
      kind: "approval_request",
      approval: {
        id: "a1",
        tool: "edit_file",
        subject: "internal/config/credentials.go · 第 205–210 行",
        reason: "这一步会写你的工作树。",
        kind: "tool",
      },
    },
  },
  // A second edit to the same file, and one under a deep path: the block used to
  // draw a row per call and let an end-ellipsis eat the filename, so neither was
  // ever seen with one shallow single-edit fixture.
  {
    wait: 300,
    ev: {
      kind: "tool_result",
      tool: tool("edit_file", {
        id: "ed0",
        readOnly: false,
        args: JSON.stringify({ path: "internal/provider/openai/streaming/chunk_decoder.go" }),
        added: 12,
        removed: 3,
        diff: "@@ -18,3 +18,4 @@\n+\t// retry once on a transient 401",
      }),
    },
  },
  {
    wait: 700,
    ev: {
      kind: "tool_result",
      tool: tool("edit_file", {
        id: "ed1",
        readOnly: false,
        args: JSON.stringify({ path: "internal/config/credentials.go" }),
        added: 6,
        removed: 4,
        diff: "@@ -205,6 +205,8 @@\n-\ts.forget(provider)\n+\treturn s.retryAfter(200 * time.Millisecond)",
      }),
    },
  },
  {
    wait: 400,
    ev: {
      kind: "tool_result",
      tool: tool("edit_file", {
        id: "ed2",
        readOnly: false,
        args: JSON.stringify({ path: "internal/config/credentials.go" }),
        added: 2,
        removed: 1,
        diff: "@@ -212,2 +212,3 @@\n+\t// #3146: 401 是瞬时态",
      }),
    },
  },
  {
    wait: 600,
    ev: {
      kind: "ask_request",
      ask: {
        id: "q1",
        questions: [
          {
            header: "退避范围",
            id: "q1-1",
            prompt: "curl A/B 证实是网关瞬时态。要不要一并给 provider 层加通用的 401 退避？",
            options: [
              { label: "一并改 retry.go", description: "根因是一个，影响所有 provider。" },
              { label: "只保留 credentials.go 这处", description: "范围守住不外溢。" },
            ],
          },
        ],
      },
    },
  },
  {
    wait: 800,
    ev: { kind: "text", text: "两处都改了，测试全绿。没动 key 存储的任何一行 —— 那条纪律保住了。" },
  },
  { wait: 100, ev: { kind: "message" } },
  { wait: 100, ev: usage(700, 90, 240) },
  {
    wait: 400,
    ev: {
      kind: "completion_summary",
      completion: {
        preset: "delivery",
        verdict: "complete",
        mutations: 2,
        checks_passed: 3,
        checks_failed: 0,
        checks_suppressed: 1,
        review: "passed",
        gap_kinds: ["suppressed"],
      },
    },
  },
  // A composed view standing next to the composer. It is in the script for the
  // same reason every other card is: a rendering path nobody sees while
  // developing is a rendering path that rots.
  {
    wait: 200,
    ev: {
      kind: "extension_surface",
      extension: {
        pluginId: "opengo", surfaceId: "quota", kind: "view",
        view: {
          slot: "composer-trailing",
          body: [
            { kind: "row", children: [
              { kind: "pip", tone: "ok" },
              { kind: "text", value: "套餐 62%", tone: "strong" },
              { kind: "text", value: "620/1000 · 7 天后重置", tone: "dim" },
            ] },
            { kind: "meter", progress: 0.62 },
          ],
        },
      },
    },
  },
  { wait: 300, ev: { kind: "turn_done" } },
];
