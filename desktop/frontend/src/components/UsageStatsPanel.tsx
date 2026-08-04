// UsageStatsPanel renders the "usage statistics" subtab inside the Models
// settings page. It reads aggregated stats from the Go backend (App.UsageStats)
// and draws three charts by hand in SVG — a GitHub-style activity heatmap, a
// stacked per-day token trend, and a per-model donut — so no chart library is
// needed and theme variables (--accent, --fg, --bg-elev-*, --stats-hue-l) drive
// the palette for both stock themes and theme packs.
// The component's styles live in UsageStatsPanel.css (loaded on demand with
// this chunk), so the ~7 KB rule block never inflates the settings bundle.
import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import { Activity, CalendarDays, Coins, Cpu, MessageSquare, MessagesSquare } from "lucide-react";
import { useI18n } from "../lib/i18n";
import { app } from "../lib/bridge";
import type { DailyTokenUsage, UsageStatsRange, UsageStatsRequest } from "../lib/types";
import { formatUsageTokens as formatTokens } from "../lib/usageStatsFormat";
import "./UsageStatsPanel.css";

const RANGE_PRESETS = ["7", "14", "30", "90"] as const;
// Every entry point that records usage (see StatsSource tags in the Go
// kernel). "all" is the unfiltered aggregate; the rest match one source label.
const SOURCES = ["all", "desktop", "cli", "serve", "bot", "remote"] as const;

// The heatmap always shows a fixed 40-week window regardless of the range
// preset (it only follows the source filter).
const HEAT_WEEKS = 40;
// Custom ranges may span up to ten years. Keep the detailed trend bounded so
// one unusual range cannot create thousands of interactive SVG nodes.
const MAX_TREND_DAYS = 180;

// Keep this lazy panel's translations in its own chunk. Putting them in the
// eager global English dictionary makes an unopened settings subtab part of
// every desktop startup bundle.
const USAGE_STATS_TRANSLATIONS = {
  en: {
    "common.loading": "Loading…",
    "common.none": "none",
    "settings.stats.range": "Time range",
    "settings.stats.rangePreset.7": "Last 7 days",
    "settings.stats.rangePreset.14": "Last 14 days",
    "settings.stats.rangePreset.30": "Last 30 days",
    "settings.stats.rangePreset.90": "Last 90 days",
    "settings.stats.rangeCustom": "Custom",
    "settings.stats.from": "From",
    "settings.stats.to": "To",
    "settings.stats.source": "Source",
    "settings.stats.source.all": "All",
    "settings.stats.source.desktop": "Desktop",
    "settings.stats.source.cli": "CLI",
    "settings.stats.source.serve": "Web",
    "settings.stats.source.bot": "Bot",
    "settings.stats.source.remote": "Remote",
    "settings.stats.refresh": "Refresh",
    "settings.stats.tokens": "Token usage",
    "settings.stats.sessions": "Completed turns",
    "settings.stats.requests": "Requests",
    "settings.stats.activeDays": "Active days",
    "settings.stats.cacheRate": "Avg cache hit rate",
    "settings.stats.cacheRateHint": "Cached input tokens as a share of all input tokens in the range",
    "settings.stats.cacheHitRate": "Cache hit rate",
    "settings.stats.hitRateLegend": "Cache hit rate",
    "settings.stats.topModel": "Most used model",
    "settings.stats.topModelHint": "Ranked by token volume, not call count",
    "settings.stats.heatmap": "Activity heatmap",
    "settings.stats.heatLess": "Less",
    "settings.stats.heatMore": "More",
    "settings.stats.dailyTrend": "Daily token trend",
    "settings.stats.trendLimited": "Showing the latest 180 days",
    "settings.stats.modelUsage": "Model usage",
    "settings.stats.total": "Total",
    "settings.stats.percent": "Share",
    "settings.stats.asOf": "As of",
    "settings.stats.empty": "No usage data in this range yet. Token usage is recorded from the day this feature ships.",
  },
  zh: {
    "common.loading": "加载中…",
    "common.none": "无",
    "settings.stats.range": "时间范围",
    "settings.stats.rangePreset.7": "最近 7 天",
    "settings.stats.rangePreset.14": "最近 14 天",
    "settings.stats.rangePreset.30": "最近 30 天",
    "settings.stats.rangePreset.90": "最近 90 天",
    "settings.stats.rangeCustom": "自定义",
    "settings.stats.from": "开始日期",
    "settings.stats.to": "结束日期",
    "settings.stats.source": "统计来源",
    "settings.stats.source.all": "全部",
    "settings.stats.source.desktop": "桌面端",
    "settings.stats.source.cli": "命令行",
    "settings.stats.source.serve": "网页端",
    "settings.stats.source.bot": "机器人",
    "settings.stats.source.remote": "远程工作台",
    "settings.stats.refresh": "刷新",
    "settings.stats.tokens": "Tokens 用量",
    "settings.stats.sessions": "完成轮次",
    "settings.stats.requests": "请求数量",
    "settings.stats.activeDays": "活跃天数",
    "settings.stats.cacheRate": "平均缓存命中率",
    "settings.stats.cacheRateHint": "时间段内缓存命中 token 占输入 token 的比例",
    "settings.stats.cacheHitRate": "缓存命中率",
    "settings.stats.hitRateLegend": "缓存命中率",
    "settings.stats.topModel": "最常用模型",
    "settings.stats.topModelHint": "按 token 用量排序，非调用次数",
    "settings.stats.heatmap": "活跃热力图",
    "settings.stats.heatLess": "较少",
    "settings.stats.heatMore": "较多",
    "settings.stats.dailyTrend": "按天 Token 趋势",
    "settings.stats.trendLimited": "仅显示最近 180 天",
    "settings.stats.modelUsage": "模型用量",
    "settings.stats.total": "总用量",
    "settings.stats.percent": "占比",
    "settings.stats.asOf": "统计截至",
    "settings.stats.empty": "当前时间范围内暂无用量数据。Token 用量从本功能启用后开始累计。",
  },
  "zh-TW": {
    "common.loading": "載入中…",
    "common.none": "無",
    "settings.stats.range": "時間範圍",
    "settings.stats.rangePreset.7": "最近 7 天",
    "settings.stats.rangePreset.14": "最近 14 天",
    "settings.stats.rangePreset.30": "最近 30 天",
    "settings.stats.rangePreset.90": "最近 90 天",
    "settings.stats.rangeCustom": "自訂",
    "settings.stats.from": "開始日期",
    "settings.stats.to": "結束日期",
    "settings.stats.source": "統計來源",
    "settings.stats.source.all": "全部",
    "settings.stats.source.desktop": "桌面端",
    "settings.stats.source.cli": "命令列",
    "settings.stats.source.serve": "網頁端",
    "settings.stats.source.bot": "機器人",
    "settings.stats.source.remote": "遠端工作台",
    "settings.stats.refresh": "重新整理",
    "settings.stats.tokens": "Tokens 用量",
    "settings.stats.sessions": "完成輪次",
    "settings.stats.requests": "請求數量",
    "settings.stats.activeDays": "活躍天數",
    "settings.stats.cacheRate": "平均快取命中率",
    "settings.stats.cacheRateHint": "時間範圍內快取命中 token 佔輸入 token 的比例",
    "settings.stats.cacheHitRate": "快取命中率",
    "settings.stats.hitRateLegend": "快取命中率",
    "settings.stats.topModel": "最常用模型",
    "settings.stats.topModelHint": "依 token 用量排序，非呼叫次數",
    "settings.stats.heatmap": "活躍熱力圖",
    "settings.stats.heatLess": "較少",
    "settings.stats.heatMore": "較多",
    "settings.stats.dailyTrend": "按天 Token 趨勢",
    "settings.stats.trendLimited": "僅顯示最近 180 天",
    "settings.stats.modelUsage": "模型用量",
    "settings.stats.total": "總用量",
    "settings.stats.percent": "佔比",
    "settings.stats.asOf": "統計截至",
    "settings.stats.empty": "目前時間範圍內暫無用量資料。Token 用量從此功能啟用後開始累計。",
  },
} as const;

