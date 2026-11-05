// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { Palette, type Command, type PaletteSetting } from "./Palette";

afterEach(() => { cleanup(); localStorage.clear(); });

const commands: Command[] = [
  { id: "new", label: "新建会话", icon: "plus", keywords: "new", keys: "Alt N", run: vi.fn() },
  { id: "export", label: "导出当前会话", icon: "download", keywords: "export", run: vi.fn() },
];
const settings: PaletteSetting[] = [
  { id: "data:backup", label: "导出与导入配置", path: "数据", keywords: "export" },
  { id: "look:theme", label: "配色", path: "外观", keywords: "" },
];
const tree = [{ root: "/w", name: "w", sessions: Array.from({ length: 9 }, (_, i) => ({ path: `/w/${i}.jsonl`, name: `s${i}`, title: i === 0 ? "修导出编码" : `会话 ${i}` })) }];

function draw(over: Partial<Parameters<typeof Palette>[0]> = {}) {
  const props = { open: true, onClose: vi.fn(), commands, tree, onOpenSession: vi.fn(), settings, onOpenSetting: vi.fn(), ...over };
  render(<Palette {...props} />);
  return props;
}
const labels = () => [...document.querySelectorAll(".palette-label")].map((e) => e.firstChild?.textContent);
const hits = () => [...document.querySelectorAll(".palette-hit span")].map((e) => e.textContent);

describe("the palette's scopes", () => {
  it("narrows to one kind with a leading symbol and shows the scope as a tag", async () => {
    draw();
    await userEvent.type(screen.getByRole("textbox"), "#修");
    expect(labels()).toEqual(["会话"]);
    expect(hits()).toEqual(["修导出编码"]);
    expect(document.querySelector(".palette-scope")?.textContent).toBe("会话");
    expect((screen.getByRole("textbox") as HTMLInputElement).value).toBe("修");
  });

  it("opens the settings kind with a comma and an action with >", async () => {
    draw();
    await userEvent.type(screen.getByRole("textbox"), ",");
    expect(labels()).toEqual(["设置"]);
    await userEvent.clear(screen.getByRole("textbox"));
    await userEvent.keyboard("{Backspace}");
    await userEvent.type(screen.getByRole("textbox"), ">");
    expect(labels()).toEqual(["快捷操作"]);
  });

  it("cycles the scope with Tab and drops it with Backspace on an empty field", async () => {
    draw();
    const box = screen.getByRole("textbox");
    const tag = () => document.querySelector(".palette-scope")?.textContent ?? "";
    await userEvent.type(box, "{Tab}");
    expect(tag()).toBe("操作");
    await userEvent.keyboard("{Tab}");
    expect(tag()).toBe("会话");
    await userEvent.keyboard("{Tab}");
    expect(tag()).toBe("设置");
    await userEvent.keyboard("{Tab}");
    expect(tag()).toBe("");
    await userEvent.keyboard("{Tab}{Backspace}");
    expect(tag()).toBe("");
  });
});

describe("the palette's answers", () => {
  it("puts the best match alone in its own group and preselects it", async () => {
    draw();
    await userEvent.type(screen.getByRole("textbox"), "导出");
    expect(labels()[0]).toBe("最佳匹配");
    expect(document.querySelectorAll(".palette-label + .palette-hit").length).toBeGreaterThan(0);
    const first = document.querySelector(".palette-hit");
    expect(first?.getAttribute("aria-selected")).toBe("true");
    expect(first?.textContent).toContain("修导出编码");
    expect(labels()).toEqual(["最佳匹配", "设置", "快捷操作"]);
  });

  it("lists at most five of each kind", () => {
    draw();
    expect(document.querySelectorAll('[data-target^="s:"]')).toHaveLength(5);
  });

  it("opens a setting through the callback with its target", async () => {
    const p = draw();
    await userEvent.type(screen.getByRole("textbox"), ",配色");
    await userEvent.keyboard("{Enter}");
    expect(p.onOpenSetting).toHaveBeenCalledWith("look:theme");
    expect(p.onClose).toHaveBeenCalled();
  });

  it("says how to widen the search when nothing matched", async () => {
    draw();
    await userEvent.type(screen.getByRole("textbox"), "zzzz");
    expect(document.querySelector(".palette-empty")?.textContent).toContain("没有匹配");
  });

  it("prints the scope keys in the footer", () => {
    draw();
    expect(document.querySelector(".palette-help")?.textContent).toMatch(/Tab 切换范围/);
    expect(document.querySelector(".palette-help")?.textContent).toMatch(/> 操作/);
  });

  it("offers the scopes as chips that narrow the list and toggle off again", async () => {
    draw();
    const sessions = screen.getByRole("button", { name: "#会话" });
    await userEvent.click(sessions);
    expect(sessions.getAttribute("aria-pressed")).toBe("true");
    expect(labels()).toEqual(["会话"]);
    await userEvent.click(sessions);
    expect(sessions.getAttribute("aria-pressed")).toBe("false");
    expect(labels()).toEqual(["快捷操作", "会话"]);
  });

  it("counts what each group found once something is typed, but not the best match", async () => {
    draw();
    await userEvent.type(screen.getByRole("textbox"), "导出");
    const heads = [...document.querySelectorAll(".palette-label")].map((e) => [e.firstChild?.textContent, e.querySelector("i")?.textContent ?? ""]);
    expect(heads[0]).toEqual(["最佳匹配", ""]);
    expect(heads.slice(1).every(([, n]) => /^\d+$/.test(n ?? ""))).toBe(true);
  });

  it("marks a session that is running or waiting on the person, with the word beside its project", async () => {
    const open = [{ root: "/w", name: "w", sessions: [
      { path: "/w/a.jsonl", name: "a", title: "跑一遍评测", runtimeId: "r1" },
      { path: "/w/b.jsonl", name: "b", title: "修导出编码", runtimeId: "r2" },
      { path: "/w/c.jsonl", name: "c", title: "站点改版" },
    ] }];
    draw({ tree: open, runOf: (id) => ({ r1: "running", r2: "halt" } as Record<string, string>)[id ?? ""] });
    await userEvent.click(screen.getByRole("button", { name: "#会话" }));
    const rows = [...document.querySelectorAll(".palette-hit")].map((e) => [e.querySelector("span")?.textContent, e.querySelector("small")?.textContent, e.querySelector(".palette-dot")?.getAttribute("data-run") ?? ""]);
    expect(rows).toEqual([["跑一遍评测", "w · 运行中", "running"], ["修导出编码", "w · 等你确认", "halt"], ["站点改版", "w", "none"]]);
  });

  it("prints the keys on the left of the foot and the scope symbols on the right", () => {
    draw();
    const foot = [...document.querySelectorAll(".palette-help span")].map((e) => e.textContent);
    expect(foot).toEqual(["↑↓ 选择 · Enter 打开 · Tab 切换范围", "> 操作 · # 会话 · , 设置"]);
  });
});
