// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { act, cleanup, render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { ServerRow } from "./ServerRow";
import { Settings } from "./Settings";
import { MockPort } from "../port/mock";
import { MockHub } from "../port/mock_hub";
import type { AgentPort, CapabilityScope, McpEntry, SessionStatus } from "../port/port";

afterEach(cleanup);

const server: McpEntry = { name: "docs", enabled: true, state: "standby", tools: 0, source: "main.json", localOverride: true };
const failure = "previous server request failed";
const fallback = "同名的另一处声明已生效，该行不会消失。";

function deferred<T>() {
  let finish!: (value: T) => void;
  let fail!: (error: Error) => void;
  const promise = new Promise<T>((resolve, reject) => { finish = resolve; fail = reject; });
  return { promise, finish, fail: () => fail(new Error(failure)) };
}

const owners = [
  ["port", "toggle"], ["root", "toggle"],
  ["return-port", "toggle"], ["return-root", "toggle"],
  ["port", "clear"], ["root", "clear"],
] as const;

it.each(owners.flatMap(([change, operation]) => (["success", "failure"] as const).map((outcome) => [change, operation, outcome] as const)))(
  "isolates an old %s owner's %s response (%s)", async (change, operation, outcome) => {
    const port = new MockPort() as unknown as AgentPort;
    const next = new MockPort() as unknown as AgentPort;
    const old = deferred<void>();
    const current = deferred<void>();
    const method = operation === "toggle" ? "setMcpEnabled" : "clearMcpOverride";
    const original = vi.spyOn(port, method).mockImplementationOnce(() => old.promise).mockImplementationOnce(() => current.promise);
    const replacement = vi.spyOn(next, method).mockImplementationOnce(() => current.promise);
    const onDone = vi.fn();
    const draw = (active: AgentPort, root: string) => <ServerRow m={server} port={active} root={root} live={root === "/workspace/main"} onDone={onDone} />;
    const control = () => operation === "toggle"
      ? screen.getByRole<HTMLButtonElement>("switch", { name: "关闭 docs" })
      : screen.getByRole<HTMLButtonElement>("button", { name: "仅本项目" });
    const view = render(draw(port, "/workspace/main"));
    await userEvent.click(control());
    const portChanged = change === "port" || change === "return-port";
    view.rerender(draw(portChanged ? next : port, portChanged ? "/workspace/main" : "/workspace/other"));
    if (change.startsWith("return")) view.rerender(draw(port, "/workspace/main"));
    expect(control().disabled).toBe(false);
    await userEvent.click(control());
    const applying = control();
    await act(async () => outcome === "success" ? old.finish() : old.fail());
    expect(applying.disabled).toBe(true);
    expect(screen.queryByText(failure)).toBeNull();
    expect(onDone).toHaveBeenCalledTimes(1);
    await userEvent.click(applying);
    const nextPort = change === "port";
    expect(original).toHaveBeenCalledTimes(nextPort ? 1 : 2);
    expect(replacement).toHaveBeenCalledTimes(nextPort ? 1 : 0);
    const call = nextPort ? replacement : original;
    const root = change === "root" ? "/workspace/other" : "/workspace/main";
    if (operation === "toggle") expect(call).toHaveBeenLastCalledWith("docs", false, "project", root);
    else expect(call).toHaveBeenLastCalledWith("docs", root);
    await act(async () => current.finish());
    expect(applying.disabled).toBe(false);
    expect(onDone).toHaveBeenCalledTimes(2);
  },
);

