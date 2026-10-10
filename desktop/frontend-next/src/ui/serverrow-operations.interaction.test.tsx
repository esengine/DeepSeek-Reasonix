// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { act, cleanup, render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { ServerRow } from "./ServerRow";
import { MockPort } from "../port/mock";
import type { AgentPort, McpEntry } from "../port/port";

afterEach(cleanup);

const entry: McpEntry = {
  name: "example-mcp", state: "failed", enabled: true, transport: "http",
  source: "/workspace/.mcp.json", localOverride: true, remembered: true, tools: 1,
  error: "previous connection diagnostic",
  toolList: [{ name: "read_example", description: "Read fixture data", readOnly: true }],
};

it.each([
  ["retry", "success"], ["retry", "failure"],
  ["remove", "success"], ["remove", "failure"],
  ["toggle", "success"], ["toggle", "failure"],
  ["load", "success"], ["load", "failure"],
  ["clear", "success"], ["clear", "failure"],
] as const)("blocks same-server conflicts during %s and restores controls after %s", async (operation, outcome) => {
  const port = new MockPort() as unknown as AgentPort;
  let finish!: () => void;
  let fail!: (error: Error) => void;
  const pending = new Promise<void>((resolve, reject) => { finish = resolve; fail = reject; });
  const calls = {
    retry: vi.spyOn(port, "reconnectMcp").mockResolvedValue({ state: "ready" }),
    remove: vi.spyOn(port, "removeMcp").mockResolvedValue({ disconnected: true, stillConfigured: false }),
    toggle: vi.spyOn(port, "setMcpEnabled").mockResolvedValue(),
    load: vi.spyOn(port, "setMcpLoad").mockResolvedValue(),
    clear: vi.spyOn(port, "clearMcpOverride").mockResolvedValue(),
  };
  if (operation === "retry") calls.retry.mockImplementationOnce(() => pending.then(() => ({ state: "ready" })));
  else if (operation === "remove") calls.remove.mockImplementationOnce(() => pending.then(() => ({ disconnected: true, stillConfigured: false })));
  else calls[operation].mockImplementationOnce(() => pending);
  const onDone = vi.fn();
  render(<>
    <ServerRow m={entry} port={port} onDone={onDone} root="/workspace" live />
    <ServerRow m={{ ...entry, name: "other-mcp" }} port={port} onDone={() => {}} root="/workspace" live />
  </>);
  const element = screen.getByText("example-mcp").closest<HTMLElement>(".srv")!;
  const row = within(element);
  const other = within(screen.getByText("other-mcp").closest<HTMLElement>(".srv")!);
  await userEvent.click(row.getByRole("button", { name: "移除 example-mcp" }));
  const controls = {
    retry: row.getByRole<HTMLButtonElement>("button", { name: "重连" }),
    remove: row.getByRole<HTMLButtonElement>("button", { name: "移除" }),
    toggle: row.getByRole<HTMLButtonElement>("switch", { name: "关闭 example-mcp" }),
    load: row.getByRole<HTMLButtonElement>("button", { name: "常驻" }),
    clear: row.getByRole<HTMLButtonElement>("button", { name: "仅本项目" }),
  };
  const cancel = row.getByRole<HTMLButtonElement>("button", { name: "取消" });
  await userEvent.click(controls[operation]);
  const locked = [...row.getAllByRole<HTMLButtonElement>("button"), controls.toggle].map((button) => button.disabled);
  const busy = element.getAttribute("aria-busy");
  for (const button of [controls.toggle, controls.clear, controls.remove, cancel, controls.retry, controls.load]) {
    await userEvent.click(button);
  }
  const confirmation = row.queryByText(/从 .* 中删除 example-mcp/);
  const otherEnabled = other.getByRole<HTMLButtonElement>("switch", { name: "关闭 other-mcp" }).disabled;
  await act(async () => {
    if (outcome === "success") finish();
    else fail(new Error(`${operation} unavailable`));
  });
  for (const [name, call] of Object.entries(calls)) expect(call).toHaveBeenCalledTimes(name === operation ? 1 : 0);
  expect(locked.every(Boolean)).toBe(true);
  expect(busy).toBe("true");
  expect(confirmation).toBeTruthy();
  expect(otherEnabled).toBe(false);
  expect(onDone).toHaveBeenCalledTimes(1);
  expect(element.getAttribute("aria-busy")).toBe("false");
  expect(row.getByRole<HTMLButtonElement>("switch", { name: "关闭 example-mcp" }).disabled).toBe(false);
  expect(row.getByRole<HTMLButtonElement>("button", { name: "仅本项目" }).disabled).toBe(false);
  expect(row.getByRole<HTMLButtonElement>("button", { name: "重连" }).disabled).toBe(false);
  if (outcome === "failure") expect(row.getByText(`${operation} unavailable`)).toBeTruthy();
  if (operation !== "remove" || outcome === "failure") {
    expect(row.getByRole<HTMLButtonElement>("button", { name: "移除" }).disabled).toBe(false);
    expect(row.getByRole<HTMLButtonElement>("button", { name: "取消" }).disabled).toBe(false);
    await userEvent.click(row.getByRole("button", { name: "取消" }));
    expect(row.queryByText(/从 .* 中删除 example-mcp/)).toBeNull();
  }
});

