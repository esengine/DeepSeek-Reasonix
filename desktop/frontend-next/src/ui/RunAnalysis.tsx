import { useLayoutEffect, useMemo, useRef, useState } from "react";
import { t } from "../i18n";
import { decimals, seconds } from "../i18n/format";
import type { TrajRow } from "../state/trajectory";
import type { TrajectoryAvailability } from "../port/wire";
import { categoryOf } from "./icons";
import { StudioIcon } from "./StudioIcon";
import { serialiseTrajectory, spanText, trajectoryFileName } from "./trajectory-export";

const SIGNAL_GAP = 22;

type Bucket = "model" | "tool" | "recovery" | "system";

const isRecovery = (row: TrajRow) =>
  row.kind === "protocol_recovery" && /retry|discard|fail|error|recover/i.test(spanText(row.payload));

const bucketOf = (row: TrajRow): Bucket =>
  row.kind === "model_round" ? "model" : isRecovery(row) ? "recovery" : row.tool ? "tool" : "system";

const labelOf = (row: TrajRow) => {
  if (row.kind === "model_round") return t("模型回合");
  if (row.tool) return row.tool;
  return spanText(row.payload) || row.kind;
};

const scrollParent = (el: HTMLElement): HTMLElement | null => {
  for (let p = el.parentElement; p; p = p.parentElement) {
    if (/auto|scroll/.test(getComputedStyle(p).overflowY) && p.scrollHeight > p.clientHeight) return p;
  }
  return null;
};

const percentile = (values: number[], p: number) => {
  if (!values.length) return 0;
  const sorted = [...values].sort((a, b) => a - b);
  return sorted[Math.min(sorted.length - 1, Math.ceil(sorted.length * p) - 1)];
};

function Inspect({ row, index }: { row: TrajRow; index: number }) {
  return (
    <section className="run-inspect" data-c={bucketOf(row)} data-inspect>
      <header><b>{index >= 0 ? `R${index + 1} · ` : ""}{labelOf(row)}</b><span>+{decimals(row.at, 2)}s</span></header>
      <div className="run-inspect-facts">
        <span><small>{t("类型")}</small><b>{row.kind}</b></span>
        <span><small>{t("耗时")}</small><b>{seconds((row.dur ?? 0) * 1000, 2)}</b></span>
        {row.tool && <span><small>{t("工具")}</small><b>{row.tool}</b></span>}
      </div>
      <p>{spanText(row.payload)}</p>
      {row.subs.map((s, i) => <p key={i}>{spanText(s)}</p>)}
    </section>
  );
}

/** A readable first stop for a run. Raw trajectory remains available for
 *  diagnosis, but this page answers the questions people ask before reading a
 *  protocol log: where time went, what overlapped, and which round is unusual. */
