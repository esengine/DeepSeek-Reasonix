// @vitest-environment jsdom
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { cleanup, render, screen, within } from "@testing-library/react";
import "./testkit";
import { boot, STORAGE } from "../i18n";
import { MockHub } from "../port/mock_hub";
import type { AgentPort, UsageReport } from "../port/port";
import { TeamRail } from "./team/TeamRail";
import { Usage } from "./Usage";

beforeEach(() => {
  localStorage.setItem(STORAGE, "zh");
  boot();
});
afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
});

const REPORT: UsageReport = {
  from: "2026-08-01", to: "2026-08-31", tokens: 400, requests: 6, turns: 3, cache_hit: 0, cache_miss: 0,
  active_days: 1, top_model: "deepseek/chat", top_provider: "deepseek", daily: [],
  models: [{ model: "deepseek/chat", provider: "deepseek", tokens: 400, percent: 100 }],
  providers: [{ provider: "deepseek", tokens: 400, percent: 100 }],
};

function port(report: UsageReport) {
  return { usage: vi.fn().mockResolvedValue(report), providers: vi.fn().mockResolvedValue([]), models: vi.fn().mockResolvedValue([]) } as unknown as AgentPort;
}

it("names the agents whose seats spent, and an agent no longer on the team as gone", async () => {
  const hub = new MockHub();
  const team = hub.team();
  await team.bootstrap("Chief");
  await team.createAgent({ name: "Ada" });
  render(<>
    <TeamRail hub={hub} active="" onAdoptPane={vi.fn()} />
    <Usage port={port({ ...REPORT, agents: [
      { agent: "a-1", tokens: 200, requests: 3, percent: 50 },
      { agent: "a-9", tokens: 100, requests: 1, percent: 25 },
    ] })} />
  </>);
  const card = (await screen.findByRole("heading", { name: "按 Agent" })).closest("section")!;
  expect(within(card).getByText("Ada")).toBeTruthy();
  expect(within(card).getByText("已删除的 Agent")).toBeTruthy();
  expect(within(card).getByText(/200\s+50\.0%/)).toBeTruthy();
});

it("shows no agent card when nothing was spent from a team seat", async () => {
  render(<Usage port={port({ ...REPORT, agents: [] })} />);
  await screen.findByRole("heading", { name: "按模型" });
  expect(screen.queryByRole("heading", { name: "按 Agent" })).toBeNull();
});
