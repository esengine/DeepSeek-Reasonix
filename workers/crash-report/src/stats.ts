import { esc, page } from "./shell";
import { type User, userNav } from "./auth";

type Daily = { date: string; users: number; opens: number };
type MetricRow = { signal: string; bucket: string; total: number };
type BarRow = { label: string; users: number };
type BarListOptions = {
  limit?: number;
  className?: string;
  labelFormatter?: (label: string) => string;
};

type OverviewCounts = {
  latestAdoptionPct: number | null;
  openReports: number;
  newLatestReports: number;
  regressedReports: number;
  criticalOpenReports: number;
};

export type StatsModule = "diagnostics" | "usage" | "preferences" | "health";

function lastDays(rows: Daily[], count: 7 | 30): Daily[] {
  const byDate = new Map(rows.map((r) => [r.date, r]));
  const out: Daily[] = [];
  for (let i = count - 1; i >= 0; i--) {
    const date = new Date(Date.now() - i * 86400000).toISOString().slice(0, 10);
    out.push(byDate.get(date) ?? { date, users: 0, opens: 0 });
  }
  return out;
}

function chartTickStep(max: number, targetTicks = 4): number {
  if (max <= targetTicks) return 1;
  const raw = Math.max(1, max) / targetTicks;
  const pow = 10 ** Math.floor(Math.log10(raw));
  const fraction = raw / pow;
  if (fraction <= 1) return pow;
  if (fraction <= 2) return 2 * pow;
  if (fraction <= 5) return 5 * pow;
  return 10 * pow;
}

function chartTickLabel(n: number): string {
  if (n >= 1_000_000) return `${Number((n / 1_000_000).toFixed(n % 1_000_000 === 0 ? 0 : 1))}m`;
  if (n >= 1_000) return `${Number((n / 1_000).toFixed(n % 1_000 === 0 ? 0 : 1))}k`;
  return String(Math.round(n));
}

function i18n(en: string, zh: string): string {
  return `<span data-i18n="en">${esc(en)}</span><span data-i18n="zh">${esc(zh)}</span>`;
}

function i18nHTML(en: string, zh: string): string {
  return `<span data-i18n="en">${en}</span><span data-i18n="zh">${zh}</span>`;
}

function dailyChart(days: Daily[]): string {
  const W = 960;
  const H = 220;
  const plotLeft = 50;
  const plotRight = 8;
  const plotTop = 16;
  const baseY = H - 26;
  const plotH = baseY - plotTop;
  const slot = (W - plotLeft - plotRight) / days.length;
  const max = Math.max(1, ...days.map((d) => d.opens));
  const step = chartTickStep(max);
  const chartMax = Math.max(step, Math.ceil(max / step) * step);
  const h = (v: number) => (v / chartMax) * plotH;
  const ticks: number[] = [];
  for (let v = 0; v <= chartMax; v += step) ticks.push(v);
  const grid = ticks
    .map((v) => {
      const y = baseY - h(v);
      return `<g><line x1="${plotLeft}" y1="${y}" x2="${W - plotRight}" y2="${y}" class="gridline"/><text x="${plotLeft - 8}" y="${y + 4}" text-anchor="end" class="ay">${chartTickLabel(v)}</text></g>`;
    })
    .join("");
  const bars = days
    .map((d, i) => {
      const x = plotLeft + i * slot;
      const label = i % 5 === 4 ? `<text x="${x + slot / 2}" y="${H - 8}" text-anchor="middle" class="ax">${d.date.slice(5)}</text>` : "";
      return `<g><title>${esc(`${d.date} — ${d.users} users · ${d.opens} opens`)}</title>
<rect x="${x}" y="${plotTop}" width="${slot}" height="${plotH}" fill="transparent" pointer-events="all"/>
<rect x="${x + slot * 0.18}" y="${baseY - h(d.opens)}" width="${slot * 0.64}" height="${h(d.opens)}" rx="3" fill="var(--accent)" opacity="0.22"/>
<rect x="${x + slot * 0.3}" y="${baseY - h(d.users)}" width="${slot * 0.4}" height="${h(d.users)}" rx="3" fill="var(--accent)"/>
${label}</g>`;
    })
    .join("");
  return `<svg class="chart" viewBox="0 0 ${W} ${H}" role="img" aria-label="Daily active installs chart"><style>.ax,.ay{font:11px var(--mono);fill:var(--ink-3)}.gridline{stroke:var(--line);stroke-width:1}</style>
${grid}${bars}</svg>`;
}

function bucketDisplayLabel(signal: string, bucket: string): string {
  if (signal.includes("_model") && bucket.startsWith("custom_")) {
    const model = bucket.slice("custom_".length).replace(/_/g, " ");
    return `<span class="bucket-prefix">custom</span><span class="bucket-main">${esc(model)}</span>`;
  }
  return esc(bucket);
}

function barRow(r: BarRow, max: number, labelFormatter?: (label: string) => string): string {
  const label = labelFormatter ? labelFormatter(r.label) : esc(r.label);
  return `<div class="row" title="${esc(r.label)}"><span class="row-label">${label}</span><div class="row-bar"><div class="bar" style="width:${Math.max(3, Math.round((r.users / max) * 100))}%"></div></div><span class="n">${r.users}</span></div>`;
}

function listBars(rows: BarRow[], options: BarListOptions = {}): string {
  if (!rows.length) return `<div class="empty">${i18n("No data in this window", "当前时间窗口暂无数据")}</div>`;
  const max = Math.max(1, ...rows.map((r) => r.users));
  const limit = options.limit ?? 5;
  const visible = limit > 0 ? rows.slice(0, limit) : rows;
  const hidden = limit > 0 ? rows.slice(limit) : [];
  const className = options.className ? ` ${esc(options.className)}` : "";
  const visibleRows = visible.map((r) => barRow(r, max, options.labelFormatter)).join("");
  if (!hidden.length) return `<div class="bars-list${className}">${visibleRows}</div>`;
  return `<div class="bars-list${className}">${visibleRows}<details class="bars-more"><summary><span class="more-closed">${i18nHTML(
    `Show ${hidden.length} more`,
    `展开 ${hidden.length} 项`,
  )}</span><span class="more-open">${i18nHTML(`Hide ${hidden.length}`, `收起 ${hidden.length} 项`)}</span></summary><div class="bars-more-list">${hidden
    .map((r) => barRow(r, max, options.labelFormatter))
    .join("")}</div></details></div>`;
}

