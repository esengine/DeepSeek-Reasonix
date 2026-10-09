// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { boot, STORAGE } from "../i18n";
import type { AgentPort, HookCatalog, HookDryRun, HookEntry } from "../port/port";
import { Hooks } from "./Hooks";

afterEach(() => {
  cleanup();
  localStorage.removeItem(STORAGE);
  boot();
});

function catalog(): HookCatalog {
  return {
    globalPath: "/fixture/global/settings.json",
    projectPath: "/fixture/project/settings.json",
    sources: [],
    events: [
      { name: "PreToolUse", blocking: true, usesMatch: true },
      { name: "PostToolUse", blocking: false, usesMatch: true },
      { name: "Stop", blocking: false, usesMatch: false },
    ],
    hooks: [
      { event: "PreToolUse", match: "write_file", command: "printf original", scope: "global" },
      { event: "Stop", command: "printf project", scope: "project" },
      { event: "Stop", command: "printf plugin", scope: "plugin", readOnly: true },
    ],
  };
}

const result: HookDryRun = { decision: "allow", exitCode: 0, durationMs: 1, blocks: false, stdout: "trial output" };

function pending<T>() {
  let finish!: (value: T) => void;
  const promise = new Promise<T>((resolve) => { finish = resolve; });
  return { promise, finish };
}

async function draw(initial = catalog()) {
  localStorage.setItem(STORAGE, "zh");
  boot();
  const hooks = vi.fn(async () => initial);
  const saveHooks = vi.fn(async (_scope: string, _rules: HookEntry[]) => {});
  const dryRunHook = vi.fn(async (_rule: HookEntry) => result);
  const onChanged = vi.fn();
  const port = { hooks, saveHooks, dryRunHook } as unknown as AgentPort;
  const view = render(<Hooks port={port} onChanged={onChanged} />);
  await userEvent.click(await screen.findByRole("button", { name: /手动添加/ }));
  return { ...view, port, hooks, saveHooks, dryRunHook, onChanged };
}

function command(container: HTMLElement, index = 0) {
  return container.querySelectorAll<HTMLInputElement>("input.cmd")[index];
}

function edit(container: HTMLElement) {
  fireEvent.change(screen.getByRole("combobox"), { target: { value: "PostToolUse" } });
  fireEvent.change(container.querySelector("input.match")!, { target: { value: "edit_file" } });
  fireEvent.change(command(container), { target: { value: "printf edited" } });
}

function expectEdited(container: HTMLElement) {
  expect(command(container).value).toBe("printf edited");
  expect((screen.getByRole("combobox") as HTMLSelectElement).value).toBe("PostToolUse");
  expect(container.querySelector<HTMLInputElement>("input.match")!.value).toBe("edit_file");
}

it("keeps the unsaved fields when the parent rerenders without a new catalog", async () => {
  const view = await draw();
  edit(view.container);
  view.rerender(<Hooks port={view.port} onChanged={() => {}} />);
  expectEdited(view.container);
  expect(view.hooks).toHaveBeenCalledTimes(1);
});

it("keeps the draft while an edited rule is being tried", async () => {
  const view = await draw();
  const trial = pending<HookDryRun>();
  view.dryRunHook.mockReturnValueOnce(trial.promise);
  edit(view.container);
  await userEvent.click(screen.getByRole("button", { name: "试运行" }));
  try {
    expectEdited(view.container);
    expect(view.dryRunHook).toHaveBeenCalledWith(expect.objectContaining({ event: "PostToolUse", match: "edit_file", command: "printf edited" }));
    expect(screen.getByRole<HTMLButtonElement>("button", { name: "运行中…" }).disabled).toBe(true);
  } finally {
    await act(async () => { trial.finish(result); });
  }
});

it("keeps the tested draft when the trial result is displayed", async () => {
  const view = await draw();
  edit(view.container);
  await userEvent.click(screen.getByRole("button", { name: "试运行" }));
  await screen.findByText("trial output");
  expectEdited(view.container);
  await userEvent.click(screen.getByRole("button", { name: "保存" }));
  expect(view.saveHooks).toHaveBeenCalledWith("user", [expect.objectContaining({ command: "printf edited", event: "PostToolUse", match: "edit_file" })]);
});

it("keeps the draft when a trial fails", async () => {
  const view = await draw();
  view.dryRunHook.mockRejectedValueOnce(new Error("trial unavailable"));
  edit(view.container);
  await userEvent.click(screen.getByRole("button", { name: "试运行" }));
  await screen.findByText("trial unavailable");
  expectEdited(view.container);
  expect(view.saveHooks).not.toHaveBeenCalled();
});

it("keeps an unsaved added rule while another rule is tried", async () => {
  const view = await draw();
  await userEvent.click(screen.getByRole("button", { name: "添加规则" }));
  fireEvent.change(command(view.container, 1), { target: { value: "printf added" } });
  await userEvent.click(screen.getAllByRole("button", { name: "试运行" })[0]);
  await screen.findByText("trial output");
  expect(view.container.querySelectorAll("input.cmd")).toHaveLength(2);
  expect(command(view.container, 1).value).toBe("printf added");
});

