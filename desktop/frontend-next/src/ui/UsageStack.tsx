import { useState } from "react";
import { t } from "../i18n";
import { niceMax } from "./usagescale";

export interface StackPoint { key: string; axis: string; tip: string; hit: number; miss: number; total: number }

const W = 860, H = 176, PL = 58, PR = 12, PT = 12, PB = 24, GAP = 2;

/** One bar per day or hour: the input side split into tokens served from the
 *  cache and tokens that were not. The provider's own total rides in the
 *  tooltip, since it is a different count and shares no axis with the split. */
export function UsageStack({ points, label, aria, single }: { points: StackPoint[]; label: (v: number) => string; aria: string; single?: boolean }) {
  const [at, setAt] = useState<number | null>(null);
  const max = niceMax(Math.max(...points.map((p) => p.hit + p.miss), 0));
  const band = (W - PL - PR) / Math.max(points.length, 1);
  const bw = Math.max(2, Math.min(18, band * 0.62));
  const x = (i: number) => PL + band * i + (band - bw) / 2;
  const h = (v: number) => (v / max) * (H - PT - PB);
  const every = Math.max(1, Math.ceil(points.length / 5));
  const hover = at === null ? null : points[at];
  return (
    <div className="uplot ustack" data-on={hover ? "" : undefined}>
      {!single && <div className="ulegend" aria-hidden="true">
        <span><i data-s="hit" />{t("usage.input_cache_hit")}</span>
        <span><i data-s="miss" />{t("usage.input_cache_miss")}</span>
      </div>}
      <svg viewBox={`0 0 ${W} ${H}`} role="img" aria-label={aria}>
        <g className="ugrid">
          {[0, max / 2, max].map((v) => <line key={v} x1={PL} x2={W - PR} y1={H - PB - h(v)} y2={H - PB - h(v)} />)}
        </g>
        <g className="uaxis">
          {[0, max / 2, max].map((v) => <text key={v} x={PL - 8} y={H - PB - h(v) + 4} textAnchor="end">{label(v)}</text>)}
          {points.map((p, i) => (i % every ? null : <text key={p.key} x={x(i) + bw / 2} y={H - 7} textAnchor="middle">{p.axis}</text>))}
        </g>
        {points.map((p, i) => {
          const hit = h(p.hit), miss = h(p.miss);
          const gap = hit > 0 && miss > 0 ? GAP : 0;
          return (
            <g key={p.key} data-on={at === i ? "" : undefined}>
              {hit > 0 && <rect className="useg" data-s="hit" x={x(i)} y={H - PB - hit} width={bw} height={hit} rx={miss > 0 ? 0 : Math.min(4, bw / 2)} />}
              {miss > 0 && <rect className="useg" data-s="miss" x={x(i)} y={H - PB - hit - gap - miss} width={bw} height={miss} rx={Math.min(4, bw / 2)} />}
            </g>
          );
        })}
        <rect className="uhit" x={PL} y={0} width={W - PL - PR} height={H} data-action-pointermove="usage.chart-hover" data-action-pointerleave="usage.chart-hover"
          onPointerMove={(e) => {
            const box = e.currentTarget.ownerSVGElement!.getBoundingClientRect();
            const i = Math.min(points.length - 1, Math.max(0, Math.floor((((e.clientX - box.left) / box.width) * W - PL) / band)));
            if (i !== at) setAt(i);
          }}
          onPointerLeave={() => setAt(null)} />
      </svg>
      {hover && at !== null && (
        <div className="utip" style={{ left: `${((x(at) + bw / 2) / W) * 100}%`, top: `${((H - PB - h(hover.hit + hover.miss)) / H) * 100}%` }}>
          <div className="d">{hover.tip}</div>
          {single ? <div className="r"><b>{label(hover.hit)}</b></div> : (
            <>
              <div className="r"><i data-s="hit" />{t("usage.input_cache_hit")} <b>{label(hover.hit)}</b></div>
              <div className="r"><i data-s="miss" />{t("usage.input_cache_miss")} <b>{label(hover.miss)}</b></div>
              <div className="r">{t("usage.provider_reported_total")} <b>{label(hover.total)}</b></div>
            </>
          )}
        </div>
      )}
    </div>
  );
}
