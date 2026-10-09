import type { Cell, ChartAxis, ChartMark, ChartSpec } from "./spec";

export interface Series {
  label: string;
  /** One value per category; null is a gap. */
  values: (number | null)[];
}

export interface Plot {
  categories: string[];
  /** Numeric or time position of each category, when the x column has one. */
  positions: number[] | null;
  series: Series[];
}

const colIndex = (spec: ChartSpec, name: string) => spec.data.columns.findIndex((c) => c.name === name);
const text = (c: Cell) => (c === null ? "" : String(c));

function position(spec: ChartSpec, x: number, c: Cell): number | null {
  if (c === null) return null;
  const type = spec.data.columns[x].type;
  if (type === "number") return typeof c === "number" ? c : null;
  if (type === "date") return Date.parse(String(c));
  return null;
}

/** Folds the rows into series over categories in first-seen order. A repeated
 *  category/series pair keeps its last row: v1 specs carry no aggregation. */
export function plotOf(spec: ChartSpec, mark: ChartMark): Plot {
  const x = colIndex(spec, mark.x);
  const color = mark.color ? colIndex(spec, mark.color) : -1;
  const cats = new Map<string, number>();
  const keys = new Map<string, Series>();
  const pos: number[] = [];
  let numeric = spec.data.columns[x].type !== "string";
  for (const row of spec.data.rows) {
    const cat = text(row[x]);
    if (!cats.has(cat)) {
      cats.set(cat, cats.size);
      const p = position(spec, x, row[x]);
      if (p === null || Number.isNaN(p)) numeric = false;
      else pos.push(p);
    }
  }
  const at = (cat: string) => cats.get(cat) as number;
  for (const row of spec.data.rows) {
    for (const y of mark.y) {
      const yi = colIndex(spec, y);
      const name = color >= 0 ? (mark.y.length > 1 ? `${y} / ${text(row[color])}` : text(row[color])) : y;
      let s = keys.get(name);
      if (!s) keys.set(name, (s = { label: name, values: Array<number | null>(cats.size).fill(null) }));
      const v = row[yi];
      const i = at(text(row[x]));
      while (s.values.length <= i) s.values.push(null);
      s.values[i] = typeof v === "number" ? v : null;
    }
  }
  const series = [...keys.values()];
  for (const s of series) while (s.values.length < cats.size) s.values.push(null);
  return { categories: [...cats.keys()], positions: numeric && pos.length === cats.size ? pos : null, series };
}

export interface Slice { label: string; value: number }

/** Pie slices are the positive rows; nulls and zeros take no angle. */
export function slicesOf(spec: ChartSpec, mark: ChartMark): Slice[] {
  const p = plotOf(spec, mark);
  const values = p.series[0]?.values ?? [];
  return p.categories.flatMap((label, i) => (values[i] && values[i]! > 0 ? [{ label, value: values[i]! }] : []));
}

export function niceTicks(lo: number, hi: number, want = 5): number[] {
  if (!(hi > lo)) return [lo];
  const raw = (hi - lo) / Math.max(1, want);
  const mag = 10 ** Math.floor(Math.log10(raw));
  const step = [1, 2, 5, 10].map((m) => m * mag).find((s) => s >= raw) as number;
  const out: number[] = [];
  const first = Math.floor(lo / step + 1e-9);
  const last = Math.ceil(hi / step - 1e-9);
  for (let k = first; k <= last; k++) out.push(k * step);
  return out;
}

const LOG_FLOOR = 1e-300;

export function logTicks(lo: number, hi: number): number[] {
  const out: number[] = [];
  const first = Math.ceil(Math.log10(Math.max(lo, LOG_FLOOR)));
  const last = Math.floor(Math.log10(Math.max(hi, LOG_FLOOR)) + 1e-9);
  for (let e = first; e <= last; e++) out.push(10 ** e);
  return out.length ? out : [lo, hi];
}

export interface YScale {
  lo: number;
  hi: number;
  log: boolean;
  ticks: number[];
  at: (v: number) => number;
}

/** Maps a value to 0 (bottom) .. 1 (top). Bars pass zero so their base is
 *  always drawn; a log scale cannot hold zero and starts at its smallest value. */
export function yScaleOf(values: number[], axis: ChartAxis | undefined, includeZero: boolean): YScale {
  const log = axis?.scale === "log";
  let lo = Math.min(...values);
  let hi = Math.max(...values);
  if (!Number.isFinite(lo)) { lo = 0; hi = 1; }
  if (log) {
    lo = Math.max(lo, LOG_FLOOR);
    hi = Math.max(hi, lo);
    lo = 10 ** Math.floor(Math.log10(lo));
    hi = 10 ** Math.ceil(Math.log10(hi));
    if (hi <= lo) hi = lo * 10;
    const span = Math.log10(hi / lo);
    return { lo, hi, log, ticks: logTicks(lo, hi), at: (v) => Math.log10(Math.max(v, lo) / lo) / span };
  }
  if (includeZero) { lo = Math.min(lo, 0); hi = Math.max(hi, 0); }
  if (hi === lo) hi = lo + 1;
  const ticks = niceTicks(lo, hi);
  lo = Math.min(lo, ticks[0]);
  hi = Math.max(hi, ticks[ticks.length - 1]);
  return { lo, hi, log, ticks, at: (v) => (v - lo) / (hi - lo) };
}

/** Stacked bars: positives climb from zero, negatives descend from it. */
export function stackOf(series: Series[]): { lo: number[][]; hi: number[][] } {
  const n = series[0]?.values.length ?? 0;
  const lo = series.map(() => Array<number>(n).fill(0));
  const hi = series.map(() => Array<number>(n).fill(0));
  for (let i = 0; i < n; i++) {
    let up = 0;
    let down = 0;
    series.forEach((s, k) => {
      const v = s.values[i] ?? 0;
      if (v >= 0) { lo[k][i] = up; up += v; hi[k][i] = up; }
      else { hi[k][i] = down; down += v; lo[k][i] = down; }
    });
  }
  return { lo, hi };
}
