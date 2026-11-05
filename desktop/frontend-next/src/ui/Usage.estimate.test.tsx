// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor, within } from "@testing-library/react";
import "./testkit";
import type { AgentPort, UsageReport } from "../port/port";
import { Usage } from "./Usage";

afterEach(() => { cleanup(); vi.restoreAllMocks(); });

const day = (d: string, over: Partial<UsageReport["daily"][number]> = {}): UsageReport["daily"][number] => ({
  day: d, total: 1_000, byModel: {}, byProvider: {}, requests: 3, turns: 2, cacheHit: 800, cacheMiss: 200, ...over,
});

const REPORT: UsageReport = {
  from: "2026-08-10", to: "2026-08-12", tokens: 3_000, requests: 9, turns: 6, cache_hit: 2_400, cache_miss: 600,
  cost: [{ amount: "4", currency: "CNY" }],
  estimateCost: [{ amount: "10", currency: "CNY" }],
  active_days: 3, top_model: "relay/a", top_provider: "relay", models: [], providers: [],
  daily: [
    day("2026-08-10", { cost: [{ amount: "4", currency: "CNY" }] }),
    day("2026-08-11", { estimateCost: [{ amount: "7", currency: "CNY" }] }),
    day("2026-08-12", { cost: undefined, estimateCost: [{ amount: "3", currency: "CNY" }] }),
  ],
};

async function open(report: UsageReport) {
  const port = { usage: vi.fn().mockResolvedValue(report), providers: vi.fn().mockResolvedValue([]), models: vi.fn().mockResolvedValue([]) } as unknown as AgentPort;
  const view = render(<Usage port={port} />);
  await waitFor(() => expect(view.container.querySelector(".utile-paid")).toBeTruthy());
  return view;
}

describe("a subscription's estimate on the usage page", () => {
  it("is shown on its own line, labelled as an estimate, and never inside the paid total", async () => {
    const { container } = await open(REPORT);
    const paid = container.querySelector(".utile-paid .v")!;
    expect(paid.textContent).toContain("4");
    expect(paid.textContent).not.toContain("14");
    expect(paid.textContent).not.toContain("10");
    const est = container.querySelector(".uest")!;
    expect(est.textContent).toContain("10");
    expect(est.textContent).toContain("等价估算");
  });

  it("sits beside the billed figure in a day's row, and a day with only an estimate shows no billed cost", async () => {
    const { container } = await open(REPORT);
    const row = (d: string) => container.querySelector<HTMLElement>(`tr[data-day="${d}"]`)!;
    expect(within(row("2026-08-10")).queryByText(/等价估算/)).toBeNull();
    expect(row("2026-08-11").querySelector(".udaily-est")?.textContent).toContain("7");
    expect(row("2026-08-11").querySelectorAll("td")[2]!.textContent).toContain("—");
  });

  it("does not call an estimate-only day unpriced", async () => {
    await open(REPORT);
    expect(screen.queryByText(/没有成本记录/)).toBeNull();
  });

  it("shows nothing extra when there is no estimate", async () => {
    const { container } = await open({ ...REPORT, estimateCost: undefined, daily: REPORT.daily.map((d) => ({ ...d, estimateCost: undefined })) });
    expect(container.querySelector(".uest")).toBeNull();
    expect(container.querySelector(".udaily-est")).toBeNull();
  });
});
