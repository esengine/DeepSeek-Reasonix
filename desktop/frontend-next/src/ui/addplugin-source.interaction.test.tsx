// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { act, cleanup, fireEvent, render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { AddPlugin } from "./AddPlugin";
import { Settings } from "./Settings";
import { install as installDrops } from "./filedrop";
import { host } from "../port/host";
import { MockPort } from "../port/mock";
import { MockHub } from "../port/mock_hub";
import type { AgentPort, PluginPlan, SessionStatus } from "../port/port";

afterEach(() => { cleanup(); vi.restoreAllMocks(); });

const source = "https://github.com/demo/notes-kit";
const other = "/workspace/other-kit";
const plan: PluginPlan = {
  ok: true, status: "planned", applied: false, source, planId: "notes-plan",
  actions: [{ kind: "skill", name: "notes", action: "copy_skill", status: "planned", riskLevel: "low" }],
};

function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (error: Error) => void;
  const promise = new Promise<T>((yes, no) => { resolve = yes; reject = no; });
  return { promise, resolve, reject };
}

function nativeDrop(path: string) {
  installDrops();
  vi.spyOn(host(), "pathsForFiles").mockReturnValue([path]);
  fireEvent.drop(document.querySelector(".addpkg")!, {
    dataTransfer: { types: ["Files"], files: [new File(["fixture"], "folder")], getData: () => "" },
  });
}

async function openSettings(port: AgentPort) {
  render(<Settings
    hub={new MockHub() as never} port={port}
    status={{ preset: "balanced", toolApprovalMode: "ask" } as SessionStatus}
    theme="light" onTheme={() => {}} contrast="" onContrast={() => {}} weight="" onWeight={() => {}}
    look={{} as never} onLook={() => {}} reloadThemes={() => {}}
    onClose={() => {}} onChanged={() => {}} onError={() => {}} at="ext:installed"
    account={null} accountUnread="" reloadAccount={() => {}}
  />);
  await userEvent.click(await screen.findByRole("button", { name: "添加" }));
  const pane = within(document.querySelector<HTMLElement>(".addpkg")!);
  await userEvent.type(pane.getByRole("textbox"), source);
  return pane;
}

it.each(["typing", "folder", "drop"])("keeps the previewed source through confirmation after %s while reading", async (input) => {
  const port = new MockPort() as unknown as AgentPort;
  const pending = deferred<PluginPlan>();
  const preview = vi.spyOn(port, "planPlugin").mockImplementationOnce(() => pending.promise);
  const pick = vi.spyOn(port, "pickFolder").mockResolvedValue(other);
  const install = vi.spyOn(port, "installPlugin").mockResolvedValue({ ...plan, status: "done", applied: true });
  const pane = await openSettings(port);
  await userEvent.click(pane.getByRole("button", { name: "查看内容" }));
  if (input === "typing") {
    await userEvent.type(pane.getByRole("textbox"), other);
  } else if (input === "folder") await userEvent.click(pane.getByRole("button", { name: "选文件夹" }));
  else nativeDrop(other);
  const value = pane.getByRole<HTMLTextAreaElement>("textbox").value;
  await act(async () => pending.resolve(plan));
  await userEvent.click(pane.getByRole("button", { name: "安装" }));
  expect(preview).toHaveBeenCalledExactlyOnceWith({ source, name: undefined, replace: false, planId: undefined });
  expect(install).toHaveBeenCalledExactlyOnceWith({ source, name: undefined, replace: false, planId: plan.planId });
  expect(value).toBe(source);
  expect(pick).not.toHaveBeenCalled();
});

