// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { Memory } from "./Memory";
import { MockPort } from "../port/mock";
import type { AgentPort, MemoryCatalog, MemoryEntry } from "../port/port";

afterEach(cleanup);

const FACTS: MemoryEntry[] = [
  { name: "merge-queue", title: "发布前先确认合并队列", activation: "relevant", type: "constraint", galaxy: "发布流程",
    importance: 3, state: "clear", noDecay: true, uses: 4, lastUsedAt: "2026-11-01", body: "queue body" },
  { name: "chinese", title: "回复用中文", activation: "pinned", type: "user", galaxy: "用户偏好",
    importance: 3, state: "clear", clearForDays: 80, uses: 6, lastUsedAt: "2026-10-30", body: "chinese body" },
  { name: "one-release", title: "每天最多发一个版本", activation: "relevant", type: "project", galaxy: "发布流程",
    importance: 2, state: "clear", clearForDays: 15, new: true, createdAt: "2026-11-02", body: "release body" },
  { name: "old-build", title: "2.3 版曾经的构建问题", activation: "relevant", type: "project",
    importance: 1, state: "faded", updatedAt: "2026-03-01", body: "old body" },
];

function fixture() {
  const port = new MockPort() as unknown as AgentPort;
  let facts = FACTS.map((m) => ({ ...m }));
  vi.spyOn(port, "memories").mockImplementation(async (): Promise<MemoryCatalog> => ({ memories: facts, recallQuery: "" }));
  const confirm = vi.spyOn(port, "confirmMemory").mockImplementation(async (name) => {
    facts = facts.map((m) => m.name === name ? { ...m, new: false } : m);
  });
  const forget = vi.spyOn(port, "forgetMemory").mockImplementation(async (name) => {
    facts = facts.filter((m) => m.name !== name);
  });
  const save = vi.spyOn(port, "saveMemory").mockResolvedValue();
  render(<Memory port={port} />);
  return { confirm, forget, save };
}

const listed = () => Array.from(document.querySelectorAll<HTMLElement>(".memrow"), (el) => el.dataset.target);
const nav = (label: string) => screen.getByRole("button", { name: new RegExp(`^${label}\\s*\\d+$`) });

it("lists the newest written or recalled first and shows the first fact's details", async () => {
  fixture();
  await waitFor(() => expect(listed()).toEqual(["one-release", "merge-queue", "chinese", "old-build"]));
  expect(document.querySelector<HTMLElement>(".memdetail")!.dataset.target).toBe("one-release");
  await userEvent.click(screen.getByRole("button", { name: /^发布前先确认合并队列/ }));
  const detail = document.querySelector<HTMLElement>(".memdetail")!;
  expect(detail.dataset.target).toBe("merge-queue");
  expect(within(detail).getByText("清晰，不会变淡")).toBeTruthy();
  expect(within(detail).getByText("4 次")).toBeTruthy();
  expect(within(detail).getByText("发布流程")).toBeTruthy();
  await userEvent.click(screen.getByRole("button", { name: /^回复用中文/ }));
  expect(within(document.querySelector<HTMLElement>(".memdetail")!).getByText("清晰，约 80 天后才会变模糊")).toBeTruthy();
});

it("narrows by the rail and by search", async () => {
  fixture();
  await waitFor(() => expect(listed()).toHaveLength(4));
  expect(nav("发布流程").textContent).toContain("2");
  await userEvent.click(nav("发布流程"));
  expect(listed()).toEqual(["one-release", "merge-queue"]);
  await userEvent.click(nav("约束"));
  expect(listed()).toEqual(["merge-queue"]);
  await userEvent.click(nav("已淡忘"));
  expect(listed()).toEqual(["old-build"]);
  await userEvent.click(nav("全部"));
  fireEvent.change(screen.getByRole("searchbox", { name: "搜索记忆" }), { target: { value: "版本" } });
  expect(listed()).toEqual(["one-release"]);
});

it("confirms or archives a new fact straight from the list", async () => {
  const { confirm, forget } = fixture();
  await waitFor(() => expect(listed()).toHaveLength(4));
  await userEvent.click(nav("新的"));
  expect(listed()).toEqual(["one-release"]);
  expect(document.querySelector(".memdetail")).toBeNull();
  const row = document.querySelector<HTMLElement>('.memrow[data-target="one-release"]')!;
  await userEvent.click(within(row).getByRole("button", { name: "对" }));
  expect(confirm).toHaveBeenCalledExactlyOnceWith("one-release");
  await waitFor(() => expect(listed()).toEqual([]));
  expect(screen.getByText("没有等你确认的记忆。")).toBeTruthy();
  expect(forget).not.toHaveBeenCalled();
});

it("moves a fact to another galaxy, importance and type through the editor", async () => {
  const { save } = fixture();
  await waitFor(() => expect(listed()).toHaveLength(4));
  await userEvent.click(screen.getByRole("button", { name: /^每天最多发一个版本/ }));
  await userEvent.click(screen.getByRole("button", { name: "编辑" }));
  fireEvent.change(screen.getByRole("combobox", { name: "星系" }), { target: { value: "工作流" } });
  fireEvent.change(screen.getByRole("combobox", { name: "重要度" }), { target: { value: "3" } });
  fireEvent.change(screen.getByRole("combobox", { name: "类型" }), { target: { value: "constraint" } });
  await userEvent.click(screen.getByRole("button", { name: "保存" }));
  expect(save).toHaveBeenCalledExactlyOnceWith({
    name: "one-release", title: "每天最多发一个版本", description: "", body: "release body", activation: "relevant",
    type: "constraint", galaxy: "工作流", importance: 3,
  });
});