function labelizeBucket(bucket: string): string {
  return bucket.replace(/^n_/, "").replace(/_/g, " ");
}

function sumMetric(rows: MetricRow[], signal: string): number {
  return rows.filter((r) => r.signal === signal).reduce((sum, r) => sum + r.total, 0);
}

function topMetricBucket(rows: MetricRow[], signal: string): string {
  const row = rows.filter((r) => r.signal === signal).sort((a, b) => b.total - a.total)[0];
  return row ? `${labelizeBucket(row.bucket)} · ${row.total}` : "none";
}

function cacheHitRate(rows: MetricRow[]): number | null {
  const cacheRows = rows.filter((r) => r.signal === "cache_hit");
  const total = cacheRows.reduce((sum, r) => sum + r.total, 0);
  if (!total) return null;
  const weighted = cacheRows.reduce((sum, r) => {
    const m = r.bucket.match(/^(\d+)_(\d+)$/);
    const midpoint = m ? (Number(m[1]) + Number(m[2])) / 2 : 0;
    return sum + midpoint * r.total;
  }, 0);
  return weighted / total;
}

function pct(n: number | null): string {
  if (n === null || !Number.isFinite(n)) return "n/a";
  return `${Math.round(n)}%`;
}

function ratioPer100(rows: MetricRow[], signal: string): number | null {
  const turns = sumMetric(rows, "turns");
  if (!turns) return null;
  return (sumMetric(rows, signal) / turns) * 100;
}

function deltaLabel(current: number | null, previous: number | null, suffix = ""): string {
  if (current === null || previous === null) return "new";
  const delta = current - previous;
  if (Math.abs(delta) < 0.05) return "flat";
  const sign = delta > 0 ? "+" : "";
  const rounded = Math.abs(delta) >= 10 ? Math.round(delta) : Number(delta.toFixed(1));
  return `${sign}${rounded}${suffix}`;
}

const METRIC_SIGNAL_LABELS: Record<string, { en: string; zh: string }> = {
  finish_reason: { en: "Finish reason", zh: "结束原因" },
  empty_final: { en: "Empty final guard", zh: "空回复拦截" },
  provider_error: { en: "Provider errors", zh: "Provider 错误" },
  cache_hit: { en: "Cache hit rate", zh: "缓存命中率" },
  tool_error: { en: "Tool errors", zh: "工具错误" },
  updater_error: { en: "Updater errors", zh: "更新器错误" },
  compaction: { en: "Compactions", zh: "压缩" },
  turns: { en: "Turns", zh: "轮次" },
  desktop_hang: { en: "Desktop hangs", zh: "桌面卡死" },
  desktop_hang_age: { en: "Desktop hang age", zh: "桌面卡死时长" },
  client_surface: { en: "Client surface", zh: "客户端形态" },
  client_version: { en: "Client version", zh: "客户端版本" },
  settings_language: { en: "Settings: language", zh: "设置：语言" },
  settings_desktop_layout: { en: "Settings: desktop style", zh: "设置：桌面风格" },
  settings_theme: { en: "Settings: light/dark", zh: "设置：深浅模式" },
  settings_theme_style: { en: "Settings: theme style", zh: "设置：主题" },
  settings_close_behavior: { en: "Settings: close behavior", zh: "设置：关闭行为" },
  settings_display_mode: { en: "Settings: transcript mode", zh: "设置：会话展示" },
  settings_auto_plan: { en: "Settings: auto plan", zh: "设置：自动计划" },
  settings_status_bar_style: { en: "Settings: status bar style", zh: "设置：信息栏样式" },
  settings_status_bar_items_count: { en: "Settings: status bar items", zh: "设置：信息栏项数" },
  settings_check_updates: { en: "Settings: update checks", zh: "设置：更新检查" },
  settings_default_model: { en: "Settings: default model", zh: "设置：默认模型" },
  settings_planner_model: { en: "Settings: planner model", zh: "设置：规划模型" },
  settings_subagent_model: { en: "Settings: subagent model", zh: "设置：子代理模型" },
  settings_subagent_effort: { en: "Settings: subagent effort", zh: "设置：子代理 effort" },
  settings_reasoning_language: { en: "Settings: reasoning language", zh: "设置：推理语言" },
  settings_provider_count: { en: "Settings: provider count", zh: "设置：Provider 数量" },
  settings_provider_access_count: { en: "Settings: enabled providers", zh: "设置：启用 Provider 数量" },
  settings_provider_access: { en: "Settings: provider access", zh: "设置：Provider 选择" },
  settings_bot_enabled: { en: "Bot: enabled", zh: "机器人：总开关" },
  settings_bot_model: { en: "Bot: default model", zh: "机器人：默认模型" },
  settings_bot_tool_approval: { en: "Bot: tool approval", zh: "机器人：工具审批" },
  settings_bot_allowlist: { en: "Bot: allowlist", zh: "机器人：白名单" },
  settings_bot_allow_all: { en: "Bot: allow all", zh: "机器人：允许所有人" },
  settings_bot_qq_enabled: { en: "Bot: QQ legacy", zh: "机器人：QQ 旧配置" },
  settings_bot_feishu_enabled: { en: "Bot: Feishu legacy", zh: "机器人：飞书旧配置" },
  settings_bot_weixin_enabled: { en: "Bot: Weixin legacy", zh: "机器人：微信旧配置" },
  settings_bot_connection_count: { en: "Bot: connection count", zh: "机器人：连接数量" },
  settings_bot_connection_provider: { en: "Bot: connection provider", zh: "机器人：连接渠道" },
  settings_bot_connection_enabled: { en: "Bot: connection enabled", zh: "机器人：连接开关" },
  settings_bot_connection_status: { en: "Bot: connection status", zh: "机器人：连接状态" },
  settings_bot_connection_model: { en: "Bot: connection model", zh: "机器人：连接模型" },
  settings_bot_connection_approval: { en: "Bot: connection approval", zh: "机器人：连接审批" },
};