it.each(["picked", "cancelled"])("waits for a %s folder before allowing a preview or another source", async (outcome) => {
  const port = new MockPort() as unknown as AgentPort;
  const pending = deferred<string | null>();
  const pick = vi.spyOn(port, "pickFolder").mockImplementationOnce(() => pending.promise);
  const preview = vi.spyOn(port, "planPlugin").mockResolvedValue(plan);
  render(<AddPlugin port={port} source={source} onClose={() => {}} onInstalled={() => {}} />);
  await userEvent.click(screen.getByRole("button", { name: "选文件夹" }));
  const textbox = screen.getByRole<HTMLTextAreaElement>("textbox");
  const locked = textbox.disabled;
  const busy = document.querySelector(".addpkg")!.getAttribute("aria-busy");
  await userEvent.click(screen.getByRole("button", { name: "选文件夹" }));
  await userEvent.click(screen.getByRole("button", { name: /查看内容|读取中/ }));
  fireEvent.keyDown(textbox, { key: "Enter", ctrlKey: true });
  nativeDrop(other);
  const value = textbox.value;
  const reads = preview.mock.calls.length;
  await act(async () => pending.resolve(outcome === "picked" ? other : null));
  expect(locked).toBe(true);
  expect(busy).toBe("true");
  expect(reads).toBe(0);
  expect(value).toBe(source);
  expect(pick).toHaveBeenCalledTimes(1);
  expect(screen.getByRole<HTMLTextAreaElement>("textbox").value).toBe(outcome === "picked" ? other : source);
  expect(screen.getByRole<HTMLTextAreaElement>("textbox").disabled).toBe(false);
  expect(screen.getByRole<HTMLButtonElement>("button", { name: "选文件夹" }).disabled).toBe(false);
  await userEvent.click(screen.getByRole("button", { name: "查看内容" }));
  expect(preview).toHaveBeenCalledExactlyOnceWith({ source: outcome === "picked" ? other : source, name: undefined, replace: false, planId: undefined });
});

it("preserves the source and allows retry after the folder picker fails", async () => {
  const port = new MockPort() as unknown as AgentPort;
  const pending = deferred<string | null>();
  const pick = vi.spyOn(port, "pickFolder").mockImplementationOnce(() => pending.promise).mockResolvedValueOnce(other);
  const preview = vi.spyOn(port, "planPlugin");
  render(<AddPlugin port={port} source={source} onClose={() => {}} onInstalled={() => {}} />);
  await userEvent.click(screen.getByRole("button", { name: "选文件夹" }));
  await act(async () => pending.reject(new Error("folder dialog unavailable")));
  expect(screen.getByRole("alert").textContent).toBe("folder dialog unavailable");
  expect(screen.getByRole<HTMLTextAreaElement>("textbox").value).toBe(source);
  expect(screen.getByRole<HTMLButtonElement>("button", { name: "查看内容" }).disabled).toBe(false);
  await userEvent.click(screen.getByRole("button", { name: "选文件夹" }));
  expect(screen.queryByRole("alert")).toBeNull();
  expect(screen.getByRole<HTMLTextAreaElement>("textbox").value).toBe(other);
  expect(pick).toHaveBeenCalledTimes(2);
  expect(preview).not.toHaveBeenCalled();
});

it.each(["failed", "empty"])("unlocks source inputs when the preview is %s", async (outcome) => {
  const port = new MockPort() as unknown as AgentPort;
  const pending = deferred<PluginPlan>();
  const preview = vi.spyOn(port, "planPlugin").mockImplementationOnce(() => pending.promise).mockResolvedValueOnce(plan);
  render(<AddPlugin port={port} source={source} onClose={() => {}} onInstalled={() => {}} />);
  await userEvent.click(screen.getByRole("button", { name: "查看内容" }));
  await act(async () => {
    if (outcome === "failed") pending.reject(new Error("source could not be read"));
    else pending.resolve({ ...plan, actions: [], error: "nothing installable" });
  });
  expect(screen.getByRole<HTMLTextAreaElement>("textbox").disabled).toBe(false);
  expect(screen.getByRole<HTMLButtonElement>("button", { name: "选文件夹" }).disabled).toBe(false);
  await userEvent.clear(screen.getByRole("textbox"));
  await userEvent.type(screen.getByRole("textbox"), other);
  await userEvent.click(screen.getByRole("button", { name: "查看内容" }));
  expect(preview).toHaveBeenLastCalledWith({ source: other, name: undefined, replace: false, planId: undefined });
});

it("keeps cancellation available while a folder choice is pending", async () => {
  const port = new MockPort() as unknown as AgentPort;
  const pending = deferred<string | null>();
  vi.spyOn(port, "pickFolder").mockImplementationOnce(() => pending.promise);
  const preview = vi.spyOn(port, "planPlugin");
  const onClose = vi.fn();
  render(<AddPlugin port={port} source={source} onClose={onClose} onInstalled={() => {}} />);
  await userEvent.click(screen.getByRole("button", { name: "选文件夹" }));
  await userEvent.click(screen.getByRole("button", { name: "取消" }));
  await userEvent.keyboard("{Escape}");
  await act(async () => pending.resolve(null));
  expect(onClose).toHaveBeenCalledTimes(2);
  expect(preview).not.toHaveBeenCalled();
});
