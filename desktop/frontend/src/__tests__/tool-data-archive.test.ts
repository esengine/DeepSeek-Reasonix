// Run: tsx src/__tests__/tool-data-archive.test.ts
//
// Verifies that the tool_result reducer archives ALL completed tools
// immediately: args are trimmed to 200 chars, output is set to undefined,
// and the dataArchived flag is set. Collapsed cards only keep tool name
// + command in memory; full data is loaded on demand via the backend.

import { initialState, reducer } from "../lib/useController";
import type { Item } from "../lib/useController";

type TestState = typeof initialState;
type ToolItem = Extract<Item, { kind: "tool" }>;

let passed = 0;
let failed = 0;

function eq<T>(a: T, b: T, label: string) {
  if (a === b) {
    process.stdout.write(`  PASS  ${label}\n`);
    passed += 1;
  } else {
    const expected = JSON.stringify(b) ?? String(b);
    const actual = JSON.stringify(a) ?? String(a);
    process.stdout.write(`  FAIL  ${label}: expected ${expected.slice(0, 120)}, got ${actual.slice(0, 120)}\n`);
    failed += 1;
  }
}

function ok(cond: boolean, label: string) {
  if (cond) {
    process.stdout.write(`  PASS  ${label}\n`);
    passed += 1;
  } else {
    process.stdout.write(`  FAIL  ${label}\n`);
    failed += 1;
  }
}

/** Run tool_dispatch + tool_result for each item and return final state. */
function addTools(state: TestState, count: number, argsLen = 5000, outputLen = 10000): TestState {
  let s = state;
  for (let i = 0; i < count; i++) {
    const id = `t${i}`;
    s = reducer(s, { type: "event", e: { kind: "turn_started" } });
    s = reducer(s, { type: "event", e: { kind: "tool_dispatch", tool: { id, name: "bash", args: "x".repeat(argsLen), readOnly: false } } });
    s = reducer(s, { type: "event", e: { kind: "tool_result", tool: { id, name: "bash", readOnly: false, output: "y".repeat(outputLen), durationMs: 100 } } });
  }
  return s;
}

function toolItems(s: TestState): ToolItem[] {
  return s.items.filter((it): it is ToolItem => it.kind === "tool");
}

console.log("\ntool data archiving on tool_result");

// ── Test 1: Every completed tool is archived immediately ──
{
  let s = addTools(initialState, 1, 5000, 10000);
  const tools = toolItems(s);
  ok(tools.length >= 1, "tool item exists after tool_result");
  ok(tools[0].dataArchived === true, "single tool is archived immediately");
  eq(tools[0].output, undefined, "output is dropped");
  ok((tools[0].args?.length ?? 0) <= 205, `args truncated to ≤200 chars (got ${tools[0].args?.length})`);
}

// ── Test 2: Multiple tools all archived (no threshold) ──
{
  let s = addTools(initialState, 50, 5000, 10000);
  const tools = toolItems(s);
  ok(tools.length >= 50, `${tools.length} tools present`);
  const allArchived = tools.every((t) => t.dataArchived === true);
  ok(allArchived, "all 50 tools archived immediately");
  const allNoOutput = tools.every((t) => t.output === undefined);
  ok(allNoOutput, "all tools have output dropped");
  const maxArgs = Math.max(...tools.map((t) => t.args?.length ?? 0));
  ok(maxArgs <= 205, `all args ≤200 chars (max ${maxArgs})`);
}

// ── Test 3: Undefined output doesn't crash ──
{
  let s = initialState;
  s = reducer(s, { type: "event", e: { kind: "turn_started" } });
  s = reducer(s, { type: "event", e: { kind: "tool_dispatch", tool: { id: "noop", name: "glob", args: JSON.stringify({ pattern: "**/*" }), readOnly: true } } });
  s = reducer(s, { type: "event", e: { kind: "tool_result", tool: { id: "noop", name: "glob", readOnly: true, output: undefined, durationMs: 5 } } });
  const tools = toolItems(s);
  ok(tools.length >= 1, "no crash when tool output is undefined");
}

