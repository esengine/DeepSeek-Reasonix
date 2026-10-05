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

afterEach(cleanup);

const source = "https://github.com/demo/notes-kit";
const plan: PluginPlan = {
  ok: true, status: "planned", applied: false, source, planId: "notes-plan",
  actions: [{ kind: "skill", name: "notes", action: "copy_skill", status: "planned", riskLevel: "low" }],
};
const installed: PluginPlan = { ...plan, status: "done", applied: true };
const failure = "package could not be installed";

function deferred() {
  let finish!: (value: PluginPlan) => void;
  let fail!: (error: Error) => void;
  const promise = new Promise<PluginPlan>((resolve, reject) => { finish = resolve; fail = reject; });
  return { promise, finish, fail: () => fail(new Error(failure)) };
}

function draw(port: AgentPort) {
  const onClose = vi.fn();
  const onChanged = vi.fn();
  render(<Settings
    hub={new MockHub() as never} port={port}
    status={{ preset: "balanced", toolApprovalMode: "ask" } as SessionStatus}
    theme="light" onTheme={() => {}} contrast="" onContrast={() => {}} weight="" onWeight={() => {}}
    look={{} as never} onLook={() => {}} reloadThemes={() => {}}
    onClose={onClose} onChanged={onChanged} onError={() => {}} at="ext:installed"
    account={null} accountUnread="" reloadAccount={() => {}}
  />);
  return { onClose, onChanged };
}

async function confirm() {
  await userEvent.click(await screen.findByRole("button", { name: "添加" }));
  const pane = within(document.querySelector<HTMLElement>(".addpkg")!);
  await userEvent.type(pane.getByRole("textbox"), source);
  await userEvent.click(pane.getByRole("button", { name: "查看内容" }));
  await userEvent.click(pane.getByRole("button", { name: "安装" }));
  return pane;
}

it.each(["success", "failure", "refused"])("keeps the confirmed new installation until %s completes", async (outcome) => {
  const port = new MockPort() as unknown as AgentPort;
  const preview = vi.spyOn(port, "planPlugin").mockResolvedValue(plan);
  const pending = deferred();
  const install = vi.spyOn(port, "installPlugin").mockImplementationOnce(() => pending.promise);
  const { onClose, onChanged } = draw(port);
  const pane = await confirm();
  await userEvent.click(pane.getByRole("button", { name: "返回" }));
  expect(pane.queryByRole("textbox")).toBeNull();
  expect(pane.getByText("notes")).toBeTruthy();
  expect(pane.getByRole<HTMLButtonElement>("button", { name: "返回" }).disabled).toBe(true);
  await userEvent.click(pane.getByRole("button", { name: "安装中…" }));
  expect(document.querySelector(".addpkg")!.getAttribute("aria-busy")).toBe("true");
  expect(preview).toHaveBeenCalledExactlyOnceWith({ source, name: undefined, replace: false, planId: undefined });
  expect(install).toHaveBeenCalledExactlyOnceWith({ source, name: undefined, replace: false, planId: plan.planId });
  expect(onClose).not.toHaveBeenCalled();
  await act(async () => {
    if (outcome === "failure") pending.fail();
    else pending.finish(outcome === "success" ? installed : { ...plan, ok: false, status: "denied", error: "installation refused" });
  });
  if (outcome === "failure") {
    expect(pane.getByRole("alert").textContent).toBe(failure);
    expect(pane.getByRole<HTMLButtonElement>("button", { name: "返回" }).disabled).toBe(false);
    expect(pane.getByRole<HTMLButtonElement>("button", { name: "安装" }).disabled).toBe(false);
    await userEvent.click(pane.getByRole("button", { name: "返回" }));
    expect(pane.getByRole<HTMLTextAreaElement>("textbox").value).toBe(source);
    expect(onChanged).not.toHaveBeenCalled();
  } else {
    expect(pane.getByRole<HTMLButtonElement>("button", { name: "完成" }).disabled).toBe(false);
    expect(onChanged).toHaveBeenCalledTimes(outcome === "success" ? 1 : 0);
    if (outcome === "refused") expect(pane.getByText("installation refused")).toBeTruthy();
    await userEvent.click(pane.getByRole("button", { name: "完成" }));
    expect(document.querySelector(".addpkg")).toBeNull();
  }
});

it("allows returning to the original source before installing", async () => {
  const port = new MockPort() as unknown as AgentPort;
  vi.spyOn(port, "planPlugin").mockResolvedValue(plan);
  const install = vi.spyOn(port, "installPlugin");
  render(<AddPlugin port={port} source={source} onClose={() => {}} onInstalled={() => {}} />);
  await userEvent.click(screen.getByRole("button", { name: "查看内容" }));
  await userEvent.click(screen.getByRole("button", { name: "返回" }));
  expect(screen.getByRole<HTMLTextAreaElement>("textbox").value).toBe(source);
  expect(install).not.toHaveBeenCalled();
});

it("allows Escape to dismiss a new-install form and still notifies Settings on completion", async () => {
  const port = new MockPort() as unknown as AgentPort;
  vi.spyOn(port, "planPlugin").mockResolvedValue(plan);
  const pending = deferred();
  const install = vi.spyOn(port, "installPlugin").mockImplementationOnce(() => pending.promise);
  const { onClose, onChanged } = draw(port);
  await confirm();
  await userEvent.keyboard("{Escape}");
  expect(document.querySelector(".addpkg")).toBeNull();
  expect(onClose).not.toHaveBeenCalled();
  expect(install).toHaveBeenCalledExactlyOnceWith({ source, name: undefined, replace: false, planId: plan.planId });
  await act(async () => pending.finish(installed));
  expect(onChanged).toHaveBeenCalledTimes(1);
});
