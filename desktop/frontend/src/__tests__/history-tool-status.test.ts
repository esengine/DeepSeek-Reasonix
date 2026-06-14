// Run: tsx src/__tests__/history-tool-status.test.ts

import { historyMessagesToItems } from "../lib/useController";
import type { HistoryMessage } from "../lib/types";

let passed = 0;
let failed = 0;

function eq(a: unknown, b: unknown, label: string) {
  if (a === b) {
    process.stdout.write(`  PASS  ${label}\n`);
    passed += 1;
  } else {
    process.stdout.write(`  FAIL  ${label}: expected ${JSON.stringify(b)}, got ${JSON.stringify(a)}\n`);
    failed += 1;
  }
}

function toolItems(messages: HistoryMessage[]) {
  return historyMessagesToItems(messages, "h").items.filter((item) => item.kind === "tool");
}

console.log("\nhistory tool status");

const lowercaseError = toolItems([
  {
    role: "assistant",
    content: "",
    toolCalls: [{ id: "todo-bad", name: "todo_write", arguments: "{\"todos\":[{\"content\":\"Bad\",\"status\":\"in_progress\"}]}" }],
  },
  {
    role: "tool",
    content: "error: rejected todo transition",
    toolCallId: "todo-bad",
    toolName: "todo_write",
  },
]);
eq(lowercaseError[0]?.kind === "tool" && lowercaseError[0].status, "error", "lowercase error result restores as error");
eq(Boolean(lowercaseError[0]?.kind === "tool" && lowercaseError[0].error), true, "lowercase error result carries error text");

const blocked = toolItems([
  {
    role: "assistant",
    content: "",
    toolCalls: [{ id: "writer", name: "write_file", arguments: "{}" }],
  },
  {
    role: "tool",
    content: "blocked: plan mode is read-only",
    toolCallId: "writer",
    toolName: "write_file",
  },
]);
eq(blocked[0]?.kind === "tool" && blocked[0].status, "error", "blocked result restores as error");

const missingResult = toolItems([
  {
    role: "assistant",
    content: "",
    toolCalls: [{ id: "step-pending", name: "complete_step", arguments: "{\"step\":\"A\"}" }],
  },
]);
eq(missingResult[0]?.kind === "tool" && missingResult[0].status, "stopped", "missing tool result restores as stopped");

const positionalResult = toolItems([
  {
    role: "assistant",
    content: "",
    toolCalls: [{ id: "", name: "todo_write", arguments: "{\"todos\":[{\"content\":\"A\",\"status\":\"in_progress\"}]}" }],
  },
  {
    role: "tool",
    content: "Todos updated",
    toolCallId: "",
    toolName: "todo_write",
  },
]);
eq(positionalResult.length, 1, "positional tool result is consumed instead of rendering as an orphan");
eq(positionalResult[0]?.kind === "tool" && positionalResult[0].status, "done", "empty-id tool call restores from positional result");
eq(positionalResult[0]?.kind === "tool" && positionalResult[0].output, "Todos updated", "empty-id tool call keeps positional output");

console.log(`\n${passed} passed, ${failed} failed`);
if (failed > 0) process.exit(1);