type UsageStatsKey = keyof typeof USAGE_STATS_TRANSLATIONS.en;
type UsageStatsTranslator = (key: UsageStatsKey) => string;

// Seed hues keep neighbouring models distinguishable. The final CSS colour
// expression blends each seed with the live theme's accent and foreground, so
// base-colour and light/dark changes need no React rerender.
const PALETTE_SEEDS = [
  "#0087be", "#78dcfa", "#00aadc", "#87a6bc", "#d54e21", "#f0821e",
  "#4ab866", "#f0b849", "#d94f4f", "#e66760", "#8c88cd", "#69c5e4",
  "#ffdf8b", "#61e064", "#bbd634", "#fdd666", "#a4dbdb", "#b51f29",
  "#f58268", "#f4979c",
];
const themedModelColor = (seed: string): string =>
  `color-mix(in srgb, color-mix(in srgb, var(--accent) 24%, ${seed}) 60%, var(--fg) 40%)`;
const PALETTE = PALETTE_SEEDS.map(themedModelColor);
// LCG-seeded Fisher–Yates: a fixed seed keeps the shuffle stable across
// renders and ranges while still picking palette colours "at random".
const PALETTE_ORDER = (() => {
  const a = [...PALETTE];
  let s = 42;
  const rnd = () => {
    s = (s * 1664525 + 1013904223) >>> 0;
    return s / 4294967296;
  };
  for (let i = a.length - 1; i > 0; i--) {
    const j = Math.floor(rnd() * (i + 1));
    [a[i], a[j]] = [a[j], a[i]];
  }
  return a;
})();
function assignModelColors(ordered: string[]): Map<string, string> {
  const map = new Map<string, string>();
  let slot = 0;
  for (const m of ordered) {
    if (map.has(m)) continue;
    if (slot < PALETTE_ORDER.length) {
      map.set(m, PALETTE_ORDER[slot]);
    } else {
      // Beyond the curated palette: retain a stable hue, then blend it with
      // the active accent and foreground for the same theme adaptation.
      map.set(m, overflowModelColor(m));
    }
    slot++;
  }
  return map;
}
const hashHue = (key: string): number => {
  let h = 0;
  for (let i = 0; i < key.length; i++) h = (h * 31 + key.charCodeAt(i)) >>> 0;
  return h % 360;
};
const overflowModelColor = (model: string): string =>
  `color-mix(in srgb, color-mix(in srgb, hsl(${hashHue(model)} 52% var(--stats-hue-l)) 76%, var(--accent) 24%) 60%, var(--fg) 40%)`;

// localDay returns today's date (plus/minus offsetDays) in the local calendar,
// matching the backend's "2006-01-02" day keys.
function localDay(offsetDays: number): string {
  const d = new Date();
  d.setDate(d.getDate() + offsetDays);
  const y = d.getFullYear();
  const m = String(d.getMonth() + 1).padStart(2, "0");
  const day = String(d.getDate()).padStart(2, "0");
  return `${y}-${m}-${day}`;
}

