import type { Tool } from "../../port/wire";
import type { ChartSpec } from "./spec";

export const SALES: ChartSpec = {
  spec_version: 1,
  title: "Sales",
  data: {
    columns: [{ name: "month", type: "string" }, { name: "revenue", type: "number" }],
    rows: [["Jan", 10], ["Feb", 30], ["Mar", 20]],
  },
  marks: [{ type: "bar", x: "month", y: ["revenue"] }],
};

export const withSpec = (over: Record<string, unknown>) => ({ ...SALES, ...over }) as unknown as ChartSpec;

export function chartCall(spec: unknown, over: Partial<Tool> = {}): Tool {
  return {
    id: "call-1",
    name: "use_capability",
    args: JSON.stringify({ action: "call", capability_id: "tool:render_chart", arguments: spec }),
    resolvedName: "render_chart",
    capabilityId: "tool:render_chart",
    output: "chart_id: chart-abc\nchart \"Sales\", 3 rows, 2 columns",
    readOnly: true,
    ...over,
  } as Tool;
}