it("keeps the draft while its save is pending", async () => {
  const view = await draw();
  const saving = pending<void>();
  view.saveHooks.mockReturnValueOnce(saving.promise);
  edit(view.container);
  await userEvent.click(screen.getByRole("button", { name: "保存" }));
  try {
    expectEdited(view.container);
    expect(screen.getByRole<HTMLButtonElement>("button", { name: "保存中…" }).disabled).toBe(true);
    expect(view.onChanged).not.toHaveBeenCalled();
  } finally {
    await act(async () => { saving.finish(); });
  }
});

it("keeps all edited fields when saving fails", async () => {
  const view = await draw();
  view.saveHooks.mockRejectedValueOnce(new Error("save unavailable"));
  edit(view.container);
  await userEvent.click(screen.getByRole("button", { name: "保存" }));
  await screen.findByText("save unavailable");
  expectEdited(view.container);
  expect(view.onChanged).not.toHaveBeenCalled();
  expect(view.hooks).toHaveBeenCalledTimes(1);
});

it("retries the edited rules after a refused save", async () => {
  const view = await draw();
  view.saveHooks.mockRejectedValueOnce(new Error("save unavailable"));
  edit(view.container);
  await userEvent.click(screen.getByRole("button", { name: "保存" }));
  await screen.findByText("save unavailable");
  await userEvent.click(screen.getByRole("button", { name: "保存" }));
  expect(view.saveHooks).toHaveBeenNthCalledWith(2, "user", [expect.objectContaining({ command: "printf edited", event: "PostToolUse", match: "edit_file" })]);
  await waitFor(() => expect(view.onChanged).toHaveBeenCalledTimes(1));
});

it("replaces the draft with the canonical catalog returned after saving", async () => {
  const view = await draw();
  const next = catalog();
  next.hooks[0] = { event: "Stop", command: "printf canonical", scope: "global" };
  view.hooks.mockResolvedValueOnce(next);
  edit(view.container);
  await userEvent.click(screen.getByRole("button", { name: "保存" }));
  await waitFor(() => expect(command(view.container).value).toBe("printf canonical"));
  expect((screen.getByRole("combobox") as HTMLSelectElement).value).toBe("Stop");
  expect(view.container.querySelector("input.match")).toBeNull();
  expect(view.onChanged).toHaveBeenCalledTimes(1);
});

it("uses the selected scope's catalog when switching away and back", async () => {
  const view = await draw();
  edit(view.container);
  await userEvent.click(screen.getByRole("radio", { name: /这个项目/ }));
  expect(command(view.container).value).toBe("printf project");
  await userEvent.click(screen.getByRole("radio", { name: /我的/ }));
  expect(command(view.container).value).toBe("printf original");
  expect(view.saveHooks).not.toHaveBeenCalled();
});

it("keeps project edits after a trial and sends them to the project save", async () => {
  const view = await draw();
  await userEvent.click(screen.getByRole("radio", { name: /这个项目/ }));
  fireEvent.change(command(view.container), { target: { value: "printf project edited" } });
  await userEvent.click(screen.getByRole("button", { name: "试运行" }));
  await screen.findByText("trial output");
  await userEvent.click(screen.getByRole("button", { name: "保存" }));
  expect(view.saveHooks).toHaveBeenCalledWith("project", [expect.objectContaining({ command: "printf project edited", scope: "project" })]);
});

it("starts again from saved rules after closing and reopening the editor", async () => {
  const view = await draw();
  edit(view.container);
  await userEvent.click(screen.getByRole("button", { name: /收起/ }));
  expect(view.container.querySelector("input.cmd")).toBeNull();
  await userEvent.click(screen.getByRole("button", { name: /手动添加/ }));
  expect(command(view.container).value).toBe("printf original");
});

it("shows plugin rules as read-only and excludes them from a user save", async () => {
  const view = await draw();
  expect(screen.getByText("printf plugin").closest(".fromplugin")).toBeTruthy();
  expect(view.container.querySelectorAll("input.cmd")).toHaveLength(1);
  await userEvent.click(screen.getByRole("button", { name: "保存" }));
  expect(view.saveHooks).toHaveBeenCalledWith("user", [expect.objectContaining({ command: "printf original", scope: "global" })]);
});

it("keeps recipe writes limited to the selected editable scope", async () => {
  const view = await draw();
  await userEvent.click(screen.getByRole("switch", { name: "文件修改后自动格式化" }));
  expect(view.saveHooks).toHaveBeenCalledWith("user", [
    expect.objectContaining({ command: "printf original", scope: "global" }),
    expect.objectContaining({ event: "PostToolUse", scope: "user" }),
  ]);
});

it("disables project scope when the catalog has no project", async () => {
  const initial = catalog();
  initial.projectPath = "";
  const view = await draw(initial);
  expect(screen.getByRole<HTMLButtonElement>("radio", { name: /这个项目/ }).disabled).toBe(true);
  expect(command(view.container).value).toBe("printf original");
});

it("keeps a newly authored rule after a failed save from an empty catalog", async () => {
  const initial = catalog();
  initial.hooks = initial.hooks.filter((h) => h.scope !== "global");
  const view = await draw(initial);
  view.saveHooks.mockRejectedValueOnce(new Error("save unavailable"));
  await userEvent.click(screen.getByRole("button", { name: "添加规则" }));
  fireEvent.change(command(view.container), { target: { value: "printf first rule" } });
  await userEvent.click(screen.getByRole("button", { name: "保存" }));
  await screen.findByText("save unavailable");
  expect(view.container.querySelectorAll("input.cmd")).toHaveLength(1);
  expect(command(view.container).value).toBe("printf first rule");
});