const AGENT_METRIC_SIGNALS = [
  "finish_reason",
  "empty_final",
  "provider_error",
  "cache_hit",
  "tool_error",
  "updater_error",
  "compaction",
  "turns",
  "desktop_hang",
  "desktop_hang_age",
];
const DEFAULT_OPEN_SETTING_GROUPS = new Set(["Client", "Models", "Providers"]);

const SETTINGS_METRIC_GROUPS: { en: string; zh: string; signals: string[] }[] = [
  {
    en: "Client",
    zh: "客户端",
    signals: ["client_surface", "client_version", "settings_language"],
  },
  {
    en: "Appearance and layout",
    zh: "外观与布局",
    signals: [
      "settings_desktop_layout",
      "settings_theme",
      "settings_theme_style",
      "settings_display_mode",
      "settings_status_bar_style",
      "settings_status_bar_items_count",
    ],
  },
  {
    en: "Models",
    zh: "模型",
    signals: [
      "settings_default_model",
      "settings_planner_model",
      "settings_subagent_model",
      "settings_subagent_effort",
      "settings_reasoning_language",
    ],
  },
  {
    en: "Providers",
    zh: "Provider",
    signals: ["settings_provider_count", "settings_provider_access_count", "settings_provider_access"],
  },
  {
    en: "Behavior toggles",
    zh: "行为开关",
    signals: ["settings_close_behavior", "settings_auto_plan", "settings_check_updates"],
  },
  {
    en: "Bots",
    zh: "机器人",
    signals: [
      "settings_bot_enabled",
      "settings_bot_model",
      "settings_bot_tool_approval",
      "settings_bot_allowlist",
      "settings_bot_allow_all",
      "settings_bot_qq_enabled",
      "settings_bot_feishu_enabled",
      "settings_bot_weixin_enabled",
      "settings_bot_connection_count",
      "settings_bot_connection_provider",
      "settings_bot_connection_enabled",
      "settings_bot_connection_status",
      "settings_bot_connection_model",
      "settings_bot_connection_approval",
    ],
  },
];

function metricSignalLabel(signal: string): string {
  const label = METRIC_SIGNAL_LABELS[signal];
  return label ? i18n(label.en, label.zh) : esc(signal);
}

function metricsBySignal(rows: MetricRow[]): Map<string, { label: string; users: number }[]> {
  const bySignal = new Map<string, { label: string; users: number }[]>();
  for (const r of rows) {
    const list = bySignal.get(r.signal) ?? [];
    list.push({ label: r.bucket, users: r.total });
    bySignal.set(r.signal, list);
  }
  return bySignal;
}

function metricBlocks(bySignal: Map<string, BarRow[]>, signals: string[], options: { barLimit?: number } = {}): string {
  return signals
    .filter((signal) => bySignal.has(signal))
    .map((signal) => {
      const rows = bySignal.get(signal) ?? [];
      return `<div class="metric-block"><h3>${metricSignalLabel(signal)}<span>${rows.length}</span></h3>${listBars(rows, {
        limit: options.barLimit ?? 5,
        className: "metric-bars",
        labelFormatter: (label) => bucketDisplayLabel(signal, label),
      })}</div>`;
    })
    .join("");
}

function metricsCards(rows: MetricRow[], signals = AGENT_METRIC_SIGNALS): string {
  if (!rows.length)
    return `<div class="empty">${i18n("No metrics yet — flows in once an opt-in build ships", "暂无运行指标 — 等 opt-in 版本发布后有数据")}</div>`;
  const bySignal = metricsBySignal(rows);
  const blocks = metricBlocks(bySignal, signals);
  return blocks ? `<div class="metrics">${blocks}</div>` : `<div class="empty">${i18n("No data in this window", "当前时间窗口暂无数据")}</div>`;
}

function settingsDashboard(rows: MetricRow[], options: { collapseSections?: boolean } = {}): string {
  const bySignal = metricsBySignal(rows);
  const sections = SETTINGS_METRIC_GROUPS.map((group) => {
    const availableSignals = group.signals.filter((signal) => bySignal.has(signal));
    const blocks = metricBlocks(bySignal, group.signals);
    if (!blocks) return "";
    const heading = `<h3>${i18n(group.en, group.zh)}<span>${i18nHTML(`${availableSignals.length} metrics`, `${availableSignals.length} 项指标`)}</span></h3>`;
    if (options.collapseSections && !DEFAULT_OPEN_SETTING_GROUPS.has(group.en)) {
      return `<details class="pref-section pref-section-collapsed"><summary>${heading}</summary><div class="metrics pref-metrics">${blocks}</div></details>`;
    }
    return `<section class="pref-section">${heading}<div class="metrics pref-metrics">${blocks}</div></section>`;
  })
    .filter(Boolean)
    .join("");
  if (!sections) return `<div class="empty">${i18n("No settings preference metrics yet", "暂无设置偏好指标")}</div>`;
  return `<div class="preference-dashboard">${sections}</div>`;
}

function healthLevel(kind: "cache" | "rate", value: number | null): "good" | "warn" | "bad" | "unknown" {
  if (value === null) return "unknown";
  if (kind === "cache") {
    if (value >= 80) return "good";
    if (value >= 50) return "warn";
    return "bad";
  }
  if (value <= 1) return "good";
  if (value <= 5) return "warn";
  return "bad";
}

function countHealthLevel(value: number): "good" | "warn" | "bad" {
  if (value <= 0) return "good";
  if (value <= 2) return "warn";
  return "bad";
}

function levelText(level: "good" | "warn" | "bad" | "unknown"): string {
  if (level === "good") return i18n("Good", "健康");
  if (level === "warn") return i18n("Watch", "关注");
  if (level === "bad") return i18n("Risk", "风险");
  return i18n("No data", "暂无数据");
}

function healthCard(
  label: { en: string; zh: string },
  value: string,
  level: "good" | "warn" | "bad" | "unknown",
  deltaHTML: string,
  detailHTML: string,
): string {
  return `<div class="health-card ${level}"><div class="health-top"><span>${i18n(label.en, label.zh)}</span><b>${levelText(level)}</b></div>
<strong>${esc(value)}</strong><small>${deltaHTML}</small><p>${detailHTML}</p></div>`;
}

function healthDeltaHTML(value: string): string {
  return i18nHTML(`${esc(value)} vs previous window`, `${esc(value)} 较上一窗口`);
}

