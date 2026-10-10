import type { ChartMark, ChartSpec } from "./spec";
import { plotOf, slicesOf, stackOf, yScaleOf, type Plot, type YScale } from "./model";
import { clip, formatValue } from "./format";
import { t } from "../../i18n";
import { count, pct } from "../../i18n/format";

export const W = 640;
export const H = 280;
const TOP = 14;
const BOTTOM = 30;
const RIGHT = 14;
const FOCUSABLE_MAX = 60;

export type Hot = (text: string | null) => void;

interface Frame {
  spec: ChartSpec;
  mark: ChartMark;
  hot: Hot;
  hatch: string;
}

const seriesClass = (i: number) => `cs${i % 5} sv${Math.min(Math.floor(i / 5), 2)}`;

function pointProps(label: string, hot: Hot, focusable: boolean) {
  return {
    tabIndex: focusable ? 0 : undefined,
    "aria-label": label,
    role: "img" as const,
    onMouseEnter: () => hot(label),
    onMouseLeave: () => hot(null),
    onFocus: () => hot(label),
    onBlur: () => hot(null),
  };
}

function Axes({ y, left, spec }: { y: YScale; left: number; spec: ChartSpec }) {
  const base = H - BOTTOM;
  const at = (v: number) => base - y.at(v) * (base - TOP);
  return (
    <g className="ax" aria-hidden="true">
      {y.ticks.map((v) => (
        <g key={v}>
          <line className="grid" x1={left} x2={W - RIGHT} y1={at(v)} y2={at(v)} />
          <text className="tick" x={left - 6} y={at(v)} textAnchor="end" dominantBaseline="middle">{formatValue(v, { ...spec.y_axis, unit: undefined })}</text>
        </g>
      ))}
      <line className="base" x1={left} x2={W - RIGHT} y1={base} y2={base} />
    </g>
  );
}

function leftMargin(y: YScale, spec: ChartSpec): number {
  const widest = Math.max(...y.ticks.map((v) => formatValue(v, { ...spec.y_axis, unit: undefined }).length));
  return Math.min(120, 14 + widest * 6.6);
}

function XLabels({ cats, xs, left }: { cats: string[]; xs: number[]; left: number }) {
  const room = (W - RIGHT - left) / Math.max(1, cats.length);
  const widest = Math.min(14, Math.max(...cats.map((c) => [...c].length)));
  const every = Math.max(1, Math.ceil((widest * 6.4 + 10) / room));
  return (
    <g className="xl" aria-hidden="true">
      {cats.map((c, i) => (i % every === 0 ? (
        <text key={i} className="tick" x={xs[i]} y={H - BOTTOM + 16} textAnchor="middle">{clip(c, 14)}</text>
      ) : null))}
    </g>
  );
}

export function BarPlot({ spec, mark, hot, hatch }: Frame) {
  const p = plotOf(spec, mark);
  const stack = mark.stacked ? stackOf(p.series) : null;
  const all = stack ? [...stack.lo.flat(), ...stack.hi.flat()] : p.series.flatMap((s) => s.values.filter((v): v is number => v !== null));
  const y = yScaleOf(all, spec.y_axis, true);
  const left = leftMargin(y, spec);
  const base = H - BOTTOM;
  const band = (W - RIGHT - left) / Math.max(1, p.categories.length);
  const slot = stack ? band * 0.7 : (band * 0.76) / Math.max(1, p.series.length);
  const at = (v: number) => base - y.at(v) * (base - TOP);
  const zero = y.log ? base : at(0);
  const focusable = p.categories.length * p.series.length <= FOCUSABLE_MAX;
  const xs = p.categories.map((_, i) => left + band * (i + 0.5));
  return (
    <svg viewBox={`0 0 ${W} ${H}`} className="chart-svg" style={{ minWidth: W }}>
      <Axes y={y} left={left} spec={spec} />
      {p.series.map((s, k) => s.values.map((v, i) => {
        if (v === null) return null;
        const a = stack ? at(stack.hi[k][i]) : at(v);
        const b = stack ? at(stack.lo[k][i]) : zero;
        const x = stack ? xs[i] - slot / 2 : left + band * i + band * 0.12 + slot * k;
        const label = `${s.label} · ${p.categories[i]} · ${formatValue(v, spec.y_axis)}`;
        return (
          <g key={`${k}:${i}`} className={`bar ${seriesClass(k)}`} {...pointProps(label, hot, focusable)}>
            <title>{label}</title>
            <rect x={x} y={Math.min(a, b)} width={Math.max(1, slot - 1)} height={Math.max(1, Math.abs(b - a))} />
            {k >= 5 && <rect className="hatch" x={x} y={Math.min(a, b)} width={Math.max(1, slot - 1)} height={Math.max(1, Math.abs(b - a))} fill={`url(#${hatch})`} />}
          </g>
        );
      }))}
      <XLabels cats={p.categories} xs={xs} left={left} />
    </svg>
  );
}

function xPositions(p: Plot, left: number): number[] {
  const right = W - RIGHT;
  if (!p.positions) return p.categories.map((_, i) => (p.categories.length === 1 ? (left + right) / 2 : left + 12 + ((right - left - 24) * i) / (p.categories.length - 1)));
  const lo = Math.min(...p.positions);
  const hi = Math.max(...p.positions);
  return p.positions.map((v) => (hi === lo ? (left + right) / 2 : left + 12 + ((v - lo) / (hi - lo)) * (right - left - 24)));
}

