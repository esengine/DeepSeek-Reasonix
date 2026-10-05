// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { AddPlugin } from "./AddPlugin";
import { MockPort } from "../port/mock";
import { boot, STORAGE } from "../i18n";
import type { AgentPort } from "../port/port";

beforeEach(() => {
  localStorage.setItem(STORAGE, "en");
  boot();
});
afterEach(() => {
  cleanup();
  localStorage.setItem(STORAGE, "zh");
  boot();
});

const source = "https://github.com/demo/review-kit";

describe("localized installation previews", () => {
  it("explains execution and file contributions while preserving the kernel diagnostics", async () => {
    const port = new MockPort() as unknown as AgentPort;
    const plan = await port.planPlugin({ source });
    const main = plan.actions![0]!;
    plan.actions![0] = {
      ...main, commandCount: 2, hookCount: 2, toolCount: 3,
      runtime: { ...main.runtime!, tools: ["lint.scan", "lint.fix"] },
    };
    plan.actions!.push({ kind: "skill", action: "copy_skill", status: "planned", riskLevel: "low", name: "notes" });
    vi.spyOn(port, "planPlugin").mockResolvedValue(plan);
    const install = vi.spyOn(port, "installPlugin");
    render(<AddPlugin port={port} source={source} onClose={() => {}} onInstalled={() => {}} />);
    await userEvent.click(screen.getByRole("button", { name: "See what it is" }));

    expect(await screen.findByRole("heading", { name: "These run programs on this machine" })).toBeTruthy();
    expect(screen.getByRole("heading", { name: "These change what is available" })).toBeTruthy();
    expect(screen.getByRole("heading", { name: "These only add files" })).toBeTruthy();
    expect(screen.getByText("Long-running process")).toBeTruthy();
    expect(screen.getByText(/Runs inside Reasonix, can read the whole session/)).toBeTruthy();
    expect(screen.getByText("New tools")).toBeTruthy();
    expect(screen.getByText("The agent can call them; every call still goes through permission checks")).toBeTruthy();
    expect(screen.getByText("Automatic hooks")).toBeTruthy();
    expect(screen.getByText("2 hooks")).toBeTruthy();
    expect(screen.getByText("3 MCP services")).toBeTruthy();
    expect(screen.getByText("2 skills · 2 commands")).toBeTruthy();
    expect(screen.getByText("bin/pack --serve")).toBeTruthy();
    expect(screen.getByText(main.riskReasons![0]!)).toBeTruthy();
    expect(install).not.toHaveBeenCalled();
  });

  it.each([
    { count: 1, expected: "This version adds: a long-running process · a hook · an external service · a standalone MCP service" },
    { count: 2, expected: "This version adds: a long-running process · 2 hooks · 2 external services · 2 standalone MCP services" },
  ])("translates $count added executable capabilities during an update", async ({ count, expected }) => {
    const port = new MockPort() as unknown as AgentPort;
    const current = (await port.plugins())[0]!;
    const plan = await port.planPlugin({ source: current.source! });
    plan.actions![0] = { ...plan.actions![0]!, hookCount: count, toolCount: count };
    if (count === 2) plan.actions!.push({ ...plan.actions![1]!, name: "pack-metrics" });
    vi.spyOn(port, "planPlugin").mockResolvedValue(plan);
    render(<AddPlugin port={port} updating={current} onClose={() => {}} onInstalled={() => {}} />);

    expect(await screen.findByText(expected)).toBeTruthy();
  });

  it.each([1, 2])("uses English singular and plural units for %s contributions", async (count) => {
    const port = new MockPort() as unknown as AgentPort;
    const plan = await port.planPlugin({ source });
    plan.actions = [{ ...plan.actions![0]!, skillCount: count, commandCount: count, agentCount: count, promptCount: count, themeCount: count, hookCount: count, toolCount: count }];
    vi.spyOn(port, "planPlugin").mockResolvedValue(plan);
    render(<AddPlugin port={port} source={source} onClose={() => {}} onInstalled={() => {}} />);
    await userEvent.click(screen.getByRole("button", { name: "See what it is" }));

    const units = count === 1 ? "1 skill · 1 command · 1 subagent · 1 prompt · 1 palette" : "2 skills · 2 commands · 2 subagents · 2 prompts · 2 palettes";
    expect(await screen.findByText(units)).toBeTruthy();
    expect(screen.getByText(count === 1 ? "1 hook" : "2 hooks")).toBeTruthy();
    expect(screen.getByText(count === 1 ? "1 MCP service" : "2 MCP services")).toBeTruthy();
  });

  it.each([1, 2])("keeps Chinese install counts and tool separators for %s contributions", async (count) => {
    localStorage.setItem(STORAGE, "zh");
    boot();
    const port = new MockPort() as unknown as AgentPort;
    const plan = await port.planPlugin({ source });
    const main = plan.actions![0]!;
    plan.actions = [{ ...main, skillCount: count, commandCount: count, agentCount: count, promptCount: count, themeCount: count, hookCount: count, toolCount: count, runtime: { ...main.runtime!, tools: ["lint.scan", "lint.fix"] } }];
    vi.spyOn(port, "planPlugin").mockResolvedValue(plan);
    render(<AddPlugin port={port} source={source} onClose={() => {}} onInstalled={() => {}} />);
    await userEvent.click(screen.getByRole("button", { name: "查看内容" }));

    expect(await screen.findByText(`${count} 个技能 · ${count} 个命令 · ${count} 个子代理 · ${count} 个提示词 · ${count} 套配色`)).toBeTruthy();
    expect(screen.getByText(`${count} 条`)).toBeTruthy();
    expect(screen.getByText(`${count} 个 MCP 服务`)).toBeTruthy();
    expect(screen.getByText("lint.scan、lint.fix")).toBeTruthy();
    expect(screen.getByText(main.riskReasons![0]!)).toBeTruthy();
  });

  it.each(["zh", "en"])("localizes only newly added runtime tools in the %s update summary", async (lang) => {
    localStorage.setItem(STORAGE, lang);
    boot();
    const port = new MockPort() as unknown as AgentPort;
    const current = (await port.plugins())[0]!;
    const plan = await port.planPlugin({ source: current.source! });
    const runtime = { ...plan.actions![0]!.runtime!, tools: ["lint.scan"] };
    current.runtime = runtime;
    plan.actions = [{ ...plan.actions![0]!, hookCount: 0, toolCount: 0, runtime: { ...runtime, tools: ["lint.scan", "lint.fix", "lint.audit"] } }];
    vi.spyOn(port, "planPlugin").mockResolvedValue(plan);
    render(<AddPlugin port={port} updating={current} onClose={() => {}} onInstalled={() => {}} />);

    expect(await screen.findByText(lang === "zh" ? "本版本新增：工具 lint.fix、lint.audit" : "This version adds: tools lint.fix, lint.audit")).toBeTruthy();
  });

  it.each([1, 2])("keeps the Chinese update summary separated by enumeration commas for %s additions", async (count) => {
    localStorage.setItem(STORAGE, "zh");
    boot();
    const port = new MockPort() as unknown as AgentPort;
    const current = (await port.plugins())[0]!;
    const plan = await port.planPlugin({ source: current.source! });
    plan.actions![0] = { ...plan.actions![0]!, hookCount: count, toolCount: count };
    if (count === 2) plan.actions!.push({ ...plan.actions![1]!, name: "pack-metrics" });
    vi.spyOn(port, "planPlugin").mockResolvedValue(plan);
    render(<AddPlugin port={port} updating={current} onClose={() => {}} onInstalled={() => {}} />);

    expect(await screen.findByText(count === 1
      ? "本版本新增：一个常驻进程、一条钩子、一个外部服务、一个独立的 MCP 服务"
      : "本版本新增：一个常驻进程、2 条钩子、2 个外部服务、2 个独立的 MCP 服务")).toBeTruthy();
  });
});
