import assert from "node:assert/strict";
import { readdir, readFile } from "node:fs/promises";
import { test } from "node:test";
import { annotateMarkdown, markdownRefs } from "./markdown-refs.mjs";

const tag = (ref) => `<${ref}>`;

test("a reference is written at line start or after whitespace, a parenthesis or a list separator", () => {
  const source = ["#1 修复", "- 见 #2", "- 修复（#3、#4）与 (#5, #6)，#7", "- 列表(#8,#9)"].join("\n");
  assert.deepEqual(markdownRefs(source), [1, 2, 3, 4, 5, 6, 7, 8, 9]);
  assert.equal(
    annotateMarkdown(source, tag),
    ["#1<1> 修复", "- 见 #2<2>", "- 修复（#3<3>、#4<4>）与 (#5<5>, #6<6>)，#7<7>", "- 列表(#8<8>,#9<9>)"].join("\n"),
  );
});

test("colours, entities, paths, anchors and escapes are not references", () => {
  const source = [
    "- 颜色#333 与 color:#123456 与 #333abc 与 #12-a",
    "- &#10; 与 a/#11 与 \\#12 与 [段落](#13) 与 [见](https://x/#14)",
  ].join("\n");
  assert.deepEqual(markdownRefs(source), []);
  assert.equal(annotateMarkdown(source, tag), source);
});

test("code spans, fences of either kind and indented code are skipped", () => {
  const source = [
    "- `#1` 与 ``a `#2` b`` 之外 #3",
    "```",
    "#4",
    "~~~",
    "#5",
    "```",
    "#6",
    "~~~~",
    "#7",
    "```",
    "#8",
    "~~~~~",
    "#9",
    "",
    "    #10 indented code",
    "    #11 still code",
    "#12",
  ].join("\n");
  assert.deepEqual(markdownRefs(source), [3, 6, 9, 12]);
});

test("indentation inside a list item is a continuation, not code", () => {
  const source = ["- 第一项", "", "    续行提到 #1", "", "段落", "", "    #2 is code"].join("\n");
  assert.deepEqual(markdownRefs(source), [1]);
});

test("HTML comments hide references on one line or across several", () => {
  const source = ["- 可见 #1 <!-- 隐藏 #2 --> 可见 #3", "<!--", "#4", "隐藏 #5 -->", "#6 <!-- #7"].join("\n");
  assert.deepEqual(markdownRefs(source), [1, 3, 6]);
});

test("annotating leaves every other byte in place", async () => {
  const dir = new URL("../release-notes/studio/", import.meta.url);
  for (const name of await readdir(dir)) {
    const source = await readFile(new URL(name, dir), "utf8");
    assert.equal(annotateMarkdown(source, () => ""), source, name);
    assert.equal(annotateMarkdown(source, tag).replace(/<\d+>/g, ""), source, name);
  }
});
