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

type CrashRow = { fingerprint: string; kind: string; count: number; last_version: string; seen: string; status: string };

export function renderStats(
  data: {
    daily: Daily[];
    versions: { label: string; users: number }[];
    platforms: { label: string; users: number }[];
    crashes: CrashRow[];
    metrics: { signal: string; bucket: string; total: number }[];
  },
  user: User,
): string {
  const days = last30Days(data.daily);
  const totalUsers = days.at(-1)?.users ?? 0;
  const anyPing = days.some((d) => d.opens > 0);
  const crashRows = data.crashes.length
    ? `<table><thead><tr><th>fingerprint</th><th>kind</th><th>status</th><th>count</th><th>last version</th><th>last seen</th></tr></thead><tbody>${data.crashes
        .map(
          (c) =>
            `<tr><td><a class="fp" href="/stats/group/${esc(c.fingerprint)}">${esc(c.fingerprint.slice(0, 8))}</a></td><td><span class="pill ${c.kind === "crash" ? "crash" : ""}">${esc(c.kind)}</span></td><td>${statusPill(c.status)}</td><td class="n">${c.count}</td><td class="n">${esc(c.last_version)}</td><td class="n">${esc(c.seen)}</td></tr>`,
        )
        .join("")}</tbody></table>`
    : `<div class="empty">No crash reports yet — that's the good kind of empty · 还没有崩溃报告</div>`;

  return page(
    "Reasonix · Stats",
    "stats",
    `<h1>Desktop stats</h1><p class="sub">Today: <b>${totalUsers}</b> active installs · anonymous launch pings and user-sent crash reports only</p>
<div class="grid">
<div class="card full"><h2>Daily active installs · 每日活跃 <b>— 30 days</b> (solid: users, faded: opens)</h2>
${anyPing ? dailyChart(days) : `<div class="empty">No pings yet — data starts flowing once a telemetry-enabled build ships · 等带统计的版本发布后这里开始有数据</div>`}</div>
<div class="card"><h2>Versions · 版本分布 <b>— 7 days</b></h2>${listBars(data.versions)}</div>
<div class="card"><h2>Platforms · 平台分布 <b>— 7 days</b></h2>${listBars(data.platforms)}</div>
<div class="card full"><h2>Agent signals · 运行指标 <b>— 7 days, opt-in aggregate</b></h2>${metricsCards(data.metrics)}</div>
<div class="card full"><h2>Crash groups · 崩溃分组 <b>— click a fingerprint for stacks</b></h2>${crashRows}</div>
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
  last_version: string;
  status: string;
  note: string;
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
<form method="post" action="/stats/group/${fp}" class="note-edit"><input type="hidden" name="action" value="note"><input type="text" name="note" placeholder="Add a note…" value="${esc(group.note)}"><button class="btn sm" type="submit">Save note</button></form></div>`;
}

export function renderGroup(
  group: Group,
  reports: { version: string; os: string; arch: string; message: string; device: string; created_at: string }[],
  user: User,
): string {
  const samples = reports.length
    ? reports
        .map((r) => {
          const dev = fmtDevice(r.device);
          return `<div class="report"><div class="meta"><span><b>${esc(r.version)}</b></span><span>${esc(r.os)}/${esc(r.arch)}</span>${
            dev ? `<span>${esc(dev)}</span>` : ""
          }<span>${esc(r.created_at.slice(0, 19).replace("T", " "))}</span></div><pre>${esc(r.message)}</pre></div>`;
        })
        .join("")
    : `<div class="empty">No raw samples stored for this group</div>`;
  const noteLine = group.note ? ` · note: ${esc(group.note)}` : "";

  return page(
    `Reasonix · ${group.fingerprint.slice(0, 8)}`,
    `stats / ${group.fingerprint.slice(0, 8)}`,
    `<h1><span class="pill ${group.kind === "crash" ? "crash" : ""}">${esc(group.kind)}</span> ${esc(group.fingerprint.slice(0, 8))} ${statusPill(group.status)}</h1>
<p class="sub"><b>${group.count}</b> occurrences · first ${esc(group.first_seen.slice(0, 10))} · last ${esc(group.last_seen.slice(0, 10))} on ${esc(group.last_version)}${noteLine}</p>
<div class="card full"><h2>Samples <b>— newest first, up to 5 kept</b></h2>${samples}</div>
${user.role === "admin" ? manageGroup(group) : ""}
<a class="back" href="/stats">← Back to stats</a>`,
    userNav(user),
  );
}
