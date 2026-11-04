// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { Feedback } from "./Feedback";
import { dropDraft } from "./feedbackdraft";
import { MockPort } from "../port/mock";
import { host } from "../port/host";
import type { AgentPort } from "../port/port";
import type { FeedbackProcessMemory } from "../port/feedback";

beforeEach(dropDraft);
afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
  sessionStorage.clear();
});

const SHELL: FeedbackProcessMemory[] = [
  { process: "renderer", workingSetKB: 3992908, privateKB: 3900000 },
  { process: "browser", workingSetKB: 120000, privateKB: 90000 },
  { process: "gpu", workingSetKB: 76000, privateKB: 60000 },
];
const KERNEL: FeedbackProcessMemory = { process: "kernel", workingSetKB: 196608, privateKB: 150000 };

function setup(shell: FeedbackProcessMemory[]) {
  vi.spyOn(host(), "processMemory").mockResolvedValue(shell);
  const port = new MockPort() as unknown as AgentPort;
  const base = port.feedbackEnv.bind(port);
  port.feedbackEnv = vi.fn(async (locale?: string) => {
    const got = await base(locale);
    return { ...got, env: { ...got.env, memory: [KERNEL] } };
  });
  port.sendFeedback = vi.fn(port.sendFeedback.bind(port));
  port.openExternal = vi.fn(async () => {});
  render(<Feedback port={port} tab="send" onClose={vi.fn()} onError={vi.fn()} />);
  return port;
}

const envBlock = async () => within((await screen.findByText("随反馈一起发送的环境信息")).closest("section") as HTMLElement);

async function sendOne(port: AgentPort) {
  await screen.findByText("随反馈一起发送的环境信息");
  await userEvent.clear(screen.getByRole("textbox", { name: /昵称/ }));
  await userEvent.type(screen.getByRole("textbox", { name: /昵称/ }), "ada");
  await userEvent.type(screen.getByRole("textbox", { name: /发生了什么/ }), "占用很高");
  await userEvent.click(screen.getByRole("button", { name: /^发送$/ }));
  await vi.waitFor(() => expect(port.sendFeedback).toHaveBeenCalled());
}

describe("the memory a report carries", () => {
  it("is shown to the person with its numbers before anything is sent, the interface first and the kernel last", async () => {
    setup(SHELL);
    expect(await (await envBlock()).findByText(/界面进程：3\.8 GB/)).toBeTruthy();
    const lines = [...document.querySelectorAll(".fbs-env dd div")].map((n) => n.textContent ?? "");
    expect(lines.map((l) => l.split("：")[0])).toEqual(["界面进程", "主进程", "图形进程", "内核"]);
    expect(lines[3]).toContain("192.0 MB");
    expect(screen.getByText(/内存一项只有进程类型和上面这些数字/)).toBeTruthy();
  });

  it("goes with the report as the shell's rows alone, which the kernel completes with its own", async () => {
    const port = setup(SHELL);
    await (await envBlock()).findByText(/界面进程/);
    await sendOne(port);
    const req = (port.sendFeedback as unknown as { mock: { calls: [{ memory?: FeedbackProcessMemory[] }][] } }).mock.calls[0][0];
    expect(req.memory).toEqual(SHELL);
    expect(JSON.stringify(req.memory)).not.toMatch(/kernel/);
  });

  it("is only the kernel's row in a tab, where there is no shell to ask, and sends no memory of its own", async () => {
    const port = setup([]);
    expect(await (await envBlock()).findByText(/内核：192\.0 MB/)).toBeTruthy();
    expect(document.querySelectorAll(".fbs-env dd div")).toHaveLength(1);
    await sendOne(port);
    const req = (port.sendFeedback as unknown as { mock: { calls: [Record<string, unknown>][] } }).mock.calls[0][0];
    expect("memory" in req).toBe(false);
  });

  it("shows no path, process id or command line anywhere in the block", async () => {
    setup(SHELL);
    await (await envBlock()).findByText(/界面进程/);
    expect(document.querySelector(".fbs-env")?.textContent ?? "").not.toMatch(/\\|\/Users|pid|--/i);
  });
});
