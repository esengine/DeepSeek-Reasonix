import { esc } from "./shell";

export function i18n(en: string, zh: string): string {
  return `<span data-i18n="en">${esc(en)}</span><span data-i18n="zh">${esc(zh)}</span>`;
}

export function i18nHTML(en: string, zh: string): string {
  return `<span data-i18n="en">${en}</span><span data-i18n="zh">${zh}</span>`;
}

export type CrashRow = {
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

export function statusPill(status: string): string {
  if (status === "resolved") return `<span class="pill status-resolved">${i18n("Resolved", "已解决")}</span>`;
  if (status === "ignored") return `<span class="pill status-ignored">${i18n("Ignored", "已忽略")}</span>`;
  return `<span class="pill status-open open">${i18n("Open", "未处理")}</span>`;
}

function crashMetric(en: string, zh: string, value: string | number, className = ""): string {
  return `<span class="crash-metric${className ? ` ${className}` : ""}"><b>${esc(value)}</b><small>${i18n(en, zh)}</small></span>`;
}

export function reportGroups(rows: CrashRow[], compact = false, windowDays: 7 | 30 = 30): string {
  if (!rows.length) return `<div class="empty">${i18n("No diagnostic reports yet — that's the good kind of empty", "还没有诊断报告，这是好消息")}</div>`;
  return `<div class="crash-list${compact ? " compact" : ""}"><div class="crash-head"><span>${i18n("summary", "摘要")}</span><span>${i18n("scope", "范围")}</span><span>${i18n("health", "状态")}</span><span title="${i18n("Window events, impact rate, identity coverage, and lifetime count", "窗口事件、影响率、身份覆盖率和累计事件")}">${i18n("triage metrics", "分诊指标")}</span></div>${rows
    .map((c) => {
      const platform = [c.last_os, c.last_arch].filter(Boolean).join("/");
      const versions = `${c.first_version || "?"} → ${c.last_version || "?"}`;
      const title = c.title || c.error_type || c.top_frame || c.fingerprint;
      const identityCoverage = Number(c.identity_coverage ?? 0) >= 0.9 && Number(c.dimension_coverage ?? 1) >= 0.9;
      const impactRate = c.impact_rate !== null && c.impact_rate !== undefined ? `${(Number(c.impact_rate) * 100).toFixed(1)}%` : "—";
      return `<a class="crash-item" href="/stats/group/${esc(c.fingerprint)}" title="${esc(title)}">
<span class="crash-summary" aria-label="${esc(title)}"><span>${c.title ? esc(clip(c.title, compact ? 150 : 180)) : `<span class="muted">${i18n("No summary captured", "暂无摘要")}</span>`}</span><small>${esc(c.fingerprint.slice(0, 8))} · ${esc(c.seen)}</small>${
        c.regressed_at ? `<em>${i18nHTML(`regressed ${esc(c.regressed_at.slice(0, 10))}`, `回归 ${esc(c.regressed_at.slice(0, 10))}`)}</em>` : ""
      }</span>
<span class="crash-scope"><span class="crash-meta"><b>${i18n("Source", "来源")}</b><span>${esc(c.source || "legacy")}</span></span><span class="crash-meta"><b>${i18n("Versions", "版本")}</b><span>${esc(versions)}</span></span><span class="crash-meta"><b>${i18n("Platform", "平台")}</b><span>${platform ? esc(platform) : "unknown platform"}</span></span>${c.last_channel && c.last_channel !== "stable" ? `<span class="crash-meta"><b>${i18n("Channel", "渠道")}</b><span>${esc(c.last_channel)}</span></span>` : ""}</span>
<span class="crash-health"><span class="pill">${esc(c.severity || "medium")}</span><span class="pill ${c.kind === "crash" ? "crash" : ""}">${esc(c.kind)}</span>${statusPill(c.status)}</span>
<span class="crash-count"><span class="crash-metrics">${crashMetric(`Affected installs (${windowDays}d)`, `受影响安装（${windowDays}天）`, Number(c.affected_installs ?? 0), "primary")}${crashMetric("Impact rate", "影响率", impactRate)}${crashMetric("Window events", "窗口事件", Number(c.window_events ?? 0))}${crashMetric("Identity coverage", "身份覆盖率", identityCoverage ? `${Math.round(Number(c.identity_coverage) * 100)}%` : "sample incomplete / 样本不完整")}${crashMetric("Lifetime events", "累计事件", Number(c.count), "full")}</span></span>
</a>`;
    })
    .join("")}</div>`;
}
