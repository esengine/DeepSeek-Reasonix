import { describe, expect, it } from "vitest";
import { chartOfTool, readChartSpec } from "./spec";
import { SALES, chartCall, withSpec } from "./fixtures";

const rows = (n: number) => Array.from({ length: n }, (_, i) => [`c${i}`, i]);

describe("readChartSpec", () => {
  it("accepts a spec the kernel would accept", () => {
    expect(readChartSpec(structuredClone(SALES))?.title).toBe("Sales");
  });

  const bad: [string, unknown][] = [
    ["a future version", { ...SALES, spec_version: 2 }],
    ["markup-free but oversized title", withSpec({ title: "x".repeat(65) })],
    ["a control character in a label", withSpec({ title: `a${String.fromCharCode(7)}b` })],
    ["a bidi override in a label", withSpec({ title: `a${String.fromCharCode(0x202e)}b` })],
    ["an unknown mark", withSpec({ marks: [{ type: "radar", x: "month", y: ["revenue"] }] })],
    ["a mark naming a column nobody declared", withSpec({ marks: [{ type: "bar", x: "nope", y: ["revenue"] }] })],
    ["a non-numeric y column", withSpec({ marks: [{ type: "bar", x: "month", y: ["month"] }] })],
    ["a stacked line", withSpec({ marks: [{ type: "line", x: "month", y: ["revenue"], stacked: true }] })],
    ["a pie with two series", withSpec({ marks: [{ type: "pie", x: "month", y: ["revenue", "revenue"] }] })],
    ["a negative pie value", withSpec({ data: { ...SALES.data, rows: [["a", -1]] }, marks: [{ type: "pie", x: "month", y: ["revenue"] }] })],
    ["zero on a log axis", withSpec({ data: { ...SALES.data, rows: [["a", 0]] }, y_axis: { scale: "log" } })],
    ["duplicate column names", withSpec({ data: { columns: [{ name: "a", type: "string" }, { name: "a", type: "number" }], rows: [] } })],
    ["a row of the wrong width", withSpec({ data: { ...SALES.data, rows: [["a"]] } })],
    ["a string in a number column", withSpec({ data: { ...SALES.data, rows: [["a", "1"]] } })],
    ["an integer past 2^53", withSpec({ data: { ...SALES.data, rows: [["a", 2 ** 63]] } })],
    ["a non-finite number", withSpec({ data: { ...SALES.data, rows: [["a", Infinity]] } })],
    ["a nested cell", withSpec({ data: { ...SALES.data, rows: [["a", [1]]] } })],
    ["an unparseable date", withSpec({ data: { columns: [{ name: "d", type: "date" }, { name: "v", type: "number" }], rows: [["soon", 1]] }, marks: [{ type: "line", x: "d", y: ["v"] }] })],
    ["more rows than the cap", withSpec({ data: { ...SALES.data, rows: rows(5001) } })],
    ["more points than the cap", withSpec({ data: { ...SALES.data, rows: rows(2001) } })],
    ["more series than the cap", withSpec({ data: { columns: [{ name: "x", type: "string" }, { name: "g", type: "string" }, { name: "v", type: "number" }], rows: Array.from({ length: 13 }, (_, i) => ["a", `g${i}`, i]) }, marks: [{ type: "line", x: "x", y: ["v"], color: "g" }] })],
    ["no marks", withSpec({ marks: [] })],
    ["a bad axis format", withSpec({ x_axis: { format: "%d" } })],
  ];
  it.each(bad)("refuses %s", (_name, spec) => {
    expect(readChartSpec(spec)).toBeNull();
  });

  it("refuses non-objects and nesting without throwing", () => {
    for (const v of [null, 1, "x", [], { spec_version: 1, data: 5 }]) expect(readChartSpec(v)).toBeNull();
  });
});

describe("chartOfTool", () => {
  it("reads the spec out of a use_capability call", () => {
    expect(chartOfTool(chartCall(SALES))?.title).toBe("Sales");
  });

  it("reads a direct render_chart call too", () => {
    const tool = chartCall(SALES, { name: "render_chart", args: JSON.stringify(SALES), resolvedName: undefined });
    expect(chartOfTool(tool)?.title).toBe("Sales");
  });

  it("draws nothing for a call that is still running", () => {
    expect(chartOfTool(chartCall(SALES, { output: undefined }))).toBeNull();
  });

  it("draws nothing for a call the kernel refused", () => {
    expect(chartOfTool(chartCall(SALES, { err: "chart.schema_invalid", output: "chart.schema_invalid" }))).toBeNull();
  });

  it("draws nothing for a lookup of the tool rather than a call", () => {
    for (const action of ["search", "inspect"]) {
      const tool = chartCall(SALES, { args: JSON.stringify({ action, capability_id: "tool:render_chart", arguments: SALES }) });
      expect(chartOfTool(tool), action).toBeNull();
    }
  });

  it("draws nothing for other tools or malformed arguments", () => {
    expect(chartOfTool(chartCall(SALES, { resolvedName: "web_fetch" }))).toBeNull();
    expect(chartOfTool(chartCall(SALES, { args: "{not json" }))).toBeNull();
    expect(chartOfTool(chartCall({ ...SALES, extra: 1, marks: [] }))).toBeNull();
  });
});
