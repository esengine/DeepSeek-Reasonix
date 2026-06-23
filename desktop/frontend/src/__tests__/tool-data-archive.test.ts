// Run: tsx src/__tests__/tool-data-archive.test.ts
//
// Verifies that the tool_result reducer archives completed tools immediately:
// output is dropped, dataArchived is set, and most args are trimmed to 200
// chars. For todo_write, only the latest successful top-level snapshot keeps
// full JSON because the bottom task panel parses that canonical entry directly.

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

function todoArgs(label: string, active = 0): string {
  return JSON.stringify({
    todos: Array.from({ length: 8 }, (_, i) => ({
      content: `${label} task ${i} ${"x".repeat(30)}`,
      status: i === active ? "in_progress" : "pending",
    })),
  });
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

// ── Test 7: History keeps only the latest successful top-level todo_write full ──
{
  const oldArgs = todoArgs("old");
  const latestArgs = todoArgs("latest", 2);
  const s = reducer(initialState, {
    type: "history",
    messages: [
      {
        role: "assistant",
        content: "",
        toolCalls: [{
          id: "todo-old",
          name: "todo_write",
          arguments: oldArgs,
        }],
      },
      {
        role: "tool",
        content: "",
        toolCallId: "todo-old",
        toolName: "todo_write",
        toolResultArchived: true,
      },
      {
        role: "assistant",
        content: "",
        toolCalls: [{
          id: "todo-latest",
          name: "todo_write",
          arguments: latestArgs,
        }],
      },
      {
        role: "tool",
        content: "",
        toolCallId: "todo-latest",
        toolName: "todo_write",
        toolResultArchived: true,
      },
    ] as any,
  });
  const tools = toolItems(s);
  const oldTodo = tools.find((tool) => tool.id === "todo-old");
  const latestTodo = tools.find((tool) => tool.id === "todo-latest");
  ok(Boolean(oldTodo), "history restored older todo_write");
  ok(Boolean(latestTodo), "history restored latest todo_write");
  ok((oldTodo?.args.length ?? 0) <= 205, "older todo_write args are truncated during history restore");
  ok(oldTodo?.args !== oldArgs, "older todo_write no longer keeps full JSON");
  eq(latestTodo?.args, latestArgs, "latest todo_write keeps full args during history restore");
  eq(JSON.parse(latestTodo?.args ?? "{}").todos.length, 8, "latest todo_write args remain parseable JSON");
}

// ── Test 8: Live updates keep only the latest successful top-level todo_write full ──
{
  const firstArgs = todoArgs("first");
  const latestArgs = todoArgs("latest", 3);
  let s = initialState;
  s = reducer(s, { type: "event", e: { kind: "turn_started" } });
  s = reducer(s, {
    type: "event",
    e: {
      kind: "tool_dispatch",
      tool: { id: "todo-first", name: "todo_write", args: firstArgs, readOnly: true },
    },
  });
  s = reducer(s, {
    type: "event",
    e: {
      kind: "tool_result",
      tool: { id: "todo-first", name: "todo_write", readOnly: true, output: "Todos updated", durationMs: 15 },
    },
  });
  s = reducer(s, {
    type: "event",
    e: {
      kind: "tool_dispatch",
      tool: { id: "todo-latest", name: "todo_write", args: latestArgs, readOnly: true },
    },
  });
  s = reducer(s, {
    type: "event",
    e: {
      kind: "tool_result",
      tool: { id: "todo-latest", name: "todo_write", readOnly: true, output: "Todos updated", durationMs: 20 },
    },
  });

  const tools = toolItems(s);
  const firstTodo = tools.find((tool) => tool.id === "todo-first");
  const latestTodo = tools.find((tool) => tool.id === "todo-latest");
  ok(Boolean(firstTodo), "first live todo_write result is recorded");
  ok(Boolean(latestTodo), "latest live todo_write result is recorded");
  eq(firstTodo?.dataArchived, true, "older live todo_write stays archived");
  ok((firstTodo?.args.length ?? 0) <= 205, "older live todo_write args are truncated");
  eq(latestTodo?.dataArchived, true, "latest live todo_write still marks output as archived");
  eq(latestTodo?.args, latestArgs, "latest live todo_write keeps full args");
  eq(JSON.parse(latestTodo?.args ?? "{}").todos.length, 8, "latest live todo_write args remain parseable JSON");
}

// ── Test 9: A later failed todo_write does not steal the canonical snapshot ──
{
  const successArgs = todoArgs("success", 1);
  const failedArgs = todoArgs("failed", 4);
  let s = initialState;
  s = reducer(s, { type: "event", e: { kind: "turn_started" } });
  s = reducer(s, {
    type: "event",
    e: {
      kind: "tool_dispatch",
      tool: { id: "todo-success", name: "todo_write", args: successArgs, readOnly: true },
    },
  });
  s = reducer(s, {
    type: "event",
    e: {
      kind: "tool_result",
      tool: { id: "todo-success", name: "todo_write", readOnly: true, output: "Todos updated", durationMs: 15 },
    },
  });
  s = reducer(s, {
    type: "event",
    e: {
      kind: "tool_dispatch",
      tool: { id: "todo-failed", name: "todo_write", args: failedArgs, readOnly: true },
    },
  });
  s = reducer(s, {
    type: "event",
    e: {
      kind: "tool_result",
      tool: { id: "todo-failed", name: "todo_write", readOnly: true, err: "write failed", durationMs: 15 },
    },
  });

  const tools = toolItems(s);
  const successTodo = tools.find((tool) => tool.id === "todo-success");
  const failedTodo = tools.find((tool) => tool.id === "todo-failed");
  eq(successTodo?.args, successArgs, "previous successful todo_write remains canonical after a later failure");
  ok((failedTodo?.args.length ?? 0) <= 205, "failed todo_write args are truncated");
  eq(failedTodo?.status, "error", "failed todo_write keeps error status");
}

// ── Test 10: A successful empty todo_write becomes canonical without unarchiving older args ──
{
  const oldArgs = todoArgs("old");
  const clearArgs = `{"todos":[]}`;
  let s = initialState;
  s = reducer(s, { type: "event", e: { kind: "turn_started" } });
  s = reducer(s, { type: "event", e: { kind: "tool_dispatch", tool: { id: "todo-old", name: "todo_write", args: oldArgs, readOnly: true } } });
  s = reducer(s, { type: "event", e: { kind: "tool_result", tool: { id: "todo-old", name: "todo_write", readOnly: true, output: "Todos updated" } } });
  s = reducer(s, { type: "event", e: { kind: "tool_dispatch", tool: { id: "todo-clear", name: "todo_write", args: clearArgs, readOnly: true } } });
  s = reducer(s, { type: "event", e: { kind: "tool_result", tool: { id: "todo-clear", name: "todo_write", readOnly: true, output: "Todos updated" } } });

  const tools = toolItems(s);
  const oldTodo = tools.find((tool) => tool.id === "todo-old");
  const clearTodo = tools.find((tool) => tool.id === "todo-clear");
  ok((oldTodo?.args.length ?? 0) <= 205, "older todo_write args stay archived when latest todo_write clears the list");
  ok(oldTodo?.args !== oldArgs, "older todo_write does not keep full JSON after a clear");
  eq(clearTodo?.args, clearArgs, "empty todo_write clear keeps parseable canonical args");
  eq(JSON.parse(clearTodo?.args ?? "{}").todos.length, 0, "empty todo_write clear remains parseable as the latest canonical list");
}

console.log(`\n${passed} passed, ${failed} failed, ${passed + failed} total`);
if (failed > 0) process.exit(1);
