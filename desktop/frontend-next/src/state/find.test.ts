import { describe, expect, it } from "vitest";
import { MAX, saidBy, search, stepped } from "./find";
import type { Item } from "./session";

const say = (id: string, text: string): Item => ({ t: "say", id, text, done: true });
const tool = (id: string, over: { args?: string; output?: string }): Item => ({
  t: "tool",
  id,
  running: false,
  children: [],
  tool: { id, name: "bash", readOnly: false, ...over },
});

describe("searching a conversation", () => {
  it("finds nothing for an empty query", () => {
    expect(search([say("a", "hello")], "")).toEqual([]);
    expect(search([say("a", "hello")], "   ")).toEqual([]);
  });

  it("reports every occurrence in transcript order", () => {
    const hits = search([say("a", "one two one"), say("b", "one")], "one");
    expect(hits.map((h) => [h.id, h.row, h.at])).toEqual([
      ["a", 0, 0],
      ["a", 0, 8],
      ["b", 1, 0],
    ]);
  });

  it("ignores case", () => {
    expect(search([say("a", "Retry The Turn")], "retry the")).toHaveLength(1);
  });

  it("does not run off the end on overlapping text", () => {
    expect(search([say("a", "aaaa")], "aa")).toHaveLength(2);
  });

  it("reads what a tool call carries, not only what a card draws", () => {
    const items = [tool("t", { args: '{"command":"go test ./internal/provider/"}', output: "FAIL TestRetryOn401" })];
    expect(search(items, "TestRetryOn401")).toHaveLength(1);
    expect(search(items, "internal/provider")).toHaveLength(1);
  });

  it("reads reasoning as well as the answer", () => {
    const item: Item = { t: "say", id: "s", text: "答案", reasoning: "先看看历史", done: true };
    expect(search([item], "历史")).toHaveLength(1);
  });

  it("stops counting past the cap", () => {
    const many = Array.from({ length: MAX + 50 }, (_, i) => say(`s${i}`, "x"));
    expect(search(many, "x")).toHaveLength(MAX);
  });

  it("keeps a row's text stable across repeated searches", () => {
    const items = [say("a", "one"), say("b", "ONE")];
    expect(search(items, "one")).toHaveLength(2);
    expect(search(items, "one")).toHaveLength(2);
  });
});

describe("what a row says", () => {
  it("covers a notice's detail", () => {
    const item: Item = { t: "notice", id: "n", level: "warn", text: "截断", detail: "bash · rg" };
    expect(saidBy(item).join(" ")).toContain("bash · rg");
  });

  it("covers what a remembered fact holds", () => {
    const item: Item = {
      t: "remember",
      id: "m",
      m: { name: "n", title: "标题", description: "一句话", scope: "project", activation: "always", body: "正文" },
    };
    expect(saidBy(item)).toContain("正文");
  });
});

describe("stepping between hits", () => {
  it("wraps in both directions", () => {
    expect(stepped(2, 3, 1)).toBe(0);
    expect(stepped(0, 3, -1)).toBe(2);
  });

  it("stays at zero with nothing to step through", () => {
    expect(stepped(0, 0, 1)).toBe(0);
    expect(stepped(0, 0, -1)).toBe(0);
  });
});