function healthDetailHTML(rows: MetricRow[], signal: string): string {
  return i18nHTML(`${esc(topMetricBucket(rows, signal))} top bucket`, `主要分桶：${esc(topMetricBucket(rows, signal))}`);
}

function agentHealth(rows: MetricRow[], previousRows: MetricRow[]): string {
  if (!rows.length) return `<div class="empty">${i18n("No agent health metrics yet", "暂无运行健康指标")}</div>`;
  const cache = cacheHitRate(rows);
  const prevCache = cacheHitRate(previousRows);
  const desktopHangs = sumMetric(rows, "desktop_hang");
  const prevDesktopHangs = sumMetric(previousRows, "desktop_hang");
  const rateCard = (signal: string, en: string, zh: string) => {
    const value = ratioPer100(rows, signal);
    const prev = ratioPer100(previousRows, signal);
    return healthCard(
      { en, zh },
      value === null ? "n/a" : `${Number(value.toFixed(value < 10 ? 1 : 0))}/100`,
      healthLevel("rate", value),
      healthDeltaHTML(deltaLabel(value, prev, "/100")),
      healthDetailHTML(rows, signal),
    );
  };
  return `<div class="health-grid">
${healthCard(
  { en: "Cache hit rate", zh: "缓存命中率" },
  pct(cache),
  healthLevel("cache", cache),
  healthDeltaHTML(deltaLabel(cache, prevCache, "pp")),
  healthDetailHTML(rows, "cache_hit"),
)}
${rateCard("provider_error", "Provider errors", "Provider 错误")}
${rateCard("tool_error", "Tool errors", "工具错误")}
${rateCard("empty_final", "Empty final guard", "空回复拦截")}
${rateCard("compaction", "Compactions", "压缩")}
${healthCard(
  { en: "Desktop hangs", zh: "桌面卡死" },
  String(desktopHangs),
  countHealthLevel(desktopHangs),
  healthDeltaHTML(deltaLabel(desktopHangs, prevDesktopHangs)),
  healthDetailHTML(rows, "desktop_hang_age"),
)}
</div>`;
}

function statusPill(status: string): string {
  if (status === "resolved") return `<span class="pill resolved">resolved</span>`;
  if (status === "ignored") return `<span class="pill ignored">ignored</span>`;
  return "";
}

type CrashRow = {
  fingerprint: string;
  kind: string;
  count: number;
  first_version: string;
  last_version: string;
  seen: string;
  status: string;
  title: string;
  source: string;
  label: string;
  error_type: string;
  top_frame: string;
  severity: string;
  last_os: string;
  last_arch: string;
  regressed_at: string;
};

function clip(s: string, n: number): string {
  return s.length > n ? `${s.slice(0, n - 1)}…` : s;
}

function filterTab(label: string, zhLabel: string, href: string, active: boolean): string {
  return `<a class="filter-tab${active ? " active" : ""}" href="${esc(href)}">${i18n(label, zhLabel)}</a>`;
}

function facetChip(row: { label: string; users: number }, active: string, hrefFor: (label: string) => string): string {
  const label = row.label || "legacy";
  return `<a class="facet-chip${active === row.label ? " active" : ""}" href="${esc(hrefFor(row.label))}" title="${esc(label)}"><span class="facet-label">${esc(label)}</span><b>${row.users}</b></a>`;
}

function facetChips(rows: { label: string; users: number }[], active: string, hrefFor: (label: string) => string, limit = 5): string {
  if (!rows.length) return `<span class="filter-empty">${i18n("none", "暂无")}</span>`;
  const visible = rows.slice(0, limit);
  const activeRow = active ? rows.find((r) => r.label === active) : undefined;
  if (activeRow && !visible.some((r) => r.label === activeRow.label)) visible.push(activeRow);
  const visibleKeys = new Set(visible.map((r) => r.label));
  const hidden = rows.filter((r) => !visibleKeys.has(r.label));
  const chips = visible.map((r) => facetChip(r, active, hrefFor)).join("");
  if (!hidden.length) return chips;
  return `${chips}<details class="facet-more"><summary>${i18nHTML(`More ${hidden.length}`, `更多 ${hidden.length}`)}</summary><div class="facet-more-list">${hidden
    .map((r) => facetChip(r, active, hrefFor))
    .join("")}</div></details>`;
}

function statCard(label: { en: string; zh: string }, value: string, note: string, href: string, tone = ""): string {
  return `<a class="overview-card ${tone}" href="${esc(href)}"><span>${i18n(label.en, label.zh)}</span><strong>${esc(value)}</strong><small>${note}</small></a>`;
}

function latestVersionShare(adoptionPct: number | null): string {
  return adoptionPct === null ? "n/a" : `${Math.round(adoptionPct)}%`;
}

function topSeverityTone(openReports: number, regressedReports: number, criticalOpenReports: number): string {
  if (criticalOpenReports || regressedReports) return "bad";
  if (openReports) return "warn";
  return "good";
}

function navLink(href: string, label: { en: string; zh: string }, active = false): string {
  return `<a${active ? ` class="active" aria-current="page"` : ""} href="${esc(href)}">${i18n(label.en, label.zh)}</a>`;
}

function preferencePanel(title: string, body: string, active: boolean): string {
  return `<section class="module-panel preference-panel${active ? " active" : ""}"${active ? ` aria-current="true"` : ""}>
<h3>${title}</h3>${body}</section>`;
}

