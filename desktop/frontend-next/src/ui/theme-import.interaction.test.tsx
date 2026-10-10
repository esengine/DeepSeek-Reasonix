// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { act, cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { ThemeImport } from "./ThemeImport";
import { MockPort } from "../port/mock";
import type { AgentPort, ThemeImport as ImportResult } from "../port/port";

afterEach(cleanup);

it.each(["success", "failure"])("locks file selection until theme import %s settles", async (outcome) => {
  const port = new MockPort() as unknown as AgentPort;
  let finish!: (result: ImportResult) => void;
  let fail!: (error: Error) => void;
  const pending = new Promise<ImportResult>((resolve, reject) => { finish = resolve; fail = reject; });
  const second = { pack: { id: "second", name: "Second Theme", tokens: {} } };
  const take = vi.spyOn(port, "importTheme").mockImplementationOnce(() => pending).mockResolvedValue(second);
  const onImported = vi.fn();
  const onUse = vi.fn();
  const view = render(<ThemeImport port={port} empty onImported={onImported} onUse={onUse} />);
  const input = view.container.querySelector<HTMLInputElement>('input[type="file"]')!;
  const firstFile = new File(["first fixture"], "first.zip", { type: "application/zip" });
  const secondFile = new File(["second fixture"], "second.zip", { type: "application/zip" });
  await userEvent.upload(input, firstFile);
  const button = screen.getByRole<HTMLButtonElement>("button", { name: "正在导入…" });
  const locked = [button.disabled, input.disabled];
  await userEvent.click(button);
  await userEvent.upload(input, secondFile);
  const callsWhilePending = take.mock.calls.length;
  const importedWhilePending = onImported.mock.calls.length;
  await act(async () => {
    if (outcome === "success") finish({ pack: { id: "first", name: "First Theme", tokens: {} }, ignored: ["notes.txt"] });
    else fail(new Error("theme import failed"));
  });
  expect(callsWhilePending).toBe(1);
  expect(locked).toEqual([true, true]);
  expect(importedWhilePending).toBe(0);
  expect(take).toHaveBeenCalledExactlyOnceWith([firstFile]);
  expect(screen.getByRole<HTMLButtonElement>("button", { name: "导入主题…" }).disabled).toBe(false);
  expect(input.disabled).toBe(false);
  expect(input.value).toBe("");
  expect(onImported).toHaveBeenCalledTimes(outcome === "success" ? 1 : 0);
  if (outcome === "success") {
    expect(screen.getByText("已导入「First Theme」。 未读取：notes.txt")).toBeTruthy();
    await userEvent.click(screen.getByRole("button", { name: "立即使用" }));
    expect(onUse).toHaveBeenCalledExactlyOnceWith("first");
  } else {
    expect(screen.getByRole("alert").textContent).toBe("theme import failed");
    expect(screen.queryByRole("button", { name: "立即使用" })).toBeNull();
    expect(onUse).not.toHaveBeenCalled();
  }
  await userEvent.upload(input, secondFile);
  expect(take).toHaveBeenCalledTimes(2);
  expect(take).toHaveBeenLastCalledWith([secondFile]);
  expect(screen.queryByRole("alert")).toBeNull();
  expect(screen.getByText("已导入「Second Theme」。")).toBeTruthy();
  await userEvent.click(screen.getByRole("button", { name: "立即使用" }));
  expect(onUse).toHaveBeenLastCalledWith("second");
  expect(onImported).toHaveBeenCalledTimes(outcome === "success" ? 2 : 1);
});

