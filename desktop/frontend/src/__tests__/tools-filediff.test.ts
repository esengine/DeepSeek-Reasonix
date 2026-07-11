// Run: tsx src/__tests__/tools-filediff.test.ts
//
// fileDiffFromWire 是前端把后端 wire 数据转成 ToolFileDiff 的解析函数。
// rename 场景的特殊性：diff/added/removed 全空，但 kind="rename" 标记了
// 这是一次重命名，前端必须识别并保留 srcPath/dstPath，不能按"空 diff"丢弃。

import { fileDiffFromWire } from "../lib/tools";

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

function deepEq(a: unknown, b: unknown, label: string) {
  if (JSON.stringify(a) === JSON.stringify(b)) {
    process.stdout.write(`  PASS  ${label}\n`);
    passed += 1;
  } else {
    process.stdout.write(`  FAIL  ${label}: expected ${JSON.stringify(b)}, got ${JSON.stringify(a)}\n`);
    failed += 1;
  }
}

console.log("\nfileDiffFromWire rename parsing");

{
  // 行为1: rename 场景 — diff/added/removed 全空，但 kind="rename" → 必须保留
  const result = fileDiffFromWire({ kind: "rename", srcPath: "/a.txt", dstPath: "/b.txt" });
  deepEq(
    result,
    { diff: "", added: 0, removed: 0, kind: "rename", srcPath: "/a.txt", dstPath: "/b.txt" },
    "rename: kind=rename preserves srcPath/dstPath even with empty diff",
  );
}

{
  // 行为2: 普通 write — diff 非空 → 正常返回，kind/srcPath/dstPath 不出现
  const result = fileDiffFromWire({ diff: "@@ -1 +1 @@\n-a\n+b\n", added: 1, removed: 1 });
  deepEq(result, { diff: "@@ -1 +1 @@\n-a\n+b\n", added: 1, removed: 1 }, "write: normal diff returned as-is");
}

{
  // 行为3: 空数据 — 无 kind，无 diff → undefined
  const result = fileDiffFromWire({});
  eq(result, undefined, "empty: no kind, no diff → undefined");
}

{
  // 行为4: 缺 kind 的 rename 数据 — 只有 srcPath/dstPath 但无 kind="rename" → undefined
  // （没有 kind 标记，前端无法区分这是 rename 还是普通空 diff）
  const result = fileDiffFromWire({ srcPath: "/a.txt", dstPath: "/b.txt" });
  eq(result, undefined, "no kind: srcPath/dstPath without kind=rename → undefined");
}

{
  // 行为5: rename 但缺 srcPath/dstPath — 仍应返回 kind="rename"（容错）
  const result = fileDiffFromWire({ kind: "rename" });
  deepEq(
    result,
    { diff: "", added: 0, removed: 0, kind: "rename", srcPath: "", dstPath: "" },
    "rename: kind without paths still preserved (graceful)",
  );
}

if (failed) {
  process.stdout.write(`\n${failed} failed, ${passed} passed\n`);
  process.exit(1);
} else {
  process.stdout.write(`\n${passed} passed\n`);
}
