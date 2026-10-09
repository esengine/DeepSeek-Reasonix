import type { Tool } from "../../port/wire";

export const CHART_TOOL = "render_chart";

export const MAX_ROWS = 5000;
export const MAX_COLUMNS = 32;
export const MAX_SERIES = 12;
export const MAX_POINTS = 2000;
export const MAX_MARKS = 8;
export const MAX_LABEL = 64;

export type ColumnType = "number" | "string" | "date";
export type MarkType = "bar" | "line" | "pie";
export type Cell = number | string | null;

export interface ChartColumn {
  name: string;
  type: ColumnType;
}

export interface ChartMark {
  type: MarkType;
  x: string;
  y: string[];
  color?: string;
  stacked?: boolean;
  donut?: boolean;
}

export interface ChartAxis {
  title?: string;
  unit?: string;
  scale?: "linear" | "log";
  format?: "integer" | "decimal" | "percent";
}

export interface ChartData {
  columns: ChartColumn[];
  rows: Cell[][];
}

export interface ChartSpec {
  spec_version: 1;
  title: string;
  data: ChartData;
  marks: ChartMark[];
  x_axis?: ChartAxis;
  y_axis?: ChartAxis;
}

const isObject = (v: unknown): v is Record<string, unknown> => typeof v === "object" && v !== null && !Array.isArray(v);
const CONTROL = /[\p{Cc}\p{Cf}\p{Zl}\p{Zp}]/u;

function label(v: unknown, required = false): v is string {
  if (v === undefined) return !required;
  return typeof v === "string" && [...v].length <= MAX_LABEL && !CONTROL.test(v);
}

function axis(v: unknown): ChartAxis | undefined | false {
  if (v === undefined) return undefined;
  if (!isObject(v) || !label(v.title) || !label(v.unit)) return false;
  if (v.scale !== undefined && v.scale !== "linear" && v.scale !== "log") return false;
  if (v.format !== undefined && v.format !== "integer" && v.format !== "decimal" && v.format !== "percent") return false;
  return v as ChartAxis;
}

function column(c: unknown): c is ChartColumn {
  return isObject(c) && label(c.name, true) && c.name !== "" && (c.type === "number" || c.type === "string" || c.type === "date");
}

function cell(v: unknown, type: ColumnType): v is Cell {
  if (v === null) return true;
  if (type === "number") return typeof v === "number" && Number.isFinite(v) && Math.abs(v) <= 2 ** 53;
  if (typeof v !== "string" || !label(v, true)) return false;
  return type !== "date" || !Number.isNaN(Date.parse(v));
}

function mark(m: unknown, cols: Map<string, ColumnType>, rows: Cell[][], yScale: string | undefined): m is ChartMark {
  if (!isObject(m) || (m.type !== "bar" && m.type !== "line" && m.type !== "pie")) return false;
  if ((m.stacked && m.type !== "bar") || (m.donut && m.type !== "pie")) return false;
  if (typeof m.x !== "string" || !cols.has(m.x)) return false;
  if (!Array.isArray(m.y) || m.y.length === 0 || m.y.some((y) => typeof y !== "string" || cols.get(y) !== "number")) return false;
  if (m.type === "pie" && (m.y.length !== 1 || m.color !== undefined || yScale === "log")) return false;
  let series = m.y.length;
  if (m.color !== undefined) {
    if (typeof m.color !== "string" || cols.get(m.color) !== "string") return false;
    const at = [...cols.keys()].indexOf(m.color);
    series *= new Set(rows.map((r) => r[at]).filter((v) => typeof v === "string")).size;
  }
  if (series > MAX_SERIES || rows.length * m.y.length > MAX_POINTS) return false;
  const ys = (m.y as string[]).map((y) => [...cols.keys()].indexOf(y));
  return rows.every((r) => ys.every((i) => {
    const v = r[i];
    return typeof v !== "number" || !((m.type === "pie" && v < 0) || (yScale === "log" && v <= 0));
  }));
}

/** Re-checks a spec the way the kernel's validator does. A stored call is
 *  untrusted input, so a spec that fails here is never drawn. */
export function readChartSpec(raw: unknown): ChartSpec | null {
  if (!isObject(raw) || raw.spec_version !== 1 || !label(raw.title, true)) return null;
  const x = axis(raw.x_axis);
  const y = axis(raw.y_axis);
  if (x === false || y === false || !isObject(raw.data)) return null;
  const { columns, rows } = raw.data;
  if (!Array.isArray(columns) || columns.length === 0 || columns.length > MAX_COLUMNS || !columns.every(column)) return null;
  const types = new Map(columns.map((c) => [c.name, c.type] as const));
  if (types.size !== columns.length || !Array.isArray(rows) || rows.length > MAX_ROWS) return null;
  if (!rows.every((r) => Array.isArray(r) && r.length === columns.length && r.every((v, i) => cell(v, columns[i].type)))) return null;
  const marks = raw.marks;
  if (!Array.isArray(marks) || marks.length === 0 || marks.length > MAX_MARKS) return null;
  if (!marks.every((m) => mark(m, types, rows as Cell[][], y?.scale))) return null;
  return raw as unknown as ChartSpec;
}

/** The chart a tool call carries, when it is a settled render_chart. A call
 *  that failed, is still running, or only looked the tool up draws nothing. */
export function chartOfTool(tool: Tool): ChartSpec | null {
  if (tool.err || tool.output === undefined) return null;
  if (tool.name !== CHART_TOOL && tool.resolvedName !== CHART_TOOL) return null;
  try {
    const args = JSON.parse(tool.args ?? "");
    if (tool.name === CHART_TOOL) return readChartSpec(args);
    return isObject(args) && args.action === "call" ? readChartSpec(args.arguments) : null;
  } catch {
    return null;
  }
}
