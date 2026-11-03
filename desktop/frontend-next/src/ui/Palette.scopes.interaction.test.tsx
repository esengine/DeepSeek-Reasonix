// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { Palette, type Command, type PaletteSetting } from "./Palette";

afterEach(cleanup);

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
const labels = () => [...document.querySelectorAll(".palette-label")].map((e) => e.textContent);
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
});