it.each(["port", "return-root"])("isolates a reconnect error returned by the old %s owner", async (change) => {
  const port = new MockPort() as unknown as AgentPort;
  const next = new MockPort() as unknown as AgentPort;
  const old = deferred<{ state: string; error?: string }>();
  const current = deferred<{ state: string; error?: string }>();
  const original = vi.spyOn(port, "reconnectMcp").mockImplementationOnce(() => old.promise).mockImplementationOnce(() => current.promise);
  const replacement = vi.spyOn(next, "reconnectMcp").mockImplementationOnce(() => current.promise);
  const onDone = vi.fn();
  const draw = (active: AgentPort, root: string) => <ServerRow m={server} port={active} root={root} live={root === "/workspace/main"} onDone={onDone} />;
  const view = render(draw(port, "/workspace/main"));
  await userEvent.click(screen.getByRole("button", { name: "立即连接" }));
  view.rerender(draw(change === "port" ? next : port, change === "port" ? "/workspace/main" : "/workspace/other"));
  if (change === "return-root") view.rerender(draw(port, "/workspace/main"));
  await userEvent.click(screen.getByRole("button", { name: "立即连接" }));
  await act(async () => old.finish({ state: "failed", error: failure }));
  const retry = screen.getByRole<HTMLButtonElement>("button", { name: "连接中…" });
  expect(retry.disabled).toBe(true);
  expect(screen.queryByText(failure)).toBeNull();
  await userEvent.click(retry);
  expect(original).toHaveBeenCalledTimes(change === "port" ? 1 : 2);
  expect(replacement).toHaveBeenCalledTimes(change === "port" ? 1 : 0);
  await act(async () => current.finish({ state: "ready" }));
  expect(screen.getByRole<HTMLButtonElement>("button", { name: "立即连接" }).disabled).toBe(false);
  expect(onDone).toHaveBeenCalledTimes(2);
});

it.each(["port", "return-root"].flatMap((change) => ["removed", "fallback", "failure"].map((outcome) => [change, outcome] as const)))(
  "keeps a new removal confirmation after old %s removal settles (%s)", async (change, outcome) => {
    const port = new MockPort() as unknown as AgentPort;
    const next = new MockPort() as unknown as AgentPort;
    const old = deferred<{ disconnected: boolean; stillConfigured: boolean }>();
    const original = vi.spyOn(port, "removeMcp").mockImplementationOnce(() => old.promise);
    const replacement = vi.spyOn(next, "removeMcp").mockResolvedValue({ disconnected: true, stillConfigured: false });
    const onDone = vi.fn();
    const draw = (active: AgentPort, root: string) => <ServerRow m={server} port={active} root={root} live={root === "/workspace/main"} onDone={onDone} />;
    const view = render(draw(port, "/workspace/main"));
    await userEvent.click(screen.getByRole("button", { name: "移除 docs" }));
    await userEvent.click(screen.getByRole("button", { name: "移除" }));
    view.rerender(draw(change === "port" ? next : port, change === "port" ? "/workspace/main" : "/workspace/other"));
    if (change === "return-root") view.rerender(draw(port, "/workspace/main"));
    expect(screen.queryByRole("button", { name: "取消" })).toBeNull();
    await userEvent.click(screen.getByRole("button", { name: "移除 docs" }));
    await act(async () => outcome === "failure" ? old.fail() : old.finish({ disconnected: true, stillConfigured: outcome === "fallback" }));
    expect(screen.getByRole("button", { name: "取消" })).toBeTruthy();
    expect(screen.queryByText(failure)).toBeNull();
    expect(screen.queryByText(fallback)).toBeNull();
    expect(original).toHaveBeenCalledTimes(1);
    expect(replacement).not.toHaveBeenCalled();
    expect(onDone).toHaveBeenCalledTimes(1);
  },
);

