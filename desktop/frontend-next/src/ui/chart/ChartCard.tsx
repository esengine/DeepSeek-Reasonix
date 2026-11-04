import { memo, useCallback, useEffect, useId, useRef, useState, type ReactNode } from "react";
import { t } from "../../i18n";
import { Sym } from "../Sym";
import { plotOf } from "./model";
import { ChartTable } from "./ChartTable";
import { BarPlot, LinePlot, PiePlot } from "./plots";
import { clip } from "./format";
import type { ChartMark, ChartSpec } from "./spec";

function describe(spec: ChartSpec): string {
  const kinds = spec.marks.map((m) => (m.type === "bar" ? t("chat.bar_chart") : m.type === "line" ? t("chat.line_chart") : t("chat.pie_chart")));
  return t("chat.kind_title_n_rows", { kind: [...new Set(kinds)].join(" · "), title: spec.title, n: spec.data.rows.length });
}

function axes(spec: ChartSpec): string {
  const one = (a?: { title?: string; unit?: string }) => [a?.title, a?.unit && `(${a.unit})`].filter(Boolean).join(" ");
  return [one(spec.x_axis), one(spec.y_axis)].filter(Boolean).join(" × ");
}

function Legend({ spec, mark }: { spec: ChartSpec; mark: ChartMark }) {
  if (mark.type === "pie") return null;
  const names = plotOf(spec, mark).series.map((s) => s.label);
  if (names.length < 2) return null;
  return (
    <ul className="chart-key">
      {names.map((n, i) => (
        <li key={n}>
          <i className={`cs${i % 5} sv${Math.min(Math.floor(i / 5), 2)}`} />
          <span className="nm">{clip(n, 28)}</span>
        </li>
      ))}
    </ul>
  );
}

const Mark = memo(function Mark({ spec, mark, hot, hatch }: { spec: ChartSpec; mark: ChartMark; hot: (s: string | null) => void; hatch: string }) {
  const frame = { spec, mark, hot, hatch };
  return mark.type === "bar" ? <BarPlot {...frame} /> : mark.type === "line" ? <LinePlot {...frame} /> : <PiePlot {...frame} />;
});

function Scroller({ scrolls, label, children }: { scrolls: boolean; label: string; children: ReactNode }) {
  const box = useRef<HTMLDivElement>(null);
  const [more, setMore] = useState({ left: false, right: false });
  const measure = useCallback(() => {
    const el = box.current;
    if (!el) return;
    const next = { left: el.scrollLeft > 1, right: el.scrollLeft + el.clientWidth < el.scrollWidth - 1 };
    setMore((m) => (m.left === next.left && m.right === next.right ? m : next));
  }, []);
  useEffect(() => {
    measure();
    const el = box.current;
    if (!el || !scrolls) return;
    const watch = new ResizeObserver(measure);
    watch.observe(el);
    return () => watch.disconnect();
  }, [measure, scrolls]);
  return (
    <div className="chart-scroll" data-more-left={more.left || undefined} data-more-right={more.right || undefined}>
      <div
        className="chart-pane"
        ref={box}
        onScroll={measure}
        data-action={scrolls ? "chart.scroll" : undefined}
        tabIndex={scrolls ? 0 : undefined}
        role={scrolls ? "region" : undefined}
        aria-label={scrolls ? label : undefined}
      >
        {children}
      </div>
    </div>
  );
}

export function ChartCard({ spec, callId }: { spec: ChartSpec; callId?: string }) {
  const uid = useId();
  const [read, setRead] = useState<string | null>(null);
  const hatch = `${uid}-hatch`;
  return (
    <div className="call" data-k="chart" data-call={callId || undefined}>
      <div className="g">
        <Sym glyph="⌗" />
      </div>
      <div className="c">
        <div className="hl">
          <span className="nm">{t("chat.chart")}</span>
          <span className="arg" title={spec.title}>{spec.title}</span>
        </div>
        <figure className="chart" role="group" aria-label={describe(spec)}>
          <svg width="0" height="0" aria-hidden="true" className="chart-defs">
            <defs>
              <pattern id={hatch} width="6" height="6" patternUnits="userSpaceOnUse" patternTransform="rotate(45)">
                <line x1="0" y1="0" x2="0" y2="6" className="hatch-line" />
              </pattern>
            </defs>
          </svg>
          {spec.marks.map((m, i) => (
            <div className="chart-mark" key={i}>
              <Scroller scrolls={m.type !== "pie"} label={t("chat.chart_title_scrolls_sideways", { title: spec.title })}>
                <Mark spec={spec} mark={m} hot={setRead} hatch={hatch} />
              </Scroller>
              <Legend spec={spec} mark={m} />
            </div>
          ))}
          {axes(spec) && <figcaption className="chart-axes">{axes(spec)}</figcaption>}
          <div className="chart-read" aria-live="polite">{read ?? t("chat.hover_or_focus_the_chart")}</div>
        </figure>
        <ChartTable spec={spec} id={`${uid}-data`} />
      </div>
    </div>
  );
}
