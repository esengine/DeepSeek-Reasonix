// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { act, cleanup, render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { AddPlugin } from "./AddPlugin";
import { Settings } from "./Settings";
import { MockPort } from "../port/mock";
import { MockHub } from "../port/mock_hub";
import type { AgentPort, PluginPlan, SessionStatus } from "../port/port";

afterEach(() => { cleanup(); vi.restoreAllMocks(); });

const source = "/author/notes-kit";
const nextSource = "/author/current-kit";
const failure = "old package request failed";
const plan = (name: string): PluginPlan => ({
  ok: true, status: "planned", applied: false, source, planId: `${name}-plan`,
  actions: [{ kind: "plugin", name, action: "copy_plugin", status: "planned", riskLevel: "low" }],
});
const installed = (name: string): PluginPlan => ({ ...plan(name), status: "done", applied: true });

function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (error: Error) => void;
  const promise = new Promise<T>((yes, no) => { resolve = yes; reject = no; });
  return { promise, resolve, reject: () => reject(new Error(failure)) };
}

function portFor(name: string) {
  const port = new MockPort() as unknown as AgentPort;
  vi.spyOn(port, "plugins").mockResolvedValue([{ name: "notes-kit", source, root: "/installed/notes-kit", version: "1.0.0", enabled: true }]);
  const preview = vi.spyOn(port, "planPlugin").mockResolvedValue(plan(name));
  const install = vi.spyOn(port, "installPlugin").mockResolvedValue(installed(name));
  return { port, preview, install };
}

function settings(port: AgentPort, onChanged = vi.fn()) {
  return <Settings
    hub={new MockHub() as never} port={port}
    status={{ preset: "balanced", toolApprovalMode: "ask" } as SessionStatus}
    theme="light" onTheme={() => {}} contrast="" onContrast={() => {}} weight="" onWeight={() => {}}
    look={{} as never} onLook={() => {}} reloadThemes={() => {}}
    onClose={() => {}} onChanged={onChanged} onError={() => {}} at="ext:installed"
    account={null} accountUnread="" reloadAccount={() => {}}
  />;
}

const pane = () => within(document.querySelector<HTMLElement>(".addpkg")!);
const nav = () => document.querySelector<HTMLButtonElement>('.prefs-nav [data-value="session"]')!;

async function inspect(value: string) {
  await userEvent.type(pane().getByRole("textbox"), value);
  await userEvent.click(pane().getByRole("button", { name: "查看内容" }));
}

async function update() {
  await screen.findByText("notes-kit");
  await userEvent.click(within(document.querySelector<HTMLElement>('[data-extension-name="notes-kit"]')!).getByRole("button", { name: "更新" }));
  await userEvent.click(await pane().findByRole("button", { name: "更新" }));
}

it("never sends the previous connection's preview ticket to the new connection", async () => {
  const old = portFor("old-preview");
  const current = portFor("current-preview");
  const view = render(settings(old.port));
  await userEvent.click(await screen.findByRole("button", { name: "添加" }));
  await inspect(source);
  view.rerender(settings(current.port));
  const staleConfirm = pane().queryByRole("button", { name: "安装" });
  if (staleConfirm) await userEvent.click(staleConfirm);
  expect(current.install).not.toHaveBeenCalled();
  expect(pane().getByRole<HTMLTextAreaElement>("textbox").value).toBe("");
  await inspect(nextSource);
  await userEvent.click(pane().getByRole("button", { name: "安装" }));
  expect(current.install).toHaveBeenCalledExactlyOnceWith({ source: nextSource, name: undefined, replace: false, planId: "current-preview-plan" });
  expect(old.install).not.toHaveBeenCalled();
});

