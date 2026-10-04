import { useEffect, useMemo, useState, type CSSProperties } from "react";
import { t } from "../i18n";
import { count, money, tokens as fmtTokens } from "../i18n/format";
import { reason } from "../i18n/kernel";
import { DEFAULT_USAGE_DAYS, type AgentPort, type Money, type UsageDay, type UsageQuery, type UsageReport } from "../port/port";
import { hostOf } from "./vendors";

const RANGES: [number, string][] = [[7, "7 天"], [30, "30 天"], [365, "全部"]];

type UsageWindow =
  | { kind: "days"; days: number }
  | { kind: "custom"; from: string; to: string }
  | { kind: "month"; month: string };

function currentMonth(): string {
  const now = new Date();
  return `${now.getFullYear()}-${String(now.getMonth() + 1).padStart(2, "0")}`;
}

function monthBounds(month: string): { from: string; to: string } | null {
  const match = /^(\d{4})-(\d{2})$/.exec(month);
  if (!match) return null;
  const year = Number(match[1]);
  const monthNumber = Number(match[2]);
  if (monthNumber < 1 || monthNumber > 12) return null;
  const last = new Date(year, monthNumber, 0).getDate();
  return { from: `${month}-01`, to: `${month}-${String(last).padStart(2, "0")}` };
}

// Sources are not a nominal category like models: two custom relays have no
// natural order a reader can remember, so a stable hue gives the bar a second
// channel beside its label. Lightness and chroma come from --chart so the
// source cards keep the model cards' contrast in either theme.
function sourceColor(source: string): string {
  let hue = 216;
  for (const ch of source) hue = (hue * 31 + (ch.codePointAt(0) ?? 0)) % 360;
  return `oklch(from var(--chart) l c ${hue})`;
}

function providerFallback(kind?: string, host?: string): string {
  return [kind?.trim(), host?.trim()].filter(Boolean).join(" · ");
}

/** The window this panel opens on. Exported because the settings contents list
 *  reports the same total beside "用量", and two windows would be two numbers. */
export const DEFAULT_DAYS = DEFAULT_USAGE_DAYS;

// Money and token counts go through i18n/format: its whole reason for existing
// is that five files each grew their own rule. Intl also spells CNY as CN¥ in
// an English window, where a bare ¥ reads as yen.
function moneyList(list?: Money[]): string {
  if (!list?.length) return "—";
  return list.map((m) => money(Number(m.amount), m.currency)).join("  ·  ");
}

// A tick ladder a person reads: round the top up to a 1/2/5-ish step.
function niceMax(raw: number): number {
  if (raw <= 0) return 1;
  const pow = Math.pow(10, Math.floor(Math.log10(raw)));
  return [1, 1.2, 1.5, 2, 2.5, 3, 4, 5, 6, 8, 10].map((m) => m * pow).find((v) => v >= raw) ?? raw;
}

interface PlotProps {
  days: UsageDay[];
  pick: (d: UsageDay) => number;
  label: (v: number) => string;
  aria: string;
}