function reportGroups(rows: CrashRow[], compact = false): string {
  if (!rows.length) return `<div class="empty">${i18n("No diagnostic reports yet — that's the good kind of empty", "还没有诊断报告，这是好消息")}</div>`;
  return `<div class="crash-list${compact ? " compact" : ""}"><div class="crash-head"><span>${i18n("summary", "摘要")}</span><span>${i18n("scope", "范围")}</span><span>${i18n("health", "状态")}</span><span>${i18n("count", "次数")}</span></div>${rows
    .map((c) => {
      const platform = [c.last_os, c.last_arch].filter(Boolean).join("/");
      const versions = `${c.first_version || "?"} → ${c.last_version || "?"}`;
      const title = c.title || c.error_type || c.top_frame || c.fingerprint;
      return `<a class="crash-item" href="/stats/group/${esc(c.fingerprint)}" title="${esc(title)}">
<span class="crash-summary"><span>${c.title ? esc(clip(c.title, compact ? 88 : 120)) : `<span class="muted">${i18n("No summary captured", "暂无摘要")}</span>`}</span><small>${esc(c.fingerprint.slice(0, 8))} · ${esc(c.seen)}</small>${
        c.regressed_at ? `<em>${i18nHTML(`regressed ${esc(c.regressed_at.slice(0, 10))}`, `回归 ${esc(c.regressed_at.slice(0, 10))}`)}</em>` : ""
      }</span>
<span class="crash-scope"><small>${esc(c.source || "legacy")}</small><small>${esc(versions)}</small><small>${platform ? esc(platform) : "unknown platform"}</small></span>
<span class="crash-health"><span class="pill">${esc(c.severity || "medium")}</span><span class="pill ${c.kind === "crash" ? "crash" : ""}">${esc(c.kind)}</span>${statusPill(c.status)}</span>
<span class="crash-count">${c.count}</span>
</a>`;
    })
    .join("")}</div>`;
}

