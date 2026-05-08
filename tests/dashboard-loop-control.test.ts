import { describe, expect, it } from "vitest";
import {
  INTERVAL_PRESETS_MS,
  formatRemaining,
  parseCustomInterval,
} from "../dashboard/src/lib/loop-control.js";

describe("INTERVAL_PRESETS_MS", () => {
  it("covers the 5s..6h window with sensible labels", () => {
    const labels = INTERVAL_PRESETS_MS.map((p) => p.label);
    expect(labels).toEqual(["30s", "1m", "5m", "15m", "1h", "6h"]);
    for (const p of INTERVAL_PRESETS_MS) {
      expect(p.ms).toBeGreaterThanOrEqual(5_000);
      expect(p.ms).toBeLessThanOrEqual(6 * 60 * 60_000);
    }
  });
});

describe("parseCustomInterval", () => {
  it("converts seconds / minutes / hours to ms", () => {
    expect(parseCustomInterval("30", "s")).toBe(30_000);
    expect(parseCustomInterval("5", "m")).toBe(300_000);
    expect(parseCustomInterval("2", "h")).toBe(7_200_000);
  });

  it("accepts decimals", () => {
    expect(parseCustomInterval("1.5", "m")).toBe(90_000);
  });

  it("rejects non-positive and non-numeric values", () => {
    expect(parseCustomInterval("0", "s")).toBeNull();
    expect(parseCustomInterval("-1", "s")).toBeNull();
    expect(parseCustomInterval("", "s")).toBeNull();
    expect(parseCustomInterval("abc", "s")).toBeNull();
  });

  it("rejects under 5s", () => {
    expect(parseCustomInterval("4", "s")).toBeNull();
    expect(parseCustomInterval("5", "s")).toBe(5_000);
  });

  it("rejects over 6h", () => {
    expect(parseCustomInterval("7", "h")).toBeNull();
    expect(parseCustomInterval("6", "h")).toBe(6 * 60 * 60_000);
  });
});

describe("formatRemaining", () => {
  it("renders sub-minute as seconds", () => {
    expect(formatRemaining(0)).toBe("0s");
    expect(formatRemaining(12_000)).toBe("12s");
    expect(formatRemaining(59_999)).toBe("59s");
  });

  it("renders sub-hour as `Xm Ys` (drops 0s)", () => {
    expect(formatRemaining(60_000)).toBe("1m");
    expect(formatRemaining(72_000)).toBe("1m 12s");
    expect(formatRemaining(5 * 60_000)).toBe("5m");
  });

  it("renders multi-hour as `Xh Ym` (drops 0m, drops seconds)", () => {
    expect(formatRemaining(60 * 60_000)).toBe("1h");
    expect(formatRemaining(2 * 60 * 60_000 + 45 * 60_000)).toBe("2h 45m");
    expect(formatRemaining(3 * 60 * 60_000 + 30 * 60_000 + 15_000)).toBe("3h 30m");
  });

  it("clamps negatives to 0s (countdown can race past zero between polls)", () => {
    expect(formatRemaining(-500)).toBe("0s");
    expect(formatRemaining(-99_999)).toBe("0s");
  });
});
