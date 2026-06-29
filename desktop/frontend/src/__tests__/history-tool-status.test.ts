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

const userTimeItems = historyMessagesToItems([
  { role: "user", content: "old prompt" },
  { role: "user", content: "new prompt", createdAt: 1_718_000_000_000 },
] as HistoryMessage[], "u").items.filter((item) => item.kind === "user");
eq(userTimeItems[0]?.kind === "user" && userTimeItems[0].createdAt, undefined, "history users without createdAt keep no timestamp");
eq(userTimeItems[1]?.kind === "user" && userTimeItems[1].createdAt, 1_718_000_000_000, "history users preserve createdAt when present");

const checkpointTurnItems = historyMessagesToItems([
  { role: "user", content: "first", checkpointTurn: 0 },
  { role: "assistant", content: "ok" },
  { role: "user", content: "second", checkpointTurn: 3 },
] as HistoryMessage[], "c").items.filter((item) => item.kind === "user");
eq(checkpointTurnItems[0]?.kind === "user" && checkpointTurnItems[0].checkpointTurn, 0, "history users preserve checkpoint turn zero");
eq(checkpointTurnItems[1]?.kind === "user" && checkpointTurnItems[1].checkpointTurn, 3, "history users preserve non-contiguous backend checkpoint turns");

const citedAssistant = historyMessagesToItems([
  {
    role: "assistant",
    content: "done",
    memoryCitations: [{ id: "mem-1", source: "MEMORY.md", lineStart: 116, lineEnd: 123, note: "workflow" }],
  },
] as HistoryMessage[], "m").items.filter((item) => item.kind === "assistant");
eq(citedAssistant[0]?.kind === "assistant" && citedAssistant[0].memoryCitations?.length, 1, "assistant history preserves memory citations");
eq(citedAssistant[0]?.kind === "assistant" && citedAssistant[0].memoryCitations?.[0]?.source, "MEMORY.md", "assistant memory citation source is preserved");
eq(citedAssistant[0]?.kind === "assistant" && citedAssistant[0].memoryCitations?.[0]?.lineEnd, 123, "assistant memory citation line range is preserved");

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

const archivedResult = toolItems([
  {
    role: "assistant",
    content: "",
    toolCalls: [{
      id: "archived-bash",
      name: "bash",
      arguments: "",
      argumentsArchived: true,
      subject: "printf x",
      summary: "600 lines",
    }],
  },
  {
    role: "tool",
    content: "",
    toolCallId: "archived-bash",
    toolName: "bash",
    toolResultArchived: true,
  },
] as HistoryMessage[]);
eq(archivedResult[0]?.kind === "tool" && archivedResult[0].status, "done", "archived successful result restores as done");
eq(archivedResult[0]?.kind === "tool" && archivedResult[0].dataArchived, true, "archived successful result is marked dataArchived immediately");
eq(archivedResult[0]?.kind === "tool" && archivedResult[0].output, undefined, "archived successful result has no initial output");
eq(archivedResult[0]?.kind === "tool" && archivedResult[0].args, "", "archived successful result has no initial args");
eq(archivedResult[0]?.kind === "tool" && archivedResult[0].subject, "printf x", "archived successful result keeps precomputed subject");
eq(archivedResult[0]?.kind === "tool" && archivedResult[0].summary, "600 lines", "archived successful result keeps precomputed summary");

const archivedError = toolItems([
  {
    role: "assistant",
    content: "",
    toolCalls: [{
      id: "archived-error",
      name: "bash",
      arguments: "",
      argumentsArchived: true,
      subject: "rm protected",
    }],
  },
  {
    role: "tool",
    content: "error: permission denied",
    toolCallId: "archived-error",
    toolName: "bash",
    toolResultArchived: true,
    toolResultError: "error: permission denied",
  },
] as HistoryMessage[]);
eq(archivedError[0]?.kind === "tool" && archivedError[0].status, "error", "archived failed result restores as error");
eq(archivedError[0]?.kind === "tool" && archivedError[0].dataArchived, true, "archived failed result is still loadable on demand");
eq(archivedError[0]?.kind === "tool" && archivedError[0].error, "error: permission denied", "archived failed result keeps bounded error preview");

console.log(`\n${passed} passed, ${failed} failed`);
if (failed > 0) process.exit(1);
