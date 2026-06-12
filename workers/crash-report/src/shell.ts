// Shared page shell + UI kit. Visual language mirrors site/src/styles/global.css
// — white, blue accent, large radii; crash stacks use the site's dark terminal.

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
header .nav{margin-left:auto;display:flex;align-items:center;gap:14px}
.navlink{color:var(--accent);text-decoration:none;font-size:14px;font-weight:500}
.navlink:hover{text-decoration:underline}
.chip{display:inline-flex;align-items:center;gap:8px;font-size:13.5px;color:var(--ink-2)}
.badge{font-size:11px;font-weight:600;letter-spacing:.02em;text-transform:uppercase;padding:2px 8px;border-radius:999px}
.badge.pending{background:oklch(0.95 0.04 80);color:oklch(0.5 0.12 80)}
.badge.viewer{background:var(--accent-soft);color:var(--accent)}
.badge.admin{background:oklch(0.95 0.05 150);color:oklch(0.48 0.13 150)}
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
td{padding:8px 14px 8px 0;border-top:1px solid var(--line);vertical-align:middle}
td.n{font-family:var(--mono);font-size:13px}
a.fp{font-family:var(--mono);font-size:13px;color:var(--accent);text-decoration:none}
a.fp:hover{text-decoration:underline}
.pill{display:inline-block;font-size:12px;font-weight:500;padding:2px 10px;border-radius:999px;background:var(--accent-soft);color:var(--accent)}
.pill.crash{background:oklch(0.95 0.03 25);color:oklch(0.55 0.18 25)}
.pill.resolved{background:oklch(0.95 0.05 150);color:oklch(0.48 0.13 150)}
.pill.ignored{background:var(--bg-soft);color:var(--ink-3)}
.back{display:inline-block;margin:18px 0 0;color:var(--accent);text-decoration:none;font-size:14px}
.report{margin-top:20px}
.report .meta{display:flex;flex-wrap:wrap;gap:8px 18px;font-size:13px;color:var(--ink-2);margin-bottom:10px}
.report .meta b{color:var(--ink);font-weight:500}
pre{background:var(--term-bg);color:#e6e8f0;border-radius:12px;padding:16px 18px;font-family:var(--mono);
font-size:12.5px;line-height:1.55;overflow:auto;white-space:pre-wrap;word-break:break-word}
.metrics{display:grid;grid-template-columns:1fr 1fr;gap:8px 28px}
.metric-block h3{font-family:var(--mono);font-size:12.5px;font-weight:600;color:var(--ink-2);margin:6px 0 4px}
form.inline{display:inline}
.authwrap{max-width:400px;margin:6vh auto 0}
.authcard{background:#fff;border:1px solid var(--line);border-radius:24px;padding:32px 34px;box-shadow:var(--shadow-1)}
.authcard h1{font-size:23px;margin:0 0 4px}
.authcard .sub{margin-bottom:22px}
.field{display:block;margin-bottom:14px}
.field label{display:block;font-size:13px;color:var(--ink-2);margin-bottom:6px}
.field input,select{width:100%;font:inherit;font-size:14.5px;color:var(--ink);background:#fff;border:1px solid var(--line);border-radius:12px;padding:10px 13px;outline:none}
.field input:focus,select:focus{border-color:var(--accent)}
.btn{font:inherit;font-size:14.5px;font-weight:600;color:#fff;background:var(--accent);border:none;border-radius:12px;padding:10px 18px;cursor:pointer}
.btn:hover{filter:brightness(1.05)}
.btn.block{width:100%}
.btn.ghost{background:var(--accent-soft);color:var(--accent)}
.btn.danger{background:oklch(0.95 0.03 25);color:oklch(0.55 0.18 25)}
.btn.sm{font-size:13px;padding:6px 12px;border-radius:10px}
.notice{font-size:13.5px;padding:10px 14px;border-radius:12px;margin-bottom:16px}
.notice.err{background:oklch(0.96 0.03 25);color:oklch(0.5 0.18 25)}
.notice.ok{background:oklch(0.95 0.05 150);color:oklch(0.45 0.13 150)}
.alt{margin-top:18px;font-size:13.5px;color:var(--ink-3);text-align:center}
.alt a{color:var(--accent);text-decoration:none}
.actions{display:flex;gap:8px;align-items:center}
td select{width:auto;padding:6px 10px;border-radius:10px}
.note-edit{margin-top:12px;display:flex;gap:8px;max-width:560px}
.note-edit input{flex:1}
@media(max-width:760px){.grid{grid-template-columns:1fr}.metrics{grid-template-columns:1fr}.wrap{padding:0 16px 48px}}
`;

export function page(title: string, crumb: string, body: string, nav = ""): string {
  return `<!doctype html><html lang="en"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1"><meta name="robots" content="noindex">
<title>${esc(title)}</title><style>${CSS}</style></head><body><div class="wrap">
<header><a class="brand" href="https://reasonix.io">${LOGO}</a><a class="brand" href="https://reasonix.io">Reasonix</a><span class="crumb">/ ${esc(crumb)}</span>${nav ? `<span class="nav">${nav}</span>` : ""}</header>
${body}</div></body></html>`;
}

export function html(body: string): Response {
  return new Response(body, { headers: { "content-type": "text/html; charset=utf-8" } });
}

export function redirect(location: string, cookie?: string): Response {
  const headers: Record<string, string> = { location };
  if (cookie) headers["set-cookie"] = cookie;
  return new Response(null, { status: 303, headers });
}
