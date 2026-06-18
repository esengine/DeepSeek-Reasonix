// Run: tsx src/__tests__/diff-rendering.test.ts

import { readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { diffLines, diffRowsFromUnifiedDiff } from "../lib/diff";
import { summarize } from "../lib/tools";
import { initialState, reducer } from "../lib/useController";
import type { Item } from "../lib/useController";

const testDir = dirname(fileURLToPath(import.meta.url));
const styles = readFileSync(resolve(testDir, "../styles.css"), "utf8").replace(/\/\*[\s\S]*?\*\//g, "");

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

function eq(a: unknown, b: unknown, label: string) {
  if (a === b) {
    process.stdout.write(`  PASS  ${label}\n`);
    passed += 1;
  } else {
    process.stdout.write(`  FAIL  ${label}: expected ${JSON.stringify(b)}, got ${JSON.stringify(a)}\n`);
    failed += 1;
  }
}

function matchingBlocks(selector: string): string[] {
  const blocks: string[] = [];
  const rule = /([^{}]+)\{([^{}]*)\}/g;
  let match: RegExpExecArray | null;
  while ((match = rule.exec(styles)) !== null) {
    const selectors = match[1].split(",").map((part) => part.trim());
    if (selectors.includes(selector)) blocks.push(match[2]);
  }
  return blocks;
}

function finalDeclaration(selector: string, property: string): string | undefined {
  let value: string | undefined;
  for (const block of matchingBlocks(selector)) {
    const declaration = new RegExp(`(?:^|;)\\s*${property}\\s*:\\s*([^;]+)`, "g");
    let match: RegExpExecArray | null;
    while ((match = declaration.exec(block)) !== null) {
      value = match[1].trim();
    }
  }
  return value;
}

type ToolItem = Extract<Item, { kind: "tool" }>;

function toolItems(s: typeof initialState): ToolItem[] {
  return s.items.filter((it): it is ToolItem => it.kind === "tool");
}

console.log("\ndiff rendering contract");

{
  const rows = diffLines("one\ntwo\nthree", "one\nTWO\nthree\nfour");
  eq(JSON.stringify(rows.map((r) => [r.type, r.oldLine ?? "", r.newLine ?? "", r.text])), JSON.stringify([
    ["ctx", 1, 1, "one"],
    ["del", 2, "", "two"],
    ["add", "", 2, "TWO"],
    ["ctx", 3, 3, "three"],
    ["add", "", 4, "four"],
  ]), "diff rows carry old/new line numbers");
}

{
  const rows = diffRowsFromUnifiedDiff([
    "--- a/settings/settings_IO.gd",
    "+++ b/settings/settings_IO.gd",
    "@@ -27,2 +27,3 @@",
    " func keep():",
    "-func save():",
    "+func save_file():",
    "+func save_backup():",
    "",
  ].join("\n"));
  eq(JSON.stringify(rows.map((r) => [r.type, r.oldLine ?? "", r.newLine ?? "", r.text])), JSON.stringify([
    ["ctx", 27, 27, "func keep():"],
    ["del", 28, "", "func save():"],
    ["add", "", 28, "func save_file():"],
    ["add", "", 29, "func save_backup():"],
  ]), "unified diff rows preserve hunk line numbers");
}

{
  eq(summarize("write_file", ""), "", "archived write_file without args does not synthesize 0 lines");
  eq(summarize("write_file", JSON.stringify({ content: "" })), "0 lines", "explicit empty write_file content still summarizes as 0 lines");
}

for (const prefix of ["diff", "inline-diff"]) {
  eq(finalDeclaration(`.${prefix}__table`, "min-width"), "max-content", `${prefix} rows share the longest scroll width`);
  eq(finalDeclaration(`.${prefix}__table`, "width"), "100%", `${prefix} table fills the visible viewport`);
  eq(finalDeclaration(`.${prefix}__row`, "width"), "100%", `${prefix} row background fills table width`);
  eq(finalDeclaration(`.${prefix}__gutter`, "position"), "sticky", `${prefix} gutter remains visible while horizontally scrolled`);
  eq(finalDeclaration(`.${prefix}__gutter`, "left"), "0", `${prefix} sticky gutter anchors at left`);
}

{
  let s = reducer(initialState, { type: "event", e: { kind: "turn_started" } });
  const fileDiff = [
    "--- a/settings/pages/video_settings.tscn",
    "+++ b/settings/pages/video_settings.tscn",
    "@@ -42,2 +42,2 @@",
    "-layout_mode = 3",
    "+layout_mode = 1",
  ].join("\n");
  s = reducer(s, {
    type: "event",
    e: {
      kind: "tool_dispatch",
      tool: {
        id: "write-existing",
        name: "write_file",
        args: JSON.stringify({ path: "settings/pages/video_settings.tscn", content: "full\nreplacement\nfile\n" }),
        readOnly: false,
        diff: fileDiff,
        added: 1,
        removed: 1,
      } as any,
    },
  });
  let [tool] = toolItems(s);
  eq(tool?.summary, "+1 -1", "writer dispatch uses preview file diff summary instead of content line count");
  eq((tool as any)?.fileDiff?.diff, fileDiff, "writer dispatch keeps preview file diff for rendering");
  s = reducer(s, { type: "event", e: { kind: "tool_result", tool: { id: "write-existing", name: "write_file", readOnly: false, output: "wrote 22 bytes", durationMs: 12 } } });
  [tool] = toolItems(s);
  eq(tool?.summary, "+1 -1", "completed writer archives with preview file diff summary");
  ok(tool?.dataArchived === true, "completed writer with preview diff is still archived");
}

{
  const fileDiff = [
    "--- a/settings/settings_IO.gd",
    "+++ b/settings/settings_IO.gd",
    "@@ -27 +27 @@",
    "-func save():",
    "+func save_file():",
  ].join("\n");
  const s = reducer(initialState, {
    type: "history",
    messages: [
      {
        role: "assistant",
        content: "",
        toolCalls: [{
          id: "hist-edit",
          name: "edit_file",
          arguments: "",
          argumentsArchived: true,
          subject: "settings/settings_IO.gd",
          diff: fileDiff,
          added: 1,
          removed: 1,
        }],
      },
      {
        role: "tool",
        content: "",
        toolCallId: "hist-edit",
        toolName: "edit_file",
        toolResultArchived: true,
      },
    ] as any,
  });
  const [tool] = toolItems(s);
  eq(tool?.summary, "+1 -1", "history writer restores preview file diff summary");
  eq((tool as any)?.fileDiff?.diff, fileDiff, "history writer restores preview file diff body");
}

{
  let s = reducer(initialState, { type: "event", e: { kind: "turn_started" } });
  s = reducer(s, {
    type: "event",
    e: {
      kind: "tool_dispatch",
      tool: {
        id: "edit-1",
        name: "edit_file",
        args: JSON.stringify({ path: "settings/settings.gd", old_string: "old\nsame", new_string: "new\nsame\nextra" }),
        readOnly: false,
      },
    },
  });
  s = reducer(s, { type: "event", e: { kind: "tool_result", tool: { id: "edit-1", name: "edit_file", readOnly: false, output: "edited settings/settings.gd", durationMs: 12 } } });
  const [tool] = toolItems(s);
  eq(tool?.summary, "+2 -1", "completed writer keeps +N -M summary after archiving");
  ok(tool?.dataArchived === true, "completed writer data is still archived");
  eq(tool?.output, undefined, "summary does not require keeping tool output");
}

{
  let s = reducer(initialState, { type: "event", e: { kind: "turn_started" } });
  s = reducer(s, {
    type: "event",
    e: {
      kind: "tool_dispatch",
      tool: {
        id: "edit-error",
        name: "edit_file",
        args: JSON.stringify({ path: "settings/settings.gd", old_string: "old", new_string: "new" }),
        readOnly: false,
      },
    },
  });
  s = reducer(s, { type: "event", e: { kind: "tool_result", tool: { id: "edit-error", name: "edit_file", readOnly: false, err: "old_string not found", durationMs: 12 } } });
  const [tool] = toolItems(s);
  eq(tool?.status, "error", "failed writer is marked as error");
  eq(tool?.summary, undefined, "failed writer clears cached +N -M summary");
}

{
  let s = reducer(initialState, { type: "event", e: { kind: "turn_started" } });
  const args = JSON.stringify({
    path: "settings/settings.gd",
    edits: [
      { old_string: "old", new_string: "new", replace_all: true },
      { old_string: "same", new_string: "same2" },
    ],
  });
  s = reducer(s, {
    type: "event",
    e: {
      kind: "tool_dispatch",
      tool: {
        id: "multi-replace-all",
        name: "multi_edit",
        args,
        readOnly: false,
      },
    },
  });
  let [tool] = toolItems(s);
  eq(tool?.summary, "", "replace_all multi_edit defers summary until result");
  s = reducer(s, {
    type: "event",
    e: {
      kind: "tool_result",
      tool: {
        id: "multi-replace-all",
        name: "multi_edit",
        readOnly: false,
        output: "multi_edit settings/settings.gd: 2 edits applied (5 total replacements)",
        durationMs: 12,
      },
    },
  });
  [tool] = toolItems(s);
  eq(tool?.summary, "2 edits · 5 replacements", "replace_all multi_edit uses applied replacement count");
}

{
  let s = reducer(initialState, { type: "event", e: { kind: "turn_started" } });
  s = reducer(s, {
    type: "event",
    e: {
      kind: "tool_dispatch",
      tool: {
        id: "tool-1",
        name: "edit_file",
        args: JSON.stringify({ path: "settings/settings.gd", old_string: "old", new_string: "new" }),
        readOnly: false,
      },
    },
  });
  s = reducer(s, {
    type: "event",
    e: {
      kind: "tool_dispatch",
      tool: {
        id: "tool-1",
        name: "read_file",
        args: JSON.stringify({ path: "settings/settings.gd" }),
        readOnly: true,
      },
    },
  });
  const [tool] = toolItems(s);
  eq(tool?.summary, undefined, "dispatch updates clear stale writer summary");
}

console.log(`\n${passed} passed, ${failed} failed, ${passed + failed} total`);
if (failed > 0) process.exit(1);