it.each(["success", "failure"])("isolates a pending preview's %s while the replacement preview is reading", async (outcome) => {
  const old = portFor("old-preview");
  const current = portFor("current-preview");
  const original = deferred<PluginPlan>();
  const replacement = deferred<PluginPlan>();
  old.preview.mockReturnValueOnce(original.promise);
  current.preview.mockReturnValueOnce(replacement.promise);
  const view = render(settings(old.port));
  await userEvent.click(await screen.findByRole("button", { name: "添加" }));
  await inspect(source);
  view.rerender(settings(current.port));
  expect(pane().getByRole<HTMLTextAreaElement>("textbox").value).toBe("");
  await inspect(nextSource);
  await act(async () => outcome === "success" ? original.resolve(plan("old-preview")) : original.reject());
  expect(pane().getByRole<HTMLButtonElement>("button", { name: "读取中…" }).disabled).toBe(true);
  expect(screen.queryByText(failure)).toBeNull();
  expect(screen.queryByText("old-preview")).toBeNull();
  await act(async () => replacement.resolve(plan("current-preview")));
  await userEvent.click(pane().getByRole("button", { name: "安装" }));
  expect(current.install).toHaveBeenCalledExactlyOnceWith({ source: nextSource, name: undefined, replace: false, planId: "current-preview-plan" });
});

it.each(["picked", "failure"])("discards a previous connection's %s folder selection", async (outcome) => {
  const old = portFor("old-preview");
  const current = portFor("current-preview");
  const pending = deferred<string | null>();
  vi.spyOn(old.port, "pickFolder").mockReturnValueOnce(pending.promise);
  const draw = (port: AgentPort) => <AddPlugin port={port} onClose={() => {}} onInstalled={() => {}} />;
  const view = render(draw(old.port));
  await userEvent.type(screen.getByRole("textbox"), source);
  await userEvent.click(screen.getByRole("button", { name: "选文件夹" }));
  view.rerender(draw(current.port));
  expect(screen.getByRole<HTMLTextAreaElement>("textbox").value).toBe("");
  await userEvent.type(screen.getByRole("textbox"), nextSource);
  await act(async () => outcome === "picked" ? pending.resolve("/old/selected-folder") : pending.reject());
  expect(screen.getByRole<HTMLTextAreaElement>("textbox").value).toBe(nextSource);
  expect(screen.queryByRole("alert")).toBeNull();
  await userEvent.click(screen.getByRole("button", { name: "查看内容" }));
  expect(current.preview).toHaveBeenCalledExactlyOnceWith({ source: nextSource, name: undefined, replace: false, planId: undefined });
});

it.each(["error", "done"])("clears settled %s feedback on a connection change", async (stage) => {
  const old = portFor("old-preview");
  const current = portFor("current-preview");
  if (stage === "error") old.preview.mockRejectedValueOnce(new Error(failure));
  const draw = (port: AgentPort) => <AddPlugin port={port} source={source} onClose={() => {}} onInstalled={() => {}} />;
  const view = render(draw(old.port));
  await userEvent.click(screen.getByRole("button", { name: "查看内容" }));
  if (stage === "done") await userEvent.click(screen.getByRole("button", { name: "安装" }));
  view.rerender(draw(current.port));
  expect(screen.queryByRole("alert")).toBeNull();
  expect(screen.queryByRole("button", { name: "完成" })).toBeNull();
  expect(screen.getByRole<HTMLTextAreaElement>("textbox").value).toBe(source);
  expect(document.activeElement).toBe(screen.getByRole("textbox"));
});

