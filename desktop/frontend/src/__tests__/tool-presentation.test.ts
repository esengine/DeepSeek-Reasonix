// Run: tsx src/__tests__/tool-presentation.test.ts
//
// toolPresentation 是 ToolCard 的展示决策适配层：把"工具名 + 参数 + 后端 fileDiff"
// 汇总成 ToolCard 只需渲染的 ToolCardPresentation。这些测试验证核心行为，而非
// 实现细节——它们应当能在重构后存活。

import { toolPresentation } from "../lib/toolPresentation";

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

console.log("\ntool presentation");

{
  // 行为1: write_file 携带后端结构化 fileDiff → unified-diff detail + 默认展开
  const result = toolPresentation({
    name: "write_file",
    args: JSON.stringify({ path: "a.ts", content: "x\n" }),
    fileDiff: { diff: "--- a/a.ts\n+++ b/a.ts\n@@ -1 +1 @@\n-x\n+y\n", added: 1, removed: 1 },
    archivedWithoutFullData: false,
    hasNested: false,
    status: "done",
  });
  eq(result.detail.kind, "unified-diff", "write_file with fileDiff → unified-diff detail");
  eq(result.defaultOpen, true, "write_file with fileDiff → default open");
}

{
  // 行为2: edit_file 无 fileDiff，但 args 含 old_string/new_string → inline-diffs + 展开
  const result = toolPresentation({
    name: "edit_file",
    args: JSON.stringify({ path: "a.ts", old_string: "foo", new_string: "bar" }),
    archivedWithoutFullData: false,
    hasNested: false,
    status: "done",
  });
  eq(result.detail.kind, "inline-diffs", "edit_file without fileDiff → inline-diffs detail");
  eq(result.defaultOpen, true, "edit_file (write tool) → default open even without diff preview");
}

{
  // 行为3: multi_edit 无 fileDiff，args 含 edits → inline-diffs + 展开
  const result = toolPresentation({
    name: "multi_edit",
    args: JSON.stringify({ path: "a.ts", edits: [{ old_string: "a", new_string: "b" }, { old_string: "c", new_string: "d" }] }),
    archivedWithoutFullData: false,
    hasNested: false,
    status: "done",
  });
  eq(result.detail.kind, "inline-diffs", "multi_edit without fileDiff → inline-diffs detail");
  if (result.detail.kind === "inline-diffs") {
    eq(result.detail.diffs.length, 2, "multi_edit derives one inline diff per edit step");
  }
  eq(result.defaultOpen, true, "multi_edit (write tool) → default open");
}

{
  // 行为4: move_file 无 fileDiff，args 无法推导 diff → none detail，但写工具仍展开
  const result = toolPresentation({
    name: "move_file",
    args: JSON.stringify({ source_path: "a.ts", destination_path: "b.ts" }),
    archivedWithoutFullData: false,
    hasNested: false,
    status: "done",
  });
  eq(result.detail.kind, "none", "move_file without fileDiff → none detail (no args-derived diff)");
  eq(result.defaultOpen, true, "move_file (write tool) → default open even without diff");
}

{
  // 行为5: bash 非写工具，无嵌套 → none + 折叠
  const result = toolPresentation({
    name: "bash",
    args: JSON.stringify({ command: "ls" }),
    archivedWithoutFullData: false,
    hasNested: false,
    status: "done",
  });
  eq(result.detail.kind, "none", "bash → none detail");
  eq(result.defaultOpen, false, "bash (non-write, no nested) → default closed");
}

{
  // 行为6: bash 非写工具 + 嵌套子代理 running → none detail 但展开
  const result = toolPresentation({
    name: "bash",
    args: JSON.stringify({ command: "ls" }),
    archivedWithoutFullData: false,
    hasNested: true,
    status: "running",
  });
  eq(result.detail.kind, "none", "bash with nested running → none detail");
  eq(result.defaultOpen, true, "bash with nested running → default open");
}

{
  // 行为7: write_file dataArchived 且无 fullData → args="" 不推导 diff → none，但写工具仍展开
  const result = toolPresentation({
    name: "write_file",
    args: "",
    archivedWithoutFullData: true,
    hasNested: false,
    status: "done",
  });
  eq(result.detail.kind, "none", "archived write_file without fullData → none detail (no args to derive)");
  eq(result.defaultOpen, true, "archived write_file → still default open (write tool)");
}

{
  // 行为8: move_file 携带 kind="rename" 的 fileDiff → rename detail + 展开
  // rename 的 diff 为空，但 kind="rename" 标记了这是一次重命名，
  // 前端应渲染 "src → dst" 卡片而非 none。
  const result = toolPresentation({
    name: "move_file",
    args: JSON.stringify({ source_path: "a.ts", destination_path: "b.ts" }),
    fileDiff: { diff: "", added: 0, removed: 0, kind: "rename", srcPath: "/a.ts", dstPath: "/b.ts" },
    archivedWithoutFullData: false,
    hasNested: false,
    status: "done",
  });
  eq(result.detail.kind, "rename", "move_file with kind=rename → rename detail");
  if (result.detail.kind === "rename") {
    eq(result.detail.srcPath, "/a.ts", "rename detail carries srcPath");
    eq(result.detail.dstPath, "/b.ts", "rename detail carries dstPath");
  }
  eq(result.defaultOpen, true, "move_file rename → default open (write tool)");
}

if (failed) {
  process.stdout.write(`\n${failed} failed, ${passed} passed\n`);
  process.exit(1);
}
process.stdout.write(`\n${passed} passed\n`);
