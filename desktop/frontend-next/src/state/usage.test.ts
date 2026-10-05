import { describe, expect, it } from "vitest";
import { foldCoverage, foldUsage, quoteCoverage } from "./usage";
import { initialState } from "./session";
import type { CostCoverage, CostQuote, Usage } from "../port/wire";
import type { Metrics } from "./session_types";

const ALL: CostCoverage[] = ["none", "complete", "partial", "incomplete"];

const quote = (over: Partial<CostQuote> = {}): CostQuote => ({
  original: { amount: "0.0021", currency: "USD" },
  estimated: false,
  costComplete: true,
  coverage: "complete",
  ...over,
});

const round = (over: Partial<Usage> = {}): Usage => ({
  promptTokens: 100,
  completionTokens: 20,
  totalTokens: 120,
  cacheHitTokens: 0,
  cacheMissTokens: 0,
  sessionCacheHitTokens: 0,
  sessionCacheMissTokens: 0,
  costQuote: quote(),
  ...over,
});

const fold = (...rounds: Usage[]): Metrics => rounds.reduce(foldUsage, initialState.metrics);

describe("cost coverage folds as a fact, not as an arrival order", () => {
  it("answers the pairs the aggregate is defined by", () => {
    expect(foldCoverage("none", "none")).toBe("none");
    expect(foldCoverage("complete", "none")).toBe("complete");
    expect(foldCoverage("complete", "complete")).toBe("complete");
    expect(foldCoverage("complete", "incomplete")).toBe("partial");
    expect(foldCoverage("incomplete", "complete")).toBe("partial");
    expect(foldCoverage("incomplete", "incomplete")).toBe("incomplete");
    expect(foldCoverage("partial", "complete")).toBe("partial");
    expect(foldCoverage("partial", "incomplete")).toBe("partial");
  });

  // Sixty-four triples is the whole space, so it is checked rather than sampled.
  it("cannot be moved by the order the rounds arrived in", () => {
    for (const a of ALL) {
      for (const b of ALL) {
        expect(foldCoverage(a, b)).toBe(foldCoverage(b, a));
        expect(foldCoverage(a, a)).toBe(a);
        for (const c of ALL) {
          expect(foldCoverage(foldCoverage(a, b), c)).toBe(foldCoverage(a, foldCoverage(b, c)));
        }
      }
    }
  });

  // The failure a boolean invites: the last round deciding for the ones before.
  it("never lets a complete run survive a later unpriced round", () => {
    for (const a of ALL) {
      expect(foldCoverage(foldCoverage(a, "complete"), "incomplete")).not.toBe("complete");
    }
    expect(fold(round(), round(), round({ costQuote: quote({ coverage: "incomplete" }) })).coverage).toBe("partial");
  });
});

describe("what a round contributes", () => {
  it("takes the kernel's name for it", () => {
    expect(quoteCoverage(quote({ coverage: "incomplete" }))).toBe("incomplete");
  });

  // One round is never partial and never none, so the old boolean is the whole
  // answer for a kernel that predates the field — a remote workspace can be one.
  it("reads a kernel that predates the field off its boolean", () => {
    expect(quoteCoverage(quote({ coverage: undefined }))).toBe("complete");
    expect(quoteCoverage(quote({ coverage: undefined, costComplete: false }))).toBe("incomplete");
  });

  it("counts a round that carried no quote at all as unpriced", () => {
    expect(quoteCoverage(undefined)).toBe("incomplete");
    expect(fold(round({ costQuote: undefined, cost: 0.5 })).coverage).toBe("incomplete");
  });
});

describe("a session's coverage", () => {
  it("starts at none, which is not a claim that pricing failed", () => {
    expect(initialState.metrics.coverage).toBe("none");
    expect(initialState.metrics.cost).toBe(0);
  });

  // Both sessions total zero. Only one of them has been billed for anything,
  // and the amount is not what tells them apart.
  it("tells a billed zero from a session that has not spent", () => {
    const billedZero = fold(round({ costQuote: quote({ original: { amount: "0", currency: "USD" } }) }));
    expect(billedZero.cost).toBe(0);
    expect(billedZero.coverage).toBe("complete");
    expect(initialState.metrics.cost).toBe(0);
    expect(initialState.metrics.coverage).toBe("none");
    expect(billedZero.coverage).not.toBe(initialState.metrics.coverage);
  });

  it("reaches partial from either direction", () => {
    const unpriced = round({ costQuote: quote({ coverage: "incomplete", costComplete: false }) });
    expect(fold(round(), unpriced).coverage).toBe("partial");
    expect(fold(unpriced, round()).coverage).toBe("partial");
  });

  it("stays incomplete while nothing has priced", () => {
    const unpriced = round({ costQuote: quote({ coverage: "incomplete", costComplete: false }) });
    expect(fold(unpriced, unpriced).coverage).toBe("incomplete");
  });
});

describe("the reason a session fell short", () => {
  it("is kept by later rounds that priced cleanly", () => {
    const unpriced = round({ costQuote: quote({ coverage: "incomplete", costComplete: false, incompleteReason: "no_price" }) });
    const m = fold(unpriced, round(), round());
    expect(m.coverage).toBe("partial");
    expect(m.incompleteReason).toBe("no_price");
  });

  it("is replaced by the most recent round that fell short", () => {
    const first = round({ costQuote: quote({ coverage: "incomplete", costComplete: false, incompleteReason: "no_price" }) });
    const later = round({ costQuote: quote({ coverage: "incomplete", costComplete: false, incompleteReason: "missing_price_or_usage" }) });
    expect(fold(first, later).incompleteReason).toBe("missing_price_or_usage");
  });

  it("never decides the state on its own", () => {
    const m = fold(round({ costQuote: quote({ incompleteReason: "display_unavailable" }) }));
    expect(m.coverage).toBe("complete");
  });
});