export function UsageStatsPanel() {
  const { locale } = useI18n();
  const t = useCallback<UsageStatsTranslator>((key) => USAGE_STATS_TRANSLATIONS[locale][key], [locale]);
  const [range, setRange] = useState<string>("30");
  const [customFrom, setCustomFrom] = useState("");
  const [customTo, setCustomTo] = useState("");
  const [source, setSource] = useState<string>("all");
  const [stats, setStats] = useState<UsageStatsRange | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const generationRef = useRef(0);

  // Heatmap window: the last HEAT_WEEKS*7 days, fixed regardless of `range`.
  const heatWindow = useMemo(() => {
    const to = localDay(0);
    const from = localDay(-(HEAT_WEEKS * 7 - 1));
    return { from, to };
  }, []);
  const [heatDaily, setHeatDaily] = useState<DailyTokenUsage[]>([]);
  const heatGenRef = useRef(0);

  const loadHeat = useCallback(async () => {
    const generation = ++heatGenRef.current;
    setHeatDaily([]);
    try {
      const res = await app.UsageStats({ range: "custom", from: heatWindow.from, to: heatWindow.to, source });
      if (heatGenRef.current !== generation) return;
      setHeatDaily(res.daily);
    } catch {
      if (heatGenRef.current !== generation) return;
      // The heatmap is auxiliary — a failed fetch leaves the current source
      // empty instead of retaining cells from the previous source.
      setHeatDaily([]);
    }
  }, [heatWindow.from, heatWindow.to, source]);

  useEffect(() => {
    void loadHeat();
  }, [loadHeat]);

  const load = useCallback(async () => {
    const generation = ++generationRef.current;
    // A custom range with empty date pickers must not hit the backend — the
    // request would carry "" from/to and fail validation. Wait for both dates.
    if (range === "custom" && (!customFrom || !customTo)) {
      setStats(null);
      setLoading(false);
      setError("");
      return;
    }
    const req: UsageStatsRequest =
      range === "custom"
        ? { range, from: customFrom, to: customTo, source }
        : { range, source };
    setStats(null);
    setLoading(true);
    setError("");
    try {
      const res = await app.UsageStats(req);
      if (generationRef.current !== generation) return; // stale response
      setStats(res);
    } catch (e) {
      if (generationRef.current !== generation) return;
      setStats(null);
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      if (generationRef.current === generation) setLoading(false);
    }
  }, [range, customFrom, customTo, source]);

  useEffect(() => {
    void load();
  }, [load]);

  // Colours follow the order models were first used (walking the daily series
  // chronologically); the hash hue is only a fallback for models that somehow
  // escape the aggregate.
  const modelOrder = useMemo(() => {
    const seen: string[] = [];
    for (const d of stats?.daily ?? []) {
      for (const m of Object.keys(d.byModel).sort()) {
        if (!seen.includes(m)) seen.push(m);
      }
    }
    for (const m of stats?.models ?? []) {
      if (!seen.includes(m.model)) seen.push(m.model);
    }
    return seen;
  }, [stats]);
  const palette = useMemo(() => assignModelColors(modelOrder), [modelOrder]);
  const colorForModel = useCallback(
    (model: string) => palette.get(model) ?? overflowModelColor(model || "unknown"),
    [palette],
  );

  return (
    <div className="usage-stats">
      {/* Section 1: range + source pickers, each in its own framed group */}
      <div className="usage-stats__toolbar">
        <div className="usage-stats__group" role="group" aria-label={t("settings.stats.range")}>
          {RANGE_PRESETS.map((r) => (
            <button
              key={r}
              type="button"
              className={`provider-add-segmented__item${range === r ? " provider-add-segmented__item--active" : ""}`}
              aria-pressed={range === r}
              onClick={() => setRange(r)}
            >
              {t(`settings.stats.rangePreset.${r}`)}
            </button>
          ))}
          <button
            type="button"
            className={`provider-add-segmented__item${range === "custom" ? " provider-add-segmented__item--active" : ""}`}
            aria-pressed={range === "custom"}
            onClick={() => setRange("custom")}
          >
            {t("settings.stats.rangeCustom")}
          </button>
        </div>
        {range === "custom" && (
          <div className="usage-stats__custom">
            <input
              type="date"
              className="mem-input"
              value={customFrom}
              max={customTo || undefined}
              onChange={(e) => setCustomFrom(e.target.value)}
              aria-label={t("settings.stats.from")}
            />
            <span className="usage-stats__custom-sep">–</span>
            <input
              type="date"
              className="mem-input"
              value={customTo}
              min={customFrom || undefined}
              max={localDay(0)}
              onChange={(e) => setCustomTo(e.target.value)}
              aria-label={t("settings.stats.to")}
            />
          </div>
        )}
        <div className="usage-stats__group" role="group" aria-label={t("settings.stats.source")}>
          {SOURCES.map((s) => (
            <button
              key={s}
              type="button"
              className={`provider-add-segmented__item${source === s ? " provider-add-segmented__item--active" : ""}`}
              aria-pressed={source === s}
              onClick={() => setSource(s)}
            >
              {t(`settings.stats.source.${s}`)}
            </button>
          ))}
        </div>
        <button type="button" className="usage-stats__refresh" onClick={() => { void load(); void loadHeat(); }} disabled={loading} aria-label={t("settings.stats.refresh")}>
          {t("settings.stats.refresh")}
        </button>
      </div>

      {error && <div className="provider-fetch-banner provider-fetch-banner--warn">{error}</div>}
      {loading && !stats && <div className="usage-stats__loading">{t("common.loading")}</div>}
      {!loading && stats && (
        <>
          <StatCards stats={stats} t={t} />
          <Heatmap daily={heatDaily} from={heatWindow.from} to={heatWindow.to} t={t} />
          <DailyTrend stats={stats} t={t} colorForModel={colorForModel} />
          <ModelUsage stats={stats} t={t} colorForModel={colorForModel} />
          {stats.to && (
            <div className="usage-stats__foot">
              {t("settings.stats.asOf")} {stats.to}
            </div>
          )}
        </>
      )}
      {!loading && !error && stats && stats.tokens === 0 && (
        <div className="usage-stats__empty">{t("settings.stats.empty")}</div>
      )}
    </div>
  );
}

// ── Section 2+3: numeric cards ────────────────────────────────────────────

