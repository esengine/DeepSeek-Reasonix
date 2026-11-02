// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import "./testkit";
import { boot, STORAGE, t } from "../i18n";
import type { AgentPort, UsageDay, UsageReport } from "../port/port";
import { Usage } from "./Usage";

// The kernel's series now says which day's money is a floor. A chart that draws
// a floor and a measurement identically makes a bad day look like a good one,
// so the floor day has to say so where the number is read: the tooltip over the
// chart and the exact day row beneath it.
const setLocale = (lang: "zh" | "en") => { localStorage.setItem(STORAGE, lang); boot(); };

afterEach(() => { cleanup(); vi.restoreAllMocks(); setLocale("zh"); });

const day = (d: string, over: Partial<UsageDay> = {}): UsageDay => ({
  day: d, total: 1_000, byModel: {}, byProvider: {}, requests: 3, turns: 2, cacheHit: 800, cacheMiss: 200, ...over,
});

const REPORT: UsageReport = {
  from: "2026-08-10", to: "2026-08-11", tokens: 2_000, requests: 6, turns: 4, cache_hit: 1_600, cache_miss: 400,
  cost: [{ amount: "4", currency: "CNY" }],
  costIncomplete: true, unpricedCalls: 1, incompleteReasons: ["missing_price_or_usage"],
  active_days: 2, top_model: "relay/a", top_provider: "relay", models: [], providers: [],
  daily: [
    day("2026-08-10", {
      cost: [{ amount: "3", currency: "CNY" }],
      countedCalls: 2, unpricedCalls: 0, estimatedCalls: 0, unnamedCardCalls: 0, unclassifiedCardCalls: 0,
    }),
    day("2026-08-11", {
      cost: [{ amount: "1", currency: "CNY" }],
      countedCalls: 2, unpricedCalls: 1, estimatedCalls: 0, unnamedCardCalls: 1, unclassifiedCardCalls: 0,
      incompleteReasons: ["missing_price_or_usage"],
    }),
  ],
};

async function open(report: UsageReport) {
  const port = {
    usage: vi.fn().mockResolvedValue(report),
    providers: vi.fn().mockResolvedValue([]),
    models: vi.fn().mockResolvedValue([]),
  } as unknown as AgentPort;
  const view = render(<Usage port={port} />);
  await waitFor(() => expect(view.container.querySelector(".uhero")).toBeTruthy());
  return view;
}

function row(container: HTMLElement, d: string): HTMLElement {
  const found = container.querySelector<HTMLElement>(`tr[data-day="${d}"]`);
  if (!found) throw new Error(`no day row for ${d}`);
  return found;
}

// The chart's hit rect maps clientX onto the rendered points; with W = 860 and
// two days the x positions are 58 and 848.
function hoverDay(clientX: number) {
  const chart = screen.getByRole("img", { name: t("每日 tokens") });
  vi.spyOn(chart, "getBoundingClientRect").mockReturnValue({ left: 0, width: 860 } as DOMRect);
  const hit = chart.querySelector(".uhit");
  if (!hit) throw new Error("the chart has no hit area");
  fireEvent.pointerMove(hit, { clientX });
}

describe("a day whose money is a floor", () => {
  it("says so in the exact day row, and leaves the measured day alone", async () => {
    const { container } = await open(REPORT);
    expect(row(container, "2026-08-11").querySelector(".udaily-floor")?.textContent).toBe("下限 · 1 笔未定价");
    expect(row(container, "2026-08-10").querySelector(".udaily-floor")).toBeNull();
  });

  it("says so in the chart tooltip, and leaves the measured day alone", async () => {
    const { container } = await open(REPORT);
    hoverDay(848);
    expect(container.querySelector(".utip .ufloor")?.textContent).toBe("下限 · 1 笔未定价");
    hoverDay(58);
    expect(container.querySelector(".utip .ufloor")).toBeNull();
  });

  it("says it in English too", async () => {
    setLocale("en");
    const { container } = await open(REPORT);
    expect(row(container, "2026-08-11").querySelector(".udaily-floor")?.textContent).toBe("floor · 1 unpriced");
    hoverDay(848);
    expect(container.querySelector(".utip .ufloor")?.textContent).toBe("floor · 1 unpriced");
  });

  it("does not invent a floor for a day that predates the field", async () => {
    const legacy: UsageReport = {
      ...REPORT,
      daily: REPORT.daily.map((d) => ({ ...d, unpricedCalls: undefined, countedCalls: undefined })),
    };
    const { container } = await open(legacy);
    expect(row(container, "2026-08-11").querySelector(".udaily-floor")).toBeNull();
    hoverDay(848);
    expect(container.querySelector(".utip .ufloor")).toBeNull();
  });
});