it.each(["success", "refused", "failure"])("locks the server row without tools until reconnect %s settles", async (outcome) => {
  const port = new MockPort() as unknown as AgentPort;
  let finish!: (result: { state: string; error?: string }) => void;
  let fail!: (error: Error) => void;
  const reconnect = vi.spyOn(port, "reconnectMcp").mockImplementationOnce(() => new Promise((resolve, reject) => { finish = resolve; fail = reject; }));
  const toggle = vi.spyOn(port, "setMcpEnabled").mockResolvedValue();
  const onDone = vi.fn();
  render(<ServerRow m={{ ...entry, tools: 0, toolList: [], localOverride: false }} port={port} onDone={onDone} root="/workspace" live />);
  const element = screen.getByText("example-mcp").closest<HTMLElement>(".srv")!;
  await userEvent.click(screen.getByRole("button", { name: "重连" }));
  const toggleButton = screen.getByRole<HTMLButtonElement>("switch", { name: "关闭 example-mcp" });
  const locked = toggleButton.disabled;
  const busy = element.getAttribute("aria-busy");
  await userEvent.click(toggleButton);
  await act(async () => {
    if (outcome === "failure") fail(new Error("retry offline"));
    else finish(outcome === "refused" ? { state: "failed", error: "service refused" } : { state: "ready" });
  });
  expect(toggle).not.toHaveBeenCalled();
  expect(reconnect).toHaveBeenCalledTimes(1);
  expect(locked).toBe(true);
  expect(busy).toBe("true");
  expect(onDone).toHaveBeenCalledTimes(1);
  expect(element.getAttribute("aria-busy")).toBe("false");
  expect(toggleButton.disabled).toBe(false);
  if (outcome !== "success") expect(screen.getByText(outcome === "refused" ? "service refused" : "retry offline")).toBeTruthy();
});

it.each([false, true])("returns to the connection diagnostic on a new action (tools=%s)", async (tools) => {
  const port = new MockPort() as unknown as AgentPort;
  let finish!: () => void;
  const pending = new Promise<void>((resolve) => { finish = resolve; });
  const toggle = vi.spyOn(port, "setMcpEnabled")
    .mockRejectedValueOnce(new Error("configuration write failed"))
    .mockImplementationOnce(() => pending);
  const onDone = vi.fn();
  render(<ServerRow m={{ ...entry, tools: tools ? 1 : 0, toolList: tools ? entry.toolList : [] }} port={port} onDone={onDone} root="/workspace" live />);
  const control = screen.getByRole<HTMLButtonElement>("switch", { name: "关闭 example-mcp" });
  expect(screen.getByText(entry.error!)).toBeTruthy();
  await userEvent.click(control);
  expect(await screen.findByText("configuration write failed")).toBeTruthy();
  expect(screen.queryByText(entry.error!)).toBeNull();
  await userEvent.click(control);
  expect(screen.queryByText("configuration write failed")).toBeNull();
  expect(screen.getByText(entry.error!)).toBeTruthy();
  expect(control.disabled).toBe(true);
  await act(async () => finish());
  expect(control.disabled).toBe(false);
  expect(toggle).toHaveBeenCalledTimes(2);
  expect(onDone).toHaveBeenCalledTimes(2);
  expect(screen.getByText(entry.error!)).toBeTruthy();
});

it.each([false, true])("explains remaining declarations after removal despite an old error (tools=%s)", async (tools) => {
  const port = new MockPort() as unknown as AgentPort;
  const remove = vi.spyOn(port, "removeMcp").mockResolvedValue({ disconnected: true, stillConfigured: true });
  const onDone = vi.fn();
  render(<ServerRow m={{ ...entry, tools: tools ? 1 : 0, toolList: tools ? entry.toolList : [] }} port={port} onDone={onDone} root="/workspace" live />);
  await userEvent.click(screen.getByRole("button", { name: "移除 example-mcp" }));
  await userEvent.click(screen.getByRole("button", { name: "移除" }));
  expect(await screen.findByText("同名的另一处声明已生效，该行不会消失。")).toBeTruthy();
  expect(screen.queryByText(entry.error!)).toBeNull();
  expect(screen.queryByRole("button", { name: "取消" })).toBeNull();
  expect(remove).toHaveBeenCalledExactlyOnceWith("example-mcp");
  expect(onDone).toHaveBeenCalledTimes(1);
});