function StatCards({ stats, t }: { stats: UsageStatsRange; t: UsageStatsTranslator }) {
  const topModel = stats.topModel || t("common.none");
  // The model name is the longest value: it may wrap to a second line on
  // narrow windows instead of being shrunk or truncated; tokens shows the
  // exact number and stays on one line (FitText shrinks it if needed).
  const cards: Array<{ icon: typeof Coins; label: string; value: string; sm?: boolean; wrap?: boolean; hint?: string }> = [
    { icon: Coins, label: t("settings.stats.tokens"), value: stats.tokens.toLocaleString("en-US") },
    // Turn markers count completed top-level turns; a conversation session may
    // contain many of them, so the user-facing label names the exact metric.
    { icon: MessageSquare, label: t("settings.stats.sessions"), value: String(stats.turns) },
    { icon: MessagesSquare, label: t("settings.stats.requests"), value: String(stats.requests) },
    { icon: CalendarDays, label: t("settings.stats.activeDays"), value: String(stats.activeDays) },
    // Average prompt-cache hit ratio over the range: cached input tokens
    // divided by all input tokens. "—" when no usage is in range yet.
    { icon: Activity, label: t("settings.stats.cacheRate"), value: cacheRateText(stats.cacheHit, stats.cacheMiss), hint: t("settings.stats.cacheRateHint") },
    // "top model" ranks by token volume (not call count) — the hint keeps the
    // metric's meaning visible next to the value.
    { icon: Cpu, label: t("settings.stats.topModel"), value: topModel, sm: true, wrap: true, hint: t("settings.stats.topModelHint") },
  ];
  return (
    <div className="usage-stats__cards">
      {cards.map((c) => (
        <div className="usage-stats__card" key={c.label} title={c.hint}>
          <div className="usage-stats__card-head">
            <c.icon className="usage-stats__card-icon" size={14} strokeWidth={2} aria-hidden="true" />
            <span className="usage-stats__card-label">{c.label}</span>
          </div>
          {c.wrap ? (
            <div className="usage-stats__card-value usage-stats__card-value--sm usage-stats__card-value--wrap">{c.value}</div>
          ) : (
            <FitText
              text={c.value}
              className={`usage-stats__card-value${c.sm ? " usage-stats__card-value--sm" : ""}`}
              maxSize={c.sm ? 14 : 22}
            />
          )}
        </div>
      ))}
    </div>
  );
}

// FitText renders `text` on a single line, shrinking the font until it fits
// the card width (long token numbers never overflow or wrap).
function FitText({ text, className, maxSize }: { text: string; className?: string; maxSize: number }) {
  const ref = useRef<HTMLDivElement>(null);
  const [size, setSize] = useState(maxSize);

  useLayoutEffect(() => {
    const el = ref.current;
    if (!el) return;
    const fit = () => {
      let s = maxSize;
      el.style.fontSize = `${s}px`;
      while (el.scrollWidth > el.clientWidth + 1 && s > 11) {
        s -= 0.5;
        el.style.fontSize = `${s}px`;
      }
      setSize(s);
    };
    fit();
    const ro = new ResizeObserver(fit);
    ro.observe(el);
    return () => ro.disconnect();
  }, [text, maxSize]);

  return (
    <div ref={ref} className={className} style={{ fontSize: size }}>
      {text}
    </div>
  );
}

// ── Section 4: GitHub-style activity heatmap ──────────────────────────────

const HEAT_BASE = 13; // cell size at which column trimming starts
const HEAT_GAP = 3;