it.each(["success", "failure"])("holds folder reveal alongside selection until import %s settles", async (outcome) => {
  const port = new MockPort() as unknown as AgentPort;
  let finish!: (result: ImportResult) => void;
  let fail!: (error: Error) => void;
  const pending = new Promise<ImportResult>((resolve, reject) => { finish = resolve; fail = reject; });
  const take = vi.spyOn(port, "importTheme").mockReturnValue(pending);
  const reveal = vi.spyOn(port, "openThemeFolder").mockResolvedValue("/fixture/themes");
  const onImported = vi.fn();
  const onUse = vi.fn();
  const view = render(<ThemeImport port={port} empty onImported={onImported} onUse={onUse} />);
  const input = view.container.querySelector<HTMLInputElement>('input[type="file"]')!;
  const first = new File(["first fixture"], "first.zip", { type: "application/zip" });
  await userEvent.upload(input, first);
  const folder = screen.getByRole<HTMLButtonElement>("button", { name: "打开主题目录" });
  expect(folder.disabled).toBe(true);
  await userEvent.click(folder);
  expect(reveal).not.toHaveBeenCalled();
  expect(screen.queryByText("主题目录：/fixture/themes")).toBeNull();
  const button = screen.getByRole<HTMLButtonElement>("button", { name: "正在导入…" });
  expect(button.disabled).toBe(true);
  expect(input.disabled).toBe(true);
  await userEvent.upload(input, new File(["second fixture"], "second.zip", { type: "application/zip" }));
  expect(take).toHaveBeenCalledExactlyOnceWith([first]);
  expect(onImported).not.toHaveBeenCalled();
  await act(async () => {
    if (outcome === "success") finish({ pack: { id: "first", name: "First Theme", tokens: {} } });
    else fail(new Error("theme import failed"));
  });
  expect(input.disabled).toBe(false);
  expect(screen.getByRole<HTMLButtonElement>("button", { name: "导入主题…" }).disabled).toBe(false);
  if (outcome === "success") {
    expect(screen.getByText("已导入「First Theme」。")).toBeTruthy();
    await userEvent.click(screen.getByRole("button", { name: "立即使用" }));
    expect(onUse).toHaveBeenCalledExactlyOnceWith("first");
    expect(onImported).toHaveBeenCalledOnce();
  } else {
    expect(screen.getByRole("alert").textContent).toBe("theme import failed");
    expect(screen.queryByRole("button", { name: "立即使用" })).toBeNull();
    expect(onImported).not.toHaveBeenCalled();
  }
  expect(folder.disabled).toBe(false);
  await userEvent.click(folder);
  expect(reveal).toHaveBeenCalledExactlyOnceWith();
  expect(screen.getByText("主题目录：/fixture/themes")).toBeTruthy();
});

it.each(["success", "failure"])("keeps a remounted import independent of the previous mount's %s", async (outcome) => {
  const port = new MockPort() as unknown as AgentPort;
  let finishOld!: (result: ImportResult) => void;
  let failOld!: (error: Error) => void;
  let finishNew!: (result: ImportResult) => void;
  const oldPending = new Promise<ImportResult>((resolve, reject) => { finishOld = resolve; failOld = reject; });
  const newPending = new Promise<ImportResult>((resolve) => { finishNew = resolve; });
  const take = vi.spyOn(port, "importTheme").mockReturnValueOnce(oldPending).mockReturnValueOnce(newPending);
  const oldImported = vi.fn();
  const onImported = vi.fn();
  const onUse = vi.fn();
  const firstView = render(<ThemeImport port={port} empty onImported={oldImported} onUse={onUse} />);
  const first = new File(["first fixture"], "first.zip", { type: "application/zip" });
  await userEvent.upload(firstView.container.querySelector<HTMLInputElement>('input[type="file"]')!, first);
  firstView.unmount();
  const view = render(<ThemeImport port={port} empty onImported={onImported} onUse={onUse} />);
  const input = view.container.querySelector<HTMLInputElement>('input[type="file"]')!;
  expect(input.disabled).toBe(false);
  expect(screen.queryByRole("alert")).toBeNull();
  expect(screen.queryByRole("button", { name: "立即使用" })).toBeNull();
  const second = new File(["second fixture"], "second.zip", { type: "application/zip" });
  await userEvent.upload(input, second);
  await act(async () => {
    if (outcome === "success") finishOld({ pack: { id: "first", name: "First Theme", tokens: {} } });
    else failOld(new Error("old import failed"));
  });
  expect(input.disabled).toBe(true);
  expect(screen.getByRole<HTMLButtonElement>("button", { name: "正在导入…" }).disabled).toBe(true);
  expect(screen.queryByText("已导入「First Theme」。")).toBeNull();
  expect(screen.queryByRole("alert")).toBeNull();
  expect(screen.queryByRole("button", { name: "立即使用" })).toBeNull();
  expect(onImported).not.toHaveBeenCalled();
  expect(oldImported).toHaveBeenCalledTimes(outcome === "success" ? 1 : 0);
  await act(async () => { finishNew({ pack: { id: "second", name: "Second Theme", tokens: {} } }); });
  expect(take.mock.calls).toEqual([[[first]], [[second]]]);
  expect(input.disabled).toBe(false);
  expect(screen.getByText("已导入「Second Theme」。")).toBeTruthy();
  expect(onImported).toHaveBeenCalledOnce();
  await userEvent.click(screen.getByRole("button", { name: "立即使用" }));
  expect(onUse).toHaveBeenCalledExactlyOnceWith("second");
});
