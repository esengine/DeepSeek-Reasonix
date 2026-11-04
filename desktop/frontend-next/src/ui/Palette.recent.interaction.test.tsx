// @vitest-environment jsdom
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { Palette, type Command } from "./Palette";

beforeEach(() => localStorage.clear());
afterEach(cleanup);

const commands: Command[] = [
  { id: "new", label: "新建会话", icon: "plus", keywords: "new", run: vi.fn() },
  { id: "export", label: "导出当前会话", icon: "download", keywords: "export", run: vi.fn() },
];
const props = () => ({ open: true, onClose: vi.fn(), commands, tree: [], onOpenSession: vi.fn() });
const labels = () => [...document.querySelectorAll(".palette-label")].map((e) => e.textContent);
const hits = () => [...document.querySelectorAll(".palette-hit span")].map((e) => e.textContent);

it("lists what was last used first when the palette opens on nothing", async () => {
  const first = render(<Palette {...props()} />);
  expect(labels()).toEqual(["快捷操作"]);
  await userEvent.click(screen.getByText("导出当前会话"));
  first.unmount();
  render(<Palette {...props()} />);
  expect(labels()).toEqual(["最近用过", "快捷操作"]);
  expect(hits()).toEqual(["导出当前会话", "新建会话"]);
});

it("does not show the recent group once something is typed", async () => {
  localStorage.setItem("palette.recent", JSON.stringify(["c:export"]));
  render(<Palette {...props()} />);
  await userEvent.type(screen.getByRole("textbox"), "新");
  expect(labels()).not.toContain("最近用过");
});