// Heatmap always renders the fixed 40-week window passed in `daily`. A wide
// container grows the cells to fill it; once the container can no longer fit
// the window at HEAT_BASE the earliest columns are trimmed first, so the most
// recent weeks stay visible (never the reverse).
function Heatmap({ daily, from, to, t }: { daily: DailyTokenUsage[]; from: string; to: string; t: UsageStatsTranslator }) {
  const [tip, setTip] = useState<{ day: string; tokens: number; requests: number; cacheHit: number; cacheMiss: number; x: number; top: number; bottom: number } | null>(null);
  const wrapRef = useRef<HTMLDivElement>(null);
  const [geom, setGeom] = useState<{ size: number; cols: number }>({ size: HEAT_BASE, cols: HEAT_WEEKS });

  useEffect(() => {
    const el = wrapRef.current;
    if (!el) return;
    const update = () => {
      const avail = Math.max(1, el.clientWidth - 2);
      const baseCols = Math.max(1, Math.floor((avail + HEAT_GAP) / (HEAT_BASE + HEAT_GAP)));
      if (baseCols >= HEAT_WEEKS) {
        // The weekday offset pushes the last day into an extra column, so size
        // the cells against the real column count — the heatmap then fills the
        // whole container edge to edge (no right-hand gap, no scrollbar).
        const so = (indexOfDay(from) + 1) % 7;
        const totalWeeks = Math.ceil((HEAT_WEEKS * 7 + so) / 7);
        const size = Math.max(HEAT_BASE, avail / totalWeeks - HEAT_GAP);
        setGeom({ size, cols: HEAT_WEEKS });
      } else {
        // Too narrow for the full window at the base size: keep the cells at
        // the base size and trim the earliest columns.
        setGeom({ size: HEAT_BASE, cols: baseCols });
      }
    };
    update();
    const ro = new ResizeObserver(update);
    ro.observe(el);
    return () => ro.disconnect();
  }, [from]);

  const byDay = new Map<string, DailyTokenUsage>();
  for (const d of daily) byDay.set(d.day, d);
  const allDays = daysBetween(from, to);
  if (allDays.length === 0) return null;
  // Keep the newest `geom.cols` weeks (7 days each) — trimming the earliest.
  const days = allDays.slice(-Math.min(geom.cols * 7, allDays.length));
  const max = Math.max(1, ...days.map((d) => byDay.get(d)?.total ?? 0));

  const rows = 7; // one per weekday, GitHub-style
  const startOffset = days[0] ? (indexOfDay(days[0]) + 1) % 7 : 0; // 0 = Monday
  // The offset pushes the last day into a further column, so the svg must be
  // wide enough for it — otherwise the newest days get clipped off the edge.
  const weeks = Math.max(1, Math.ceil((days.length + startOffset) / 7));
  // Keep the tooltip inside the heatmap container: flip below when the cell
  // sits too close to the top edge.
  const wrapW = wrapRef.current?.clientWidth ?? 400;
  const tipX = tip ? Math.max(120, Math.min(tip.x, wrapW - 120)) : 0;
  const tipAbove = tip ? tip.top >= 72 : true;
  const tipY = tip ? (tipAbove ? tip.top - 10 : tip.bottom + 10) : 0;

  return (
    <section className="usage-stats__section">
      <div className="usage-stats__section-head">
        <h3 className="usage-stats__section-title">{t("settings.stats.heatmap")}</h3>
        <div className="usage-stats__heatmap-legend">
          <span>{t("settings.stats.heatLess")}</span>
          <i className="usage-stats__heat-cell usage-stats__heat-cell--1" style={{ width: geom.size, height: geom.size }} />
          <i className="usage-stats__heat-cell usage-stats__heat-cell--2" style={{ width: geom.size, height: geom.size }} />
          <i className="usage-stats__heat-cell usage-stats__heat-cell--3" style={{ width: geom.size, height: geom.size }} />
          <i className="usage-stats__heat-cell usage-stats__heat-cell--4" style={{ width: geom.size, height: geom.size }} />
          <i className="usage-stats__heat-cell usage-stats__heat-cell--5" style={{ width: geom.size, height: geom.size }} />
          <span>{t("settings.stats.heatMore")}</span>
        </div>
      </div>
      <div className="usage-stats__heatmap-wrap" ref={wrapRef}>
        <svg className="usage-stats__heatmap" width={weeks * (geom.size + HEAT_GAP) + HEAT_GAP} height={rows * (geom.size + HEAT_GAP) + HEAT_GAP} role="img" aria-label={t("settings.stats.heatmap")}>
          {days.map((day, i) => {
            const col = Math.floor((i + startOffset) / 7);
            const row = (i + startOffset) % 7;
            const rec = byDay.get(day);
            const tokens = rec?.total ?? 0;
            const level = tokens === 0 ? 0 : 1 + Math.floor((tokens / max) * 4); // 1..5
            const x = HEAT_GAP + col * (geom.size + HEAT_GAP);
            const y = HEAT_GAP + row * (geom.size + HEAT_GAP);
            return (
              <rect
                key={day}
                className={`usage-stats__heat-cell usage-stats__heat-cell--${level}`}
                x={x}
                y={y}
                width={geom.size}
                height={geom.size}
                rx={Math.max(1.5, geom.size * 0.2)}
                onMouseEnter={(e) => {
                  const wrap = wrapRef.current;
                  if (!wrap) return;
                  const wr = wrap.getBoundingClientRect();
                  const r = e.currentTarget.getBoundingClientRect();
                  setTip({ day, tokens, requests: rec?.requests ?? 0, cacheHit: rec?.cacheHit ?? 0, cacheMiss: rec?.cacheMiss ?? 0, x: r.left + r.width / 2 - wr.left, top: r.top - wr.top, bottom: r.bottom - wr.top });
                }}
                onMouseLeave={() => setTip(null)}
              />
            );
          })}
        </svg>
        {tip && (
          <div className="usage-stats__tip usage-stats__tip--chart" style={{ transform: `translate(${tipX}px, ${tipY}px) translate(-50%, ${tipAbove ? "-100%" : "0"})` }}>
            <div className="usage-stats__tip-title">{tip.day}</div>
            <div>{t("settings.stats.tokens")}: {formatTokens(tip.tokens)}</div>
            <div>{t("settings.stats.requests")}: {tip.requests}</div>
            <div>{t("settings.stats.cacheHitRate")}: {cacheRateText(tip.cacheHit, tip.cacheMiss)}</div>
          </div>
        )}
      </div>
    </section>
  );
}

// ── Section 5: stacked daily token trend ──────────────────────────────────