// ── Test 4: Running (in-flight) tools keep full args for subject/UI ──
{
  let s = initialState;
  s = reducer(s, { type: "event", e: { kind: "turn_started" } });
  s = reducer(s, { type: "event", e: { kind: "tool_dispatch", tool: { id: "run1", name: "bash", args: '{"command":"echo hello"}', readOnly: false } } });
  // Before tool_result: tool is running, args should still be full
  const before = toolItems(s);
  ok(before.length >= 1, "tool exists while running");
  eq(before[0].status, "running", "tool is running");
  eq(before[0].dataArchived, undefined, "running tool not archived yet");
  eq(before[0].args, '{"command":"echo hello"}', "running tool keeps full args");

  // After tool_result: archived
  s = reducer(s, { type: "event", e: { kind: "tool_result", tool: { id: "run1", name: "bash", readOnly: false, output: "hello world", durationMs: 50 } } });
  const after = toolItems(s);
  ok(after[0].dataArchived === true, "tool archived after result");
  eq(after[0].output, undefined, "output dropped after result");
}

// ── Test 5: Total string size reduction in a long session ──
{
  const TOOL_COUNT = 500;
  const ARGS_SIZE = 5000;
  const OUTPUT_SIZE = 10000;
  let s = addTools(initialState, TOOL_COUNT, ARGS_SIZE, OUTPUT_SIZE);
  const tools = toolItems(s);
  ok(tools.length >= TOOL_COUNT, `${tools.length} tools present`);

  // All tools should be archived: args ≤200, no output
  const totalStringBytes = tools.reduce((sum, t) => sum + (t.args?.length ?? 0) + (t.output?.length ?? 0), 0);
  // Expected: each tool has ~200 chars args + 0 output = ~200 per tool
  const expectedMax = TOOL_COUNT * 205;
  ok(totalStringBytes <= expectedMax, `total string size ${totalStringBytes.toLocaleString()} ≤ ${expectedMax.toLocaleString()} (${(100 * totalStringBytes / expectedMax).toFixed(0)}% of max)`);

  const withoutArchive = TOOL_COUNT * (ARGS_SIZE + OUTPUT_SIZE);
  const reduction = (withoutArchive - totalStringBytes) / withoutArchive;
  ok(reduction > 0.95, `archive removed ${(reduction * 100).toFixed(0)}% of tool string data`);
}

// ── Test 6: Restored history starts light, without a full-output transient ──
{
  const output = "z".repeat(100_000);
  const args = JSON.stringify({ command: "printf z" });
  const s = reducer(initialState, {
    type: "history",
    messages: [
      {
        role: "assistant",
        content: "",
        toolCalls: [{
          id: "hist-bash",
          name: "bash",
          arguments: "",
          argumentsArchived: true,
          subject: "printf z",
          summary: "1 line",
        }],
      },
      {
        role: "tool",
        content: "",
        toolCallId: "hist-bash",
        toolName: "bash",
        toolResultArchived: true,
      },
    ] as any,
  });
  const tools = toolItems(s);
  ok(tools.length === 1, "history restored one archived tool");
  eq(tools[0].dataArchived, true, "history archived tool is marked archived");
  eq(tools[0].output, undefined, "history archived tool has no output");
  eq(tools[0].args, "", "history archived tool has no args");
  eq(tools[0].subject, "printf z", "history archived tool keeps subject");
  eq(tools[0].summary, "1 line", "history archived tool keeps summary");
  const totalStringBytes = tools.reduce((sum, t) => sum + (t.args?.length ?? 0) + (t.output?.length ?? 0), 0);
  ok(totalStringBytes < args.length + output.length, "history restore avoids large args/output strings");
}

// ── Test 7: Restored todo_write keeps full structured args for the todo panel ──
{
  const todos = Array.from({ length: 8 }, (_, i) => ({
    content: `Task ${i} ${"x".repeat(30)}`,
    status: i === 0 ? "in_progress" : "pending",
  }));
  const args = JSON.stringify({ todos });
  const s = reducer(initialState, {
    type: "history",
    messages: [
      {
        role: "assistant",
        content: "",
        toolCalls: [{
          id: "todo-long",
          name: "todo_write",
          arguments: args,
        }],
      },
      {
        role: "tool",
        content: "",
        toolCallId: "todo-long",
        toolName: "todo_write",
        toolResultArchived: true,
      },
    ] as any,
  });
  const tools = toolItems(s);
  const todo = tools.find((tool) => tool.name === "todo_write");
  ok(Boolean(todo), "history restored todo_write");
  eq(todo?.args, args, "todo_write args are not truncated during history restore");
  eq(JSON.parse(todo?.args ?? "{}").todos.length, todos.length, "todo_write args remain parseable JSON");
}

console.log(`\n${passed} passed, ${failed} failed, ${passed + failed} total`);
if (failed > 0) process.exit(1);
