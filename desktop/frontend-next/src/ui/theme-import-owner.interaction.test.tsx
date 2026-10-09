// @vitest-environment jsdom
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { act, cleanup, fireEvent, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { ThemeImport } from "./ThemeImport";
import { MockPort } from "../port/mock";
import { boot, STORAGE } from "../i18n";
import type { AgentPort, ThemeImport as Receipt } from "../port/port";

beforeEach(() => { localStorage.setItem(STORAGE, "zh"); boot(); });
afterEach(() => { cleanup(); vi.restoreAllMocks(); });

function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (reason: unknown) => void;
  const promise = new Promise<T>((yes, no) => { resolve = yes; reject = no; });
  return { promise, resolve, reject };
}

const receipt = (id: string): Receipt => ({ pack: { id, name: id, tokens: {} } });
const port = () => new MockPort() as unknown as AgentPort;
const upload = () => fireEvent.change(document.querySelector('input[data-action="theme.import"]')!, {
  target: { files: [new File(["fixture"], "theme.zip")] },
});

function draw(p: AgentPort, imported = vi.fn(), use = vi.fn()) {
  const view = (next: AgentPort) => <ThemeImport port={next} empty onImported={imported} onUse={use} />;
  return { ...render(view(p)), view, imported, use };
}

it("keeps same-connection feedback, then clears the imported action before another kernel can use it", async () => {
  const a = port(), b = port();
  vi.spyOn(a, "importTheme").mockResolvedValue(receipt("remote-pack"));
  vi.spyOn(b, "importTheme").mockResolvedValue(receipt("local-pack"));
  const ui = draw(a);
  upload();
  await screen.findByRole("button", { name: "立即使用" });
  ui.rerender(ui.view(a));
  expect(screen.getByRole("status").textContent).toContain("remote-pack");
  ui.rerender(ui.view(b));
  expect(screen.queryByRole("button", { name: "立即使用" })).toBeNull();
  expect(screen.getByRole("status").textContent).not.toContain("remote-pack");
  upload();
  await userEvent.click(await screen.findByRole("button", { name: "立即使用" }));
  expect(ui.use).toHaveBeenCalledExactlyOnceWith("local-pack");
});

it.each(["folder", "failure"])("clears a completed %s result when the connection changes", async (result) => {
  const a = port(), b = port();
  vi.spyOn(a, "openThemeFolder").mockImplementation(async () => {
    if (result === "failure") throw new Error("remote folder unavailable");
    return "/remote/themes";
  });
  const ui = draw(a);
  await userEvent.click(screen.getByRole("button", { name: "打开主题目录" }));
  if (result === "failure") await screen.findByRole("alert");
  else expect(screen.getByRole("status").textContent).toContain("/remote/themes");
  ui.rerender(ui.view(b));
  expect(screen.queryByRole("alert")).toBeNull();
  expect(screen.getByRole("status").textContent).not.toContain("/remote/themes");
});

it.each(["success", "failure"])("a late import %s cannot notify or unlock the new connection", async (result) => {
  const a = port(), b = port();
  const old = deferred<Receipt>(), next = deferred<Receipt>();
  vi.spyOn(a, "importTheme").mockReturnValue(old.promise);
  const importing = vi.spyOn(b, "importTheme").mockReturnValue(next.promise);
  const ui = draw(a);
  upload();
  ui.rerender(ui.view(b));
  expect(screen.getByRole<HTMLButtonElement>("button", { name: "导入主题…" }).disabled).toBe(false);
  upload();
  expect(importing).toHaveBeenCalledTimes(1);
  await act(async () => {
    if (result === "success") old.resolve(receipt("remote-pack"));
    else old.reject(new Error("remote import refused"));
  });
  expect(ui.imported).not.toHaveBeenCalled();
  expect(screen.queryByRole("alert")).toBeNull();
  expect(screen.queryByRole("button", { name: "立即使用" })).toBeNull();
  expect(screen.getByRole<HTMLButtonElement>("button", { name: "正在导入…" }).disabled).toBe(true);
  expect(screen.getByRole<HTMLButtonElement>("button", { name: "打开主题目录" }).disabled).toBe(true);
  upload();
  expect(importing).toHaveBeenCalledTimes(1);
  await act(async () => next.resolve(receipt("local-pack")));
  expect(ui.imported).toHaveBeenCalledTimes(1);
  expect(screen.getByRole("status").textContent).toContain("local-pack");
});

it.each(["success", "failure"])("ignores a late folder %s from a previous connection", async (result) => {
  const a = port(), b = port();
  const old = deferred<string>();
  vi.spyOn(a, "openThemeFolder").mockReturnValue(old.promise);
  vi.spyOn(b, "openThemeFolder").mockResolvedValue("/local/themes");
  const ui = draw(a);
  await userEvent.click(screen.getByRole("button", { name: "打开主题目录" }));
  ui.rerender(ui.view(b));
  await act(async () => {
    if (result === "success") old.resolve("/remote/themes");
    else old.reject(new Error("remote folder refused"));
  });
  expect(screen.getByRole("status").textContent).not.toContain("/remote/themes");
  expect(screen.queryByRole("alert")).toBeNull();
  await userEvent.click(screen.getByRole("button", { name: "打开主题目录" }));
  expect(screen.getByRole("status").textContent).toContain("/local/themes");
});

it("does not revive an old import when switching away and back to the same port", async () => {
  const a = port(), b = port(), old = deferred<Receipt>();
  vi.spyOn(a, "importTheme").mockReturnValue(old.promise);
  const ui = draw(a);
  upload();
  ui.rerender(ui.view(b));
  ui.rerender(ui.view(a));
  await act(async () => old.resolve(receipt("previous-visit")));
  expect(ui.imported).not.toHaveBeenCalled();
  expect(screen.queryByRole("button", { name: "立即使用" })).toBeNull();
  expect(screen.getByRole<HTMLButtonElement>("button", { name: "导入主题…" }).disabled).toBe(false);
});

it("keeps the pending lock and result through a same-port parent refresh", async () => {
  const a = port(), request = deferred<Receipt>();
  const importing = vi.spyOn(a, "importTheme").mockReturnValue(request.promise);
  const ui = draw(a);
  upload();
  ui.rerender(ui.view(a));
  upload();
  expect(importing).toHaveBeenCalledTimes(1);
  expect(screen.getByRole<HTMLButtonElement>("button", { name: "正在导入…" }).disabled).toBe(true);
  await act(async () => request.resolve(receipt("current-pack")));
  expect(ui.imported).toHaveBeenCalledTimes(1);
  expect(screen.getByRole("status").textContent).toContain("current-pack");
});
