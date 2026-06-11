// Stats pages. Visual language mirrors site/src/styles/global.css — white, blue
// accent, large radii; crash stacks render in the site's dark terminal style.

export function esc(s: unknown): string {
  return String(s).replace(/[&<>"']/g, (c) => `&#${c.charCodeAt(0)};`);
}

const LOGO = `<svg viewBox="0 0 64 64" class="logo"><defs><linearGradient id="g" x1="0" y1="0" x2="1" y2="1"><stop offset="0" stop-color="#4f9dff"/><stop offset=".55" stop-color="#7a6bff"/><stop offset="1" stop-color="#c46bff"/></linearGradient></defs><rect width="64" height="64" rx="15" fill="url(#g)"/><text x="32" y="46" font-family="Inter,Segoe UI,Arial,sans-serif" font-size="42" font-weight="800" fill="#fff" text-anchor="middle">R</text></svg>`;

const CSS = `
:root{--accent:oklch(0.55 0.17 257);--accent-soft:color-mix(in oklch,var(--accent) 8%,white);
--bg-soft:oklch(0.976 0.0025 247);--ink:oklch(0.25 0.006 260);--ink-2:oklch(0.47 0.008 260);
--ink-3:oklch(0.6 0.008 260);--line:oklch(0.925 0.004 247);--term-bg:oklch(0.25 0.006 260);
--shadow-1:0 1px 2px oklch(0.35 0.01 260/0.18),0 1px 3px 1px oklch(0.35 0.01 260/0.08);
--sans:"Outfit","Helvetica Neue","PingFang SC","Microsoft YaHei",sans-serif;
--mono:"JetBrains Mono","SF Mono",Menlo,Consolas,monospace}
*{margin:0;padding:0;box-sizing:border-box}
body{font-family:var(--sans);background:#fff;color:var(--ink);-webkit-font-smoothing:antialiased}
.wrap{max-width:1060px;margin:0 auto;padding:0 28px 64px}
header{display:flex;align-items:center;gap:10px;height:68px}
.logo{width:28px;height:28px;border-radius:8px}
header .brand{font-size:17px;font-weight:600;color:var(--ink);text-decoration:none}
header .crumb{color:var(--ink-3);font-size:15px}
h1{font-size:30px;font-weight:600;letter-spacing:-0.02em;margin:18px 0 6px}
.sub{color:var(--ink-2);font-size:14.5px;margin-bottom:28px}
.grid{display:grid;grid-template-columns:1fr 1fr;gap:20px}
.card{background:#fff;border:1px solid var(--line);border-radius:24px;padding:24px 28px;box-shadow:var(--shadow-1)}
.card.full{grid-column:1/-1}
.card h2{font-size:15px;font-weight:600;color:var(--ink-2);margin-bottom:16px}
.card h2 b{color:var(--ink)}
.empty{color:var(--ink-3);font-size:14px;padding:14px 0}
svg.chart{width:100%;height:auto;display:block}
.row{display:grid;grid-template-columns:minmax(90px,auto) 1fr 3.5em;align-items:center;gap:12px;padding:7px 0;font-size:14px}
.row+.row{border-top:1px solid var(--line)}
.row .bar{height:10px;border-radius:999px;background:linear-gradient(90deg,#4f9dff,#7a6bff);min-width:4px}
.row .n{text-align:right;font-family:var(--mono);font-size:13px;color:var(--ink-2)}
table{border-collapse:collapse;width:100%;font-size:14px}
th{text-align:left;color:var(--ink-3);font-weight:500;font-size:12.5px;padding:0 14px 8px 0}
td{padding:8px 14px 8px 0;border-top:1px solid var(--line)}
td.n{font-family:var(--mono);font-size:13px}
a.fp{font-family:var(--mono);font-size:13px;color:var(--accent);text-decoration:none}
a.fp:hover{text-decoration:underline}
.pill{display:inline-block;font-size:12px;font-weight:500;padding:2px 10px;border-radius:999px;background:var(--accent-soft);color:var(--accent)}
.pill.crash{background:oklch(0.95 0.03 25);color:oklch(0.55 0.18 25)}
.back{display:inline-block;margin:18px 0 0;color:var(--accent);text-decoration:none;font-size:14px}
.report{margin-top:20px}
.report .meta{display:flex;flex-wrap:wrap;gap:8px 18px;font-size:13px;color:var(--ink-2);margin-bottom:10px}
.report .meta b{color:var(--ink);font-weight:500}
pre{background:var(--term-bg);color:#e6e8f0;border-radius:12px;padding:16px 18px;font-family:var(--mono);
font-size:12.5px;line-height:1.55;overflow:auto;white-space:pre-wrap;word-break:break-word}
.metrics{display:grid;grid-template-columns:1fr 1fr;gap:8px 28px}
.metric-block h3{font-family:var(--mono);font-size:12.5px;font-weight:600;color:var(--ink-2);margin:6px 0 4px}
@media(max-width:760px){.grid{grid-template-columns:1fr}.metrics{grid-template-columns:1fr}.wrap{padding:0 16px 48px}}
`;

function page(title: string, crumb: string, body: string): string {
  return `<!doctype html><html lang="en"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1"><meta name="robots" content="noindex">
<title>${esc(title)}</title><style>${CSS}</style></head><body><div class="wrap">
<header><a class="brand" href="https://reasonix.io">${LOGO}</a><a class="brand" href="https://reasonix.io">Reasonix</a><span class="crumb">/ ${esc(crumb)}</span></header>
${body}</div></body></html>`;
}

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

export function renderStats(data: {
  daily: Daily[];
  versions: { label: string; users: number }[];
  platforms: { label: string; users: number }[];
  crashes: { fingerprint: string; kind: string; count: number; last_version: string; seen: string }[];
  metrics: { signal: string; bucket: string; total: number }[];
}): string {
  const days = last30Days(data.daily);
  const totalUsers = days.at(-1)?.users ?? 0;
  const anyPing = days.some((d) => d.opens > 0);
  const crashRows = data.crashes.length
    ? `<table><thead><tr><th>fingerprint</th><th>kind</th><th>count</th><th>last version</th><th>last seen</th></tr></thead><tbody>${data.crashes
        .map(
          (c) =>
            `<tr><td><a class="fp" href="/stats/group/${esc(c.fingerprint)}">${esc(c.fingerprint.slice(0, 8))}</a></td><td><span class="pill ${c.kind === "crash" ? "crash" : ""}">${esc(c.kind)}</span></td><td class="n">${c.count}</td><td class="n">${esc(c.last_version)}</td><td class="n">${esc(c.seen)}</td></tr>`,
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

export function renderGroup(
  group: { fingerprint: string; kind: string; count: number; first_seen: string; last_seen: string; last_version: string },
  reports: { version: string; os: string; arch: string; message: string; device: string; created_at: string }[],
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

  return page(
    `Reasonix · ${group.fingerprint.slice(0, 8)}`,
    `stats / ${group.fingerprint.slice(0, 8)}`,
    `<h1><span class="pill ${group.kind === "crash" ? "crash" : ""}">${esc(group.kind)}</span> ${esc(group.fingerprint.slice(0, 8))}</h1>
<p class="sub"><b>${group.count}</b> occurrences · first ${esc(group.first_seen.slice(0, 10))} · last ${esc(group.last_seen.slice(0, 10))} on ${esc(group.last_version)}</p>
<div class="card full"><h2>Samples <b>— newest first, up to 5 kept</b></h2>${samples}</div>
<a class="back" href="/stats">← Back to stats</a>`,
  );
}
