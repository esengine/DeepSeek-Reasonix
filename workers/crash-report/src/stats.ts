import { esc, page } from "./shell";
import { type User, userNav } from "./auth";

type Daily = { date: string; users: number; opens: number };

function last30Days(rows: Daily[]): Daily[] {
  const byDate = new Map(rows.map((r) => [r.date, r]));
  const out: Daily[] = [];
  for (let i = 29; i >= 0; i--) {
    const date = new Date(Date.now() - i * 86400000).toISOString().slice(0, 10);
    out.push(byDate.get(date) ?? { date, users: 0, opens: 0 });
  }
  return out;
}

function dailyChart(days: Daily[]): string {
  const W = 960;
  const H = 220;
  const padX = 8;
  const baseY = H - 26;
  const slot = (W - padX * 2) / days.length;
  const max = Math.max(1, ...days.map((d) => d.opens));
  const h = (v: number) => (v / max) * (H - 60);
  const bars = days
    .map((d, i) => {
      const x = padX + i * slot;
      const label = i % 5 === 4 ? `<text x="${x + slot / 2}" y="${H - 8}" text-anchor="middle" class="ax">${d.date.slice(5)}</text>` : "";
      return `<g><title>${d.date} — ${d.users} users · ${d.opens} opens</title>
<rect x="${x + slot * 0.18}" y="${baseY - h(d.opens)}" width="${slot * 0.64}" height="${h(d.opens)}" rx="3" fill="var(--accent)" opacity="0.22"/>
<rect x="${x + slot * 0.3}" y="${baseY - h(d.users)}" width="${slot * 0.4}" height="${h(d.users)}" rx="3" fill="var(--accent)"/>
${label}</g>`;
    })
    .join("");
  return `<svg class="chart" viewBox="0 0 ${W} ${H}"><style>.ax{font:11px var(--mono);fill:var(--ink-3)}</style>
<line x1="${padX}" y1="${baseY}" x2="${W - padX}" y2="${baseY}" stroke="var(--line)"/>${bars}</svg>`;
}

function listBars(rows: { label: string; users: number }[]): string {
  if (!rows.length) return `<div class="empty">No data in the last 7 days · 近 7 天暂无数据</div>`;
  const max = Math.max(1, ...rows.map((r) => r.users));
  return rows
    .map(
      (r) =>
        `<div class="row"><span>${esc(r.label)}</span><div><div class="bar" style="width:${Math.max(3, Math.round((r.users / max) * 100))}%"></div></div><span class="n">${r.users}</span></div>`,
    )
    .join("");
}

