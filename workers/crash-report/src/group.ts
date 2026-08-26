import { type User, userNav } from "./auth";
import { esc, page } from "./shell";
import { clip, i18n, i18nHTML, statusPill } from "./stats";

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

export type ReportSample = {
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
  webview2: string;
  web_runtime: string;
};

type GroupDiagnosticSummary = {
  windowEvents: number;
  identifiedEvents: number;
  affectedInstalls: number;
  distributions: { facet: string; value: string; installs: number; events: number }[];
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
  let webRuntime = "";
  try {
    const diagnostic = JSON.parse(r.web_runtime || r.webview2 || "") as Record<string, unknown>;
    webRuntime = Object.entries(diagnostic)
      .filter(([, value]) => value !== "" && value !== undefined && value !== null)
      .map(([key, value]) => `${key}: ${String(value)}`)
      .join("\n");
  } catch {
    webRuntime = "";
  }
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
${webRuntime ? `<details class="sample-nested"><summary>Web Runtime</summary><pre>${esc(webRuntime)}</pre></details>` : ""}
</div></details>`;
}

function sampleReports(reports: ReportSample[], options: { limit?: number } = {}): string {
  if (!reports.length) return `<div class="empty">${i18n("No raw samples stored for this group", "这个分组没有保存原始样本")}</div>`;
  const limit = options.limit ?? 10;
  const visible = reports.slice(0, limit);
  const hidden = reports.slice(limit);
  const visibleSamples = visible.map((r, i) => sampleReport(r, i)).join("");
  const hiddenSamples = hidden.map((r, i) => sampleReport(r, i + limit)).join("");
  const history = hidden.length > 0
    ? `<details class="sample-more"><summary>${i18nHTML(`Historical samples ${hidden.length}`, `历史样本 ${hidden.length}`)}</summary><div class="sample-more-list">${hiddenSamples}</div></details>`
    : "";
  return `<div class="sample-list">${visibleSamples}${history}</div>`;
}

export function renderGroup(
  group: Group,
  reports: ReportSample[],
  user: User,
  diagnostics?: GroupDiagnosticSummary,
  lifecycle?: { state: "active" | "compacted" | "archiving" | "archived"; epoch: number },
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
  ].filter(Boolean).map(([label, value]) => `<span><b>${label}</b>${esc(value)}</span>`).join("");
  const metrics = [
    [i18n("Occurrences", "出现次数"), String(group.count)],
    ...(diagnostics ? [
      [i18n("Affected installs (30d)", "受影响安装（30 天）"), String(diagnostics.affectedInstalls)],
      [i18n("Window events (30d)", "窗口事件（30 天）"), String(diagnostics.windowEvents)],
      [i18n("Identity coverage", "身份覆盖率"), diagnostics.windowEvents > 0 && diagnostics.identifiedEvents / diagnostics.windowEvents >= 0.9 ? `${Math.round(diagnostics.identifiedEvents / diagnostics.windowEvents * 100)}%` : "sample incomplete / 样本不完整"],
    ] : []),
    [i18n("First seen", "首次出现"), `${group.first_seen.slice(0, 10)} · ${group.first_version || "?"}`],
    [i18n("Last seen", "最近出现"), `${group.last_seen.slice(0, 10)} · ${group.last_version || "?"}`],
    [i18n("Version range", "版本范围"), `${group.first_version || "?"} → ${group.last_version || "?"}`],
    group.resolved_in && [i18n("Resolved in", "解决版本"), group.resolved_in],
    group.regressed_at && [i18n("Regressed", "回归时间"), group.regressed_at.slice(0, 10)],
  ].filter(Boolean).map(([label, value]) => `<div><span>${label}</span><b>${esc(value)}</b></div>`).join("");
  const distributions = diagnostics?.distributions.length
    ? `<div class="card full sample-card"><h2>${i18n("30-day technical distributions", "30 天技术分布")}</h2><div class="group-metrics">${diagnostics.distributions
        .map((row) => `<div><span>${esc(row.facet)} · ${esc(row.value)}</span><b>${row.installs} ${i18n("installs", "安装")} · ${row.events} ${i18n("events", "事件")}</b></div>`)
        .join("")}</div></div>`
    : "";
  const lifecycleNotice = lifecycle?.state === "compacted"
    ? `<div class="card full"><p>${i18n("Recent samples were removed by the 30-day policy; only the first retained-cycle sample remains.", "最近样本已按 30 天策略清理；仅保留当前保留周期的首个样本。")}</p></div>`
    : lifecycle?.state === "archiving"
      ? `<div class="card full"><p>${i18n("This group is completing its 60-day Firebase sample archive.", "该分组正在执行 60 天 Firebase 样本归档。")}</p></div>`
      : lifecycle?.state === "archived"
        ? `<div class="card full"><p>${i18n("Firebase raw samples were removed; D1 aggregates, status, notes, and audit history remain.", "Firebase 原始样本已清理；D1 聚合、状态、备注和审计仍保留。")}</p></div>`
        : lifecycle && lifecycle.epoch > 1
          ? `<div class="card full"><p>${i18nHTML(`Samples belong to retained cycle ${lifecycle.epoch}; Lifetime First Seen remains the D1 value above.`, `样本属于第 ${lifecycle.epoch} 个保留周期；Lifetime First Seen 仍以上方 D1 值为准。`)}</p></div>`
          : "";
  return page(
    `Reasonix · ${group.fingerprint.slice(0, 8)}`,
    `stats / ${group.fingerprint.slice(0, 8)}`,
    `<section class="group-hero"><div class="group-nav"><a class="back" href="/stats">${i18n("Back to stats", "返回统计")}</a><button class="btn ghost sm copy-btn" type="button" data-copy="${esc(group.fingerprint)}"><span class="copy-label">${i18n("Copy fingerprint", "复制指纹")}</span></button></div>
<div class="group-title"><span class="pill ${group.kind === "crash" ? "crash" : ""}">${esc(group.kind)}</span><h1>${esc(group.fingerprint.slice(0, 8))}</h1>${status}</div>
${group.title ? `<p class="summary group-summary">${esc(group.title)}</p>` : ""}
<div class="group-tags">${tags}</div>
<div class="group-metrics">${metrics}</div>
${group.note ? `<p class="group-note">${i18n("Note", "备注")}: ${esc(group.note)}</p>` : ""}</section>
${lifecycleNotice}
<div class="card full sample-card"><h2>${i18nHTML("Samples <b>— newest first, retained-cycle first plus latest 5 kept</b>", "样本 <b>— 最新优先，保留当前周期首个样本和最近 5 个</b>")}</h2>${samples}</div>
${distributions}
${user.role === "admin" ? manageGroup(group) : ""}
<a class="back" href="/stats">${i18n("Back to stats", "返回统计")}</a>`,
    userNav(user),
  );
}