it.each(["port", "root"].flatMap((change) => [false, true].map((tools) => [change, tools] as const)))(
  "clears the previous %s owner's error and confirmation (tools=%s)", async (change, tools) => {
    const port = new MockPort() as unknown as AgentPort;
    const next = new MockPort() as unknown as AgentPort;
    vi.spyOn(port, "setMcpEnabled").mockRejectedValue(new Error(failure));
    const m: McpEntry = { ...server, tools: tools ? 1 : 0, toolList: tools ? [{ name: "read_docs" }] : [] };
    const draw = (active: AgentPort, root: string) => <ServerRow m={m} port={active} root={root} live={root === "/workspace/main"} onDone={() => {}} />;
    const view = render(draw(port, "/workspace/main"));
    await userEvent.click(screen.getByRole("switch"));
    expect(await screen.findByText(failure)).toBeTruthy();
    await userEvent.click(screen.getByRole("button", { name: "移除 docs" }));
    view.rerender(draw(change === "port" ? next : port, change === "port" ? "/workspace/main" : "/workspace/other"));
    expect(screen.queryByRole("button", { name: "取消" })).toBeNull();
    expect(screen.queryByText(failure)).toBeNull();
  },
);

it("dismisses removal confirmation when choosing another managed project", async () => {
  const port = new MockPort() as unknown as AgentPort;
  const scopes: CapabilityScope[] = [
    { root: "/workspace/main", name: "main", key: "main", repo: false, current: true, overrides: 1 },
    { root: "/workspace/other", name: "other", key: "other", repo: false, current: false, overrides: 1 },
  ];
  vi.spyOn(port, "capabilityScopes").mockResolvedValue(scopes);
  vi.spyOn(port, "mcp").mockImplementation(async (root) => ({ servers: [server], scope: scopes[root ? 1 : 0], live: !root }));
  const remove = vi.spyOn(port, "removeMcp");
  render(<Settings
    hub={new MockHub() as never} port={port}
    status={{ preset: "balanced", toolApprovalMode: "ask" } as SessionStatus}
    theme="light" onTheme={() => {}} contrast="" onContrast={() => {}} weight="" onWeight={() => {}}
    look={{} as never} onLook={() => {}} reloadThemes={() => {}}
    onClose={() => {}} onChanged={() => {}} onError={() => {}} at="ext:installed"
    account={null} accountUnread="" reloadAccount={() => {}}
  />);
  await userEvent.click(await screen.findByRole("button", { name: "移除 docs" }));
  expect(screen.getByRole("button", { name: "取消" })).toBeTruthy();
  await userEvent.click(within(document.querySelector<HTMLElement>(".scopebar")!).getByRole("button", { name: "main" }));
  await userEvent.click(screen.getByRole("option", { name: /other/ }));
  await screen.findByText("/workspace/other");
  expect(screen.queryByRole("button", { name: "取消" })).toBeNull();
  expect(screen.queryByRole("button", { name: "移除" })).toBeNull();
  expect(remove).not.toHaveBeenCalled();
});

it("preserves the current owner's busy state, errors and removal fallback", async () => {
  const port = new MockPort() as unknown as AgentPort;
  const pending = deferred<void>();
  const toggle = vi.spyOn(port, "setMcpEnabled").mockImplementationOnce(() => pending.promise);
  vi.spyOn(port, "removeMcp").mockResolvedValue({ disconnected: true, stillConfigured: true });
  const onDone = vi.fn();
  const draw = () => <ServerRow m={{ ...server }} port={port} root="/workspace/main" live onDone={onDone} />;
  const view = render(draw());
  await userEvent.click(screen.getByRole("switch"));
  view.rerender(draw());
  const control = screen.getByRole<HTMLButtonElement>("switch");
  expect(control.disabled).toBe(true);
  await userEvent.click(control);
  expect(toggle).toHaveBeenCalledTimes(1);
  await act(async () => pending.fail());
  expect(control.disabled).toBe(false);
  expect(screen.getByText(failure)).toBeTruthy();
  await userEvent.click(screen.getByRole("button", { name: "移除 docs" }));
  await userEvent.click(screen.getByRole("button", { name: "移除" }));
  expect(screen.getByText(fallback)).toBeTruthy();
  expect(screen.queryByRole("button", { name: "取消" })).toBeNull();
  expect(onDone).toHaveBeenCalledTimes(2);
});
