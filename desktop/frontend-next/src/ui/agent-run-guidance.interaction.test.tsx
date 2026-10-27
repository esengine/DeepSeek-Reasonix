// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { act, cleanup, fireEvent, render, screen } from "@testing-library/react";
import "./testkit";
import { AgentRunView } from "./AgentRunView";
import type { AgentPort } from "../port/port";

afterEach(cleanup);

function port(steer = vi.fn(async () => {}), state = "running") {
  return {
    workspaceImageURL: () => "",
    steerSubagent: steer,
    subagentTranscript: vi.fn(async (ref: string) => ({
      run: { ref, name: "audit", state }, messages: [], after: 0, next: 0, total: 0, live: false,
    })),
  } as unknown as AgentPort;
}

it("queues guidance for the viewed child without claiming it was read", async () => {
  const p = port();
  render(<AgentRunView port={p} run={{ ref: "sa_1", name: "audit" }} shown />);
  const input = await screen.findByRole("textbox", { name: "追加指导" });
  fireEvent.change(input, { target: { value: "check the parser first" } });
  fireEvent.click(screen.getByRole("button", { name: "发送指导" }));
  expect(await screen.findByRole("status")).toHaveProperty("textContent", "指导已排队，将在下一个可处理的边界读取。");
  expect(p.steerSubagent).toHaveBeenCalledWith("sa_1", "check the parser first");
  expect((input as HTMLTextAreaElement).value).toBe("");
});

it("retains a rejected draft and prevents duplicate sends while pending", async () => {
  let reject!: (e: Error) => void;
  const send = vi.fn(() => new Promise<void>((_resolve, fail) => { reject = fail; }));
  render(<AgentRunView port={port(send)} run={{ ref: "sa_1", name: "audit" }} shown />);
  const input = await screen.findByRole("textbox", { name: "追加指导" });
  fireEvent.change(input, { target: { value: "keep this draft" } });
  const button = screen.getByRole("button", { name: "发送指导" });
  fireEvent.click(button);
  fireEvent.click(button);
  expect(send).toHaveBeenCalledTimes(1);
  await act(async () => { reject(new Error("run has finished")); });
  expect((await screen.findByRole("alert")).textContent).toContain("run has finished");
  expect((input as HTMLTextAreaElement).value).toBe("keep this draft");
});

it("does not offer guidance to a completed child", async () => {
  render(<AgentRunView port={port(undefined, "completed")} run={{ ref: "sa_1", name: "audit" }} shown />);
  await screen.findByText("已完成");
  expect(screen.queryByRole("textbox", { name: "追加指导" })).toBeNull();
});

it("does not apply an old send result to a different child", async () => {
  let resolve!: () => void;
  const send = vi.fn(() => new Promise<void>((done) => { resolve = done; }));
  const p = port(send);
  const view = render(<AgentRunView port={p} run={{ ref: "sa_1", name: "audit" }} shown />);
  fireEvent.change(await screen.findByRole("textbox", { name: "追加指导" }), { target: { value: "old task" } });
  fireEvent.click(screen.getByRole("button", { name: "发送指导" }));
  view.rerender(<AgentRunView port={p} run={{ ref: "sa_2", name: "other" }} shown />);
  fireEvent.change(await screen.findByRole("textbox", { name: "追加指导" }), { target: { value: "new task" } });
  await act(async () => { resolve(); });
  expect((screen.getByRole("textbox", { name: "追加指导" }) as HTMLTextAreaElement).value).toBe("new task");
  expect(screen.queryByRole("status")).toBeNull();
});

it("preserves drafts when its tab is hidden and shown", async () => {
  const p = port();
  const run = { ref: "sa_1", name: "audit" };
  const view = render(<AgentRunView port={p} run={run} shown />);
  fireEvent.change(await screen.findByRole("textbox", { name: "追加指导" }), { target: { value: "keep across tabs" } });
  view.rerender(<AgentRunView port={p} run={run} shown={false} />);
  view.rerender(<AgentRunView port={p} run={run} shown />);
  expect((await screen.findByRole("textbox", { name: "追加指导" }) as HTMLTextAreaElement).value).toBe("keep across tabs");
});

it("does not clear newer text when the previous send finishes", async () => {
  let resolve!: () => void;
  const p = port(vi.fn(() => new Promise<void>((done) => { resolve = done; })));
  render(<AgentRunView port={p} run={{ ref: "sa_1", name: "audit" }} shown />);
  const input = await screen.findByRole("textbox", { name: "追加指导" });
  fireEvent.change(input, { target: { value: "first" } });
  fireEvent.click(screen.getByRole("button", { name: "发送指导" }));
  fireEvent.change(input, { target: { value: "next" } });
  await act(async () => { resolve(); });
  expect((input as HTMLTextAreaElement).value).toBe("next");
});
