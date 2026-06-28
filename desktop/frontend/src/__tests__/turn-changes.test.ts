// Run: tsx src/__tests__/turn-changes.test.ts

import { summarizeTurnChanges } from "../lib/turnChanges";
import { initialState, reducer } from "../lib/useController";
import type { Item } from "../lib/useController";

let passed = 0;
let failed = 0;

function ok(value: unknown, label: string) {
  if (value) {
    process.stdout.write(`  PASS  ${label}\n`);
    passed += 1;
  } else {
    process.stdout.write(`  FAIL  ${label}\n`);
    failed += 1;
  }
}

function eq(actual: unknown, expected: unknown, label: string) {
  if (actual === expected) {
    process.stdout.write(`  PASS  ${label}\n`);
    passed += 1;
  } else {
    process.stdout.write(`  FAIL  ${label}: expected ${JSON.stringify(expected)}, got ${JSON.stringify(actual)}\n`);
    failed += 1;
  }
}

function unified(path: string, body: string): string {
  return [`--- a/${path}`, `+++ b/${path}`, body].join("\n");
}

console.log("\nturn changes contract");

{
  const items: Item[] = [
    { kind: "user", id: "u1", text: "change files" },
    { kind: "assistant", id: "a1", text: "done", reasoning: "", streaming: false },
    {
      kind: "tool",
      id: "edit-1",
      name: "edit_file",
      args: JSON.stringify({ path: "src/app.ts" }),
      readOnly: false,
      status: "done",
      fileDiff: { diff: unified("src/app.ts", "@@ -1 +1 @@\n-old\n+new"), added: 1, removed: 1 },
    },
    {
      kind: "tool",
      id: "edit-2",
      name: "multi_edit",
      args: JSON.stringify({ path: "src/app.ts" }),
      readOnly: false,
      status: "done",
      fileDiff: { diff: unified("src/app.ts", "@@ -5,0 +6,2 @@\n+extra\n+lines"), added: 2, removed: 0 },
    },
    {
      kind: "tool",
      id: "failed-edit",
      name: "edit_file",
      args: JSON.stringify({ path: "src/app.ts" }),
      readOnly: false,
      status: "error",
      error: "old_string not found",
      fileDiff: { diff: unified("src/app.ts", "@@ -1 +1 @@\n-old\n+wrong"), added: 1, removed: 1 },
    },
    {
      kind: "tool",
      id: "read-1",
      name: "read_file",
      args: JSON.stringify({ path: "src/app.ts" }),
      readOnly: true,
      status: "done",
    },
  ];
  const summary = summarizeTurnChanges(items);
  ok(summary, "completed writer diffs produce a turn summary");
  eq(summary?.files.length, 1, "same file is grouped once");
  eq(summary?.added, 3, "adds are summed across successful patches");
  eq(summary?.removed, 1, "removals are summed across successful patches");
  eq(summary?.files[0]?.path, "src/app.ts", "file path comes from tool args");
  eq(summary?.files[0]?.patches.length, 2, "failed and read-only tools are ignored");
}

{
  const items: Item[] = [
    {
      kind: "tool",
      id: "hist-subject",
      name: "write_file",
      args: "",
      readOnly: false,
      status: "done",
      subject: "src/new.ts",
      fileDiff: { diff: unified("src/new.ts", "@@ -0,0 +1,2 @@\n+one\n+two"), added: 2, removed: 0 },
    },
    {
      kind: "tool",
      id: "hist-diff-path",
      name: "edit_file",
      args: "",
      readOnly: false,
      status: "done",
      fileDiff: { diff: unified("docs/readme.md", "@@ -1 +1 @@\n-old\n+new"), added: 1, removed: 1 },
    },
  ];
  const summary = summarizeTurnChanges(items);
  eq(summary?.files.length, 2, "archived tool calls still resolve changed files");
  eq(summary?.files.map((file) => file.path).join(","), "src/new.ts,docs/readme.md", "subjects and diff headers provide fallback paths");
}

{
  let state = reducer(initialState, { type: "event", e: { kind: "turn_started" } });
  state = reducer(state, {
    type: "event",
    e: {
      kind: "tool_dispatch",
      tool: {
        id: "live-edit",
        name: "edit_file",
        args: JSON.stringify({ path: "src/live.ts", old_string: "old", new_string: "new" }),
        readOnly: false,
      },
    },
  });
  const tool = state.items.find((item): item is Extract<Item, { kind: "tool" }> => item.kind === "tool" && item.id === "live-edit");
  eq(tool?.subject, "src/live.ts", "live writer dispatch caches subject before args archiving");
}

console.log(`\n${passed} passed, ${failed} failed`);
if (failed > 0) process.exit(1);