export function renderStats(
  data: {
    daily: Daily[];
    versions: { label: string; users: number }[];
    platforms: { label: string; users: number }[];
    crashes: CrashRow[];
    metrics: MetricRow[];
    previousMetrics: MetricRow[];
    metricUsers: MetricRow[];
    sources: { label: string; users: number }[];
    overview: OverviewCounts;
    latestVersion: string;
    filters: {
      status: string;
      source: string;
      version: string;
      os: string;
      platform: string;
      newLatest: boolean;
      regressed: boolean;
      windowDays: 7 | 30;
      preferenceMode: "users" | "opens";
    };
  },
  user: User,
  activeModule: StatsModule = "usage",
): string {
  const days = lastDays(data.daily, data.filters.windowDays);
  const range = data.filters.windowDays;
  const rangeText = `${range}d`;
  const totalUsers = days.at(-1)?.users ?? 0;
  const anyPing = days.some((d) => d.opens > 0);
  const agentMetrics = data.metrics.filter((r) => AGENT_METRIC_SIGNALS.includes(r.signal));
  const previousAgentMetrics = data.previousMetrics.filter((r) => AGENT_METRIC_SIGNALS.includes(r.signal));
  const settingsMetrics = data.metrics.filter((r) => r.signal === "client_surface" || r.signal === "client_version" || r.signal.startsWith("settings_"));
  const settingsMetricUsers = data.metricUsers.filter((r) => r.signal === "client_surface" || r.signal === "client_version" || r.signal.startsWith("settings_"));
  const cache = cacheHitRate(agentMetrics);
  const providerRate = ratioPer100(agentMetrics, "provider_error");
  const toolRate = ratioPer100(agentMetrics, "tool_error");
  const desktopHangs = sumMetric(agentMetrics, "desktop_hang");
  const healthWatchCount =
    [healthLevel("cache", cache), healthLevel("rate", providerRate), healthLevel("rate", toolRate)].filter((v) => v === "warn" || v === "bad").length +
    (desktopHangs > 0 ? 1 : 0);
  const modulePath = (module: StatsModule) => (module === "usage" ? "/stats" : `/stats/${module}`);
  const filterQS = (patch: Record<string, string>, module: StatsModule = activeModule) => {
    const params = new URLSearchParams();
    const put = (k: string, v: string) => {
      if (v) params.set(k, v);
    };
    put("status", data.filters.status);
    put("source", data.filters.source);
    put("version", data.filters.version);
    put("os", data.filters.os);
    put("platform", data.filters.platform);
    if (data.filters.newLatest) params.set("new", "latest");
    if (data.filters.regressed) params.set("regressed", "1");
    if (data.filters.windowDays === 7) params.set("window", "7d");
    if (module === "preferences" && data.filters.preferenceMode === "opens") params.set("prefs", "opens");
    for (const [k, v] of Object.entries(patch)) {
      if (v) params.set(k, v);
      else params.delete(k);
    }
    const qs = params.toString();
    const path = modulePath(module);
    return qs ? `${path}?${qs}` : path;
  };
  const clearFiltersHref = filterQS({ status: "", source: "", version: "", os: "", platform: "", new: "", regressed: "" });
  const hasFilters = Boolean(
    data.filters.status || data.filters.source || data.filters.version || data.filters.os || data.filters.platform || data.filters.newLatest || data.filters.regressed,
  );
  const windowControls = `<div class="segmented" aria-label="Time window">
<a class="${range === 7 ? "active" : ""}"${range === 7 ? ` aria-current="true"` : ""} href="${esc(filterQS({ window: "7d" }))}">7d</a>
<a class="${range === 30 ? "active" : ""}"${range === 30 ? ` aria-current="true"` : ""} href="${esc(filterQS({ window: "" }))}">30d</a>
</div>`;
  const preferenceControls = `<div class="segmented" aria-label="Preference metric mode">
<a class="${data.filters.preferenceMode === "users" ? "active" : ""}"${data.filters.preferenceMode === "users" ? ` aria-current="true"` : ""} href="${esc(
    filterQS({ prefs: "" }, "preferences"),
  )}">${i18n("Installs", "按安装")}</a>
<a class="${data.filters.preferenceMode === "opens" ? "active" : ""}"${data.filters.preferenceMode === "opens" ? ` aria-current="true"` : ""} href="${esc(
    filterQS({ prefs: "opens" }, "preferences"),
  )}">${i18n("Opens", "按启动")}</a>
</div>`;
  const overviewTone = topSeverityTone(data.overview.openReports, data.overview.regressedReports, data.overview.criticalOpenReports);
  const overview = `<section class="overview-grid">
${statCard({ en: "Active today", zh: "今日活跃" }, String(totalUsers), i18n("anonymous installs", "匿名安装"), filterQS({}, "usage"))}
${statCard({ en: "Latest adoption", zh: "最新版本占比" }, latestVersionShare(data.overview.latestAdoptionPct), i18nHTML(`latest ${esc(data.latestVersion || "n/a")}`, `最新 ${esc(data.latestVersion || "n/a")}`), filterQS({}, "usage"))}
${statCard({ en: "Open reports", zh: "未处理报告" }, String(data.overview.openReports), i18n("needs triage", "需要分诊"), filterQS({}, "diagnostics"), overviewTone)}
${statCard({ en: "New in latest", zh: "最新新增" }, String(data.overview.newLatestReports), i18n("first seen on latest", "首次出现在最新版"), filterQS({}, "diagnostics"), data.overview.newLatestReports ? "warn" : "good")}
${statCard({ en: "Regressions", zh: "回归问题" }, String(data.overview.regressedReports), i18n("previously resolved", "曾经解决后复现"), filterQS({}, "diagnostics"), data.overview.regressedReports ? "bad" : "good")}
${statCard({ en: "Agent health", zh: "运行健康" }, healthWatchCount ? String(healthWatchCount) : "OK", i18nHTML(`${pct(cache)} cache · ${providerRate === null ? "n/a" : Number(providerRate.toFixed(1))}/100 provider · ${desktopHangs} hangs`, `${pct(cache)} 缓存 · ${providerRate === null ? "n/a" : Number(providerRate.toFixed(1))}/100 Provider · ${desktopHangs} 次卡死`), filterQS({}, "health"), healthWatchCount ? "warn" : "good")}
</section>`;
  const pageOverview = activeModule === "usage" ? overview : "";
  const dashboardNav = `<nav class="site-nav" aria-label="Stats navigation">
${navLink(filterQS({}, "usage"), { en: "Home", zh: "主页" }, activeModule === "usage")}
${navLink(filterQS({}, "diagnostics"), { en: "Diagnostics", zh: "诊断分诊" }, activeModule === "diagnostics")}
${navLink(filterQS({}, "preferences"), { en: "Preferences", zh: "设置偏好" }, activeModule === "preferences")}
${navLink(filterQS({}, "health"), { en: "Agent Health", zh: "运行健康" }, activeModule === "health")}
</nav>`;
  const filters = `<div class="filter-card"><div class="filter-head"><h2>${i18n("Report filters", "诊断筛选")}</h2><span>${i18nHTML(`latest ${esc(data.latestVersion || "n/a")}`, `最新 ${esc(data.latestVersion || "n/a")}`)}</span></div>
<div class="filter-tabs">
${filterTab("All", "全部", clearFiltersHref, !hasFilters)}
${filterTab("Open", "未处理", filterQS({ status: "open" }), data.filters.status === "open")}
${filterTab("Resolved", "已解决", filterQS({ status: "resolved" }), data.filters.status === "resolved")}
${filterTab("Ignored", "已忽略", filterQS({ status: "ignored" }), data.filters.status === "ignored")}
${filterTab("New in latest", "最新新增", filterQS({ new: data.filters.newLatest ? "" : "latest" }), data.filters.newLatest)}
${filterTab("Regressed", "回归", filterQS({ regressed: data.filters.regressed ? "" : "1" }), data.filters.regressed)}
</div>
<div class="facet-grid">
<section><h3>${i18n("Source", "来源")}</h3><div class="facet-list">${facetChips(data.sources, data.filters.source, (label) => filterQS({ source: label }), 4)}</div></section>
<section><h3>${i18n("Version", "版本")}</h3><div class="facet-list">${facetChips(data.versions, data.filters.version, (label) => filterQS({ version: label }), 5)}</div></section>
<section><h3>${i18n("Platform", "平台")}</h3><div class="facet-list">${facetChips(data.platforms, data.filters.platform, (label) => filterQS({ platform: label }), 4)}</div></section>
</div></div>`;
  const usageModule = `<section id="usage" class="card full module-card"><div class="module-head"><div><span>${i18n("Module", "模块")}</span><h2>${i18n("Usage distribution", "使用分布")}</h2></div></div>
<div class="module-panel wide"><h3>${i18nHTML(`Daily active installs <b>— ${rangeText}</b> (solid: users, faded: opens)`, `每日活跃 <b>— ${rangeText}</b>（实线：用户，淡色：打开次数）`)}</h3>
${anyPing ? dailyChart(days) : `<div class="empty">${i18n("No pings yet — data starts flowing once a telemetry-enabled build ships", "暂无启动 ping — 等带统计的版本发布后这里开始有数据")}</div>`}</div>
<div class="module-split">
<section class="module-panel"><h3>${i18nHTML(`Versions <b>— ${rangeText}</b>`, `版本分布 <b>— ${rangeText}</b>`)}</h3>${listBars(data.versions)}</section>
<section class="module-panel"><h3>${i18nHTML(`Platforms <b>— ${rangeText}</b>`, `平台分布 <b>— ${rangeText}</b>`)}</h3>${listBars(data.platforms)}</section>
</div></section>`;
  const diagnosticsModule = `<section id="diagnostics" class="card full module-card"><div class="module-head"><div><span>${i18n("Module", "模块")}</span><h2>${i18n("Diagnostic triage", "诊断分诊")}</h2></div><a class="module-action" href="#top">${i18n("Back to overview", "回到概览")}</a></div>
<section class="module-panel"><h3>${i18nHTML("Needs attention <b>— top 10 prioritized diagnostics</b>", "优先处理 <b>— 最需要看的 10 条诊断</b>")}</h3>${reportGroups(data.crashes.slice(0, 10), true)}</section>
${filters}
<section class="module-panel"><h3>${i18nHTML("All report groups <b>— open, regression, severity, count, recency</b>", "全部诊断分组 <b>— 未处理、回归、严重性、次数和最近出现</b>")}</h3>${reportGroups(data.crashes)}</section>
</section>`;
  const installsPanel = preferencePanel(
    i18nHTML(`Deduplicated installs <b>— ${rangeText}</b>`, `按安装去重 <b>— ${rangeText}</b>`),
    settingsDashboard(settingsMetricUsers, { collapseSections: true }),
    data.filters.preferenceMode === "users",
  );
  const opensPanel = preferencePanel(
    i18nHTML(`Launch/open snapshots <b>— ${rangeText}</b>`, `启动/开启快照 <b>— ${rangeText}</b>`),
    settingsDashboard(settingsMetrics, { collapseSections: true }),
    data.filters.preferenceMode === "opens",
  );
  const preferencePanels = data.filters.preferenceMode === "opens" ? `${opensPanel}${installsPanel}` : `${installsPanel}${opensPanel}`;
  const preferencesModule = `<section id="preferences" class="card full module-card"><div class="module-head"><div><span>${i18n("Module", "模块")}</span><h2>${i18n("Settings preferences", "设置偏好")}</h2></div><div class="module-actions">${preferenceControls}</div></div>
<div class="preference-compare">${preferencePanels}</div></section>`;
  const healthModule = `<section id="health" class="card full module-card"><div class="module-head"><div><span>${i18n("Module", "模块")}</span><h2>${i18n("Agent health", "运行健康")}</h2></div><div class="module-actions"><a class="module-action" href="${esc(filterQS({}, "preferences"))}">${i18n("Preferences", "设置偏好")}</a></div></div>
<section class="module-panel"><h3>${i18nHTML(`Health summary <b>— ${rangeText}, compared with previous window</b>`, `健康摘要 <b>— ${rangeText}，对比上一窗口</b>`)}</h3>${agentHealth(agentMetrics, previousAgentMetrics)}</section>
<section class="module-panel"><h3>${i18nHTML(`Signal distributions <b>— ${rangeText}, opt-in aggregate</b>`, `信号分布 <b>— ${rangeText}，opt-in 汇总</b>`)}</h3>${metricsCards(agentMetrics)}</section>
</section>`;
  const activeModuleHTML: Record<StatsModule, string> = {
    diagnostics: diagnosticsModule,
    usage: usageModule,
    preferences: preferencesModule,
    health: healthModule,
  };

  return page(
    "Reasonix · Crash & Telemetry",
    "health",
    `${dashboardNav}
<div id="top" class="hero-line"><div><h1>${i18n("Crash & Telemetry", "桌面端健康看板")}</h1><p class="sub">${i18nHTML(
      `${rangeText} window · anonymous launch pings, opt-in aggregate metrics, and user-sent diagnostic reports only`,
      `${rangeText} 时间窗口 · 仅包含匿名启动 ping、opt-in 汇总指标和用户发送的诊断报告`,
    )}</p></div>${windowControls}</div>
${pageOverview}
<div class="grid">
${activeModuleHTML[activeModule]}
</div>`,
    userNav(user),
  );
}

