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

export function i18n(en: string, zh: string): string {
  return `<span data-i18n="en">${esc(en)}</span><span data-i18n="zh">${esc(zh)}</span>`;
}

export function i18nHTML(en: string, zh: string): string {
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
  updater_event: { en: "Updater events", zh: "更新器事件" },
  compaction: { en: "Compactions", zh: "压缩" },
  turns: { en: "Turns", zh: "轮次" },
  desktop_hang: { en: "Desktop hangs", zh: "桌面卡死" },
  desktop_hang_age: { en: "Desktop hang age", zh: "桌面卡死时长" },
  desktop_exit: { en: "Desktop exits", zh: "桌面退出" },
  desktop_exit_phase: { en: "Abnormal exit phase", zh: "异常退出阶段" },
  desktop_uptime: { en: "Uptime before exit", zh: "退出前运行时长" },
  desktop_install: { en: "Install profile", zh: "安装方式" },
  desktop_update_transition: { en: "Update transition", zh: "升级阶段" },
  desktop_restore: { en: "Window restore", zh: "窗口恢复" },
  desktop_webview2_failure: { en: "WebView2 failures", zh: "WebView2 故障" },
  desktop_web_runtime_failure: { en: "Web runtime failures", zh: "Web Runtime 故障" },
  desktop_web_runtime_outcome: { en: "Web runtime outcomes", zh: "Web Runtime 结果" },
  recovery_failure: { en: "Recovery failures", zh: "恢复失败" },
  recovery_rule_continue: { en: "Rule recovery continues", zh: "规则恢复继续" },
  recovery_review_continue: { en: "Review recovery continues", zh: "复核恢复继续" },
  recovery_human_prompt: { en: "Recovery prompts", zh: "恢复询问" },
  recovery_human_continue: { en: "Human recovery continues", zh: "人工恢复继续" },
  recovery_human_revise: { en: "Human recovery revisions", zh: "人工恢复修订" },
  recovery_review_error: { en: "Recovery review errors", zh: "恢复复核错误" },
  recovery_repeat_prompt: { en: "Repeated recovery prompts", zh: "重复恢复询问" },
  recovery_review_latency: { en: "Recovery review latency", zh: "恢复复核耗时" },
  client_surface: { en: "Client surface", zh: "客户端形态" },
  client_version: { en: "Client version", zh: "客户端版本" },
  settings_language: { en: "Settings: language", zh: "设置：语言" },
  settings_desktop_layout: { en: "Settings: desktop style", zh: "设置：桌面风格" },
  settings_theme: { en: "Settings: light/dark", zh: "设置：深浅模式" },
  settings_theme_style: { en: "Settings: theme style", zh: "设置：主题" },
  settings_close_behavior: { en: "Settings: close behavior", zh: "设置：关闭行为" },
  settings_display_mode: { en: "Settings: transcript mode", zh: "设置：会话展示" },
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
  cli_mode: { en: "CLI mode", zh: "CLI 模式" },
  cli_profile: { en: "CLI profile", zh: "CLI 配置档" },
  cli_permission_mode: { en: "CLI permission mode", zh: "CLI 权限模式" },
  cli_session_mode: { en: "CLI session mode", zh: "CLI 会话模式" },
  cli_turn_latency: { en: "CLI turn latency", zh: "CLI turn 延迟" },
  cli_exit: { en: "CLI turn outcome", zh: "CLI turn 结果" },
};

const AGENT_METRIC_SIGNALS = [
  "finish_reason",
  "empty_final",
  "provider_error",
  "cache_hit",
  "tool_error",
  "updater_error",
  "updater_event",
  "compaction",
  "turns",
  "desktop_hang",
  "desktop_hang_age",
  "desktop_exit",
  "desktop_exit_phase",
  "desktop_uptime",
  "desktop_install",
  "desktop_update_transition",
  "desktop_restore",
  "desktop_webview2_failure",
  "desktop_web_runtime_failure",
  "desktop_web_runtime_outcome",
  "cli_turn_latency",
  "cli_exit",
  "recovery_failure",
  "recovery_rule_continue",
  "recovery_review_continue",
  "recovery_human_prompt",
  "recovery_human_continue",
  "recovery_human_revise",
  "recovery_review_error",
  "recovery_repeat_prompt",
  "recovery_review_latency",
];
const DEFAULT_OPEN_SETTING_GROUPS = new Set(["Client", "Models", "Providers"]);

