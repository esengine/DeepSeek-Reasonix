// @vitest-environment jsdom
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { boot, STORAGE, t } from "../i18n";
import type { GlobalShortcutInfo } from "../port/host";
import { acceleratorLabel } from "./keys";

const fixture = vi.hoisted(() => ({ shell: true, now: { binding: "", status: "disabled" } as GlobalShortcutInfo, apply: vi.fn(), read: vi.fn() }));
vi.mock("../port/host", () => ({ host: () => ({ inShell: () => fixture.shell, globalShortcut: fixture.read, setGlobalShortcut: fixture.apply }) }));
import { GlobalShortcutSection } from "./GlobalShortcutSection";

beforeEach(() => {
  localStorage.clear(); localStorage.setItem(STORAGE, "zh"); boot();
  fixture.shell = true; fixture.now = { binding: "", status: "disabled" };
  fixture.read.mockImplementation(async () => fixture.now);
  fixture.apply.mockReset();
});
afterEach(cleanup);

it("is absent in a browser and cannot configure a remote host", async () => {
  fixture.shell = false; fixture.now = { binding: "", status: "unsupported" };
  render(<GlobalShortcutSection />);
  await Promise.resolve();
  expect(screen.queryByRole("textbox")).toBeNull();
  expect(fixture.apply).not.toHaveBeenCalled();
});

it("applies immediately through the shell, then disables without localStorage state", async () => {
  fixture.apply.mockImplementation(async (binding: string) => ({ binding, status: binding ? "registered" : "disabled" }));
  render(<GlobalShortcutSection />);
  await screen.findByText(t("全局快捷键已关闭"));
  await userEvent.type(screen.getByRole("textbox"), "Control+Shift+Space");
  await userEvent.click(screen.getByRole("button", { name: t("应用快捷键") }));
  expect(fixture.apply).toHaveBeenLastCalledWith("Control+Shift+Space");
  expect((await screen.findByRole("status")).textContent).toContain(acceleratorLabel("Control+Shift+Space"));
  expect(localStorage.length).toBe(1);
  await userEvent.click(screen.getByRole("button", { name: t("清除快捷键") }));
  expect(fixture.apply).toHaveBeenLastCalledWith("");
  expect((screen.getByRole("textbox") as HTMLInputElement).value).toBe("");
  expect(screen.getByRole("status").textContent).toBe(t("全局快捷键已关闭"));
});

it.each(["invalid", "unavailable", "save"] as const)("shows %s rebind failure beside the still-active old binding", async error => {
  fixture.now = { binding: "Control+Space", status: "registered" };
  fixture.apply.mockResolvedValue({ ...fixture.now, error });
  render(<GlobalShortcutSection />);
  await screen.findByText(t("已注册：{value}", { value: acceleratorLabel("Control+Space") }));
  await userEvent.clear(screen.getByRole("textbox"));
  await userEvent.type(screen.getByRole("textbox"), "occupied");
  await userEvent.click(screen.getByRole("button", { name: t("应用快捷键") }));
  expect(await screen.findByRole("alert")).toBeTruthy();
  expect(screen.getByRole("status").textContent).toContain(acceleratorLabel("Control+Space"));
  expect((screen.getByRole("textbox") as HTMLInputElement).value).toBe("occupied");
});

it("reports startup failure and lets the person retry the saved binding", async () => {
  fixture.now = { binding: "Control+Space", status: "unavailable" };
  fixture.apply.mockResolvedValue({ binding: "Control+Space", status: "registered" });
  render(<GlobalShortcutSection />);
  await screen.findByText(t("系统未能注册此快捷键：可能已被占用、未获授权，或当前桌面不支持。"));
  await userEvent.click(screen.getByRole("button", { name: t("应用快捷键") }));
  expect(fixture.apply).toHaveBeenCalledWith("Control+Space");
  expect(screen.getByRole("status").textContent).toContain(acceleratorLabel("Control+Space"));
});