function fmtDevice(deviceJSON: string): string {
  try {
    const d = JSON.parse(deviceJSON) as { osVersion?: string; cpu?: string; cores?: number; ramGb?: number };
    return [d.osVersion, d.cpu, d.cores ? `${d.cores} cores` : "", d.ramGb ? `${d.ramGb} GB RAM` : ""]
      .filter(Boolean)
      .join(" · ");
  } catch {
    return "";
  }
}

export type Group = {
  fingerprint: string;
  kind: string;
  count: number;
  first_seen: string;
  last_seen: string;
  first_version: string;
  last_version: string;
  status: string;
  note: string;
  title: string;
  source: string;
  label: string;
  error_type: string;
  top_frame: string;
  severity: string;
  last_os: string;
  last_arch: string;
  last_build_commit: string;
  last_channel: string;
  resolved_in: string;
  resolved_at: string;
  regressed_at: string;
};

type ReportSample = {
  version: string;
  os: string;
  arch: string;
  message: string;
  device: string;
  created_at: string;
  source: string;
  label: string;
  error_type: string;
  error_message: string;
  top_frame: string;
  build_commit: string;
  channel: string;
  language: string;
  view: string;
  breadcrumbs: string;
  component_stack: string;
  stack: string;
  occurred_at: string;
};

function manageGroup(group: Group): string {
  const fp = esc(group.fingerprint);
  const setStatus = (s: string, label: string, zhLabel: string, cls: string) =>
    group.status === s
      ? ""
      : `<form method="post" action="/stats/group/${fp}" class="inline"><input type="hidden" name="action" value="status"><input type="hidden" name="status" value="${s}"><button class="btn ${cls} sm" type="submit">${i18n(label, zhLabel)}</button></form>`;
  return `<div class="card full manage-card"><div class="manage-head"><h2>${i18nHTML("Manage <b>— admin</b>", "管理 <b>— 管理员</b>")}</h2><div class="manage-actions">${setStatus("resolved", "Mark resolved", "标记已解决", "ghost")}${setStatus("ignored", "Ignore", "忽略", "ghost")}${setStatus("open", "Reopen", "重新打开", "ghost")}
<form method="post" action="/stats/group/${fp}" class="inline" onsubmit="return confirm('Delete this crash group and all its samples?')"><input type="hidden" name="action" value="delete"><button class="btn danger sm" type="submit">${i18n("Delete group", "删除分组")}</button></form></div></div>
<div class="manage-grid">
<form method="post" action="/stats/group/${fp}" class="manage-form"><input type="hidden" name="action" value="resolution"><label>${i18n("Resolved in", "解决版本")}<input type="text" name="resolvedIn" placeholder="v1.10.1" value="${esc(group.resolved_in)}"></label><button class="btn sm" type="submit">${i18n("Save", "保存")}</button></form>
<form method="post" action="/stats/group/${fp}" class="manage-form"><input type="hidden" name="action" value="severity"><label>${i18n("Severity", "严重级别")}<select name="severity"><option${group.severity === "low" ? " selected" : ""}>low</option><option${group.severity === "medium" ? " selected" : ""}>medium</option><option${group.severity === "high" ? " selected" : ""}>high</option><option${group.severity === "critical" ? " selected" : ""}>critical</option></select></label><button class="btn sm" type="submit">${i18n("Save", "保存")}</button></form>
<form method="post" action="/stats/group/${fp}" class="manage-form wide"><input type="hidden" name="action" value="note"><label>${i18n("Note", "备注")}<input type="text" name="note" placeholder="${esc("Add investigation note")}" value="${esc(group.note)}"></label><button class="btn sm" type="submit">${i18n("Save", "保存")}</button></form>
</div></div>`;
}

function breadcrumbsList(json: string): string {
  try {
    const rows = JSON.parse(json) as { cat?: string; msg?: string }[];
    if (!Array.isArray(rows) || rows.length === 0) return "";
    return `<details class="sample-nested"><summary>${i18n("breadcrumbs", "面包屑")}</summary><pre>${esc(rows.map((b) => `[${b.cat ?? ""}] ${b.msg ?? ""}`).join("\n"))}</pre></details>`;
  } catch {
    return "";
  }
}