function metricsCards(rows: { signal: string; bucket: string; total: number }[]): string {
  if (!rows.length)
    return `<div class="empty">No metrics yet — flows in once an opt-in build ships · 等 opt-in 版本发布后有数据</div>`;
  const bySignal = new Map<string, { label: string; users: number }[]>();
  for (const r of rows) {
    const list = bySignal.get(r.signal) ?? [];
    list.push({ label: r.bucket, users: r.total });
    bySignal.set(r.signal, list);
  }
  return `<div class="metrics">${[...bySignal.entries()]
    .map(([signal, list]) => `<div class="metric-block"><h3>${esc(signal)}</h3>${listBars(list)}</div>`)
    .join("")}</div>`;
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

export function renderStats(
  data: {
    daily: Daily[];
    versions: { label: string; users: number }[];
    platforms: { label: string; users: number }[];
    crashes: CrashRow[];
    metrics: { signal: string; bucket: string; total: number }[];
    sources: { label: string; users: number }[];
    latestVersion: string;
    filters: { status: string; source: string; version: string; os: string; platform: string; newLatest: boolean; regressed: boolean };
  },
  user: User,
): string {
  const days = last30Days(data.daily);
  const totalUsers = days.at(-1)?.users ?? 0;
  const anyPing = days.some((d) => d.opens > 0);
  const filterQS = (patch: Record<string, string>) => {
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
    for (const [k, v] of Object.entries(patch)) {
      if (v) params.set(k, v);
      else params.delete(k);
    }
    const qs = params.toString();
    return qs ? `/stats?${qs}` : "/stats";
  };
  const sourceLinks = data.sources.length
    ? data.sources.map((s) => `<a class="chip" href="${esc(filterQS({ source: s.label }))}">${esc(s.label || "legacy")} <b>${s.users}</b></a>`).join("")
    : `<span class="muted">no sources yet</span>`;
  const versionLinks = data.versions.length
    ? data.versions.slice(0, 8).map((v) => `<a class="chip" href="${esc(filterQS({ version: v.label }))}">${esc(v.label)} <b>${v.users}</b></a>`).join("")
    : `<span class="muted">no versions yet</span>`;
  const platformLinks = data.platforms.length
    ? data.platforms.map((p) => `<a class="chip" href="${esc(filterQS({ platform: p.label }))}">${esc(p.label)} <b>${p.users}</b></a>`).join("")
    : `<span class="muted">no platforms yet</span>`;
  const filters = `<div class="card full"><h2>Report filters · 诊断筛选 <b>— latest ${esc(data.latestVersion || "n/a")}</b></h2>
<div class="actions">
<a class="btn sm ghost" href="/stats">All</a>
<a class="btn sm ghost" href="${esc(filterQS({ status: "open" }))}">Open</a>
<a class="btn sm ghost" href="${esc(filterQS({ status: "resolved" }))}">Resolved</a>
<a class="btn sm ghost" href="${esc(filterQS({ status: "ignored" }))}">Ignored</a>
<a class="btn sm ghost" href="${esc(filterQS({ new: data.filters.newLatest ? "" : "latest" }))}">New in latest</a>
<a class="btn sm ghost" href="${esc(filterQS({ regressed: data.filters.regressed ? "" : "1" }))}">Regressed</a>
</div>
<div class="actions">${sourceLinks}</div>
<div class="actions">${versionLinks}</div>
<div class="actions">${platformLinks}</div></div>`;
  const crashRows = data.crashes.length
    ? `<table><thead><tr><th>fingerprint</th><th>summary</th><th>source</th><th>severity</th><th>kind</th><th>status</th><th>count</th><th>versions</th><th>platform</th><th>last seen</th></tr></thead><tbody>${data.crashes
        .map(
          (c) =>
            `<tr><td><a class="fp" href="/stats/group/${esc(c.fingerprint)}">${esc(c.fingerprint.slice(0, 8))}</a></td><td class="summary"${c.title ? ` title="${esc(c.title)}"` : ""}>${c.title ? esc(clip(c.title, 90)) : `<span class="muted">—</span>`}${c.regressed_at ? ` <span class="pill ignored">regressed</span>` : ""}</td><td>${esc(c.source || "legacy")}</td><td><span class="pill">${esc(c.severity || "medium")}</span></td><td><span class="pill ${c.kind === "crash" ? "crash" : ""}">${esc(c.kind)}</span></td><td>${statusPill(c.status)}</td><td class="n">${c.count}</td><td class="n">${esc(c.first_version || "?")} → ${esc(c.last_version)}</td><td class="n">${esc([c.last_os, c.last_arch].filter(Boolean).join("/"))}</td><td class="n">${esc(c.seen)}</td></tr>`,
        )
        .join("")}</tbody></table>`
    : `<div class="empty">No reports yet — that's the good kind of empty · 还没有诊断报告</div>`;

  return page(
    "Reasonix · Stats",
    "stats",
    `<h1>Desktop stats</h1><p class="sub">Today: <b>${totalUsers}</b> active installs · anonymous launch pings and user-sent diagnostic reports only</p>
<div class="grid">
<div class="card full"><h2>Daily active installs · 每日活跃 <b>— 30 days</b> (solid: users, faded: opens)</h2>
${anyPing ? dailyChart(days) : `<div class="empty">No pings yet — data starts flowing once a telemetry-enabled build ships · 等带统计的版本发布后这里开始有数据</div>`}</div>
<div class="card"><h2>Versions · 版本分布 <b>— 7 days</b></h2>${listBars(data.versions)}</div>
<div class="card"><h2>Platforms · 平台分布 <b>— 7 days</b></h2>${listBars(data.platforms)}</div>
<div class="card full"><h2>Agent signals · 运行指标 <b>— 7 days, opt-in aggregate</b></h2>${metricsCards(data.metrics)}</div>
${filters}
<div class="card full"><h2>Report groups · 诊断分组 <b>— click a fingerprint for details</b></h2>${crashRows}</div>
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

function manageGroup(group: Group): string {
  const fp = esc(group.fingerprint);
  const setStatus = (s: string, label: string, cls: string) =>
    group.status === s
      ? ""
      : `<form method="post" action="/stats/group/${fp}" class="inline"><input type="hidden" name="action" value="status"><input type="hidden" name="status" value="${s}"><button class="btn ${cls} sm" type="submit">${label}</button></form>`;
  return `<div class="card full" style="margin-top:20px"><h2>Manage <b>— admin</b></h2>
<div class="actions">${setStatus("resolved", "Mark resolved", "ghost")}${setStatus("ignored", "Ignore", "ghost")}${setStatus("open", "Reopen", "ghost")}
<form method="post" action="/stats/group/${fp}" class="inline" onsubmit="return confirm('Delete this crash group and all its samples?')"><input type="hidden" name="action" value="delete"><button class="btn danger sm" type="submit">Delete group</button></form></div>
<form method="post" action="/stats/group/${fp}" class="note-edit"><input type="hidden" name="action" value="resolution"><input type="text" name="resolvedIn" placeholder="Resolved in version…" value="${esc(group.resolved_in)}"><button class="btn sm" type="submit">Save resolved version</button></form>
<form method="post" action="/stats/group/${fp}" class="note-edit"><input type="hidden" name="action" value="severity"><select name="severity"><option${group.severity === "low" ? " selected" : ""}>low</option><option${group.severity === "medium" ? " selected" : ""}>medium</option><option${group.severity === "high" ? " selected" : ""}>high</option><option${group.severity === "critical" ? " selected" : ""}>critical</option></select><button class="btn sm" type="submit">Save severity</button></form>
<form method="post" action="/stats/group/${fp}" class="note-edit"><input type="hidden" name="action" value="note"><input type="text" name="note" placeholder="Add a note…" value="${esc(group.note)}"><button class="btn sm" type="submit">Save note</button></form></div>`;
}

function breadcrumbsList(json: string): string {
  try {
    const rows = JSON.parse(json) as { cat?: string; msg?: string }[];
    if (!Array.isArray(rows) || rows.length === 0) return "";
    return `<details><summary>breadcrumbs</summary><pre>${esc(rows.map((b) => `[${b.cat ?? ""}] ${b.msg ?? ""}`).join("\n"))}</pre></details>`;
  } catch {
    return "";
  }
}

export function renderGroup(
  group: Group,
  reports: {
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
  }[],
  user: User,
): string {
  const samples = reports.length
    ? reports
        .map((r) => {
          const dev = fmtDevice(r.device);
          const structured = [
            r.source && `source ${r.source}`,
            r.label && `label ${r.label}`,
            r.error_type && `type ${r.error_type}`,
            r.top_frame && `top ${r.top_frame}`,
            r.build_commit && `build ${r.build_commit}`,
            r.channel && `channel ${r.channel}`,
            r.view && `view ${r.view}`,
          ]
            .filter(Boolean)
            .map((x) => `<span>${esc(String(x))}</span>`)
            .join("");
          return `<div class="report"><div class="meta"><span><b>${esc(r.version)}</b></span><span>${esc(r.os)}/${esc(r.arch)}</span>${
            dev ? `<span>${esc(dev)}</span>` : ""
          }<span>${esc(r.created_at.slice(0, 19).replace("T", " "))}</span>${structured}</div><pre>${esc(r.message)}</pre>${breadcrumbsList(r.breadcrumbs)}</div>`;
        })
        .join("")
    : `<div class="empty">No raw samples stored for this group</div>`;
  const noteLine = group.note ? ` · note: ${esc(group.note)}` : "";
  const resolvedLine = group.resolved_in ? ` · resolved in ${esc(group.resolved_in)}` : "";
  const regressLine = group.regressed_at ? ` · regressed ${esc(group.regressed_at.slice(0, 10))}` : "";
  const groupMeta = [group.source, group.label, group.error_type, group.top_frame, group.severity, [group.last_os, group.last_arch].filter(Boolean).join("/")].filter(Boolean).join(" · ");

  return page(
    `Reasonix · ${group.fingerprint.slice(0, 8)}`,
    `stats / ${group.fingerprint.slice(0, 8)}`,
    `<h1><span class="pill ${group.kind === "crash" ? "crash" : ""}">${esc(group.kind)}</span> ${esc(group.fingerprint.slice(0, 8))} ${statusPill(group.status)}</h1>
${group.title ? `<p class="summary">${esc(group.title)}</p>` : ""}
${groupMeta ? `<p class="sub">${esc(groupMeta)}</p>` : ""}
<p class="sub"><b>${group.count}</b> occurrences · first ${esc(group.first_seen.slice(0, 10))} on ${esc(group.first_version || "?")} · last ${esc(group.last_seen.slice(0, 10))} on ${esc(group.last_version)}${resolvedLine}${regressLine}${noteLine}</p>
<div class="card full"><h2>Samples <b>— newest first, first sample plus latest 5 kept</b></h2>${samples}</div>
${user.role === "admin" ? manageGroup(group) : ""}
<a class="back" href="/stats">← Back to stats</a>`,
    userNav(user),
  );
}
