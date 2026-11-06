import { useEffect, useMemo, useState, type CSSProperties } from "react";
import { UsageStack, type StackPoint } from "./UsageStack";
import { usageCsv } from "./usagecsv";
import { t } from "../i18n";
import { count, money, tokens as fmtTokens } from "../i18n/format";
import { reason } from "../i18n/kernel";
import { DEFAULT_USAGE_DAYS, type AgentPort, type Money, type UsageQuery, type UsageReport } from "../port/port";
import { hostOf } from "./vendors";

const RANGES: [number, string][] = [[7, "usage.n_7_days"], [30, "usage.n_30_days"], [365, "common.all"]];

type UsageWindow =
  | { kind: "today" }
  | { kind: "days"; days: number }
  | { kind: "custom"; from: string; to: string }
  | { kind: "month"; month: string };

function localDay(): string {
  const now = new Date();
  return `${now.getFullYear()}-${String(now.getMonth() + 1).padStart(2, "0")}-${String(now.getDate()).padStart(2, "0")}`;
}

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

// A tick ladder a person reads: round the top up to a 1/2/5-ish step. An empty
// series still needs two distinct halves, or both upper ticks label as 1.

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

interface BreakdownRow {
  key: string;
  label: string;
  total: number;
  requests: number;
  cost?: Money[];
  estimateCost?: Money[];
}

