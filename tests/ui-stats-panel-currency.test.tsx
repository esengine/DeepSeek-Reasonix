import { render } from "ink";
import React from "react";
import { describe, expect, it } from "vitest";
import { StatsPanel } from "../src/cli/ui/StatsPanel.js";
import type { SessionSummary } from "../src/telemetry/stats.js";
import { makeFakeStdin, makeFakeStdout } from "./helpers/ink-stdio.js";

const SUMMARY: SessionSummary = {
  turns: 5,
  totalCostUsd: 0.0308,
  totalInputCostUsd: 0.01,
  totalOutputCostUsd: 0.02,
  claudeEquivalentUsd: 0.5,
  savingsVsClaudePct: 0.94,
  cacheHitRatio: 0.8,
  lastPromptTokens: 1000,
  lastTurnCostUsd: 0.0001,
};

function renderPanel(balance: { currency: string; total: number } | null): string {
  const stdout = makeFakeStdout();
  const { unmount } = render(React.createElement(StatsPanel, { summary: SUMMARY, balance }), {
    stdout: stdout as never,
    stdin: makeFakeStdin() as never,
  });
  unmount();
  return stdout.text();
}

describe("StatsPanel — top-bar cost + balance follow wallet currency", () => {
  it("USD wallet: cost shows $0.0308 and balance shows $0.91", () => {
    const text = renderPanel({ currency: "USD", total: 0.91 });
    expect(text).toContain("[$0.0308]");
    expect(text).toContain("[w $0.91]");
    expect(text).not.toContain("¥");
  });

  it("CNY wallet: USD cost is converted to ¥ and balance shows ¥6.55", () => {
    const text = renderPanel({ currency: "CNY", total: 6.55 });
    expect(text).toContain("[¥0.2218]");
    expect(text).toContain("[w ¥6.55]");
  });

  it("no wallet: cost defaults to ¥ (matches pre-fix unconditional ¥)", () => {
    const text = renderPanel(null);
    expect(text).toContain("[¥0.2218]");
    expect(text).not.toContain("[w ");
  });
});
