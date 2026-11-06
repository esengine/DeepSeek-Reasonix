// @vitest-environment jsdom
import { cleanup, render } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, expect, it } from "vitest";
import { ContextSummaryCard } from "./ContextSummaryCard";
import type { ContextBreakdown } from "../port/port";
import "./testkit";

afterEach(cleanup);

const breakdown = (over: Partial<ContextBreakdown> = {}): ContextBreakdown => ({
  used: 40_000, window: 1_000_000, compact_at: 160_000, boundary: "capacity", capacity_at: 800_000,
  system: 5_000, tools: 6_000, user: 2_000, reply: 3_000, output: 24_000,
  largest: [
    { class: "output", tool: "read_file", tokens: 12_000 },
    { class: "output", tool: "grep", tokens: 8_000, active: true },
    { class: "user", tokens: 1_500 },
  ],
  ...over,
});
const card = (context: ContextBreakdown) => <ContextSummaryCard context={context} mcp={[]} percent={4} onManage={() => {}} />;
const toggle = (cls: string) => document.querySelector<HTMLButtonElement>(`[data-action="context.largest"][data-value="${cls}"]`);

it("opens a class to the messages that made it heavy, naming the tool and the running turn", async () => {
  const user = userEvent.setup();
  render(card(breakdown()));
  expect(document.querySelector(".chrome-context-items")).toBeNull();
  expect(toggle("output")?.getAttribute("aria-expanded")).toBe("false");

  await user.click(toggle("output")!);
  const list = document.querySelector(".chrome-context-items")!;
  expect(toggle("output")?.getAttribute("aria-expanded")).toBe("true");
  expect(list.textContent).toContain("read_file");
  expect(list.textContent).toContain("grep");
  expect(list.querySelector(".head")?.textContent).toContain("工具输出");
  expect(list.querySelectorAll("li:not(.head)")).toHaveLength(2);
  expect(list.querySelectorAll("li:not(.head)")[1].textContent).toContain("进行中的回合");
});

it("keeps one class open at a time", async () => {
  const user = userEvent.setup();
  render(card(breakdown()));
  await user.click(toggle("output")!);
  await user.click(toggle("user")!);
  expect(document.querySelectorAll(".chrome-context-items")).toHaveLength(1);
  expect(toggle("output")?.getAttribute("aria-expanded")).toBe("false");
  expect(toggle("user")?.getAttribute("aria-expanded")).toBe("true");
  await user.click(toggle("user")!);
  expect(document.querySelector(".chrome-context-items")).toBeNull();
});

it("offers no list for a class with no holders named, nor at all from a kernel that names none", () => {
  render(card(breakdown()));
  expect(toggle("reply")).toBeNull();
  expect(toggle("system")).toBeNull();
  cleanup();
  render(card(breakdown({ largest: undefined })));
  expect(document.querySelector('[data-action="context.largest"]')).toBeNull();
  expect(document.querySelectorAll(".chrome-context-parts b")).toHaveLength(5);
});

it("says where the window folds on its own, beside the capacity", () => {
  render(card(breakdown({ window: 128_000, compact_at: 115_000 })));
  const total = document.querySelector(".chrome-context-total")!;
  expect(total.textContent).toContain("模型容量");
  expect(total.textContent).toContain("到 115K 自动压缩");
});

it("says nothing about a fold the kernel has no threshold for", () => {
  render(card(breakdown({ compact_at: 0 })));
  expect(document.querySelector(".chrome-context-total")!.textContent).not.toContain("自动压缩");
});
