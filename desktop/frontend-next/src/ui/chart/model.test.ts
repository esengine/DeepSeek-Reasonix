import { describe, expect, it } from "vitest";
import { logTicks, niceTicks, plotOf, slicesOf, stackOf, yScaleOf } from "./model";
import type { ChartSpec } from "./spec";

const grouped: ChartSpec = {
  spec_version: 1,
  title: "t",
  data: {
    columns: [{ name: "d", type: "date" }, { name: "g", type: "string" }, { name: "v", type: "number" }],
    rows: [["2024-01-01", "a", 1], ["2024-01-01", "b", 2], ["2024-02-01", "a", null], ["2024-02-01", "b", 4]],
  },
  marks: [{ type: "line", x: "d", y: ["v"], color: "g" }],
};

describe("plotOf", () => {
  it("splits one series per distinct color value and keeps first-seen order", () => {
    const p = plotOf(grouped, grouped.marks[0]);
    expect(p.categories).toEqual(["2024-01-01", "2024-02-01"]);
    expect(p.series.map((s) => s.label)).toEqual(["a", "b"]);
    expect(p.series[0].values).toEqual([1, null]);
    expect(p.series[1].values).toEqual([2, 4]);
  });

  it("gives dates and numbers a position and strings none", () => {
    expect(plotOf(grouped, grouped.marks[0]).positions).toEqual([Date.parse("2024-01-01"), Date.parse("2024-02-01")]);
    const s: ChartSpec = { ...grouped, data: { columns: [{ name: "d", type: "string" }, { name: "v", type: "number" }], rows: [["a", 1]] }, marks: [{ type: "bar", x: "d", y: ["v"] }] };
    expect(plotOf(s, s.marks[0]).positions).toBeNull();
  });

  it("keeps the last row of a repeated category", () => {
    const s: ChartSpec = { ...grouped, data: { columns: [{ name: "d", type: "string" }, { name: "v", type: "number" }], rows: [["a", 1], ["a", 9]] }, marks: [{ type: "bar", x: "d", y: ["v"] }] };
    expect(plotOf(s, s.marks[0]).series[0].values).toEqual([9]);
  });

  it("names a series by column and value when several columns split", () => {
    const m = { ...grouped.marks[0], y: ["v", "v"] };
    expect(plotOf(grouped, m).series.map((s) => s.label)).toContain("v / a");
  });
});

describe("slicesOf", () => {
  it("leaves out nulls and zeros", () => {
    const s: ChartSpec = { ...grouped, data: { columns: [{ name: "k", type: "string" }, { name: "v", type: "number" }], rows: [["a", 3], ["b", 0], ["c", null], ["d", 1]] }, marks: [{ type: "pie", x: "k", y: ["v"] }] };
    expect(slicesOf(s, s.marks[0])).toEqual([{ label: "a", value: 3 }, { label: "d", value: 1 }]);
  });
});

describe("scales", () => {
  it("picks round ticks that cover the data and reaches zero for bars", () => {
    const y = yScaleOf([13, 87], undefined, true);
    expect(y.ticks[0]).toBe(0);
    expect(y.ticks[y.ticks.length - 1]).toBeGreaterThanOrEqual(87);
    expect(y.at(0)).toBe(0);
    expect(y.at(y.hi)).toBe(1);
  });

  it("does not stretch a constant series into nothing", () => {
    const y = yScaleOf([5, 5], undefined, false);
    expect(y.hi).toBeGreaterThan(y.lo);
  });

  it("puts a log axis on powers of ten", () => {
    const y = yScaleOf([3, 4000], { scale: "log" }, true);
    expect(y.ticks).toEqual([1, 10, 100, 1000, 10000]);
    expect(y.at(10)).toBeCloseTo(0.25);
  });

  it("survives values near the bottom of the float range on a log axis", () => {
    for (const tiny of [5e-324, 1e-320, 1e-310]) {
      const y = yScaleOf([tiny, 10], { scale: "log" }, false);
      expect(y.lo).toBeGreaterThan(0);
      expect(y.ticks.length).toBeLessThan(700);
      expect(Number.isFinite(y.at(tiny))).toBe(true);
    }
    expect(logTicks(0, 10).length).toBeLessThan(700);
  });

  it("returns a tick for a degenerate range", () => {
    expect(niceTicks(2, 2)).toEqual([2]);
    expect(logTicks(5, 6).length).toBeGreaterThan(0);
  });
});

describe("stackOf", () => {
  it("stacks positives upward and negatives downward from zero", () => {
    const { lo, hi } = stackOf([{ label: "a", values: [2, -1] }, { label: "b", values: [3, -2] }]);
    expect([lo[1][0], hi[1][0]]).toEqual([2, 5]);
    expect([lo[1][1], hi[1][1]]).toEqual([-3, -1]);
  });
});