function DailyTrend({ stats, t, colorForModel }: { stats: UsageStatsRange; t: UsageStatsTranslator; colorForModel: (m: string) => string }) {
  const daily = stats.daily;
  const trendDaily = daily.length > MAX_TREND_DAYS ? daily.slice(-MAX_TREND_DAYS) : daily;
  const trendLimited = trendDaily.length !== daily.length;
  const [tip, setTip] = useState<{ day: string; total: number; byModel: Record<string, number>; cacheHit: number; cacheMiss: number; cx: number; top: number; bottom: number } | null>(null);
  const [hover, setHover] = useState<string | null>(null);
  const wrapRef = useRef<HTMLDivElement>(null);

  const W = 720;
  const H = 220;
  const padL = 46;
  const padR = 65; // 50 for the hit-rate axis labels + 15 for the rightmost column's half-bar
  const padB = 26;
  const padT = 10;
  const plotH = H - padT - padB;
  const MIN_COL = 14; // viewBox units per day once trimming kicks in

  // The svg fills its container (width 100%). When the full series fits, the
  // viewBox is set to the actual container width and the columns spread
  // across it — so the chart always spans edge to edge, never letterboxed by a
  // fixed viewBox. When the container is too narrow for a readable column
  // pitch we trim the earliest days so the newest are always visible.
  const [view, setView] = useState<{ avail: number; trimN: number | null }>({ avail: W, trimN: null });

  useEffect(() => {
    const el = wrapRef.current;
    if (!el || trendDaily.length === 0) return;
    const update = () => {
      const avail = Math.max(1, el.clientWidth);
      if (avail >= W) {
        setView({ avail, trimN: null }); // full series, stretched to fill
        return;
      }
      // Trim: keep as many of the newest days as fit at MIN_COL pitch.
      const maxN = Math.max(1, Math.floor((avail - padL - padR) / MIN_COL) + 1);
      setView({ avail, trimN: Math.min(trendDaily.length, maxN) });
    };
    update();
    const ro = new ResizeObserver(update);
    ro.observe(el);
    return () => ro.disconnect();
  }, [trendDaily.length]);

  if (trendDaily.length === 0) return null;

  const visible = view.trimN !== null ? trendDaily.slice(-view.trimN) : trendDaily;
  const n = visible.length;
  const step =
    n > 1
      ? view.trimN !== null
        ? MIN_COL
        : (view.avail - padL - padR) / (n - 1)
      : Math.max(1, view.avail - padL - padR);
  const barW = Math.max(3, Math.min(30, step * 0.62));
  // Columns are drawn from their left edge — the leftmost column starts exactly
  // at padL — and centred on padL + barHalf + i*step, so the edge columns never
  // cover the token labels (left) or the hit-rate axis labels (right).
  const barHalf = barW / 2;
  // In stretched mode the viewBox width equals the container width (1:1), so
  // text stays 10px while bars spread to fill; trimmed mode keeps MIN_COL.
  const plotWUsed = view.trimN !== null ? padL + (n - 1) * step + barW + padR : view.avail;
  const maxTotal = Math.max(1, ...visible.map((d) => d.total));
  const ticks = niceTicks(maxTotal, 4);

  // Prompt-cache hit ratio curve: one point per day that has usage (0/0 days
  // have no ratio). The points feed a Catmull-Rom -> Bezier path so the line
  // reads as a smooth curve and simply carries across data-less days instead
  // of breaking into segments.
  const trendPoints: Array<{ x: number; y: number }> = [];
  const trendPointByDay = new Map<string, { x: number; y: number }>();
  visible.forEach((d, i) => {
    const rate = cacheRate(d.cacheHit, d.cacheMiss);
    if (rate === null) return;
    const pt = { x: padL + barHalf + i * step, y: padT + plotH - (rate / 100) * plotH };
    trendPoints.push(pt);
    trendPointByDay.set(d.day, pt);
  });
  const trendPath = smoothPath(trendPoints);
  const trendTipPt = tip ? trendPointByDay.get(tip.day) : undefined;
  const rateTicks = [0, 25, 50, 75, 100];

  // Tooltip is viewport-fixed and only clamped to the settings content pane
  // (left/right/top/bottom), so it may float over the chart freely but never
  // escapes the settings page. It sits above the column, flipping below when
  // the column is too close to the top of the pane.
  const contentRect = wrapRef.current?.closest(".settings-center__content")?.getBoundingClientRect();
  const cLeft = contentRect?.left ?? 0;
  const cRight = contentRect?.right ?? window.innerWidth;
  const cTop = contentRect?.top ?? 0;
  const cBottom = contentRect?.bottom ?? window.innerHeight;
  const TIP_W = 260;
  const tipX = tip ? Math.max(cLeft + TIP_W / 2 + 8, Math.min(tip.cx, cRight - TIP_W / 2 - 8)) : 0;
  const tipRows = tip ? Object.keys(tip.byModel).length + 1 : 0; // +1 for the cache ratio row
  const tipH = 46 + 18 * tipRows;
  const tipAbove = tip ? tip.top - cTop >= tipH + 10 : true;
  let tipY = tip ? (tipAbove ? tip.top - 10 : tip.bottom + 10) : 0;
  // "Below" is only used when it actually fits; otherwise fall back above.
  if (tip && !tipAbove && tipY + tipH > cBottom - 8) tipY = tip.top - 10;

  return (
    <section className="usage-stats__section">
      <div className="usage-stats__section-head">
        <h3 className="usage-stats__section-title">{t("settings.stats.dailyTrend")}</h3>
        {trendLimited && <span className="usage-stats__section-note">{t("settings.stats.trendLimited")}</span>}
      </div>
      <div className="usage-stats__chart-wrap" ref={wrapRef}>
        <svg className="usage-stats__chart" width="100%" height={H} viewBox={`0 0 ${plotWUsed} ${H}`} onMouseLeave={() => { setTip(null); setHover(null); }}>
          {ticks.map((tk) => {
            const y = padT + plotH - (tk / maxTotal) * plotH;
            return (
              <g key={tk}>
                <line className="usage-stats__grid" x1={padL} y1={y} x2={padL + (n - 1) * step + barW} y2={y} />
                <text className="usage-stats__axis" x={padL - 6} y={y + 3} textAnchor="end">{formatCompact(tk)}</text>
              </g>
            );
          })}
          {visible.map((d, i) => {
            const x = padL + barHalf + i * step - barW / 2;
            const modelOrder = Object.entries(d.byModel).sort((a, b) => b[1] - a[1]);
            let yBottom = padT + plotH;
            const bars = modelOrder.map(([model, tokens]) => {
              const h = (tokens / maxTotal) * plotH;
              const y = yBottom - h;
              yBottom = y;
              return { model, tokens, x, y, h, color: colorForModel(model), dimmed: hover !== null && hover !== model };
            });
            // Bars build bottom-up; the whole column is one hover target.
            const hovered = tip?.day === d.day;
            return (
              <g key={d.day}>
                {bars.map((b) => {
                  // Hovered columns widen horizontally only, centred on the bar:
                  // scaleX around the bar's own centre (transform-origin: center)
                  // so both edges grow at the same rate — the earlier width/x
                  // animation visibly bulged left first, then right.
                  return (
                    <rect
                      key={`${d.day}-${b.model}`}
                      className={`usage-stats__bar${b.dimmed ? " usage-stats__bar--dim" : ""}`}
                      x={b.x}
                      y={b.y}
                      width={barW}
                      height={b.h}
                      fill={b.color}
                      style={hovered ? { transform: `scaleX(${(barW + 3) / barW})` } : undefined}
                    />
                  );
                })}
                {/* Invisible whole-column hit target: hovering anywhere in the
                    column's vertical span (even a zero-value day) shows the
                    day's per-model breakdown, the cache hit ratio, and (when the
                    column has data) the trend point that sits on top. */}
                <rect
                  className="usage-stats__bar-hit"
                  x={x}
                  y={padT}
                  width={barW}
                  height={plotH}
                  onMouseEnter={(e) => {
                    const r = e.currentTarget.getBoundingClientRect();
                    setTip({ day: d.day, total: d.total, byModel: d.byModel, cacheHit: d.cacheHit, cacheMiss: d.cacheMiss, cx: r.left + r.width / 2, top: r.top, bottom: r.bottom });
                  }}
                  onMouseLeave={() => setTip(null)}
                />
                {(i % Math.max(1, Math.floor(n / 8)) === 0 || i === n - 1) && (
                  <text className="usage-stats__axis" x={padL + barHalf + i * step} y={H - 8} textAnchor="middle">{shortDay(d.day)}</text>
                )}
              </g>
            );
          })}
          {/* Cache hit-rate curve draws after the bars so it sits on top. */}
          <path className="usage-stats__trend" d={trendPath} fill="none" strokeWidth={2} strokeLinejoin="round" strokeLinecap="round" />
          {/* The hovered day's rate point lights up on the curve when that day
              has data; no marker is shown otherwise. */}
          {trendTipPt && (
            <circle className="usage-stats__trend-dot" cx={trendTipPt.x} cy={trendTipPt.y} r={4} />
          )}
          {/* Right-hand axis for the hit-rate scale (0/25/50/75/100%). */}
          {rateTicks.map((p) => {
            const y = padT + plotH - (p / 100) * plotH;
            return (
              <g key={`rate-${p}`}>
                <text className="usage-stats__axis usage-stats__axis--rate" x={padL + (n - 1) * step + barW + 8} y={y + 3}>{p}%</text>
              </g>
            );
          })}
        </svg>
        {tip && (
          <div className="usage-stats__tip usage-stats__tip--chart usage-stats__tip--screen" style={{ transform: `translate(${tipX}px, ${tipY}px) translate(-50%, ${tipAbove ? "-100%" : "0"})` }}>
            <div className="usage-stats__tip-title">{tip.day}</div>
            <div>{t("settings.stats.total")}: {formatTokens(tip.total)}</div>
            {Object.entries(tip.byModel).sort((a, b) => b[1] - a[1]).map(([m, v]) => (
              <div key={m} className="usage-stats__tip-row"><i className="usage-stats__legend-swatch" style={{ background: colorForModel(m) }} />{m}: {formatTokens(v)}</div>
            ))}
            <div>{t("settings.stats.cacheHitRate")}: {cacheRateText(tip.cacheHit, tip.cacheMiss)}</div>
          </div>
        )}
        <div className="usage-stats__legend">
          {Object.keys(aggregateByModel(trendDaily)).map((model) => (
            <span key={model} className="usage-stats__legend-item" onMouseEnter={() => setHover(model)} onMouseLeave={() => setHover(null)}>
              <i className="usage-stats__legend-swatch" style={{ background: colorForModel(model) }} />
              {model}
            </span>
          ))}
          <span className="usage-stats__legend-item" aria-hidden="true">
            <i className="usage-stats__legend-swatch usage-stats__legend-swatch--trend" />
            {t("settings.stats.hitRateLegend")}
          </span>
        </div>
      </div>
    </section>
  );
}