// A chart is an overview; a phone cannot hover it precisely. This list is the
// exact record underneath the same data, and it stays available on every input.
function BreakdownTable({ rows, hourly }: { rows: BreakdownRow[]; hourly: boolean }) {
  const title = hourly ? t("usage.hourly_details") : t("usage.daily_details");
  return (
    <section className="ucard udaily">
      <div className="ucard-h"><h3>{title}</h3><span className="hint">{hourly ? t("usage.exact_totals_for_each_hour") : t("usage.exact_totals_for_each_day")} <span className="utable-scroll-hint">{t("usage.scroll_sideways_for_all_columns")}</span></span></div>
      <div className="utable-wrap" role="region" aria-label={title} tabIndex={0}>
        <table className="utable" aria-label={title}>
          <thead>
            <tr><th>{hourly ? t("usage.time") : t("usage.date")}</th><th>Tokens</th><th>{t("usage.spend")}</th><th>{t("common.requests")}</th></tr>
          </thead>
          <tbody>
            {rows.map((row) => (
              <tr key={row.key} data-day={hourly ? undefined : row.key} data-hour={hourly ? row.key : undefined}>
                <td className="ud">{row.label}</td>
                <td>{count(row.total)}</td>
                <td>
                  {moneyList(row.cost)}
                  {row.estimateCost?.length ? <span className="udaily-est" title={t("usage.subscription_use_priced_at_api")}>{t("usage.api_priced_estimate")} {moneyList(row.estimateCost)}</span> : null}
                </td>
                <td>{count(row.requests)}</td>
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
    if (range.kind === "today") {
      const day = localDay();
      return { from: day, to: day };
    }
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
  const unpricedDays = (report?.daily.filter((d) => d.total > 0 && !d.cost?.length && !d.estimateCost?.length) ?? []).length;
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

  // A one-day window reads in hours; the kernel sends the buckets for exactly
  // that case. "Today" stops at the current hour so the empty future does not
  // read as a drop to zero.
  const isSingleDay = !!report && report.from === report.to;
  const hours = report?.hourly ?? [];
  const hourView = isSingleDay && hours.length > 0;
  const visibleHours = hourView && report!.to === localDay()
    ? hours.slice(0, new Date().getHours() + 1)
    : hours;
  const usagePoints: StackPoint[] = hourView
    ? visibleHours.map((h) => ({ key: h.hour, axis: h.hour, tip: h.hour, hit: h.cacheHit, miss: h.cacheMiss, total: h.total }))
    : (report?.daily ?? []).map((d) => ({ key: d.day, axis: d.day.slice(5), tip: d.day, hit: d.cacheHit, miss: d.cacheMiss, total: d.total }));
  const dayRows: BreakdownRow[] = (report?.daily ?? [])
    .slice()
    .sort((a, b) => b.day.localeCompare(a.day))
    .map((d) => ({ key: d.day, label: d.day, total: d.total, requests: d.requests, cost: d.cost, estimateCost: d.estimateCost }));
  const hourRows: BreakdownRow[] = visibleHours.map((h) => ({
    key: h.hour, label: h.hour, total: h.total, requests: h.requests, cost: h.cost, estimateCost: h.estimateCost,
  }));

  return (
    <div className="usage">
      <div className="uhead">
        <div className="uranges" role="group" aria-label={t("usage.time_range")}>
          <button type="button" data-action="usage.range.today" aria-pressed={range.kind === "today"} onClick={() => setRange({ kind: "today" })}>{t("usage.today")}</button>
          {RANGES.map(([n, lbl]) => (
            <button key={n} type="button" aria-pressed={range.kind === "days" && range.days === n} onClick={() => setRange({ kind: "days", days: n })}>{t(lbl)}</button>
          ))}
          <button type="button" aria-pressed={range.kind === "custom"} onClick={openCustom}>{t("common.custom")}</button>
          <button type="button" aria-pressed={range.kind === "month"} onClick={openMonth}>{t("usage.calendar_month")}</button>
        </div>
        {report && <span className="uspan">{report.from} → {report.to}</span>}
        {report && (
          <button type="button" className="uexport" data-action="usage.export" onClick={() => {
            const url = URL.createObjectURL(new Blob([usageCsv(report)], { type: "text/csv;charset=utf-8" }));
            const link = document.createElement("a");
            link.href = url;
            link.download = `reasonix-usage-${report.from}-${report.to}.csv`;
            link.click();
            URL.revokeObjectURL(url);
          }}>{t("usage.export_csv")}</button>
        )}
      </div>

      {range.kind === "custom" && (
        <form
          className="udates"
          onSubmit={(e) => {
            e.preventDefault();
            if (customValid) setRange({ kind: "custom", from: customFrom, to: customTo });
          }}
        >
          <label>{t("usage.start_date")}<input data-action="usage.range.start" type="date" value={customFrom} max={customTo || undefined} onChange={(e) => setCustomFrom(e.target.value)} required /></label>
          <label>{t("usage.end_date")}<input data-action="usage.range.end" type="date" value={customTo} min={customFrom || undefined} onChange={(e) => setCustomTo(e.target.value)} required /></label>
          <button type="submit" disabled={!customValid}>{t("usage.apply")}</button>
        </form>
      )}

      {range.kind === "month" && (
        <label className="umonth">
          {t("usage.choose_month")}
          <input
            data-action="usage.range.month"
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
      {!report && !err && <div className="uwait">{t("usage.reading_the_records")}</div>}
      {report && <>
      <div className="utiles">
        <div className="utile utile-paid">
          {/* "Paid" is only honest while every folded quote came from the
              provider and priced its call. One fallback-priced or unpriced row
              and the label needs the caveat. */}
          <div className="k">{report.costEstimated || report.costIncomplete ? t("usage.spend_2") : t("usage.paid")}</div>
          <div className="v">{moneyList(report.cost)}</div>
          {report.estimateCost?.length ? <div className="m uest">{t("usage.api_priced_estimate")} {moneyList(report.estimateCost)} · {t("usage.subscription_use_priced_at_api_2")}</div> : null}
          {pricedFrom && <div className="m">{t("usage.from_day_n_days_recorded", { day: pricedFrom.slice(5), n: priced.length })}</div>}
          {report.costEstimated && <div className="m">{t("usage.includes_estimates")}</div>}
          {report.costIncomplete && (
            <div className="m">
              {t("usage.unpriced_calls_not_included")}
              {report.unpricedCalls ? ` · ${t("usage.n_calls_unpriced", { n: report.unpricedCalls })}` : null}
            </div>
          )}
        </div>
        <div className="utile"><div className="k">{t("usage.input")}</div><div className="v">{fmtTokens(input)}</div>
          <div className="m">{t("usage.cache_hit_rate_note", { rate: hitRate.toFixed(1) })}</div></div>
        <div className="utile"><div className="k">{t("usage.provider_reported_total")}</div><div className="v">{fmtTokens(report.tokens)}</div>
          <div className="m">{t("usage.total_includes_output")}</div></div>
        <div className="utile"><div className="k">{t("usage.requests")}</div><div className="v">{fmtTokens(report.requests)}</div>
          <div className="m">{t("usage.n_turns", { n: fmtTokens(report.turns) })}</div>
          <div className="m">{t("usage.of_n_days_avg", { n: report.daily.length, avg: fmtTokens(report.active_days ? Math.round(report.requests / report.active_days) : 0) })}</div></div>
      </div>

      <div className="ucharts">
      <section className="ucard">
        <div className="ucard-h"><h3>{hourView ? t("usage.hourly_usage") : t("usage.daily_usage")}</h3><span className="hint">{hourView ? t("usage.hover_for_an_hour") : t("usage.hover_for_a_day")}</span></div>
        <UsageStack points={usagePoints} label={(v) => fmtTokens(Math.round(v))} aria={hourView ? t("usage.hourly_tokens") : t("usage.daily_tokens")} />
      </section>

      {/* One chart per currency, for the same reason tokens and money get their
          own: two currencies do not share an axis either, and the panel keeping
          them apart everywhere else would be undone by one merged plot. */}
      <div className="ucharts-spend">
      {currencies.map((code, at) => (
        <section className="ucard" key={code}>
          <div className="ucard-h"><h3>{hourView ? t("usage.hourly_spend") : t("usage.daily_spend")}</h3><span className="hint">{code}</span></div>
          <p className="ucard-s">{t("usage.drawn_apart_from_usage_different")}</p>
          <UsageStack single
            points={(hourView
              ? visibleHours.map((h) => ({ key: h.hour, axis: h.hour, tip: h.hour, value: Number(h.cost?.find((c) => c.currency === code)?.amount ?? 0) }))
              : priced.filter((d) => d.cost?.some((c) => c.currency === code)).map((d) => ({ key: d.day, axis: d.day.slice(5), tip: d.day, value: Number(d.cost?.find((c) => c.currency === code)?.amount ?? 0) })))
              .map((p) => ({ key: p.key, axis: p.axis, tip: p.tip, hit: p.value, miss: 0, total: p.value }))}
            label={(v) => money(v, code)}
            aria={hourView ? t("usage.hourly_spend") : t("usage.daily_spend")}
          />
          {at === currencies.length - 1 && unpricedDays > 0 && (
            <div className="unote">
              {t("usage.n_more_days_have_usage", { n: unpricedDays })}
            </div>
          )}
        </section>
      ))}
      </div>
      </div>

      <BreakdownTable rows={hourView ? hourRows : dayRows} hourly={hourView} />

      <div className="utwo">
        <section className="ucard">
          <div className="ucard-h"><h3>{t("usage.by_model")}</h3></div>
          <Bars rows={report.models.map((m) => [m.model, m.model, m.tokens, `${fmtTokens(m.tokens)}  ${m.percent.toFixed(1)}%`])} />
        </section>
        <section className="ucard">
          <div className="ucard-h"><h3>{t("common.by_source")}</h3></div>
          <Bars rows={report.providers.map((p) => [p.provider, providerLabels[p.provider] ?? t("usage.unknown_source"), p.tokens, `${fmtTokens(p.tokens)}  ${p.percent.toFixed(1)}%`, sourceColor(p.provider)])} />
        </section>
      </div>
      </>}
    </div>
  );
}