function sampleReport(r: ReportSample, i: number): string {
  const dev = fmtDevice(r.device);
  const platform = [r.os, r.arch].filter(Boolean).join("/");
  const title = r.error_message || r.message.split("\n").find((line) => line.trim()) || r.error_type || "sample";
  const structured = [
    r.source && [i18n("source", "来源"), r.source],
    r.label && [i18n("label", "标签"), r.label],
    r.error_type && [i18n("type", "类型"), r.error_type],
    r.top_frame && [i18n("top", "顶层"), r.top_frame],
    r.build_commit && [i18n("build", "构建"), r.build_commit],
    r.channel && [i18n("channel", "渠道"), r.channel],
    r.view && [i18n("view", "视图"), r.view],
  ]
    .filter(Boolean)
    .map(([label, value]) => `<span><b>${label}</b>${esc(value)}</span>`)
    .join("");
  const stack = r.stack || r.component_stack;
  return `<details class="sample" ${i === 0 ? "open" : ""}><summary>
<span class="sample-id"><b>${esc(r.version)}</b><small>${esc(platform || "unknown platform")}</small></span>
<span class="sample-title">${esc(clip(title, 110))}</span>
<span class="sample-time">${esc((r.occurred_at || r.created_at).slice(0, 19).replace("T", " "))}</span>
</summary>
<div class="sample-body">
<div class="sample-meta">${dev ? `<span><b>${i18n("device", "设备")}</b>${esc(dev)}</span>` : ""}${structured}</div>
<div class="sample-actions"><button class="btn ghost sm copy-btn" type="button" data-copy="${esc(r.message)}"><span class="copy-label">${i18n("Copy message", "复制消息")}</span></button>${
    stack
      ? `<button class="btn ghost sm copy-btn" type="button" data-copy="${esc(stack)}"><span class="copy-label">${i18n("Copy stack", "复制堆栈")}</span></button>`
      : ""
  }</div>
<pre>${esc(r.message)}</pre>
${stack ? `<details class="sample-nested"><summary>${i18n("stack", "堆栈")}</summary><pre>${esc(stack)}</pre></details>` : ""}
${breadcrumbsList(r.breadcrumbs)}
</div></details>`;
}

function sampleReports(reports: ReportSample[], options: { limit?: number } = {}): string {
  if (!reports.length) return `<div class="empty">${i18n("No raw samples stored for this group", "这个分组没有保存原始样本")}</div>`;
  const limit = options.limit ?? 10;
  const visible = reports.slice(0, limit);
  const hidden = reports.slice(limit);
  const visibleSamples = visible.map((r, i) => sampleReport(r, i)).join("");
  const hiddenSamples = hidden.map((r, i) => sampleReport(r, i + limit)).join("");
  const history =
    hidden.length > 0
      ? `<details class="sample-more"><summary>${i18nHTML(`Historical samples ${hidden.length}`, `历史样本 ${hidden.length}`)}</summary><div class="sample-more-list">${hiddenSamples}</div></details>`
      : "";
  return `<div class="sample-list">${visibleSamples}${history}</div>`;
}

export function renderGroup(
  group: Group,
  reports: ReportSample[],
  user: User,
): string {
  const samples = sampleReports(reports);
  const platform = [group.last_os, group.last_arch].filter(Boolean).join("/");
  const status = statusPill(group.status) || `<span class="pill open">${i18n("open", "未处理")}</span>`;
  const tags = [
    [i18n("source", "来源"), group.source || "legacy"],
    group.label && [i18n("label", "标签"), group.label],
    group.error_type && [i18n("type", "类型"), group.error_type],
    group.top_frame && [i18n("top frame", "顶层帧"), group.top_frame],
    platform && [i18n("platform", "平台"), platform],
    group.last_build_commit && [i18n("build", "构建"), group.last_build_commit],
    group.last_channel && [i18n("channel", "渠道"), group.last_channel],
  ]
    .filter(Boolean)
    .map(([label, value]) => `<span><b>${label}</b>${esc(value)}</span>`)
    .join("");
  const metrics = [
    [i18n("Occurrences", "出现次数"), String(group.count)],
    [i18n("First seen", "首次出现"), `${group.first_seen.slice(0, 10)} · ${group.first_version || "?"}`],
    [i18n("Last seen", "最近出现"), `${group.last_seen.slice(0, 10)} · ${group.last_version || "?"}`],
    [i18n("Version range", "版本范围"), `${group.first_version || "?"} → ${group.last_version || "?"}`],
    group.resolved_in && [i18n("Resolved in", "解决版本"), group.resolved_in],
    group.regressed_at && [i18n("Regressed", "回归时间"), group.regressed_at.slice(0, 10)],
  ]
    .filter(Boolean)
    .map(([label, value]) => `<div><span>${label}</span><b>${esc(value)}</b></div>`)
    .join("");

  return page(
    `Reasonix · ${group.fingerprint.slice(0, 8)}`,
    `stats / ${group.fingerprint.slice(0, 8)}`,
    `<section class="group-hero"><div class="group-nav"><a class="back" href="/stats">${i18n("Back to stats", "返回统计")}</a><button class="btn ghost sm copy-btn" type="button" data-copy="${esc(group.fingerprint)}"><span class="copy-label">${i18n("Copy fingerprint", "复制指纹")}</span></button></div>
<div class="group-title"><span class="pill ${group.kind === "crash" ? "crash" : ""}">${esc(group.kind)}</span><h1>${esc(group.fingerprint.slice(0, 8))}</h1>${status}</div>
${group.title ? `<p class="summary group-summary">${esc(group.title)}</p>` : ""}
<div class="group-tags">${tags}</div>
<div class="group-metrics">${metrics}</div>
${group.note ? `<p class="group-note">${i18n("Note", "备注")}: ${esc(group.note)}</p>` : ""}</section>
<div class="card full sample-card"><h2>${i18nHTML("Samples <b>— newest first, first sample plus latest 5 kept</b>", "样本 <b>— 最新优先，保留首个样本和最近 5 个</b>")}</h2>${samples}</div>
${user.role === "admin" ? manageGroup(group) : ""}
<a class="back" href="/stats">${i18n("Back to stats", "返回统计")}</a>`,
    userNav(user),
  );
}