// ── Section 6: per-model donut + list ─────────────────────────────────────

function ModelUsage({ stats, t, colorForModel }: { stats: UsageStatsRange; t: UsageStatsTranslator; colorForModel: (m: string) => string }) {
  const models = stats.models;
  const [tip, setTip] = useState<{ model: string; tokens: number; percent: number; x: number; y: number } | null>(null);
  const [hover, setHover] = useState<string | null>(null);
  const donutRef = useRef<HTMLDivElement>(null);

  if (models.length === 0) return null;

  // The ring's outer edge stays fixed; thickening the stroke only grows it
  // inward (shrinking the hole), so the donut never overflows the svg viewport
  // and gets clipped into a square.
  const OUTER = 118; // ring outer radius
  const SW = 36; // ring thickness
  const R = OUTER - SW / 2; // circle path radius
  const CX = OUTER + 2; // svg centre (240x240 viewBox)
  const CIRC = 2 * Math.PI * R;
  const total = Math.max(1, stats.tokens);
  let offset = 0;

  const tipX = tip ? Math.max(110, Math.min(tip.x, (donutRef.current?.clientWidth ?? 240) - 110)) : 0;
  const tipAbove = tip ? tip.y >= 100 : true;
  const tipY = tip ? (tipAbove ? tip.y - 14 : tip.y + 14) : 0;

  return (
    <section className="usage-stats__section">
      <h3 className="usage-stats__section-title">{t("settings.stats.modelUsage")}</h3>
      <div className="usage-stats__models">
        <div className="usage-stats__donut-wrap" ref={donutRef}>
          <svg className="usage-stats__donut" width={CX * 2} height={CX * 2} viewBox={`0 0 ${CX * 2} ${CX * 2}`} role="img" aria-label={t("settings.stats.modelUsage")}>
            <circle className="usage-stats__donut-track" cx={CX} cy={CX} r={R} fill="none" strokeWidth={SW} />
            {models.map((m) => {
              const frac = m.tokens / total;
              const dash = frac * CIRC;
              const active = hover === m.model || tip?.model === m.model;
              const el = (
                <circle
                  key={m.model}
                  className={`usage-stats__donut-seg${hover !== null && !active ? " usage-stats__donut-seg--dim" : ""}`}
                  cx={CX}
                  cy={CX}
                  r={R}
                  fill="none"
                  stroke={colorForModel(m.model)}
                  strokeDasharray={`${dash} ${CIRC - dash}`}
                  strokeDashoffset={-offset}
                  transform={`rotate(-90 ${CX} ${CX})`}
                  style={{ strokeWidth: active ? SW + 5 : SW, transition: "stroke-width 0.12s ease" }}
                  onMouseEnter={(e) => {
                    const dw = donutRef.current;
                    setHover(m.model);
                    if (!dw) return;
                    const dr = dw.getBoundingClientRect();
                    setTip({ model: m.model, tokens: m.tokens, percent: m.percent, x: e.clientX - dr.left, y: e.clientY - dr.top });
                  }}
                  onMouseLeave={() => { setHover(null); setTip(null); }}
                />
              );
              offset += dash;
              return el;
            })}
            <text className="usage-stats__donut-center" x={CX} y={CX + 8} textAnchor="middle">{formatCompact(stats.tokens)}</text>
            <text className="usage-stats__donut-label" x={CX} y={CX + 26} textAnchor="middle">{t("settings.stats.tokens")}</text>
          </svg>
          {tip && (
            <div className="usage-stats__tip usage-stats__tip--chart" style={{ transform: `translate(${tipX}px, ${tipY}px) translate(-50%, ${tipAbove ? "-100%" : "0"})` }}>
              <div className="usage-stats__tip-title">{tip.model}</div>
              <div>{t("settings.stats.total")}: {formatTokens(tip.tokens)}</div>
              <div>{t("settings.stats.percent")}: {formatPercent(tip.percent)}</div>
            </div>
          )}
        </div>
        <ul className="usage-stats__model-list">
          {models.map((m) => (
            <li
              key={m.model}
              className="usage-stats__model-row"
              onMouseEnter={() => setHover(m.model)}
              onMouseLeave={() => setHover(null)}
            >
              <i className="usage-stats__legend-swatch" style={{ background: colorForModel(m.model) }} />
              <span className="usage-stats__model-name">{m.model}</span>
              <span className="usage-stats__model-provider">{providerOf(m.model)}</span>
              <span className="usage-stats__model-tokens">{formatTokens(m.tokens)}</span>
              <span className="usage-stats__model-pct">{formatPercent(m.percent)}</span>
            </li>
          ))}
        </ul>
      </div>
    </section>
  );
}

