import { describe, expect, it } from "vitest";
import {
  HEAT_CAP,
  HEAT_WEIGHT,
  REPUTATION_WEIGHT,
  approvalRate,
  decayedInstalls,
  heat,
  needsReview,
  recommendScore,
  wilsonLowerBound,
} from "./ranking";

describe("wilsonLowerBound", () => {
  it("matches the textbook 95% bound", () => {
    expect(wilsonLowerBound(0, 0)).toBe(0);
    expect(wilsonLowerBound(1, 0)).toBeCloseTo(0.2065, 4);
    expect(wilsonLowerBound(10, 0)).toBeCloseTo(0.7225, 4);
    expect(wilsonLowerBound(50, 50)).toBeCloseTo(0.4038, 4);
    expect(wilsonLowerBound(0, 10)).toBe(0);
  });

  it("ranks many votes above a lucky few at the same ratio", () => {
    expect(wilsonLowerBound(90, 10)).toBeGreaterThan(wilsonLowerBound(9, 1));
    expect(wilsonLowerBound(9, 1)).toBeGreaterThan(wilsonLowerBound(1, 0));
  });
});

describe("decayedInstalls", () => {
  const today = "2026-09-27";
  it("halves every fourteen days and ignores the thirty-first day back", () => {
    expect(decayedInstalls([{ date: today, count: 8 }], today)).toBe(8);
    expect(decayedInstalls([{ date: "2026-09-13", count: 8 }], today)).toBeCloseTo(4, 10);
    expect(decayedInstalls([{ date: "2026-08-29", count: 8 }], today)).toBeGreaterThan(0);
    expect(decayedInstalls([{ date: "2026-08-28", count: 8 }], today)).toBe(0);
  });

  it("counts a future-dated row as today", () => {
    expect(decayedInstalls([{ date: "2026-09-28", count: 3 }], today)).toBe(3);
  });
});

describe("heat", () => {
  it("is logarithmic and capped at one", () => {
    expect(heat(0)).toBe(0);
    expect(heat(HEAT_CAP)).toBeCloseTo(1, 12);
    expect(heat(HEAT_CAP * 10)).toBe(1);
    expect(heat(10)).toBeCloseTo(Math.log(11) / Math.log(HEAT_CAP + 1), 12);
  });
});

describe("recommendScore", () => {
  it("weights reputation and heat as published", () => {
    expect(REPUTATION_WEIGHT + HEAT_WEIGHT).toBeCloseTo(1, 12);
    const today = "2026-09-27";
    const s = recommendScore(10, 0, [{ date: today, count: 10 }], today);
    expect(REPUTATION_WEIGHT).toBe(0.75);
    expect(HEAT_WEIGHT).toBe(0.25);
    expect(HEAT_CAP).toBe(5000);
    expect(s).toBeCloseTo(0.75 * wilsonLowerBound(10, 0) + 0.25 * heat(10), 5);
    expect(recommendScore(10, 0, [{ date: today, count: 10 }], today)).toBe(s);
    expect(recommendScore(0, 0, [], today)).toBe(0);
  });

  it("keeps any number of minted install ids below a solid vote record", () => {
    const today = "2026-09-27";
    const flooded = recommendScore(0, 0, [{ date: today, count: 1_000_000 }], today);
    expect(flooded).toBeCloseTo(HEAT_WEIGHT, 6);
    expect(recommendScore(50, 2, [], today)).toBeGreaterThan(flooded);
  });
});

describe("review flag", () => {
  it("needs ten down-votes and under thirty percent approval", () => {
    expect(needsReview(0, 9)).toBe(false);
    expect(needsReview(0, 10)).toBe(true);
    expect(needsReview(4, 10)).toBe(true); // 28.6%
    expect(needsReview(5, 10)).toBe(false); // 33.3%
    expect(approvalRate(0, 0)).toBeNull();
    expect(approvalRate(3, 1)).toBe(0.75);
  });
});
