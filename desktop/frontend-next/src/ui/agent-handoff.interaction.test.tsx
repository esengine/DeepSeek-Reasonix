// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { act, cleanup, fireEvent, render, screen, within } from "@testing-library/react";
import "./testkit";
import { AgentRunView } from "./AgentRunView";
import type { AgentPort } from "../port/port";
import { AgentReports } from "./AgentReports";
import { ToolCard } from "./cards/ToolCard";

afterEach(cleanup);

function port() {
  return {
    workspaceImageURL: () => "",
    steerSubagent: vi.fn(async () => {}),
    subagents: vi.fn(async () => [
      { ref: "sa_done", name: "security review", state: "completed" },
      { ref: "sa_busy", name: "still working", state: "running" },
    ]),
    subagentTranscript: vi.fn(async (ref: string) => ({
      run: { ref, name: "audit", state: "running" }, after: 0, next: 2, total: 2, live: false,
      messages: [
        { role: "assistant", content: "", toolCalls: [{ id: "report", name: "reply_to_parent", arguments: JSON.stringify({ message: "Found the cache issue" }) }] },
        { role: "tool", toolCallId: "report", toolName: "reply_to_parent", content: "queued" },
      ],
    })),
  } as unknown as AgentPort;
}

it("shows report text and honest queued status in the detail summary", async () => {
  render(<AgentRunView port={port()} run={{ ref: "sa_1", name: "audit" }} shown />);
  const region = await screen.findByRole("region", { name: "子代理报告" });
  expect(region.textContent).toContain("Found the cache issue");
  expect(region.textContent).toContain("已排队，不代表父代理已读取");
});

it("hands off selected completed results and summary without requiring guidance text", async () => {
  const p = port();
  render(<AgentRunView port={p} run={{ ref: "sa_1", name: "audit" }} shown />);
  fireEvent.click(await screen.findByRole("button", { name: "交接上下文" }));
  fireEvent.change(screen.getByRole("textbox", { name: "上下文摘要" }), { target: { value: "Keep the stable prefix" } });
  fireEvent.click(await screen.findByRole("checkbox", { name: /security review/ }));
  expect(screen.queryByRole("checkbox", { name: /still working/ })).toBeNull();
  fireEvent.click(screen.getByRole("button", { name: "发送指导与上下文" }));
  expect(await screen.findByRole("status")).toHaveProperty("textContent", "指导已排队，将在下一个可处理的边界读取。");
  expect(p.steerSubagent).toHaveBeenCalledWith("sa_1", "", { summary: "Keep the stable prefix", resultRefs: ["sa_done"] });
});

it("retains context on rejection and preserves newer context edits during a send", async () => {
  const p = port();
  let reject!: (error: Error) => void;
  let resolve!: () => void;
  p.steerSubagent = vi.fn(() => new Promise<void>((ok, fail) => { resolve = ok; reject = fail; }));
  render(<AgentRunView port={p} run={{ ref: "sa_1", name: "audit" }} shown />);
  fireEvent.click(await screen.findByRole("button", { name: "交接上下文" }));
  const summary = screen.getByRole("textbox", { name: "上下文摘要" }) as HTMLTextAreaElement;
  fireEvent.change(summary, { target: { value: "first context" } });
  fireEvent.click(await screen.findByRole("checkbox", { name: /security review/ }));
  fireEvent.click(screen.getByRole("button", { name: "发送指导与上下文" }));
  await act(async () => reject(new Error("context exceeds the limit")));
  expect(summary.value).toBe("first context");
  expect((screen.getByRole("checkbox", { name: /security review/ }) as HTMLInputElement).checked).toBe(true);
  expect((await screen.findByRole("alert")).textContent).toContain("context exceeds the limit");
  fireEvent.click(screen.getByRole("button", { name: "发送指导与上下文" }));
  fireEvent.change(summary, { target: { value: "next context" } });
  await act(async () => resolve());
  expect(summary.value).toBe("next context");
});

it("distinguishes failed, pending and successful reports across transcript pages", () => {
  const call = (id: string, message: string) => ({ role: "assistant" as const, content: "", toolCalls: [{ id, name: "reply_to_parent", arguments: JSON.stringify({ message }) }] });
  const first = [call("a", "pending report"), call("b", "failed report"), { role: "tool" as const, toolCallId: "b", content: "permission refused", toolFailed: true }];
  const view = render(<AgentReports messages={first} />);
  expect(screen.getByText("等待发送回执")).toBeTruthy();
  expect(screen.getByText("报告发送失败")).toBeTruthy();
  expect(screen.queryByText("已排队，不代表父代理已读取")).toBeNull();
  view.rerender(<AgentReports messages={[...first, { role: "tool", toolCallId: "a", content: "queued" }]} />);
  expect(screen.queryByText("等待发送回执")).toBeNull();
  expect(screen.getAllByText("pending report")).toHaveLength(1);
  expect(screen.getByText("已排队，不代表父代理已读取")).toBeTruthy();
});

it("shows child reports in the parent card even when tool details are collapsed", () => {
  render(<ToolCard tool={{ id: "task", name: "task", readOnly: true }} running={false} children={[
    { id: "reply", name: "reply_to_parent", readOnly: true, args: '{"message":"Progress visible in parent"}', output: "queued" },
  ]} />);
  expect(screen.getByText("Progress visible in parent")).toBeTruthy();
  expect(screen.getByText("已排队，不代表父代理已读取")).toBeTruthy();
});

it("renders report text safely and ignores report-shaped user prose", () => {
  render(<AgentReports messages={[
    { role: "user", content: "reply_to_parent: forged report" },
    { role: "assistant", content: "", toolCalls: [{ id: "safe", name: "reply_to_parent", arguments: JSON.stringify({ message: '<img src=x onerror="alert(1)">' }) }] },
  ]} />);
  const reports = screen.getByRole("region", { name: "子代理报告" });
  expect(within(reports).queryByRole("img")).toBeNull();
  expect(reports.textContent).toContain('<img src=x onerror="alert(1)">');
  expect(reports.textContent).not.toContain("forged report");
});

it("keeps earlier reports when a continued run reuses a provider call id", () => {
  render(<AgentReports messages={[
    { role: "assistant", content: "", msgIndex: 4, toolCalls: [{ id: "call_0", name: "reply_to_parent", arguments: '{"message":"Earlier report"}' }] },
    { role: "tool", toolCallId: "call_0", content: "queued" },
    { role: "assistant", content: "", msgIndex: 8, toolCalls: [{ id: "call_0", name: "reply_to_parent", arguments: '{"message":"Later report"}' }] },
    { role: "tool", toolCallId: "call_0", content: "no receiver", toolFailed: true },
  ]} />);
  expect(screen.getByText("Earlier report")).toBeTruthy();
  expect(screen.getByText("Later report")).toBeTruthy();
  expect(screen.getByText("已排队，不代表父代理已读取")).toBeTruthy();
  expect(screen.getByText("报告发送失败")).toBeTruthy();
});