// ── formatting helpers ────────────────────────────────────────────────────

function providerOf(model: string): string {
  return model.includes("/") ? model.split("/")[0] : "default";
}

function formatCompact(n: number): string {
  if (n >= 1e9) return (n / 1e9).toFixed(1) + "B";
  if (n >= 1e6) return (n / 1e6).toFixed(1) + "M";
  if (n >= 1e3) return (n / 1e3).toFixed(1) + "k";
  return String(n);
}

function formatPercent(p: number): string {
  return (Math.round(p * 10) / 10).toFixed(1) + "%";
}

// cacheRate returns the prompt-cache hit ratio as 0..100 from cached/missed
// input tokens, or null when there is no usage to judge (0/0).
function cacheRate(hit: number, miss: number): number | null {
  const total = hit + miss;
  if (total <= 0) return null;
  return (hit / total) * 100;
}

// smoothPath builds a Catmull-Rom spline through the points, converted to
// cubic Beziers, so the hit-rate line reads as a smooth curve. Points skip
// data-less days, so the curve simply carries across them (no misleading dip).
function smoothPath(pts: Array<{ x: number; y: number }>): string {
  if (pts.length === 0) return "";
  if (pts.length === 1) return `M ${pts[0].x} ${pts[0].y}`;
  let d = `M ${pts[0].x} ${pts[0].y}`;
  for (let i = 0; i < pts.length - 1; i++) {
    const p0 = pts[i - 1] ?? pts[i];
    const p1 = pts[i];
    const p2 = pts[i + 1];
    const p3 = pts[i + 2] ?? p2;
    // Catmull-Rom tangents (tension 0.5), offset for the cubic Bezier control points.
    const c1x = p1.x + (p2.x - p0.x) / 6;
    const c1y = p1.y + (p2.y - p0.y) / 6;
    const c2x = p2.x - (p3.x - p1.x) / 6;
    const c2y = p2.y - (p3.y - p1.y) / 6;
    d += ` C ${c1x} ${c1y}, ${c2x} ${c2y}, ${p2.x} ${p2.y}`;
  }
  return d;
}

// cacheRateText formats a hit ratio for display; no data renders as "—".
function cacheRateText(hit: number, miss: number): string {
  const r = cacheRate(hit, miss);
  return r === null ? "—" : formatPercent(r);
}

// daysBetween produces local-calendar date strings (no UTC shift) so the keys
// match the backend's "2006-01-02" day keys.
function daysBetween(from: string, to: string): string[] {
  const out: string[] = [];
  const start = new Date(from + "T00:00:00");
  const end = new Date(to + "T00:00:00");
  for (let d = new Date(start); d <= end; d.setDate(d.getDate() + 1)) {
    const y = d.getFullYear();
    const m = String(d.getMonth() + 1).padStart(2, "0");
    const day = String(d.getDate()).padStart(2, "0");
    out.push(`${y}-${m}-${day}`);
  }
  return out;
}

function indexOfDay(day: string): number {
  // 0 = Monday … 6 = Sunday
  const d = new Date(day + "T00:00:00");
  return (d.getDay() + 6) % 7;
}

function shortDay(day: string): string {
  const [, m, d] = day.split("-");
  return `${Number(m)}/${Number(d)}`;
}

function aggregateByModel(daily: DailyTokenUsage[]): Record<string, number> {
  const out: Record<string, number> = {};
  for (const d of daily) {
    for (const [m, v] of Object.entries(d.byModel)) out[m] = (out[m] ?? 0) + v;
  }
  return out;
}

function niceTicks(max: number, count: number): number[] {
  const raw = max / count;
  const mag = Math.pow(10, Math.floor(Math.log10(raw)));
  const norm = raw / mag;
  const step = (norm <= 1 ? 1 : norm <= 2 ? 2 : norm <= 5 ? 5 : 10) * mag;
  const out: number[] = [];
  for (let v = step; v <= max; v += step) out.push(v);
  return out;
}
