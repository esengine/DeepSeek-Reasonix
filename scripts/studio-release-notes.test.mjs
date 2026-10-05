import assert from "node:assert/strict";
import { readdir, readFile } from "node:fs/promises";
import { test } from "node:test";
import { markdownRefs } from "./markdown-refs.mjs";
import { CreditError, CreditErrorCode, resolveCredits } from "./release-credits.mjs";
import { renderStudioNotes } from "./studio-release-notes.mjs";

const notes = `本版修复两件事。

## 修复

- 读图模型不再声称看不到图片（#20）(44150b0aa)
- Windows 窗口可以打开 #21
- 依赖升级 #22，并补上 #20 的回归测试
- 会话不再丢失（#23）
`;

const authors = {
  20: { kind: "pull", login: "alice", bot: false },
  21: { kind: "issue", fixes: [] },
  22: { kind: "pull", login: "dependabot[bot]", bot: true },
  23: { kind: "issue", fixes: [{ number: 30, login: "bob", bot: false }] },
};

test("pull requests credit their authors, fixed issues their fixers, the rest stay plain", async () => {
  const calls = [];
  const { credits, failures } = await resolveCredits(markdownRefs(notes), async (ref) => {
    calls.push(ref);
    return authors[ref];
  });
  assert.deepEqual(failures, []);
  assert.deepEqual(calls.sort(), [20, 21, 22, 23]);
  assert.equal(
    renderStudioNotes(notes, credits),
    `本版修复两件事。

## 修复

- 读图模型不再声称看不到图片（#20 by @alice）(44150b0aa)
- Windows 窗口可以打开 #21
- 依赖升级 #22，并补上 #20 by @alice 的回归测试
- 会话不再丢失（#23 fixed in #30 by @bob）

## 贡献者

感谢本版本的贡献者：@alice、@bob
`,
  );
});

test("an unresolved ref stays plain and is reported by code", async () => {
  const { credits, failures } = await resolveCredits(markdownRefs(notes), async (ref) => {
    if (ref === 20) throw new CreditError(CreditErrorCode.unreachable, "#20: down", { ref });
    return authors[ref];
  });
  assert.deepEqual(failures.map((failure) => [failure.ref, failure.code]), [[20, CreditErrorCode.unreachable]]);
  const rendered = renderStudioNotes(notes, credits);
  assert.doesNotMatch(rendered, /#20 by/);
  assert.match(rendered, /#23 fixed in #30 by @bob/);
  assert.match(rendered, /感谢本版本的贡献者：@bob\n$/);
});

test("committed notes render unchanged when nothing is credited", async () => {
  const dir = new URL("../release-notes/studio/", import.meta.url);
  for (const name of await readdir(dir)) {
    const source = await readFile(new URL(name, dir), "utf8");
    assert.equal(renderStudioNotes(source, new Map()), `${source.trimEnd()}\n`, name);
  }
});

test("a number naming a discussion or nothing stays plain and only warns", async () => {
  const source = "- 讨论见 #40，颜色改为 #123456\n";
  const { credits, failures, warnings } = await resolveCredits(markdownRefs(source), async (ref) =>
    ref === 40 ? { kind: "discussion" } : { kind: "missing" },
  );
  assert.deepEqual(failures, []);
  assert.deepEqual(
    warnings.map((warning) => warning.code),
    [CreditErrorCode.discussion, CreditErrorCode.notFound],
  );
  assert.equal(renderStudioNotes(source, credits), source);
});

test("an item whose only author is excluded renders plain and lists no contributor", () => {
  const credits = new Map([
    [1, { kind: "pull", login: "owner", bot: true }],
    [2, { kind: "issue", fixes: [{ number: 9, login: "owner", bot: true }] }],
  ]);
  assert.equal(renderStudioNotes("摘要。\n\n## 修复\n\n- 一 #1\n- 二 #2\n", credits), "摘要。\n\n## 修复\n\n- 一 #1\n- 二 #2 fixed in #9\n");
});