// One series, so no legend: the card's title names it. Area fill, recessive
// grid, an emphasised endpoint, and a crosshair that reads the nearest day.
function Plot({ days, pick, label, aria }: PlotProps) {
  const [at, setAt] = useState<number | null>(null);
  const W = 860, H = 176, PL = 58, PR = 12, PT = 12, PB = 24;
  const max = niceMax(Math.max(...days.map(pick), 0));
  const x = (i: number) => PL + (days.length < 2 ? 0 : (i * (W - PL - PR)) / (days.length - 1));
  const y = (v: number) => PT + (1 - v / max) * (H - PT - PB);
  // The 365-day range rebuilds a ~365-segment path string; none of it depends
  // on which day the cursor is over, so a hover must not pay for it.
  const { pts, line, fill } = useMemo(() => {
    const at2 = days.map((d, i) => [x(i), y(pick(d))] as const);
    const path = at2.map((p, i) => (i ? "L" : "M") + p[0].toFixed(1) + " " + p[1].toFixed(1)).join(" ");
    return {
      pts: at2, line: path,
      fill: at2.length ? `${path} L${x(days.length - 1).toFixed(1)} ${H - PB} L${PL} ${H - PB} Z` : "",
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [days, pick, max]);
  const every = Math.max(1, Math.ceil(days.length / 5));
  const hover = at === null ? null : days[at];

  return (
    <div className="uplot" data-on={hover ? "" : undefined}>
      <svg viewBox={`0 0 ${W} ${H}`} role="img" aria-label={aria}>
        <g className="ugrid">
          {[0, max / 2, max].map((v) => (
            <line key={v} x1={PL} x2={W - PR} y1={y(v)} y2={y(v)} />
          ))}
        </g>
        <g className="uaxis">
          {[0, max / 2, max].map((v) => (
            <text key={v} x={PL - 8} y={y(v) + 4} textAnchor="end">{label(v)}</text>
          ))}
          {days.map((d, i) =>
            i % every ? null : (
              <text key={d.day} x={x(i)} y={H - 7} textAnchor="middle">{d.day.slice(5)}</text>
            ),
          )}
        </g>
        {fill && <path className="uarea" d={fill} />}
        {pts.length > 1 && <path className="uline" d={line} />}
        {pts.length > 0 && <circle className="ucap" cx={pts[pts.length - 1][0]} cy={pts[pts.length - 1][1]} r={4} />}
        {hover && at !== null && (
          <>
            <line className="ucross" x1={x(at)} x2={x(at)} y1={PT} y2={H - PB} />
            <circle className="umark" cx={x(at)} cy={y(pick(hover))} r={4.5} />
          </>
        )}
        <rect
          className="uhit" x={PL} y={0} width={W - PL - PR} height={H}
          onPointerMove={(e) => {
            const box = e.currentTarget.ownerSVGElement!.getBoundingClientRect();
            const vx = ((e.clientX - box.left) / box.width) * W;
            let best = 0, dist = Infinity;
            days.forEach((_, i) => {
              const d = Math.abs(x(i) - vx);
              if (d < dist) { dist = d; best = i; }
            });
            // Only when the nearest day actually changed: pointermove fires far
            // faster than the reading can change.
            if (best !== at) setAt(best);
          }}
          onPointerLeave={() => setAt(null)}
        />
      </svg>
      {hover && at !== null && (
        <div className="utip" style={{ left: `${(x(at) / W) * 100}%`, top: `${(y(pick(hover)) / H) * 100}%` }}>
          <div className="d">{hover.day}</div>
          <div className="v">{label(pick(hover))}</div>
        </div>
      )}
    </div>
  );
}

type BarRow = [key: string, name: string, value: number, note: string, color?: string];

// Model rows stay nominal and share the chart hue. A provider row may carry a
// stable colour because two custom relays otherwise read as the same source.
function Bars({ rows }: { rows: BarRow[] }) {
  const top = Math.max(...rows.map((r) => r[2]), 1);
  return (
    <div className="urows">
      {rows.map(([key, name, value, note, color]) => (
        <div className="urow" key={key}>
          <div className="urow-t">
            <span className="n" title={name}>{name}</span>
            <span className="v">{note}</span>
          </div>
          <div className="utrack">
            <i
              className="ufill"
              style={{ width: `${(value / top) * 100}%`, ...(color ? { "--row-color": color } : {}) } as CSSProperties}
            />
          </div>
        </div>
      ))}
    </div>
  );
}

// A chart is an overview; a phone cannot hover it precisely. This list is the
// exact record underneath the same data, and it stays available on every input.
function DailyTable({ days }: { days: UsageDay[] }) {
  return (
    <section className="ucard udaily">
      <div className="ucard-h"><h3>{t("每日明细")}</h3><span className="hint">{t("精确到每天")} <span className="utable-scroll-hint">{t("左右滚动查看")}</span></span></div>
      <div className="utable-wrap" role="region" aria-label={t("每日明细")} tabIndex={0}>
        <table className="utable" aria-label={t("每日明细")}>
          <thead>
            <tr><th>{t("日期")}</th><th>Tokens</th><th>{t("支出")}</th><th>{t("请求")}</th></tr>
          </thead>
          <tbody>
            {days.map((d) => (
              <tr key={d.day} data-day={d.day}>
                <td className="ud">{d.day}</td>
                <td>{count(d.total)}</td>
                <td>{moneyList(d.cost)}</td>
                <td>{count(d.requests)}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </section>
  );
}

export function Usage({ port }: { port: AgentPort }) {
  const [range, setRange] = useState<UsageWindow>({ kind: "days", days: DEFAULT_DAYS });
  const [customFrom, setCustomFrom] = useState("");
  const [customTo, setCustomTo] = useState("");
  const [month, setMonth] = useState("");
  const [providerLabels, setProviderLabels] = useState<Record<string, string>>({});
  const [report, setReport] = useState<UsageReport | null>(null);
  const [err, setErr] = useState("");

  const query = useMemo<UsageQuery>(() => {
    if (range.kind === "days") return { days: range.days };
    if (range.kind === "custom") return { from: range.from, to: range.to };
    return monthBounds(range.month) ?? { days: DEFAULT_DAYS };
  }, [range]);

  useEffect(() => {
    let live = true;
    setErr("");
    port.usage(query).then(
      (r) => live && setReport(r),
      (e) => live && setErr(reason(e)),
    );
    return () => { live = false; };
  }, [port, query]);

  useEffect(() => {
    let live = true;
    Promise.all([
      port.providers().catch(() => []),
      port.models().catch(() => []),
    ]).then(([providers, models]) => {
      if (!live) return;
      const labels: Record<string, string> = {};
      // A source can be absent from providers() while still being reachable
      // through the model catalog. Fill those first, then let providers()
      // override them with the connection panel's canonical metadata.
      for (const m of models) {
        const label = m.displayName?.trim() || providerFallback(m.kind, m.vendor);
        if (label) labels[m.provider] = label;
      }
      for (const p of providers) {
        const label = p.displayName?.trim() || providerFallback(p.kind, hostOf(p.baseUrl));
        if (label) labels[p.name] = label;
      }
      setProviderLabels(labels);
    });
    return () => { live = false; };
  }, [port]);

  const openCustom = () => {
    const from = customFrom || report?.from || "";
    const to = customTo || report?.to || "";
    setCustomFrom(from);
    setCustomTo(to);
    if (from && to) setRange({ kind: "custom", from, to });
  };

  const openMonth = () => {
    const picked = month || report?.from.slice(0, 7) || currentMonth();
    setMonth(picked);
    setRange({ kind: "month", month: picked });
  };

  const customValid = customFrom !== "" && customTo !== "" && customFrom <= customTo;

  // The first day that carries a cost. Records older than the cost field have
  // tokens and no price, and showing them as free would understate the bill.
  const pricedFrom = useMemo(
    () => report?.daily.find((d) => d.cost?.length)?.day ?? "",
    [report],
  );
  const priced = useMemo(
    () => report?.daily.filter((d) => d.cost?.length) ?? [],
    [report],
  );
  const unpricedDays = (report?.daily.filter((d) => d.total > 0 && !d.cost?.length) ?? []).length;
  // Every currency the range touched, in the order the kernel sorted them.
  const currencies = useMemo(() => {
    const seen: string[] = [];
    for (const day of report?.daily ?? []) {
      for (const entry of day.cost ?? []) {
        if (!seen.includes(entry.currency)) seen.push(entry.currency);
      }
    }
    return seen;
  }, [report]);
  // The rate partitions the input side only (cache_hit + cache_miss), while
  // report.tokens counts prompt + completion. Stating one against the other
  // would put the panel's headline claim on the wrong denominator.
  const input = report ? report.cache_hit + report.cache_miss : 0;
  const hitRate = input > 0 ? (report!.cache_hit / input) * 100 : 0;

  return (
    <div className="usage">
      <div className="uhead">
        <div className="uranges" role="group" aria-label={t("时间范围")}>
          {RANGES.map(([n, lbl]) => (
            <button key={n} type="button" aria-pressed={range.kind === "days" && range.days === n} onClick={() => setRange({ kind: "days", days: n })}>{t(lbl)}</button>
          ))}
          <button type="button" aria-pressed={range.kind === "custom"} onClick={openCustom}>{t("自定义")}</button>
          <button type="button" aria-pressed={range.kind === "month"} onClick={openMonth}>{t("自然月")}</button>
        </div>
        {report && <span className="uspan">{report.from} → {report.to}</span>}
      </div>

      {range.kind === "custom" && (
        <form
          className="udates"
          onSubmit={(e) => {
            e.preventDefault();
            if (customValid) setRange({ kind: "custom", from: customFrom, to: customTo });
          }}
        >
          <label>{t("开始日期")}<input type="date" value={customFrom} max={customTo || undefined} onChange={(e) => setCustomFrom(e.target.value)} required /></label>
          <label>{t("结束日期")}<input type="date" value={customTo} min={customFrom || undefined} onChange={(e) => setCustomTo(e.target.value)} required /></label>
          <button type="submit" disabled={!customValid}>{t("应用")}</button>
        </form>
      )}

      {range.kind === "month" && (
        <label className="umonth">
          {t("选择月份")}
          <input
            type="month"
            value={month}
            onChange={(e) => {
              setMonth(e.target.value);
              if (monthBounds(e.target.value)) setRange({ kind: "month", month: e.target.value });
            }}
          />
        </label>
      )}

      {err && <div className="uerr" role="alert">{err}</div>}
      {!report && !err && <div className="uwait">{t("正在读取记录…")}</div>}
      {report && <>
      <section className="uhero">
        {/* "Paid" is only honest while every folded quote came from the
            provider. One fallback-priced row and the label needs the caveat. */}
        <div className="uhero-k">{report.costEstimated ? t("花费") : t("实付")}</div>
        <div className="uhero-fig">
          <div className="uhero-v">{moneyList(report.cost)}</div>
          {pricedFrom && <span className="uhero-scope">{t("{day} 起 · {n} 天有记录", { day: pricedFrom.slice(5), n: priced.length })}</span>}
          {report.costEstimated && <span className="uhero-scope">{t("含估算")}</span>}
        </div>
        <p className="uhero-note">
          {t("该时间段送入的 {tok} 输入 tokens 中，{rate}% 命中前缀缓存 —— 命中部分按缓存价计费，较未命中低一个数量级。", {
            tok: fmtTokens(input), rate: hitRate.toFixed(1),
          })}
        </p>
      </section>

      <div className="utiles">
        <div className="utile"><div className="k">Tokens</div><div className="v">{fmtTokens(report.tokens)}</div>
          <div className="m">{t("其中输入 {n}：命中 {a} · 未命中 {b}", { n: fmtTokens(input), a: fmtTokens(report.cache_hit), b: fmtTokens(report.cache_miss) })}</div></div>
        <div className="utile"><div className="k">{t("缓存命中率")}</div><div className="v">{hitRate.toFixed(1)}%</div>
          <div className="m">{t("命中率越高成本越低 —— 需保持前缀稳定")}</div></div>
        <div className="utile"><div className="k">Turns</div><div className="v">{fmtTokens(report.turns)}</div>
          <div className="m">{t("{n} 次 API 请求", { n: fmtTokens(report.requests) })}</div></div>
        <div className="utile"><div className="k">{t("活跃天数")}</div><div className="v">{report.active_days}</div>
          <div className="m">{t("共 {n} 天中", { n: report.daily.length })}</div></div>
      </div>

      <section className="ucard">
        <div className="ucard-h"><h3>{t("每日用量")}</h3><span className="hint">{t("悬停查看某天")}</span></div>
        <Plot days={report.daily} pick={(d) => d.total} label={(v) => fmtTokens(Math.round(v))} aria={t("每日 tokens")} />
      </section>

      <DailyTable days={report.daily} />

      {/* One chart per currency, for the same reason tokens and money get their
          own: two currencies do not share an axis either, and the panel keeping
          them apart everywhere else would be undone by one merged plot. */}
      {currencies.map((code, at) => (
        <section className="ucard" key={code}>
          <div className="ucard-h"><h3>{t("每日支出")}</h3><span className="hint">{code}</span></div>
          <p className="ucard-s">{t("与用量分开画：两者量纲不同，共用一根轴会读错")}</p>
          <Plot
            days={priced.filter((d) => d.cost?.some((c) => c.currency === code))}
            pick={(d) => Number(d.cost?.find((c) => c.currency === code)?.amount ?? 0)}
            label={(v) => money(v, code)}
            aria={t("每日支出")}
          />
          {at === currencies.length - 1 && unpricedDays > 0 && (
            <div className="unote">
              {t("另有 {n} 天有用量但没有成本记录 —— 不是那几天没花费，是成本字段后来才开始持久化。", { n: unpricedDays })}
            </div>
          )}
        </section>
      ))}

      <div className="utwo">
        <section className="ucard">
          <div className="ucard-h"><h3>{t("按模型")}</h3></div>
          <Bars rows={report.models.map((m) => [m.model, m.model, m.tokens, `${fmtTokens(m.tokens)}  ${m.percent.toFixed(1)}%`])} />
        </section>
        <section className="ucard">
          <div className="ucard-h"><h3>{t("按来源")}</h3></div>
          <Bars rows={report.providers.map((p) => [p.provider, providerLabels[p.provider] ?? t("未知来源"), p.tokens, `${fmtTokens(p.tokens)}  ${p.percent.toFixed(1)}%`, sourceColor(p.provider)])} />
        </section>
      </div>
      </>}
    </div>
  );
}
