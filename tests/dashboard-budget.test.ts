import { describe, expect, it } from "vitest";
import {
  QUICK_CAPS_USD,
  budgetTone,
  bumpSuggestions,
  deriveBudgetState,
} from "../dashboard/src/lib/budget.js";

describe("deriveBudgetState", () => {
  it("returns off when cap is null / 0 / negative", () => {
    expect(deriveBudgetState(null, 0.5)).toEqual({ kind: "off", spent: 0.5 });
    expect(deriveBudgetState(0, 0.5)).toEqual({ kind: "off", spent: 0.5 });
    expect(deriveBudgetState(-1, 0.5)).toEqual({ kind: "off", spent: 0.5 });
    expect(deriveBudgetState(undefined, undefined)).toEqual({ kind: "off", spent: 0 });
  });

  it("returns running below 80%", () => {
    expect(deriveBudgetState(5, 0)).toMatchObject({ kind: "running", cap: 5, spent: 0, pct: 0 });
    expect(deriveBudgetState(5, 3.99)).toMatchObject({ kind: "running" });
  });

  it("returns warn at exactly 80% and up to 99.99%", () => {
    expect(deriveBudgetState(5, 4.0)).toMatchObject({ kind: "warn", pct: 80 });
    expect(deriveBudgetState(5, 4.99)).toMatchObject({ kind: "warn" });
  });

  it("returns exhausted at 100% and above", () => {
    expect(deriveBudgetState(5, 5.0)).toMatchObject({ kind: "exhausted", pct: 100 });
    expect(deriveBudgetState(5, 7.5)).toMatchObject({ kind: "exhausted", pct: 150 });
  });

  it("clamps a negative spent reading to 0", () => {
    expect(deriveBudgetState(5, -1)).toMatchObject({ kind: "running", spent: 0 });
  });
});

describe("budgetTone", () => {
  it("maps each state to the matching CSS class", () => {
    expect(budgetTone({ kind: "off", spent: 0 })).toBe("");
    expect(budgetTone({ kind: "running", cap: 5, spent: 0, pct: 0 })).toBe("");
    expect(budgetTone({ kind: "warn", cap: 5, spent: 4, pct: 80 })).toBe("warn");
    expect(budgetTone({ kind: "exhausted", cap: 5, spent: 5, pct: 100 })).toBe("err");
  });
});

describe("bumpSuggestions", () => {
  it("rounds small caps (<$1) to one decimal, snapping the 4× into the next bucket", () => {
    const r = bumpSuggestions(0.4);
    // 0.4 × 1.5 = 0.6 → 0.6, 0.4 × 2 = 0.8 → 0.8, 0.4 × 4 = 1.6 → snaps to half-dollar 2.
    expect(r).toEqual([0.6, 0.8, 2]);
  });

  it("rounds mid caps (<$10) to half-dollar", () => {
    const r = bumpSuggestions(5);
    expect(r).toEqual([7.5, 10, 20]);
  });

  it("rounds large caps to whole dollars", () => {
    const r = bumpSuggestions(25);
    expect(r).toEqual([38, 50, 100]);
  });

  it("rounds very large caps to nearest $5", () => {
    const r = bumpSuggestions(200);
    expect(r).toEqual([300, 400, 800]);
  });

  it("returns empty for non-positive cap", () => {
    expect(bumpSuggestions(0)).toEqual([]);
    expect(bumpSuggestions(-1)).toEqual([]);
  });
});

describe("QUICK_CAPS_USD", () => {
  it("ships sensible round dollar starting points", () => {
    expect(QUICK_CAPS_USD).toEqual([1, 5, 10, 25, 50]);
  });
});