it("does not call an IPC transport failure a successful registration", async () => {
  fixture.apply.mockRejectedValue(new Error("shell unavailable"));
  render(<GlobalShortcutSection />);
  await screen.findByText(t("全局快捷键已关闭"));
  await userEvent.click(screen.getByRole("button", { name: t("应用快捷键") }));
  expect(await screen.findByRole("alert")).toBeTruthy();
  expect(screen.getByRole("status").textContent).toBe(t("全局快捷键已关闭"));
});

it("records a modifier chord without submitting it, presents it, and clears it", async () => {
  fixture.apply.mockImplementation(async (binding: string) => ({ binding, status: binding ? "registered" : "disabled" }));
  render(<GlobalShortcutSection />);
  await screen.findByText(t("全局快捷键已关闭"));
  await userEvent.click(screen.getByRole("button", { name: t("录制快捷键") }));
  const input = screen.getByRole("textbox") as HTMLInputElement;
  expect(document.activeElement).toBe(input);
  expect(input.readOnly).toBe(true);
  fireEvent.keyDown(input, { key: "c", code: "KeyC" });
  fireEvent.keyDown(input, { key: "A", code: "KeyA", shiftKey: true });
  fireEvent.keyDown(input, { key: " ", code: "Space", shiftKey: true });
  expect(input.value).toBe("");
  expect(input.readOnly).toBe(true);
  fireEvent.keyDown(input, { key: "Control", ctrlKey: true });
  expect(input.value).toBe("");
  fireEvent.keyDown(input, { key: " ", code: "Space", ctrlKey: true, shiftKey: true });
  expect(input.value).toBe("Control+Shift+Space");
  expect(input.readOnly).toBe(false);
  expect(screen.getByText(acceleratorLabel("Control+Shift+Space")).tagName).toBe("KBD");
  expect(fixture.apply).not.toHaveBeenCalled();
  await userEvent.click(screen.getByRole("button", { name: t("应用快捷键") }));
  expect(fixture.apply).toHaveBeenLastCalledWith("Control+Shift+Space");
  await userEvent.click(screen.getByRole("button", { name: t("清除快捷键") }));
  expect(fixture.apply).toHaveBeenLastCalledWith("");
  expect(input.value).toBe("");
});

it("can cancel capture with Escape or Tab without changing the existing choice", async () => {
  fixture.now = { binding: "Control+Space", status: "registered" };
  render(<GlobalShortcutSection />);
  await screen.findByRole("status");
  const input = screen.getByRole("textbox") as HTMLInputElement;
  for (const key of ["Escape", "Tab"]) {
    await userEvent.click(screen.getByRole("button", { name: t("录制快捷键") }));
    await userEvent.keyboard(`{${key}}`);
    expect(input.readOnly).toBe(false);
    expect(input.value).toBe("Control+Space");
  }
  expect(fixture.apply).not.toHaveBeenCalled();
});

it("records Command, Option, navigation and plus keys without Electron grammar", async () => {
  render(<GlobalShortcutSection />);
  await screen.findByRole("status");
  const input = screen.getByRole("textbox") as HTMLInputElement;
  for (const [key, binding] of [["ArrowUp", "Alt+Super+Up"], ["+", "Alt+Super+Plus"]]) {
    await userEvent.click(screen.getByRole("button", { name: t("录制快捷键") }));
    fireEvent.keyDown(input, { key, altKey: true, metaKey: true });
    expect(input.value).toBe(binding);
  }
  expect(fixture.apply).not.toHaveBeenCalled();
});

it.each([
  [{ key: "˚", code: "KeyK", altKey: true }, "Alt+K"],
  [{ key: "!", code: "Digit1", ctrlKey: true, shiftKey: true }, "Control+Shift+1"],
  [{ key: "?", code: "Slash", ctrlKey: true, shiftKey: true }, "Control+Shift+/"],
])("records a chord when the layout produces $key", async (event, binding) => {
  render(<GlobalShortcutSection />);
  await screen.findByRole("status");
  await userEvent.click(screen.getByRole("button", { name: t("录制快捷键") }));
  const input = screen.getByRole("textbox") as HTMLInputElement;
  fireEvent.keyDown(input, event);
  expect(input.value).toBe(binding);
  expect(fixture.apply).not.toHaveBeenCalled();
});
