/**
 * UsageCard balance rendering - verifies the currency symbol matches the
 * balance currency, not hardcoded ¥.
 *
 * These tests import the REAL UsageCard component and render it through
 * Ink.  They FAIL today because UsageCard:74 and UsageCard:95 hardcode ¥.
 */
import { render } from "ink";
import React from "react";
import { describe, expect, it } from "vitest";
import { UsageCard } from "../src/cli/ui/cards/UsageCard.js";
import type { UsageCard as UsageCardData } from "../src/cli/ui/state/cards.js";

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

function makeFakeStdout() {
  const chunks: string[] = [];
  return {
    columns: 120,
    rows: 30,
    isTTY: true,
    write(chunk: string) {
      chunks.push(chunk);
      return true;
    },
    on() {},
    off() {},
    text(): string {
      // biome-ignore lint/suspicious/noControlCharactersInRegex: stripping ANSI SGR codes
      return chunks.join("").replace(/\x1b\[[0-9;]*m/g, "");
    },
  };
}

function baseCard(overrides: Partial<UsageCardData> = {}): UsageCardData {
  return {
    kind: "usage" as const,
    id: "u1",
    ts: Date.now(),
    turn: 3,
    tokens: { prompt: 500, reason: 200, output: 100, promptCap: 1024 },
    cacheHit: 0.8,
    cost: 0.002,
    sessionCost: 0.05,
    elapsedMs: 1200,
    ...overrides,
  };
}

function renderCard(card: UsageCardData): string {
  const stdout = makeFakeStdout();
  const { unmount } = render(React.createElement(UsageCard, { card }), {
    stdout: stdout as any,
    stdin: process.stdin as any,
  });
  unmount();
  return stdout.text();
}

// ---------------------------------------------------------------------------
// tests
// ---------------------------------------------------------------------------

describe("UsageCard - balance currency symbol", () => {
  it("full card: shows $ for USD balance", () => {
    const card = baseCard({ balance: 0.91, balanceCurrency: "USD" } as any);
    const text = renderCard(card);
    expect(text).toContain("$0.91");
  });

  it("full card: shows ¥ for CNY balance", () => {
    const card = baseCard({ balance: 6.55, balanceCurrency: "CNY" } as any);
    const text = renderCard(card);
    expect(text).toContain("¥6.55");
  });

  it("full card: hides balance entirely when undefined", () => {
    const card = baseCard({ balance: undefined });
    const text = renderCard(card);
    // When balance is undefined, the entire "· balance ¥…" segment is
    // not rendered at all - not even the "balance" label.
    expect(text).not.toContain("balance ¥");
    expect(text).not.toContain("balance $");
  });

  it("compact row: shows $ for USD balance", () => {
    const card = baseCard({ balance: 0.91, balanceCurrency: "USD", compact: true } as any);
    const text = renderCard(card);
    expect(text).toContain("$0.91");
  });

  it("compact row: shows ¥ for CNY balance", () => {
    const card = baseCard({ balance: 6.55, balanceCurrency: "CNY", compact: true } as any);
    const text = renderCard(card);
    expect(text).toContain("¥6.55");
  });

  // Turn/session costs in the card must follow wallet currency, not unconditional ¥.
  // (Header renders `formatCost(cost)`; body renders `formatCost(sessionCost, …, 3)`.)

  it("full card: USD wallet renders $ for turn cost (header) AND session cost (body)", () => {
    const card = baseCard({
      cost: 0.0308,
      sessionCost: 0.064,
      balance: 0.71,
      balanceCurrency: "USD",
    } as any);
    const text = renderCard(card);
    expect(text).toContain("$0.0308");
    expect(text).toContain("$0.064");
  });

  it("compact row: USD wallet renders $ for turn cost", () => {
    const card = baseCard({
      cost: 0.0308,
      balance: 0.71,
      balanceCurrency: "USD",
      compact: true,
    } as any);
    const text = renderCard(card);
    expect(text).toContain("$0.0308");
  });
});
