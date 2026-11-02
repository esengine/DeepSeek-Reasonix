// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, waitFor } from "@testing-library/react";
import "./testkit";
import { boot, STORAGE } from "../i18n";
import type { AgentPort, UsageReport } from "../port/port";
import { Usage } from "./Usage";

// The kernel's total already knew its money was incomplete; this is the line
// that says how many calls it could not price. A total that names only the
// floor leaves the reader unable to ask which calls were under it.
const setLocale = (lang: "zh" | "en") => { localStorage.setItem(STORAGE, lang); boot(); };

afterEach(() => { cleanup(); vi.restoreAllMocks(); setLocale("zh"); });

const REPORT: UsageReport = {
  from: "2026-08-10", to: "2026-08-12", tokens: 3_000, requests: 9, turns: 6, cache_hit: 2_400, cache_miss: 600,
  cost: [{ amount: "4", currency: "CNY" }],
  // Four calls priced, five the range could not price, for two reasons.
  costIncomplete: true, unpricedCalls: 5, incompleteReasons: ["missing_price_or_usage", "no_price"],
  active_days: 3, top_model: "relay/a", top_provider: "relay", models: [], providers: [], daily: [],
};

async function open(report: UsageReport) {
  const port = { usage: vi.fn().mockResolvedValue(report), providers: vi.fn().mockResolvedValue([]), models: vi.fn().mockResolvedValue([]) } as unknown as AgentPort;
  const view = render(<Usage port={port} />);
  await waitFor(() => expect(view.container.querySelector(".uhero")).toBeTruthy());
  return view;
}

// The scopes beside the figure: the caveats a person reads before trusting it.
function scopes(container: HTMLElement): string {
  return Array.from(container.querySelectorAll(".uhero-scope")).map((el) => el.textContent ?? "").join(" | ");
}

describe("the unpriced-call count on the usage page", () => {
  it("says how many calls the total could not price, in Chinese", async () => {
    const { container } = await open(REPORT);
    expect(scopes(container)).toContain("未定价调用未计入");
    expect(scopes(container)).toContain("其中 5 笔未定价");
  });

  it("says it in English too", async () => {
    setLocale("en");
    const { container } = await open(REPORT);
    expect(scopes(container)).toContain("unpriced calls not included");
    expect(scopes(container)).toContain("5 calls unpriced");
  });

  it("shows nothing extra for a fully priced range, in either language", async () => {
    const priced: UsageReport = { ...REPORT, costIncomplete: false, unpricedCalls: 0, incompleteReasons: undefined };
    for (const lang of ["zh", "en"] as const) {
      setLocale(lang);
      const { container, unmount } = await open(priced);
      expect(scopes(container)).not.toContain(lang === "zh" ? "未定价" : "unpriced");
      unmount();
    }
  });

  it("does not invent a zero count for a payload that predates the field", async () => {
    const { container } = await open({ ...REPORT, unpricedCalls: undefined });
    expect(scopes(container)).toContain("未定价调用未计入");
    expect(scopes(container)).not.toContain("其中 0 笔未定价");
  });
});
