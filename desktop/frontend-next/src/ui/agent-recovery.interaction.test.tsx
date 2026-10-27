// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import type { AgentPort } from "../port/port";
import { AgentRecovery } from "./AgentRecovery";

afterEach(cleanup);
const plan = { id: "fr_one", snapshot: "snapshot", ready: true, tasks: [{ id: "one", description: "调查", prompt: "", dependsOn: [], state: "completed", ref: "sa_done" }, { id: "two", description: "实现", prompt: "Update parser", dependsOn: ["one"], state: "unfinished" }] };

it("previews completed and unfinished tasks before sending a snapshot-bound recovery request", async () => {
  const send = vi.fn(async () => {});
  const read = vi.fn(async () => [plan]);
  const port = { fleetRecoveryPlans: read, requestFleetRecovery: send } as unknown as AgentPort;
  render(<AgentRecovery port={port} runRef="sa_child" />);
  expect(read).not.toHaveBeenCalled();
  fireEvent.click(screen.getByText("查看恢复计划"));
  await screen.findByText("调查");
  expect(screen.getByText("实现")).toBeTruthy();
  expect(send).not.toHaveBeenCalled();
  fireEvent.click(screen.getByText("请求恢复执行"));
  await waitFor(() => expect(send).toHaveBeenCalledWith("sa_child", "fr_one", "snapshot"));
  expect((await screen.findByRole("status")).textContent).toContain("恢复请求已加入当前会话");
});

it("drops a reply for a different child", async () => {
  let complete!: (plans: typeof plan[]) => void;
  const port = { fleetRecoveryPlans: vi.fn(() => new Promise(resolve => { complete = resolve; })), requestFleetRecovery: vi.fn() } as unknown as AgentPort;
  const view = render(<AgentRecovery port={port} runRef="old" />);
  fireEvent.click(screen.getByText("查看恢复计划"));
  view.rerender(<AgentRecovery port={port} runRef="new" />);
  complete([plan]);
  await waitFor(() => expect(screen.queryByText("调查")).toBeNull());
});

it("prevents duplicate submission and preserves the preview after a refusal", async () => {
  let reject!: (cause: Error) => void;
  const send = vi.fn(() => new Promise<void>((_, fail) => { reject = fail; }));
  const port = { fleetRecoveryPlans: vi.fn(async () => [plan]), requestFleetRecovery: send } as unknown as AgentPort;
  render(<AgentRecovery port={port} runRef="sa_child" />);
  fireEvent.click(screen.getByText("查看恢复计划"));
  const button = await screen.findByText("请求恢复执行");
  fireEvent.click(button); fireEvent.click(button);
  expect(send).toHaveBeenCalledTimes(1);
  reject(new Error("recovery unavailable"));
  await screen.findByRole("alert");
  expect(screen.getByText("调查")).toBeTruthy();
  expect((button as HTMLButtonElement).disabled).toBe(false);
  expect(screen.queryByRole("status")).toBeNull();
});