export function LinePlot({ spec, mark, hot }: Frame) {
  const p = plotOf(spec, mark);
  const y = yScaleOf(p.series.flatMap((s) => s.values.filter((v): v is number => v !== null)), spec.y_axis, false);
  const left = leftMargin(y, spec);
  const base = H - BOTTOM;
  const at = (v: number) => base - y.at(v) * (base - TOP);
  const xs = xPositions(p, left);
  const order = p.positions ? p.categories.map((_, i) => i).sort((a, b) => p.positions![a] - p.positions![b]) : p.categories.map((_, i) => i);
  const dots = p.categories.length * p.series.length <= FOCUSABLE_MAX;
  return (
    <svg viewBox={`0 0 ${W} ${H}`} className="chart-svg" style={{ minWidth: W }}>
      <Axes y={y} left={left} spec={spec} />
      {p.series.map((s, k) => {
        const runs: number[][] = [];
        let run: number[] = [];
        for (const i of order) {
          const v = s.values[i];
          if (v === null) { if (run.length) runs.push(run); run = []; continue; }
          run.push(i);
        }
        if (run.length) runs.push(run);
        return (
          <g key={k} className={`line ${seriesClass(k)}`}>
            {runs.map((indices, r) => {
              const d = indices.map((i, j) => `${j ? "L" : "M"}${xs[i].toFixed(1)} ${at(s.values[i]!).toFixed(1)}`).join("");
              const hit = indices.length === 1 ? `${d}L${xs[indices[0]].toFixed(1)} ${at(s.values[indices[0]]!).toFixed(1)}` : d;
              return (
                <g key={r}>
                  <path d={d} />
                  <path className="line-hit" d={hit} aria-hidden="true" onMouseMove={(e) => {
                    const rect = e.currentTarget.ownerSVGElement!.getBoundingClientRect();
                    if (!rect.width) return;
                    const x = ((e.clientX - rect.left) / rect.width) * W;
                    const i = indices.reduce((best, next) => Math.abs(xs[next] - x) < Math.abs(xs[best] - x) ? next : best);
                    hot(`${s.label} · ${p.categories[i]} · ${formatValue(s.values[i]!, spec.y_axis)}`);
                  }} onMouseLeave={() => hot(null)}>
                    <title>{s.label}</title>
                  </path>
                </g>
              );
            })}
            {s.values.map((v, i) => {
              if (v === null) return null;
              const label = `${s.label} · ${p.categories[i]} · ${formatValue(v, spec.y_axis)}`;
              return dots
                ? <circle key={i} className="pt" cx={xs[i]} cy={at(v)} r={3.4} {...pointProps(label, hot, true)}><title>{label}</title></circle>
                : null;
            })}
          </g>
        );
      })}
      <XLabels cats={order.map((i) => p.categories[i])} xs={order.map((i) => xs[i])} left={left} />
    </svg>
  );
}

const TAU = Math.PI * 2;

function arc(cx: number, cy: number, r: number, inner: number, a0: number, a1: number): string {
  const pt = (rad: number, a: number) => `${(cx + rad * Math.sin(a)).toFixed(2)} ${(cy - rad * Math.cos(a)).toFixed(2)}`;
  if (a1 - a0 >= TAU - 1e-6) {
    const ring = (rad: number) => `M${cx} ${cy - rad}A${rad} ${rad} 0 1 1 ${cx} ${cy + rad}A${rad} ${rad} 0 1 1 ${cx} ${cy - rad}`;
    return inner ? `${ring(r)}${ring(inner)}` : ring(r);
  }
  const big = a1 - a0 > Math.PI ? 1 : 0;
  const outer = `M${pt(r, a0)}A${r} ${r} 0 ${big} 1 ${pt(r, a1)}`;
  return inner ? `${outer}L${pt(inner, a1)}A${inner} ${inner} 0 ${big} 0 ${pt(inner, a0)}Z` : `${outer}L${cx} ${cy}Z`;
}

export function PiePlot({ spec, mark, hot }: Frame) {
  const slices = slicesOf(spec, mark);
  const total = slices.reduce((n, s) => n + s.value, 0);
  if (slices.length === 0) return <div className="chart-empty">{t("没有可画的数据")}</div>;
  const r = 104;
  let angle = 0;
  return (
    <div className="chart-pie">
      <svg viewBox={`0 0 ${W / 2} ${H}`} className="chart-svg pie">
        {slices.map((s, i) => {
          const a0 = angle;
          angle += (s.value / total) * TAU;
          const label = `${s.label} · ${formatValue(s.value, spec.y_axis)} · ${pct(s.value / total, 1)}`;
          return (
            <path key={i} className={`slice ${seriesClass(i)}`} d={arc(W / 4, H / 2, r, mark.donut ? r * 0.58 : 0, a0, angle)} fillRule="evenodd" {...pointProps(label, hot, slices.length <= FOCUSABLE_MAX)}><title>{label}</title></path>
          );
        })}
      </svg>
      <ul className="chart-key">
        {slices.slice(0, 12).map((s, i) => (
          <li key={i}>
            <i className={seriesClass(i)} />
            <span className="nm">{clip(s.label, 24)}</span>
            <span className="v">{pct(s.value / total, 1)}</span>
          </li>
        ))}
        {slices.length > 12 && <li className="more">{`+${count(slices.length - 12)}`}</li>}
      </ul>
    </div>
  );
}
