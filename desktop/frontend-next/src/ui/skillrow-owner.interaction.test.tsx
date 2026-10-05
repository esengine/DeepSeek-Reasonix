// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { act, cleanup, render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { StrictMode } from "react";
import "./testkit";
import { SkillRow } from "./SkillRow";
import { Settings } from "./Settings";
import { MockPort } from "../port/mock";
import { MockHub } from "../port/mock_hub";
import type { AgentPort, CapabilityScope, SessionStatus, SkillEntry } from "../port/port";

afterEach(cleanup);

const skill: SkillEntry = { name: "explore", slashName: "explore", scope: "project", enabled: true, switchScope: "project" };

const scopes: CapabilityScope[] = [
  { root: "/workspace/main", name: "main", key: "main", repo: false, current: true, overrides: 1 },
  { root: "/workspace/other", name: "other", key: "other", repo: false, current: false, overrides: 1 },
];

async function scopeCatalog(port: AgentPort) {
  const catalog = await port.mcp();
  vi.spyOn(port, "capabilityScopes").mockResolvedValue(scopes);
  vi.spyOn(port, "mcp").mockImplementation(async (root) => ({ ...catalog, scope: scopes[root === "/workspace/other" ? 1 : 0], live: !root }));
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

async function chooseProject(from: string, to: string) {
  await userEvent.click(within(document.querySelector<HTMLElement>(".scopebar")!).getByRole("button", { name: from }));
  await userEvent.click(screen.getByRole("option", { name: new RegExp(to) }));
  await screen.findByText(`/workspace/${to}`);
}

function deferred() {
  let finish!: () => void;
  let fail!: (error: Error) => void;
  const promise = new Promise<void>((resolve, reject) => { finish = resolve; fail = reject; });
  return { promise, finish, settle: (outcome: string) => outcome === "success" ? finish() : fail(new Error("previous skill request failed")) };
}

const owners = [
  ["port", "toggle"], ["root", "toggle"],
  ["return-port", "toggle"], ["return-root", "toggle"],
  ["port", "clear"], ["root", "clear"],
] as const;

it.each(owners.flatMap(([change, operation]) => (["success", "failure"] as const).map((outcome) => [change, operation, outcome] as const)))(
  "isolates the old %s owner's %s result (%s)", async (change, operation, outcome) => {
    const port = new MockPort() as unknown as AgentPort;
    const next = new MockPort() as unknown as AgentPort;
    const old = deferred();
    const current = deferred();
    const method = operation === "toggle" ? "setSkillEnabled" : "clearSkillOverride";
    const originalCall = vi.spyOn(port, method).mockImplementationOnce(() => old.promise).mockImplementationOnce(() => current.promise);
    const nextCall = vi.spyOn(next, method).mockImplementationOnce(() => current.promise);
    const onDone = vi.fn();
    const onFailed = vi.fn();
    const draw = (active: AgentPort, root: string) => <SkillRow sk={skill} implicit port={active} root={root} onDone={onDone} onFailed={onFailed} />;
    const control = () => operation === "toggle"
      ? screen.getByRole<HTMLButtonElement>("switch", { name: "关闭 explore" })
      : screen.getByRole<HTMLButtonElement>("button", { name: "仅本项目" });
    const view = render(draw(port, "/workspace/main"));
    await userEvent.click(control());
    const portChanged = change === "port" || change === "return-port";
    view.rerender(draw(portChanged ? next : port, portChanged ? "/workspace/main" : "/workspace/other"));
    if (change.startsWith("return")) view.rerender(draw(port, "/workspace/main"));
    expect(control().disabled).toBe(false);
    await userEvent.click(control());
    const applying = control();
    await act(async () => old.settle(outcome));
    expect(applying.disabled).toBe(true);
    expect(onDone).toHaveBeenCalledTimes(1);
    expect(onFailed.mock.calls.every(([text]) => text === "")).toBe(true);
    await userEvent.click(applying);
    const nextPort = change === "port";
    expect(originalCall).toHaveBeenCalledTimes(nextPort ? 1 : 2);
    expect(nextCall).toHaveBeenCalledTimes(nextPort ? 1 : 0);
    const currentCall = nextPort ? nextCall : originalCall;
    const root = change === "root" ? "/workspace/other" : "/workspace/main";
    if (operation === "toggle") expect(currentCall).toHaveBeenLastCalledWith("explore", false, "project", root);
    else expect(currentCall).toHaveBeenLastCalledWith("explore", root);
    await act(async () => current.finish());
    expect(applying.disabled).toBe(false);
    expect(onDone).toHaveBeenCalledTimes(2);
  },
);

it.each(["toggle", "clear"])("preserves a pending %s across a same-owner refresh", async (operation) => {
  const port = new MockPort() as unknown as AgentPort;
  const pending = deferred();
  const method = operation === "toggle" ? "setSkillEnabled" : "clearSkillOverride";
  const call = vi.spyOn(port, method).mockImplementationOnce(() => pending.promise);
  const onDone = vi.fn();
  const onFailed = vi.fn();
  const draw = () => <SkillRow sk={{ ...skill }} implicit port={port} root="/workspace/main" onDone={onDone} onFailed={onFailed} />;
  const view = render(draw());
  await userEvent.click(operation === "toggle" ? screen.getByRole("switch") : screen.getByRole("button", { name: "仅本项目" }));
  view.rerender(draw());
  const control = screen.getByRole<HTMLButtonElement>("switch");
  expect(control.disabled).toBe(true);
  await userEvent.click(control);
  expect(call).toHaveBeenCalledTimes(1);
  await act(async () => pending.finish());
  expect(control.disabled).toBe(false);
  expect(onDone).toHaveBeenCalledTimes(1);
});

it.each(["success", "failure"])("isolates a skill %s after choosing another project in Settings", async (outcome) => {
  const port = new MockPort() as unknown as AgentPort;
  const old = deferred();
  const current = deferred();
  const onChanged = vi.fn();
  await scopeCatalog(port);
  const skills = vi.spyOn(port, "skills").mockResolvedValue({ implicit: true, skills: [skill] });
  const toggle = vi.spyOn(port, "setSkillEnabled").mockImplementationOnce(() => old.promise).mockImplementationOnce(() => current.promise);
  render(settings(port, onChanged));
  await userEvent.click(await screen.findByRole("switch", { name: "关闭 explore" }));
  await chooseProject("main", "other");
  const control = screen.getByRole<HTMLButtonElement>("switch", { name: "关闭 explore" });
  expect(control.disabled).toBe(false);
  await userEvent.click(control);
  expect(toggle).toHaveBeenLastCalledWith("explore", false, "project", "/workspace/other");
  const reads = skills.mock.calls.length;
  await act(async () => old.settle(outcome));
  expect(control.disabled).toBe(true);
  expect(onChanged).toHaveBeenCalledTimes(1);
  expect(skills).toHaveBeenCalledTimes(reads + 1);
  expect(skills).toHaveBeenLastCalledWith("/workspace/other");
  expect(screen.queryByText("previous skill request failed")).toBeNull();
  await act(async () => current.finish());
  expect(control.disabled).toBe(false);
  expect(onChanged).toHaveBeenCalledTimes(2);
  expect(skills).toHaveBeenLastCalledWith("/workspace/other");
  expect(within(document.querySelector<HTMLElement>(".scopebar")!).getByText("other")).toBeTruthy();
});

it("refreshes a completed skill change after returning to its project", async () => {
  const port = new MockPort() as unknown as AgentPort;
  await scopeCatalog(port);
  const pending = deferred();
  let mainEnabled = true;
  const skills = vi.spyOn(port, "skills").mockImplementation(async (root) => ({
    implicit: true, skills: [{ ...skill, enabled: root === "/workspace/other" || mainEnabled }],
  }));
  vi.spyOn(port, "setSkillEnabled").mockImplementation(async () => {
    await pending.promise;
    mainEnabled = false;
  });
  const onChanged = vi.fn();
  render(settings(port, onChanged));
  await userEvent.click(await screen.findByRole("switch", { name: "关闭 explore" }));
  await chooseProject("main", "other");
  await chooseProject("other", "main");
  const control = screen.getByRole<HTMLButtonElement>("switch", { name: "关闭 explore" });
  expect(control.disabled).toBe(false);
  expect(skills).toHaveBeenLastCalledWith("/workspace/main");
  const reads = skills.mock.calls.length;
  await act(async () => pending.finish());
  expect(skills).toHaveBeenCalledTimes(reads + 1);
  expect(skills).toHaveBeenLastCalledWith("/workspace/main");
  expect((await screen.findByRole("switch", { name: "启用 explore" })).getAttribute("aria-checked")).toBe("false");
  expect(onChanged).toHaveBeenCalledTimes(1);
});

it.each(["success", "failure"])("refreshes the selected project after an absent skill settles (%s)", async (outcome) => {
  const port = new MockPort() as unknown as AgentPort;
  await scopeCatalog(port);
  const pending = deferred();
  const skills = vi.spyOn(port, "skills").mockImplementation(async (root) => ({
    implicit: true, skills: root === "/workspace/other" ? [] : [skill],
  }));
  vi.spyOn(port, "setSkillEnabled").mockImplementationOnce(() => pending.promise);
  const onChanged = vi.fn();
  render(settings(port, onChanged));
  await userEvent.click(await screen.findByRole("switch", { name: "关闭 explore" }));
  await chooseProject("main", "other");
  expect(screen.queryByRole("switch", { name: "关闭 explore" })).toBeNull();
  const reads = skills.mock.calls.length;
  await act(async () => pending.settle(outcome));
  expect(skills).toHaveBeenCalledTimes(reads + 1);
  expect(skills).toHaveBeenLastCalledWith("/workspace/other");
  expect(screen.queryByText("previous skill request failed")).toBeNull();
  expect(onChanged).toHaveBeenCalledTimes(1);
});

it.each(["success", "failure"])("rejects a previous Settings connection's completion (%s)", async (outcome) => {
  const port = new MockPort() as unknown as AgentPort;
  const next = new MockPort() as unknown as AgentPort;
  await scopeCatalog(port);
  await scopeCatalog(next);
  const old = deferred();
  const current = deferred();
  vi.spyOn(port, "skills").mockResolvedValue({ implicit: true, skills: [skill] });
  const skills = vi.spyOn(next, "skills").mockResolvedValue({ implicit: true, skills: [skill] });
  vi.spyOn(port, "setSkillEnabled").mockImplementationOnce(() => old.promise);
  const toggle = vi.spyOn(next, "setSkillEnabled").mockImplementationOnce(() => current.promise);
  const onChanged = vi.fn();
  const nextChanged = vi.fn();
  const view = render(settings(port, onChanged));
  await userEvent.click(await screen.findByRole("switch", { name: "关闭 explore" }));
  view.rerender(settings(next, nextChanged));
  const control = screen.getByRole<HTMLButtonElement>("switch", { name: "关闭 explore" });
  expect(control.disabled).toBe(false);
  await userEvent.click(control);
  expect(toggle).toHaveBeenCalledTimes(1);
  const reads = skills.mock.calls.length;
  await act(async () => old.settle(outcome));
  expect(control.disabled).toBe(true);
  expect(skills).toHaveBeenCalledTimes(reads);
  expect(onChanged).not.toHaveBeenCalled();
  expect(nextChanged).not.toHaveBeenCalled();
  expect(screen.queryByText("previous skill request failed")).toBeNull();
  await act(async () => current.finish());
  expect(control.disabled).toBe(false);
  expect(skills).toHaveBeenCalledTimes(reads + 1);
  expect(nextChanged).toHaveBeenCalledTimes(1);
});

it("keeps an active StrictMode skill request owned until completion", async () => {
  const port = new MockPort() as unknown as AgentPort;
  const pending = deferred();
  vi.spyOn(port, "setSkillEnabled").mockImplementationOnce(() => pending.promise);
  const onFailed = vi.fn();
  const onDone = vi.fn();
  render(<StrictMode><SkillRow sk={skill} implicit port={port} root="/workspace/main" onDone={onDone} onFailed={onFailed} /></StrictMode>);
  await userEvent.click(screen.getByRole("switch"));
  await act(async () => pending.settle("failure"));
  expect(onFailed).toHaveBeenLastCalledWith("previous skill request failed");
  expect(screen.getByRole<HTMLButtonElement>("switch").disabled).toBe(false);
  expect(onDone).toHaveBeenCalledTimes(1);
});

it("notifies the current Settings callback when the connection stays the same", async () => {
  const port = new MockPort() as unknown as AgentPort;
  await scopeCatalog(port);
  vi.spyOn(port, "skills").mockResolvedValue({ implicit: true, skills: [skill] });
  const pending = deferred();
  vi.spyOn(port, "setSkillEnabled").mockImplementationOnce(() => pending.promise);
  const before = vi.fn();
  const current = vi.fn();
  const view = render(settings(port, before));
  await userEvent.click(await screen.findByRole("switch", { name: "关闭 explore" }));
  view.rerender(settings(port, current));
  await act(async () => pending.finish());
  expect(before).not.toHaveBeenCalled();
  expect(current).toHaveBeenCalledTimes(1);
});

it.each(["success", "failure"])("notifies Settings without an old error after leaving installed skills (%s)", async (outcome) => {
  const port = new MockPort() as unknown as AgentPort;
  await scopeCatalog(port);
  const skills = vi.spyOn(port, "skills").mockResolvedValue({ implicit: true, skills: [skill] });
  const pending = deferred();
  vi.spyOn(port, "setSkillEnabled").mockImplementationOnce(() => pending.promise);
  const onChanged = vi.fn();
  render(settings(port, onChanged));
  await userEvent.click(await screen.findByRole("switch", { name: "关闭 explore" }));
  await userEvent.click(screen.getByRole("tab", { name: "发现" }));
  expect(screen.queryByRole("switch", { name: "关闭 explore" })).toBeNull();
  const reads = skills.mock.calls.length;
  await act(async () => pending.settle(outcome));
  expect(skills).toHaveBeenCalledTimes(reads + 1);
  expect(screen.queryByText("previous skill request failed")).toBeNull();
  expect(onChanged).toHaveBeenCalledTimes(1);
});
