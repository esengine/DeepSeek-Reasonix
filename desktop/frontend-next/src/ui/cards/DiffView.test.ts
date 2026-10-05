import { describe, expect, it } from "vitest";
import { parseDiff, parseDiffSections, pathFromDiff, splitDiffSections } from "./DiffView";

// 行号住在 @@ 头里。此前整段按 \n 切开、序号直接用下标，于是一处改在第 205 行的
// 改动在卡上标成 1、2、3 —— 而那个头本身还被当成代码渲染了出来。
describe("读一段 unified diff", () => {
  it("行号来自 @@ 头，不是数组下标", () => {
    const rows = parseDiff("@@ -61,2 +61,9 @@\n context\n+\tadded\n-\tremoved");
    // 删除行留空：它在改动后的文件里不存在，而这一列问的是「跳过去落在第几行」。
    expect(rows.map((r) => ("no" in r ? r.no : "hunk"))).toEqual([61, 62, null]);
    expect(rows.map((r) => ("sign" in r ? r.sign : "·"))).toEqual([" ", "+", "-"]);
  });

  it("@@ 头自己不进正文", () => {
    const rows = parseDiff("@@ -61,2 +61,9 @@\n+one");
    expect(rows).toHaveLength(1);
    expect("text" in rows[0] && rows[0].text).toBe("one");
  });

  // 缩进块的形状就是靠空行读出来的，filter(l => l.length > 0) 把它们整行丢掉了。
  it("保留空行", () => {
    const rows = parseDiff("@@ -1,3 +1,3 @@\n+a\n+\n+b");
    expect(rows).toHaveLength(3);
    expect("text" in rows[1] && rows[1].text).toBe("");
  });

  // 两处相隔几十行的改动此前连成一片，读起来像它们挨着。
  it("两段之间标出跳过了多少行", () => {
    const rows = parseDiff("@@ -1,1 +1,1 @@\n+a\n@@ -40,1 +40,1 @@\n+b");
    expect(rows.some((r) => "skipped" in r && r.skipped === 38)).toBe(true);
  });

  it("文件头不是内容", () => {
    const rows = parseDiff("diff --git a/x b/x\nindex 1..2\n--- a/x\n+++ b/x\n@@ -1,1 +1,1 @@\n+a");
    expect(rows).toHaveLength(1);
  });

  // 裸片段给不出真行号，就不给 —— 编一个比留空更坏。
  it("没有 @@ 头时不编行号", () => {
    const rows = parseDiff("+a\n-b");
    expect(rows.every((r) => "no" in r && r.no === null)).toBe(true);
  });

  // 末尾换行是 diff 的终止符，不是一行空内容：按 \n 切开会在每张卡底部留一个
  // 带行号的空行。
  it("末尾换行不产生空行", () => {
    expect(parseDiff("@@ -1,1 +1,1 @@\n+a\n")).toHaveLength(1);
    expect(parseDiff("@@ -1,1 +1,1 @@\n+a\n\n")).toHaveLength(1);
  });

  // git show 的 commit 头与缩进消息是前言，不是正文里的上下文行。
  it("git show 的 commit 头不进正文", () => {
    const rows = parseDiff("commit 0f2a1b9c\nAuthor: A <a@b>\nDate:   now\n\n    subject\n\ndiff --git a/x b/x\n--- a/x\n+++ b/x\n@@ -1,1 +1,1 @@\n+a\n");
    expect(rows).toHaveLength(1);
    expect("text" in rows[0] && rows[0].text).toBe("a");
  });
});

// 被标记的 shell 结果没有工具参数来指出文件，文件名只能从 diff 本身读。
describe("从 diff 读文件名", () => {
  it("取第一个 +++ b/…", () => {
    expect(pathFromDiff("diff --git a/x.go b/x.go\n--- a/x.go\n+++ b/x.go\n@@ -1 +1 @@\n-a\n+b\n")).toBe("x.go");
  });

  it("删除的文件退回 --- a/…", () => {
    expect(pathFromDiff("--- a/gone.go\n+++ /dev/null\n@@ -1 +0,0 @@\n-a\n")).toBe("gone.go");
  });

  it("没有文件头时给不出名字", () => {
    expect(pathFromDiff("@@ -1 +1 @@\n-a\n+b\n")).toBeUndefined();
  });
});

// 一份多文件 diff 此前只报第一个文件名，其余文件的行混在下面、看不出属于谁。
describe("按文件切段", () => {
  const two = "diff --git a/one.go b/one.go\nindex 1..2\n--- a/one.go\n+++ b/one.go\n@@ -1 +1 @@\n-old\n+new\n" +
    "diff --git a/two.go b/two.go\nindex 3..4\n--- a/two.go\n+++ b/two.go\n@@ -1 +1 @@\n-gone\n+kept\n";

  it("每个文件一段，各带自己的名字与 +A -B", () => {
    const secs = parseDiffSections(two);
    expect(secs.map((s) => s.path)).toEqual(["one.go", "two.go"]);
    expect(secs.map((s) => `${s.added}/${s.removed}`)).toEqual(["1/1", "1/1"]);
    expect(secs[0].rows).toHaveLength(2);
    expect(secs[1].rows).toHaveLength(2);
  });

  it("git show 的 commit 前言不成为一段", () => {
    const secs = parseDiffSections("commit 0f2a1b9c\nAuthor: A <a@b>\nDate:   now\n\n    subject\n\n" + two);
    expect(secs.map((s) => s.path)).toEqual(["one.go", "two.go"]);
  });

  it("非 git 风格按 --- /+++ 对切段", () => {
    const secs = parseDiffSections("--- a/x.go\n+++ b/x.go\n@@ -1 +1 @@\n-a\n+b\n--- a/y.go\n+++ b/y.go\n@@ -1 +1 @@\n-c\n+d\n");
    expect(secs.map((s) => s.path)).toEqual(["x.go", "y.go"]);
  });

  it("单文件 diff 只有一段", () => {
    expect(splitDiffSections("--- a/x.go\n+++ b/x.go\n@@ -1 +1 @@\n-a\n+b\n")).toHaveLength(1);
  });

  it("删除的文件退回 --- a/…", () => {
    expect(parseDiffSections("--- a/gone.go\n+++ /dev/null\n@@ -1 +0,0 @@\n-a\n")[0].path).toBe("gone.go");
  });
});