export function RunAnalysis({ rows, availability, onSave, onBack }: {
  rows: TrajRow[];
  availability?: TrajectoryAvailability;
  onSave: (name: string, content: string) => Promise<string | null>;
  onBack: () => void;
}) {
  const activities = useMemo(
    () => rows.filter((r) => (r.dur ?? 0) > 0 || isRecovery(r)),
    [rows],
  );
  const span = Math.max(0, ...rows.map((r) => r.at + (r.dur ?? 0)));
  const totals = useMemo(() => {
    const out: Record<Bucket, number> = { model: 0, tool: 0, recovery: 0, system: 0 };
    for (const row of activities) out[bucketOf(row)] += row.dur ?? 0;
    return out;
  }, [activities]);
  const activityTotal = Object.values(totals).reduce((n, v) => n + v, 0);
  const parallel = Math.max(0, activityTotal - span);
  const rounds = activities.filter((r) => r.kind === "model_round" && r.dur);
  const tools = activities.filter((r) => r.tool);
  const recoveries = rows.filter(isRecovery);
  const signals = rows.filter((r) => !r.dur && !r.tool && r.kind !== "event");
  const first = activities.length ? Math.min(...activities.map((r) => r.at)) : 0;
  const [pick, setPick] = useState<{ seq: number; from: "row" | "signal" } | null>(null);
  const [member, setMember] = useState<number | null>(null);
  const [zoom, setZoom] = useState(1);
  const [signalWidth, setSignalWidth] = useState(0);
  const timeline = useRef<HTMLDivElement>(null);
  const signalLine = useRef<HTMLDivElement>(null);
  // Marks closer than one hit target are one marker with a count: stacking them
  // in lanes turned a run's burst of simultaneous signals into a tall column.
  const clusters: Array<{ rows: TrajRow[]; left: number }> = [];
  let anchor = -Infinity;
  for (const row of [...signals].sort((a, b) => a.at - b.at)) {
    const left = row.at / Math.max(span, 1);
    const x = left * signalWidth;
    const last = clusters[clusters.length - 1];
    if (last && x - anchor < SIGNAL_GAP) last.rows.push(row);
    else {
      clusters.push({ rows: [row], left });
      anchor = x;
    }
  }
  useLayoutEffect(() => {
    const line = signalLine.current;
    if (!line) return;
    const measure = () => setSignalWidth(line.clientWidth);
    measure();
    const observer = new ResizeObserver(measure);
    observer.observe(line);
    return () => observer.disconnect();
  }, [signals.length > 0]);
  const [saved, setSaved] = useState<string | null>(null);
  const picked = rows.find((r) => r.seq === pick?.seq) ?? null;
  const selected = pick?.from === "row" ? pick.seq : null;
  const root = useRef<HTMLDivElement>(null);
  const clickedTop = useRef<{ el: HTMLElement; top: number } | null>(null);
  const toggle = (seq: number, from: "row" | "signal", el: HTMLElement) => {
    clickedTop.current = { el, top: el.getBoundingClientRect().top };
    setMember(null);
    setPick((cur) => (cur?.seq === seq && cur.from === from ? null : { seq, from }));
  };
  useLayoutEffect(() => {
    const was = clickedTop.current;
    clickedTop.current = null;
    const box = root.current;
    if (!was || !box) return;
    const scroller = scrollParent(box);
    if (was.el.isConnected && scroller) scroller.scrollTop += was.el.getBoundingClientRect().top - was.top;
    box.querySelector("[data-inspect]")?.scrollIntoView?.({ block: "nearest" });
  }, [pick]);

  if (rows.length === 0) {
    return (
      <div className="run-empty">
        <strong>{t("还没有可分析的运行")}</strong>
        <span>{t("发送任务后，这里会按真实运行记录显示模型、工具、重试与并行耗时。")}</span>
      </div>
    );
  }

  const buckets: Array<[Bucket, string]> = [
    ["model", t("模型")], ["tool", t("工具")], ["recovery", t("重试")], ["system", t("系统")],
  ];

  return (
    <div className="run-analysis" ref={root}>
      <header className="run-analysis-head">
        <span>
          <button className="run-export run-back" type="button" data-action="pane.view" data-value="flow" aria-label={t("返回正文")} title={t("返回正文")} onClick={onBack}>
            <StudioIcon name="chevron" />
          </button>
          <b>{t("运行分析")}</b>
          <small>{t("本轮 · 基于真实记录")}</small>
        </span>
        <span className="run-analysis-actions">
          {saved && <small title={saved}>{t("已导出")}</small>}
          <span className="run-live"><i />{t("{n} 条记录", { n: rows.length })}</span>
          <button
            className="run-export"
            onClick={() => {
              const name = trajectoryFileName();
              void onSave(name, serialiseTrajectory(rows, availability)).then((to) => setSaved(to ?? name));
            }}
          >
            {t("导出运行记录")}
          </button>
        </span>
      </header>

      <section className="run-kpis" aria-label={t("运行摘要")}>
        <div><small>{t("总时长")}</small><strong>{seconds(span * 1000, span < 10 ? 1 : 0)}</strong><em>span</em></div>
        <div><small>{t("模型 P95")}</small><strong>{rounds.length ? seconds(percentile(rounds.map((r) => r.dur ?? 0), .95) * 1000, 1) : "—"}</strong><em>{t("{n} 个回合", { n: rounds.length })}</em></div>
        <div><small>{t("工具耗时")}</small><strong>{seconds(totals.tool * 1000, totals.tool < 10 ? 1 : 0)}</strong><em>{t("{n} 次调用", { n: tools.length })}</em></div>
        <div><small>{t("并行节省")}</small><strong>{seconds(parallel * 1000, parallel < 10 ? 1 : 0)}</strong><em>{t("累计活动减墙钟")}</em></div>
        <div><small>{t("重试")}</small><strong>{recoveries.length}</strong><em>{recoveries.length ? t("需要关注") : t("无异常")}</em></div>
        <div><small>{t("首项活动")}</small><strong>{seconds(first * 1000, 1)}</strong><em>{t("相对本轮开始")}</em></div>
      </section>

      <section className="run-budget">
        <div className="run-section-head"><b>{t("时间分布")}</b><span>{t("累计活动 {time}", { time: seconds(activityTotal * 1000, 1) })}</span></div>
        <div className="run-budget-bar" aria-label={t("各类活动耗时占比")}>
          {buckets.map(([key, label]) => totals[key] > 0 && (
            <i key={key} data-c={key} style={{ width: `${(totals[key] / Math.max(activityTotal, .001)) * 100}%` }} title={`${label} ${seconds(totals[key] * 1000, 1)}`}>
              <span>{label} {seconds(totals[key] * 1000, 1)}</span>
            </i>
          ))}
        </div>
        <div className="run-legend">
          {buckets.map(([key, label]) => <span key={key}><i data-c={key} />{label}</span>)}
        </div>
      </section>

      <div className="run-timeline-controls">
        <label>{t("时间轴缩放")}<input type="range" min="1" max="16" step="1" value={zoom}
          aria-label={t("时间轴缩放")} aria-valuetext={`${zoom}×`} onChange={(e) => setZoom(Number(e.target.value))} /></label>
        <output>{zoom}×</output>
        <button className="run-export" onClick={() => { setZoom(1); if (timeline.current) timeline.current.scrollLeft = 0; }}>{t("重置缩放")}</button>
      </div>
      <div className="run-timeline-scroll" ref={timeline} role="region" aria-label={t("时间轴")} tabIndex={0}>
        <div className="run-timeline-canvas" style={{ width: `${zoom * 100}%` }}>
      {signals.length > 0 && (
        <section className="run-signals">
          <div className="run-section-head"><b>{t("信号轨")}</b><span>{t("与下方共用时间轴")}</span></div>
          <p className="run-section-note">{t("没有时长、也不是工具调用的运行记录，位置就是时间，无法拖动；缩放后可横向滚动，挨得太近的信号合并成一个带数量的标记。点一个看它的记录。")}</p>
          <div className="run-signal-track"><div className="run-signal-line" ref={signalLine}>
            {clusters.map(({ rows: group, left }) => {
              const row = group[0];
              const open = pick?.from === "signal" && pick.seq === row.seq;
              return (
                <button data-action="analysis.signal" data-target={String(row.seq)} key={row.seq} style={{ left: `${left * 100}%` }} onClick={(e) => toggle(row.seq, "signal", e.currentTarget)}
                  title={group.length > 1 ? t("{n} 个信号", { n: group.length }) : spanText(row.payload)}
                  aria-label={group.length > 1 ? `${t("{n} 个信号", { n: group.length })} · +${decimals(row.at, 2)}s` : `${labelOf(row)} · +${decimals(row.at, 2)}s`} aria-expanded={open}>
                  {group.length > 1 ? <b data-c={bucketOf(row)}>{group.length}</b> : <i data-c={bucketOf(row)} />}
                </button>
              );
            })}
          </div></div>
          {picked && pick?.from === "signal" && (() => {
            const group = clusters.find((c) => c.rows[0].seq === picked.seq)?.rows ?? [picked];
            if (group.length === 1) return <Inspect row={picked} index={activities.findIndex((r) => r.seq === picked.seq)} />;
            const chosen = group.find((r) => r.seq === member) ?? null;
            return (
              <div className="run-signal-group">
                <ul>
                  {group.map((r) => (
                    <li key={r.seq}>
                      <button data-action="analysis.signal" data-target={String(r.seq)} onClick={() => setMember((cur) => (cur === r.seq ? null : r.seq))} aria-expanded={chosen?.seq === r.seq}>
                        <i data-c={bucketOf(r)} /><span>{labelOf(r)}</span><small>+{decimals(r.at, 2)}s</small>
                      </button>
                    </li>
                  ))}
                </ul>
                {chosen && <Inspect row={chosen} index={activities.findIndex((r) => r.seq === chosen.seq)} />}
              </div>
            );
          })()}
        </section>
      )}

      <section className="run-rounds">
        <div className="run-section-head"><b>{t("活动回合")}</b><span>{t("点击一行查看记录")}</span></div>
        <ol>
          {activities.map((row, index) => {
            const start = (row.at / Math.max(span, 1)) * 100;
            const width = ((row.dur ?? 0) / Math.max(span, 1)) * 100;
            const bucket = bucketOf(row);
            const cat = row.tool ? categoryOf(row.tool) : bucket;
            return (
              <li key={row.seq} data-on={selected === row.seq ? "" : undefined}>
                <button data-action="analysis.row" data-target={String(row.seq)} onClick={(e) => toggle(row.seq, "row", e.currentTarget)} aria-expanded={selected === row.seq}>
                  <span className="run-round-id">R{index + 1}</span>
                  <span className="run-round-track"><i data-c={cat} style={{ left: `${start}%`, width: `${width}%` }} /></span>
                  <span className="run-round-copy"><b>{labelOf(row)}</b><small>{seconds((row.dur ?? 0) * 1000, 1)}</small></span>
                  <span className="run-round-kind" data-c={bucket}>{row.kind.replaceAll("_", " ")}</span>
                </button>
                {selected === row.seq && <Inspect row={row} index={index} />}
              </li>
            );
          })}
        </ol>
      </section>
        </div>
      </div>
    </div>
  );
}