const SETTINGS_METRIC_GROUPS: { en: string; zh: string; signals: string[] }[] = [
  {
    en: "Client",
    zh: "客户端",
    signals: ["client_surface", "client_version", "settings_language", "cli_mode", "cli_profile", "cli_permission_mode", "cli_session_mode"],
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
    signals: ["settings_close_behavior", "settings_check_updates"],
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
  const abnormalExits = rows.filter((r) => r.signal === "desktop_exit" && r.bucket === "abnormal").reduce((sum, r) => sum + r.total, 0);
  const prevAbnormalExits = previousRows.filter((r) => r.signal === "desktop_exit" && r.bucket === "abnormal").reduce((sum, r) => sum + r.total, 0);
  const webRuntimeFailures = sumMetric(rows, "desktop_web_runtime_failure") + sumMetric(rows, "desktop_webview2_failure");
  const prevWebRuntimeFailures = sumMetric(previousRows, "desktop_web_runtime_failure") + sumMetric(previousRows, "desktop_webview2_failure");
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
${healthCard(
  { en: "Abnormal desktop exits", zh: "桌面异常退出" },
  String(abnormalExits),
  countHealthLevel(abnormalExits),
  healthDeltaHTML(deltaLabel(abnormalExits, prevAbnormalExits)),
  healthDetailHTML(rows, "desktop_exit_phase"),
)}
${healthCard(
  { en: "Web runtime process failures", zh: "Web Runtime 进程故障" },
  String(webRuntimeFailures),
  countHealthLevel(webRuntimeFailures),
  healthDeltaHTML(deltaLabel(webRuntimeFailures, prevWebRuntimeFailures)),
  healthDetailHTML(rows, sumMetric(rows, "desktop_web_runtime_failure") ? "desktop_web_runtime_failure" : "desktop_webview2_failure"),
)}
</div>`;
}

export function statusPill(status: string): string {
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
  last_channel: string;
  regressed_at: string;
  development?: boolean;
  affected_installs?: number;
  window_events?: number;
  identified_events?: number;
  identity_coverage?: number;
  dimension_coverage?: number;
  impact_rate?: number | null;
};

export function clip(s: string, n: number): string {
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

function reportGroups(rows: CrashRow[], compact = false): string {
  if (!rows.length) return `<div class="empty">${i18n("No diagnostic reports yet — that's the good kind of empty", "还没有诊断报告，这是好消息")}</div>`;
  return `<div class="crash-list${compact ? " compact" : ""}"><div class="crash-head"><span>${i18n("summary", "摘要")}</span><span>${i18n("scope", "范围")}</span><span>${i18n("health", "状态")}</span><span title="${i18n("Window events and affected installs; lifetime count remains visible for historical context", "窗口事件数和受影响安装数；同时保留全生命周期累计次数")}">${i18n("window / lifetime", "窗口 / 累计")}</span></div>${rows
    .map((c) => {
      const platform = [c.last_os, c.last_arch].filter(Boolean).join("/");
      const versions = `${c.first_version || "?"} → ${c.last_version || "?"}`;
      const title = c.title || c.error_type || c.top_frame || c.fingerprint;
      return `<a class="crash-item" href="/stats/group/${esc(c.fingerprint)}" title="${esc(title)}">
<span class="crash-summary"><span>${c.title ? esc(clip(c.title, compact ? 88 : 120)) : `<span class="muted">${i18n("No summary captured", "暂无摘要")}</span>`}</span><small>${esc(c.fingerprint.slice(0, 8))} · ${esc(c.seen)}</small>${
        c.regressed_at ? `<em>${i18nHTML(`regressed ${esc(c.regressed_at.slice(0, 10))}`, `回归 ${esc(c.regressed_at.slice(0, 10))}`)}</em>` : ""
      }</span>
<span class="crash-scope"><small>${esc(c.source || "legacy")}</small><small>${esc(versions)}</small><small>${platform ? esc(platform) : "unknown platform"}</small>${c.last_channel && c.last_channel !== "stable" ? `<small>${esc(c.last_channel)}</small>` : ""}</span>
<span class="crash-health"><span class="pill">${esc(c.severity || "medium")}</span><span class="pill ${c.kind === "crash" ? "crash" : ""}">${esc(c.kind)}</span>${statusPill(c.status)}</span>
<span class="crash-count"><b>${Number(c.affected_installs ?? 0)} ${i18n("installs", "安装")}</b><small>${Number(c.window_events ?? 0)} ${i18n("events", "事件")} · ${Number(c.identity_coverage ?? 0) >= 0.9 && Number(c.dimension_coverage ?? 1) >= 0.9 ? `${Math.round(Number(c.identity_coverage) * 100)}% ${i18n("identified", "已关联")}${c.impact_rate !== null && c.impact_rate !== undefined ? ` · ${(c.impact_rate * 100).toFixed(1)}% ${i18n("impact", "影响率")}` : ""}` : i18n("sample incomplete", "样本不完整")}</small><small>${c.count} ${i18n("lifetime", "累计")}</small></span>
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
    /** Oldest computed_at in the rollup; empty when the window was queried live. */
    sources: { label: string; users: number }[];
    diagnosticFacets?: {
      osBuilds: BarRow[];
      osRevisions: BarRow[];
      distros: BarRow[];
      distroVersions: BarRow[];
      kernels: BarRow[];
      sessions: BarRow[];
      architectures: BarRow[];
      channels: BarRow[];
      runtimes: BarRow[];
      runtimeEngines: BarRow[];
      failureKinds: BarRow[];
      failureReasons: BarRow[];
      exitCodes: BarRow[];
      recoveries: BarRow[];
      gpuStates: BarRow[];
    };
    installationLinkedSince?: string;
    overview: OverviewCounts;
    latestVersion: string;
    filters: {
      surface: "desktop" | "cli";
      status: string;
      source: string;
      version: string;
      os: string;
      platform: string;
      osBuild?: string;
      osRevision?: string;
      distroId?: string;
      distroVersion?: string;
      kernelVersion?: string;
      sessionType?: string;
      arch?: string;
      channel?: string;
      runtimeVersion?: string;
      runtimeEngine?: string;
      failureKind?: string;
      failureReason?: string;
      exitCode?: string;
      recovery?: string;
      gpu?: string;
      newLatest: boolean;
      regressed: boolean;
      windowDays: 7 | 30;
    };
  },
  user: User,
  activeModule: StatsModule = "usage",
): string {
  const days = lastDays(data.daily, data.filters.windowDays);
  const range = data.filters.windowDays;
  const rangeText = `${range}d`;
  const diagnosticFacets = data.diagnosticFacets ?? {
    osBuilds: [], osRevisions: [], distros: [], distroVersions: [], kernels: [], sessions: [],
    architectures: [], channels: [], runtimes: [], runtimeEngines: [],
    failureKinds: [], failureReasons: [], exitCodes: [], recoveries: [], gpuStates: [],
  };
  const totalUsers = days.at(-1)?.users ?? 0;
  const anyPing = days.some((d) => d.opens > 0);
  const agentMetrics = data.metrics.filter((r) => AGENT_METRIC_SIGNALS.includes(r.signal));
  const previousAgentMetrics = data.previousMetrics.filter((r) => AGENT_METRIC_SIGNALS.includes(r.signal));
  const isSettingsSignal = (signal: string) =>
    signal === "client_surface" || signal === "client_version" || signal.startsWith("settings_") ||
    ["cli_mode", "cli_profile", "cli_permission_mode", "cli_session_mode"].includes(signal);
  const settingsMetrics = data.metrics.filter((r) => isSettingsSignal(r.signal));
  const cache = cacheHitRate(agentMetrics);
  const providerRate = ratioPer100(agentMetrics, "provider_error");
  const toolRate = ratioPer100(agentMetrics, "tool_error");
  const desktopHangs = sumMetric(agentMetrics, "desktop_hang");
  const abnormalExits = agentMetrics
    .filter((r) => r.signal === "desktop_exit" && r.bucket === "abnormal")
    .reduce((sum, r) => sum + r.total, 0);
  const webViewFailures = sumMetric(agentMetrics, "desktop_web_runtime_failure") + sumMetric(agentMetrics, "desktop_webview2_failure");
  const healthWatchCount =
    [healthLevel("cache", cache), healthLevel("rate", providerRate), healthLevel("rate", toolRate)].filter((v) => v === "warn" || v === "bad").length +
    (desktopHangs > 0 ? 1 : 0) +
    (abnormalExits > 0 ? 1 : 0) +
    (webViewFailures > 0 ? 1 : 0);
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
    put("osBuild", data.filters.osBuild ?? "");
    put("osRevision", data.filters.osRevision ?? "");
    put("distro", data.filters.distroId ?? "");
    put("distroVersion", data.filters.distroVersion ?? "");
    put("kernel", data.filters.kernelVersion ?? "");
    put("session", data.filters.sessionType ?? "");
    put("arch", data.filters.arch ?? "");
    put("channel", data.filters.channel ?? "");
    put("runtime", data.filters.runtimeVersion ?? "");
    put("engine", data.filters.runtimeEngine ?? "");
    put("failureKind", data.filters.failureKind ?? "");
    put("reason", data.filters.failureReason ?? "");
    put("exitCode", data.filters.exitCode ?? "");
    put("recovery", data.filters.recovery ?? "");
    put("gpu", data.filters.gpu ?? "");
    put("surface", data.filters.surface === "cli" ? "cli" : "");
    if (data.filters.newLatest) params.set("new", "latest");
    if (data.filters.regressed) params.set("regressed", "1");
    if (data.filters.windowDays === 7) params.set("window", "7d");
    for (const [k, v] of Object.entries(patch)) {
      if (v) params.set(k, v);
      else params.delete(k);
    }
    const qs = params.toString();
    const path = modulePath(module);
    return qs ? `${path}?${qs}` : path;
  };
  const clearFiltersHref = filterQS({ status: "", source: "", version: "", os: "", platform: "", osBuild: "", osRevision: "", distro: "", distroVersion: "", kernel: "", session: "", arch: "", channel: "", engine: "", runtime: "", failureKind: "", reason: "", exitCode: "", recovery: "", gpu: "", new: "", regressed: "" });
  const hasFilters = Boolean(
    data.filters.status || data.filters.source || data.filters.version || data.filters.os || data.filters.platform || data.filters.osBuild || data.filters.osRevision || data.filters.distroId || data.filters.distroVersion || data.filters.kernelVersion || data.filters.sessionType || data.filters.arch || data.filters.channel || data.filters.runtimeEngine || data.filters.runtimeVersion || data.filters.failureKind || data.filters.failureReason || data.filters.exitCode || data.filters.recovery || data.filters.gpu || data.filters.newLatest || data.filters.regressed,
  );
  const windowControls = `<div class="segmented" aria-label="Time window">
<a class="${range === 7 ? "active" : ""}"${range === 7 ? ` aria-current="true"` : ""} href="${esc(filterQS({ window: "7d" }))}">7d</a>
<a class="${range === 30 ? "active" : ""}"${range === 30 ? ` aria-current="true"` : ""} href="${esc(filterQS({ window: "" }))}">30d</a>
</div>`;
  const surfaceControls = `<div class="segmented" aria-label="Client surface">
<a class="${data.filters.surface === "desktop" ? "active" : ""}"${data.filters.surface === "desktop" ? ` aria-current="true"` : ""} href="${esc(filterQS({ surface: "" }))}">${i18n("Desktop", "桌面端")}</a>
<a class="${data.filters.surface === "cli" ? "active" : ""}"${data.filters.surface === "cli" ? ` aria-current="true"` : ""} href="${esc(filterQS({ surface: "cli" }))}">CLI</a>
</div>`;
  const overviewTone = topSeverityTone(data.overview.openReports, data.overview.regressedReports, data.overview.criticalOpenReports);
  const isDevelopmentDiagnostic = (row: CrashRow) => row.development ?? row.fingerprint.startsWith("dev:");
  const releaseCrashes = data.crashes.filter(
    (row) => row.kind !== "performance" && row.severity !== "low" && !isDevelopmentDiagnostic(row),
  );
  const performanceDiagnostics = data.crashes.filter(
    (row) => row.kind === "performance" && !isDevelopmentDiagnostic(row),
  );
  const developmentDiagnostics = data.crashes.filter(isDevelopmentDiagnostic);
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
  const linkedSince = data.installationLinkedSince
    ? `<p class="muted">${i18n("Installation-linked data available since", "可关联安装数据起始于")} ${esc(data.installationLinkedSince)}</p>`
    : "";
  const filters = `<div class="filter-card"><div class="filter-head"><h2>${i18n("Report filters", "诊断筛选")}</h2><span>${i18nHTML(`latest ${esc(data.latestVersion || "n/a")}`, `最新 ${esc(data.latestVersion || "n/a")}`)}</span></div>${linkedSince}
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
<section><h3>${i18n("Windows build / revision", "Windows build / revision")}</h3><div class="facet-list">${facetChips(diagnosticFacets.osBuilds, data.filters.osBuild ?? "", (label) => filterQS({ osBuild: label }), 6)}${facetChips(diagnosticFacets.osRevisions, data.filters.osRevision ?? "", (label) => filterQS({ osRevision: label }), 4)}${data.filters.osBuild !== "17763" ? `<a class="facet-chip" href="${esc(filterQS({ osBuild: "17763" }))}"><span class="facet-label">LTSC 2019 · 17763</span></a>` : ""}</div></section>
<section><h3>${i18n("Linux distribution / session", "Linux 发行版 / 会话")}</h3><div class="facet-list">${facetChips(diagnosticFacets.distros, data.filters.distroId ?? "", (label) => filterQS({ distro: label }), 5)}${facetChips(diagnosticFacets.distroVersions, data.filters.distroVersion ?? "", (label) => filterQS({ distroVersion: label }), 4)}${facetChips(diagnosticFacets.kernels, data.filters.kernelVersion ?? "", (label) => filterQS({ kernel: label }), 4)}${facetChips(diagnosticFacets.sessions, data.filters.sessionType ?? "", (label) => filterQS({ session: label }), 4)}</div></section>
<section><h3>${i18n("Architecture / channel", "架构 / 渠道")}</h3><div class="facet-list">${facetChips(diagnosticFacets.architectures, data.filters.arch ?? "", (label) => filterQS({ arch: label }), 4)}${facetChips(diagnosticFacets.channels, data.filters.channel ?? "", (label) => filterQS({ channel: label }), 4)}</div></section>
<section><h3>Web Runtime</h3><div class="facet-list">${facetChips(diagnosticFacets.runtimeEngines, data.filters.runtimeEngine ?? "", (label) => filterQS({ engine: label }), 3)}${facetChips(diagnosticFacets.runtimes, data.filters.runtimeVersion ?? "", (label) => filterQS({ runtime: label }), 5)}</div></section>
<section><h3>${i18n("Failure kind / reason / exit", "故障类型 / 原因 / 退出码")}</h3><div class="facet-list">${facetChips(diagnosticFacets.failureKinds, data.filters.failureKind ?? "", (label) => filterQS({ failureKind: label }), 5)}${facetChips(diagnosticFacets.failureReasons, data.filters.failureReason ?? "", (label) => filterQS({ reason: label }), 5)}${facetChips(diagnosticFacets.exitCodes, data.filters.exitCode ?? "", (label) => filterQS({ exitCode: label }), 4)}</div></section>
<section><h3>${i18n("Recovery / GPU", "恢复 / GPU")}</h3><div class="facet-list">${facetChips(diagnosticFacets.recoveries, data.filters.recovery ?? "", (label) => filterQS({ recovery: label }), 4)}${facetChips(diagnosticFacets.gpuStates, data.filters.gpu ?? "", (label) => filterQS({ gpu: label }), 3)}</div></section>
</div></div>`;
  const usageModule = `<section id="usage" class="card full module-card"><div class="module-head"><div><span>${i18n("Module", "模块")}</span><h2>${i18n("Usage distribution", "使用分布")}</h2></div></div>
<div class="module-panel wide"><h3>${i18nHTML(`Daily active installs <b>— ${rangeText}</b> (solid: users, faded: opens)`, `每日活跃 <b>— ${rangeText}</b>（实线：用户，淡色：打开次数）`)}</h3>
${anyPing ? dailyChart(days) : `<div class="empty">${i18n("No pings yet — data starts flowing once a telemetry-enabled build ships", "暂无启动 ping — 等带统计的版本发布后这里开始有数据")}</div>`}</div>
<div class="module-split">
<section class="module-panel"><h3>${i18nHTML(`Versions <b>— ${rangeText}</b>`, `版本分布 <b>— ${rangeText}</b>`)}</h3>${listBars(data.versions)}</section>
<section class="module-panel"><h3>${i18nHTML(`Platforms <b>— ${rangeText}</b>`, `平台分布 <b>— ${rangeText}</b>`)}</h3>${listBars(data.platforms)}</section>
</div></section>`;
  const diagnosticsModule = `<section id="diagnostics" class="card full module-card"><div class="module-head"><div><span>${i18n("Module", "模块")}</span><h2>${i18n("Diagnostic triage", "诊断分诊")}</h2></div><a class="module-action" href="#top">${i18n("Back to overview", "回到概览")}</a></div>
<p class="sub">${i18n("Installation-linked data is available only from the diagnostics-v2 deployment date; historical device counts are not backfilled.", "可关联安装的数据仅从 diagnostics-v2 部署日起提供；历史设备数不回填。")}</p>
<section class="module-panel"><h3>${i18nHTML("Needs attention <b>— top 10 release crashes and exceptions</b>", "优先处理 <b>— 正式版崩溃与异常 Top 10</b>")}</h3>${reportGroups(releaseCrashes.slice(0, 10), true)}</section>
${performanceDiagnostics.length ? `<section class="module-panel"><h3>${i18nHTML("Performance signals <b>— tracked separately from crashes</b>", "性能信号 <b>— 与崩溃分开统计</b>")}</h3>${reportGroups(performanceDiagnostics.slice(0, 5), true)}</section>` : ""}
${developmentDiagnostics.length ? `<section class="module-panel"><h3>${i18nHTML("Development diagnostics <b>— excluded from release priority</b>", "开发版诊断 <b>— 不计入正式版优先级</b>")}</h3>${reportGroups(developmentDiagnostics.slice(0, 5), true)}</section>` : ""}
${filters}
<section class="module-panel"><h3>${i18nHTML("All report groups <b>— open, regression, severity, count, recency</b>", "全部诊断分组 <b>— 未处理、回归、严重性、次数和最近出现</b>")}</h3>${reportGroups(data.crashes)}</section>
</section>`;
  const preferencesModule = `<section id="preferences" class="card full module-card"><div class="module-head"><div><span>${i18n("Module", "模块")}</span><h2>${i18n("Settings preferences", "设置偏好")}</h2></div></div>
<section class="module-panel"><h3>${i18nHTML(`Launch/open snapshots <b>— ${rangeText}</b>`, `启动/开启快照 <b>— ${rangeText}</b>`)}</h3>${settingsDashboard(settingsMetrics, { collapseSections: true })}</section></section>`;
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
<div id="top" class="hero-line"><div><h1>${i18n("Crash & Telemetry", "客户端健康看板")}</h1><p class="sub">${i18nHTML(
      `${rangeText} window · anonymous launch pings, opt-in aggregate metrics, and user-sent diagnostic reports only`,
      `${rangeText} 时间窗口 · 仅包含匿名启动 ping、opt-in 汇总指标和用户发送的诊断报告`,
    )}</p></div><div class="module-actions">${surfaceControls}${windowControls}</div></div>
${pageOverview}
<div class="grid">
${activeModuleHTML[activeModule]}
</div>`,
    userNav(user),
  );
}