it.each(["success", "failure", "refusal"])("keeps the replacement installation busy after an old apply returns %s", async (outcome) => {
  const old = portFor("old-preview");
  const current = portFor("current-preview");
  const original = deferred<PluginPlan>();
  const replacement = deferred<PluginPlan>();
  old.install.mockReturnValueOnce(original.promise);
  current.install.mockReturnValueOnce(replacement.promise);
  const changed = vi.fn();
  const view = render(settings(old.port, changed));
  await userEvent.click(await screen.findByRole("button", { name: "添加" }));
  await inspect(source);
  await userEvent.click(pane().getByRole("button", { name: "安装" }));
  view.rerender(settings(current.port, changed));
  expect(pane().getByRole<HTMLTextAreaElement>("textbox").value).toBe("");
  await inspect(nextSource);
  await userEvent.click(pane().getByRole("button", { name: "安装" }));
  await act(async () => {
    if (outcome === "failure") original.reject();
    else original.resolve(outcome === "success" ? installed("old-preview") : { ...plan("old-preview"), ok: false, status: "denied" });
  });
  expect(pane().getByRole<HTMLButtonElement>("button", { name: "安装中…" }).disabled).toBe(true);
  expect(screen.queryByText(failure)).toBeNull();
  expect(changed).not.toHaveBeenCalled();
  await act(async () => replacement.resolve(installed("current-preview")));
  expect(pane().getByRole("button", { name: "完成" })).toBeTruthy();
  expect(changed).toHaveBeenCalledTimes(1);
  expect(old.install).toHaveBeenCalledTimes(1);
  expect(current.install).toHaveBeenCalledTimes(1);
});

it("retains canonical completion after returning to the original port without restoring old form state", async () => {
  const old = portFor("old-preview");
  const current = portFor("current-preview");
  const pending = deferred<PluginPlan>();
  old.install.mockReturnValueOnce(pending.promise);
  const changed = vi.fn();
  const view = render(settings(old.port, changed));
  await userEvent.click(await screen.findByRole("button", { name: "添加" }));
  await inspect(source);
  await userEvent.click(pane().getByRole("button", { name: "安装" }));
  view.rerender(settings(current.port, changed));
  view.rerender(settings(old.port, changed));
  expect(pane().getByRole<HTMLTextAreaElement>("textbox").value).toBe("");
  await act(async () => pending.resolve(installed("old-preview")));
  expect(changed).toHaveBeenCalledTimes(1);
  expect(pane().queryByRole("button", { name: "完成" })).toBeNull();
  expect(pane().getByRole<HTMLTextAreaElement>("textbox").value).toBe("");
});

it("preserves a same-connection pending apply across ordinary rerenders", async () => {
  const current = portFor("current-preview");
  const pending = deferred<PluginPlan>();
  current.install.mockReturnValueOnce(pending.promise);
  const changed = vi.fn();
  const view = render(settings(current.port, changed));
  await userEvent.click(await screen.findByRole("button", { name: "添加" }));
  await inspect(nextSource);
  await userEvent.click(pane().getByRole("button", { name: "安装" }));
  view.rerender(settings(current.port, changed));
  expect(pane().getByRole<HTMLButtonElement>("button", { name: "安装中…" }).disabled).toBe(true);
  await act(async () => pending.resolve(installed("current-preview")));
  expect(pane().getByRole("button", { name: "完成" })).toBeTruthy();
  expect(changed).toHaveBeenCalledTimes(1);
});

it.each(["success", "failure"])("releases an old update selection but keeps a new update locked after an old %s completion", async (outcome) => {
  const old = portFor("old-preview");
  const other = portFor("other-preview");
  const first = deferred<PluginPlan>();
  const second = deferred<PluginPlan>();
  old.install.mockReturnValueOnce(first.promise).mockReturnValueOnce(second.promise);
  const changed = vi.fn();
  const view = render(settings(old.port, changed));
  await update();
  expect(nav().disabled).toBe(true);
  await act(async () => view.rerender(settings(other.port, changed)));
  expect(document.querySelector(".addpkg")).toBeNull();
  expect(nav().disabled).toBe(false);
  await act(async () => view.rerender(settings(old.port, changed)));
  await update();
  expect(nav().disabled).toBe(true);
  await act(async () => outcome === "success" ? first.resolve(installed("old-preview")) : first.reject());
  expect(nav().disabled).toBe(true);
  expect(pane().getByRole<HTMLButtonElement>("button", { name: "更新中…" }).disabled).toBe(true);
  await act(async () => second.resolve(installed("current-preview")));
  expect(nav().disabled).toBe(false);
  expect(pane().getByRole("button", { name: "完成" })).toBeTruthy();
  expect(changed).toHaveBeenCalledTimes(outcome === "success" ? 2 : 1);
});
