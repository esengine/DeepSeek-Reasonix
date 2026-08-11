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
html{scroll-behavior:smooth}
body{font-family:var(--sans);background:#fff;color:var(--ink);-webkit-font-smoothing:antialiased}
.wrap{max-width:1060px;margin:0 auto;padding:0 28px 64px}
header{display:flex;align-items:center;gap:10px;height:68px}
.logo{width:28px;height:28px;border-radius:8px}
header .brand{font-size:17px;font-weight:600;color:var(--ink);text-decoration:none}
header .crumb{color:var(--ink-3);font-size:15px}
header .nav{display:flex;align-items:center;gap:14px}
.navlink{color:var(--accent);text-decoration:none;font-size:14px;font-weight:500}
.navlink:hover{text-decoration:underline}
.site-nav{display:flex;align-items:center;gap:6px;margin:4px 0 10px}
.site-nav a{display:inline-flex;align-items:center;min-height:34px;padding:7px 13px;border-radius:999px;color:var(--ink-2);text-decoration:none;font-size:14px;font-weight:600}
.site-nav a:hover{background:var(--bg-soft);color:var(--ink)}
.site-nav a.active,.site-nav a[aria-current="page"]{background:var(--accent-soft);color:var(--accent)}
.chip{display:inline-flex;align-items:center;gap:8px;font-size:13.5px;color:var(--ink-2)}
.lang-switch{margin-left:auto;display:inline-flex;align-items:center;gap:2px;padding:3px;border:1px solid var(--line);border-radius:12px;background:var(--bg-soft)}
.lang-switch button{font:inherit;font-size:12.5px;font-weight:700;color:var(--ink-2);background:transparent;border:0;border-radius:9px;padding:5px 10px;cursor:pointer}
.lang-switch button.active{background:#fff;color:var(--accent);box-shadow:var(--shadow-1)}
body[data-lang="en"] [data-i18n="zh"],body[data-lang="zh"] [data-i18n="en"]{display:none!important}
.badge{font-size:11px;font-weight:600;letter-spacing:.02em;text-transform:uppercase;padding:2px 8px;border-radius:999px}
.badge.pending{background:oklch(0.95 0.04 80);color:oklch(0.5 0.12 80)}
.badge.viewer{background:var(--accent-soft);color:var(--accent)}
.badge.admin{background:oklch(0.95 0.05 150);color:oklch(0.48 0.13 150)}
h1{font-size:30px;font-weight:600;letter-spacing:-0.02em;margin:18px 0 6px}
.sub{color:var(--ink-2);font-size:14.5px;margin-bottom:28px}
.hero-line{display:flex;align-items:flex-start;justify-content:space-between;gap:18px;margin-top:6px}
.hero-line .sub{margin-bottom:18px}
.segmented{display:inline-flex;align-items:center;gap:3px;padding:3px;border:1px solid var(--line);border-radius:13px;background:var(--bg-soft);white-space:nowrap}
.segmented a{display:inline-flex;align-items:center;justify-content:center;min-height:30px;padding:6px 11px;border-radius:10px;color:var(--ink-2);text-decoration:none;font-size:12.5px;font-weight:700}
.segmented a.active,.segmented a[aria-current="true"]{background:#fff;color:var(--accent);box-shadow:var(--shadow-1)}
.overview-grid{display:grid;grid-template-columns:repeat(6,minmax(0,1fr));gap:10px;margin:0 0 20px}
.overview-card{border:1px solid var(--line);border-radius:16px;background:linear-gradient(180deg,#fff,var(--bg-soft));padding:14px 15px;min-width:0;text-decoration:none}
.overview-card:hover{border-color:color-mix(in oklch,var(--accent) 36%,var(--line));transform:translateY(-1px)}
.overview-card span,.overview-card small{display:block;color:var(--ink-3);font-size:12.5px;line-height:1.3}
.overview-card strong{display:block;color:var(--ink);font-family:var(--mono);font-size:24px;line-height:1.15;margin:6px 0 4px;white-space:nowrap;overflow:hidden;text-overflow:ellipsis}
.overview-card.good{border-color:oklch(0.88 0.05 150);background:oklch(0.985 0.015 150)}
.overview-card.warn{border-color:oklch(0.9 0.07 80);background:oklch(0.985 0.018 80)}
.overview-card.bad{border-color:oklch(0.89 0.06 25);background:oklch(0.985 0.016 25)}
.module-nav{display:grid;grid-template-columns:repeat(4,minmax(0,1fr));gap:10px;margin:0 0 20px}
.module-link{display:grid;grid-template-columns:1fr auto;grid-template-areas:"label value" "note note";gap:4px 10px;border:1px solid var(--line);border-radius:16px;background:#fff;color:var(--ink);text-decoration:none;padding:13px 14px;box-shadow:var(--shadow-1)}
.module-link:hover{border-color:color-mix(in oklch,var(--accent) 36%,var(--line));background:var(--accent-soft)}
.module-link.active,.module-link[aria-current="page"]{border-color:color-mix(in oklch,var(--accent) 45%,var(--line));background:var(--accent-soft)}
.module-link span{grid-area:label;color:var(--ink-2);font-size:13px;font-weight:700}
.module-link b{grid-area:value;font-family:var(--mono);font-size:13px;color:var(--accent)}
.module-link small{grid-area:note;color:var(--ink-3);font-size:12.5px;white-space:nowrap;overflow:hidden;text-overflow:ellipsis}
.grid{display:grid;grid-template-columns:1fr 1fr;gap:20px}
.card{background:#fff;border:1px solid var(--line);border-radius:24px;padding:24px 28px;box-shadow:var(--shadow-1)}
.card.full{grid-column:1/-1}
.card h2{font-size:15px;font-weight:600;color:var(--ink-2);margin-bottom:16px}
.card h2 b{color:var(--ink)}
.module-card{scroll-margin-top:18px}
.module-card+.module-card{margin-top:2px}
.module-head{display:flex;align-items:center;justify-content:space-between;gap:14px;margin-bottom:16px}
.module-head span{display:block;font-size:11px;text-transform:uppercase;letter-spacing:.08em;color:var(--ink-3);font-weight:800;margin-bottom:4px}
.module-head h2{margin:0;color:var(--ink);font-size:19px}
.module-actions{display:flex;align-items:center;justify-content:flex-end;gap:10px;flex-wrap:wrap}
.module-action{color:var(--accent);text-decoration:none;font-size:13.5px;font-weight:700;white-space:nowrap}
.module-action:hover{text-decoration:underline}
.module-panel{border:1px solid var(--line);border-radius:18px;background:var(--bg-soft);padding:16px 18px;margin-top:12px;min-width:0}
.module-panel.wide{margin-top:0}
.module-panel h3{font-size:13px;font-weight:700;color:var(--ink-2);margin:0 0 12px}
.module-panel h3 b{color:var(--ink)}
.module-split{display:grid;grid-template-columns:1fr 1fr;gap:12px;align-items:start}
.module-split.wide{grid-template-columns:minmax(0,1fr) minmax(0,1fr)}
.card-title-row{display:flex;align-items:center;justify-content:space-between;gap:14px;margin-bottom:16px}
.card-title-row h2{margin:0}
.empty{color:var(--ink-3);font-size:14px;padding:14px 0}
svg.chart{width:100%;height:auto;display:block}
.bars-list{min-width:0}
.row{display:grid;grid-template-columns:minmax(0,1.15fr) minmax(76px,1fr) minmax(3.8em,max-content);align-items:center;gap:12px;padding:7px 0;font-size:14px;min-width:0}
.row+.row{border-top:1px solid var(--line)}
.row-label{display:flex;align-items:center;min-width:0;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
.row-bar{min-width:0}
.row .bar{height:10px;border-radius:999px;background:linear-gradient(90deg,#4f9dff,#7a6bff);min-width:4px}
.row .n{text-align:right;font-family:var(--mono);font-size:13px;color:var(--ink-2);white-space:nowrap}
.bucket-prefix{display:inline-flex;align-items:center;margin-right:6px;padding:1px 6px;border-radius:8px;background:var(--accent-soft);color:var(--accent);font-size:12px;font-weight:700}
.bucket-main{display:inline-block;min-width:0;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
.bars-more{margin-top:6px}
.bars-more summary{display:inline-flex;align-items:center;min-height:30px;padding:5px 9px;border:1px dashed var(--line);border-radius:10px;background:#fff;color:var(--accent);font-size:12.5px;font-weight:700;cursor:pointer;list-style:none}
.bars-more summary::-webkit-details-marker{display:none}
.bars-more summary:hover{border-color:color-mix(in oklch,var(--accent) 34%,var(--line))}
.bars-more .more-open{display:none}
.bars-more[open] .more-closed{display:none}
.bars-more[open] .more-open{display:inline}
.bars-more-list{max-height:260px;overflow-y:auto;overflow-x:hidden;margin-top:7px;padding:5px 8px;border:1px solid var(--line);border-radius:12px;background:#fff}
table{border-collapse:collapse;width:100%;font-size:14px}
th{text-align:left;color:var(--ink-3);font-weight:500;font-size:12.5px;padding:0 14px 8px 0}
td{padding:8px 14px 8px 0;border-top:1px solid var(--line);vertical-align:middle}
td.n{font-family:var(--mono);font-size:13px}
td.summary{max-width:480px;white-space:nowrap;overflow:hidden;text-overflow:ellipsis;font-family:var(--mono);font-size:12.5px}
.reg-table{table-layout:fixed}
.reg-table .c-pkg{width:32%}.reg-table .c-kind{width:8%}.reg-table .c-pub{width:14%}
.reg-table .c-ver{width:16%}.reg-table .c-sub{width:10%}.reg-table .c-act{width:20%}
.rowlinks{display:flex;align-items:center;flex-wrap:wrap;gap:8px;margin-top:6px}
.muted{color:var(--ink-3)}
p.summary{font-family:var(--mono);font-size:14px;margin:2px 0 6px;word-break:break-word}
a.fp{font-family:var(--mono);font-size:13px;color:var(--accent);text-decoration:none}
a.fp:hover{text-decoration:underline}
.pill{display:inline-block;font-size:12px;font-weight:500;padding:2px 10px;border-radius:999px;background:var(--accent-soft);color:var(--accent)}
.pill.crash{background:oklch(0.95 0.03 25);color:oklch(0.55 0.18 25)}
.pill.open{background:oklch(0.96 0.025 250);color:var(--ink-2)}
.pill.resolved{background:oklch(0.95 0.05 150);color:oklch(0.48 0.13 150)}
.pill.ignored{background:var(--bg-soft);color:var(--ink-3)}
.back{display:inline-block;margin:18px 0 0;color:var(--accent);text-decoration:none;font-size:14px}
.report{margin-top:20px}
.report .meta{display:flex;flex-wrap:wrap;gap:8px 18px;font-size:13px;color:var(--ink-2);margin-bottom:10px}
.report .meta b{color:var(--ink);font-weight:500}
pre{background:var(--term-bg);color:#e6e8f0;border-radius:12px;padding:16px 18px;font-family:var(--mono);
font-size:12.5px;line-height:1.55;overflow:auto;white-space:pre-wrap;word-break:break-word}
.metrics{display:grid;grid-template-columns:1fr 1fr;gap:8px 28px}
.metric-block h3{display:flex;align-items:center;justify-content:space-between;gap:8px;font-family:var(--mono);font-size:12.5px;font-weight:600;color:var(--ink-2);margin:6px 0 4px}
.metric-block h3 span{font-family:var(--mono);font-size:11px;color:var(--ink-3);font-weight:600}
.preference-dashboard{display:flex;flex-direction:column;gap:14px}
.preference-compare{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:12px;align-items:start}
.preference-panel{background:#fff}
.preference-panel.active{border-color:color-mix(in oklch,var(--accent) 42%,var(--line));box-shadow:0 0 0 3px var(--accent-soft)}
.pref-section{border:1px solid var(--line);border-radius:14px;background:#fff;padding:14px 15px}
.pref-section>h3,.pref-section summary h3{display:flex;align-items:center;justify-content:space-between;gap:12px;font-size:13px;font-weight:700;color:var(--ink);margin:0 0 10px}
.pref-section>h3 span,.pref-section summary h3 span{font-size:11.5px;color:var(--ink-3);font-weight:700;white-space:nowrap}
.pref-section summary{cursor:pointer;list-style:none}
.pref-section summary::-webkit-details-marker{display:none}
.pref-section summary h3{margin:0}
.pref-section summary h3::after{content:"+";font-family:var(--mono);font-size:13px;color:var(--accent);line-height:1}
.pref-section[open] summary h3::after{content:"-"}
.pref-section[open] summary h3{margin-bottom:10px}
.pref-section-collapsed summary:hover h3{color:var(--accent)}
.pref-section .metric-block h3{color:var(--ink-3)}
.pref-metrics{grid-template-columns:1fr;gap:12px}
.pref-metrics .metric-block{min-width:0}
.pref-metrics .row{grid-template-columns:minmax(7rem,1.2fr) minmax(5rem,.9fr) minmax(4.4em,max-content)}
.health-grid{display:grid;grid-template-columns:repeat(5,minmax(0,1fr));gap:12px}
.health-card{border:1px solid var(--line);border-radius:16px;background:var(--bg-soft);padding:14px 15px;min-width:0}
.health-card.good{background:oklch(0.985 0.015 150);border-color:oklch(0.88 0.05 150)}
.health-card.warn{background:oklch(0.985 0.018 80);border-color:oklch(0.9 0.07 80)}
.health-card.bad{background:oklch(0.985 0.016 25);border-color:oklch(0.89 0.06 25)}
.health-top{display:flex;align-items:center;justify-content:space-between;gap:8px;margin-bottom:8px}
.health-top span{font-size:12.5px;color:var(--ink-2);font-weight:700}
.health-top b{font-size:11px;text-transform:uppercase;color:var(--ink-3);font-weight:800}
.health-card strong{display:block;font-family:var(--mono);font-size:22px;color:var(--ink);margin-bottom:4px}
.health-card small{display:block;color:var(--ink-3);font-family:var(--mono);font-size:11.5px;margin-bottom:9px}
.health-card p{color:var(--ink-2);font-size:12.5px;line-height:1.35;word-break:break-word}
.filter-card{margin-top:12px;padding:16px 18px;border:1px solid var(--line);border-radius:18px;background:#fff;position:sticky;top:10px;z-index:2}
.filter-head{display:flex;align-items:center;justify-content:space-between;gap:16px;margin-bottom:14px}
.filter-head h2{margin:0}
.filter-head span{font-family:var(--mono);font-size:12.5px;color:var(--ink-3);white-space:nowrap}
.filter-tabs{display:flex;flex-wrap:wrap;gap:8px;margin-bottom:18px}
.filter-tab{display:inline-flex;align-items:center;min-height:34px;padding:7px 13px;border-radius:12px;border:1px solid var(--line);
background:var(--bg-soft);color:var(--ink-2);text-decoration:none;font-size:13.5px;font-weight:600}
.filter-tab:hover{border-color:color-mix(in oklch,var(--accent) 34%,var(--line));color:var(--accent)}
.filter-tab.active{background:var(--accent);border-color:var(--accent);color:#fff}
.facet-grid{display:grid;grid-template-columns:repeat(3,minmax(0,1fr));gap:16px}
.facet-grid section{min-width:0}
.facet-grid h3{font-size:12px;font-weight:600;color:var(--ink-3);margin:0 0 8px;text-transform:uppercase}
.facet-list{display:flex;flex-wrap:wrap;gap:7px;min-width:0}
.facet-chip{display:inline-grid;grid-template-columns:minmax(0,auto) auto;align-items:center;gap:8px;max-width:100%;min-width:0;
padding:6px 9px;border:1px solid var(--line);border-radius:11px;background:#fff;color:var(--ink-2);text-decoration:none;font-size:13px}
.facet-chip:hover{border-color:color-mix(in oklch,var(--accent) 30%,var(--line));color:var(--accent)}
.facet-chip.active{background:var(--accent-soft);border-color:color-mix(in oklch,var(--accent) 45%,var(--line));color:var(--accent)}
.facet-chip b{font-family:var(--mono);font-size:12px;color:inherit}
.facet-label{overflow:hidden;text-overflow:ellipsis;white-space:nowrap;min-width:0}
.facet-more{flex-basis:100%;min-width:0}
.facet-more summary{display:inline-flex;align-items:center;min-height:32px;padding:6px 10px;border:1px dashed var(--line);border-radius:11px;background:var(--bg-soft);color:var(--accent);font-size:13px;font-weight:700;cursor:pointer;list-style:none}
.facet-more summary::-webkit-details-marker{display:none}
.facet-more summary:hover{border-color:color-mix(in oklch,var(--accent) 34%,var(--line))}
.facet-more-list{display:flex;flex-wrap:wrap;gap:7px;max-height:150px;overflow:auto;margin-top:8px;padding:8px;border:1px solid var(--line);border-radius:13px;background:#fff}
.filter-empty{font-size:13px;color:var(--ink-3)}
.crash-card{overflow:hidden}
.crash-list{display:flex;flex-direction:column;gap:2px}
.crash-head,.crash-item{display:grid;grid-template-columns:minmax(300px,1fr) minmax(185px,.55fr) 150px 62px;gap:16px;align-items:center}
.crash-head{padding:0 12px 9px;color:var(--ink-3);font-size:12.5px;font-weight:600;border-bottom:1px solid var(--line)}
.crash-item{margin:0 -12px;padding:14px 12px;border-radius:14px;color:var(--ink);text-decoration:none;border-bottom:1px solid var(--line)}
.crash-item:hover{background:var(--bg-soft)}
.crash-fingerprint{display:flex;flex-direction:column;gap:3px;min-width:0}
.crash-fingerprint b{font-family:var(--mono);font-size:13px;color:var(--accent);font-weight:700}
.crash-fingerprint small,.crash-scope small{font-family:var(--mono);font-size:11.5px;color:var(--ink-3);overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
.crash-summary{display:flex;flex-direction:column;gap:5px;min-width:0}
.crash-summary>span{font-family:var(--mono);font-size:12.8px;line-height:1.45;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
.crash-summary small{font-family:var(--mono);font-size:11.5px;color:var(--ink-3);overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
.crash-summary em{font-size:12px;font-style:normal;color:oklch(0.55 0.18 25)}
.crash-scope{display:flex;flex-direction:column;gap:4px;min-width:0}
.crash-health{display:flex;flex-wrap:wrap;gap:6px;align-items:center}
.crash-count{font-family:var(--mono);font-size:14px;font-weight:700;color:var(--ink);text-align:right}
.group-hero{padding:28px 0 22px}
.group-nav{display:flex;align-items:center;justify-content:space-between;gap:12px;margin-bottom:18px}
.group-nav .back{margin:0}
.group-title{display:flex;align-items:center;gap:12px;flex-wrap:wrap;margin-bottom:8px}
.group-title h1{margin:0;font-family:var(--mono);font-size:34px}
.group-summary{font-size:17px;margin:0 0 12px}
.group-tags{display:flex;flex-wrap:wrap;gap:8px;margin:0 0 18px}
.group-tags span{display:inline-flex;align-items:center;gap:6px;border:1px solid var(--line);border-radius:11px;background:#fff;padding:6px 9px;font-size:13px;color:var(--ink-2)}
.group-tags b{font-size:12px;color:var(--ink-3);font-weight:600}
.crash-list.compact .crash-head{display:none}
.crash-list.compact .crash-item{padding:12px;margin:0 -12px;grid-template-columns:minmax(260px,1fr) minmax(160px,.5fr) 140px 52px}
.group-metrics{display:grid;grid-template-columns:repeat(4,minmax(0,1fr));gap:10px}
.group-metrics div{border:1px solid var(--line);border-radius:14px;background:var(--bg-soft);padding:12px 13px;min-width:0}
.group-metrics span{display:block;font-size:12px;color:var(--ink-3);margin-bottom:5px}
.group-metrics b{display:block;font-family:var(--mono);font-size:13px;color:var(--ink);white-space:nowrap;overflow:hidden;text-overflow:ellipsis}
.group-note{margin-top:12px;color:var(--ink-2);font-size:13.5px}
.sample-card h2{display:flex;align-items:center;justify-content:space-between;gap:12px}
.sample-list{display:flex;flex-direction:column;gap:10px}
.sample{border:1px solid var(--line);border-radius:16px;background:#fff;overflow:hidden}
.sample summary{display:grid;grid-template-columns:140px minmax(0,1fr) 170px;gap:14px;align-items:center;padding:14px 16px;cursor:pointer;list-style:none}
.sample summary::-webkit-details-marker{display:none}
.sample summary:hover{background:var(--bg-soft)}
.sample[open] summary{border-bottom:1px solid var(--line)}
.sample-id{display:flex;flex-direction:column;gap:3px;min-width:0}
.sample-id b{font-family:var(--mono);font-size:14px;color:var(--ink)}
.sample-id small,.sample-time{font-family:var(--mono);font-size:12px;color:var(--ink-3);white-space:nowrap;overflow:hidden;text-overflow:ellipsis}
.sample-title{font-family:var(--mono);font-size:12.8px;white-space:nowrap;overflow:hidden;text-overflow:ellipsis}
.sample-time{text-align:right}
.sample-body{padding:14px 16px 16px}
.sample-meta{display:flex;flex-wrap:wrap;gap:8px;margin-bottom:12px}
.sample-meta span{display:inline-flex;align-items:center;gap:6px;border:1px solid var(--line);border-radius:10px;padding:5px 8px;color:var(--ink-2);font-size:12.5px}
.sample-meta b{color:var(--ink-3);font-weight:600}
.sample-actions{display:flex;gap:8px;flex-wrap:wrap;margin-bottom:12px}
.sample-more{border:1px dashed var(--line);border-radius:16px;background:var(--bg-soft);padding:10px}
.sample-more>summary{cursor:pointer;list-style:none;color:var(--accent);font-size:13.5px;font-weight:700;padding:4px 6px}
.sample-more>summary::-webkit-details-marker{display:none}
.sample-more-list{display:flex;flex-direction:column;gap:10px;margin-top:10px}
.copy-btn[data-state="copied"]{background:oklch(0.95 0.05 150);color:oklch(0.48 0.13 150)}
.copy-btn[data-state="copied"] .copy-label{display:none}
.copy-btn[data-state="copied"]::after{content:"Copied"}
body[data-lang="zh"] .copy-btn[data-state="copied"]::after{content:"已复制"}
.copy-btn[data-state="failed"]{background:oklch(0.96 0.03 25);color:oklch(0.55 0.18 25)}
.copy-btn[data-state="failed"] .copy-label{display:none}
.copy-btn[data-state="failed"]::after{content:"Copy failed"}
body[data-lang="zh"] .copy-btn[data-state="failed"]::after{content:"复制失败"}
.sample-nested{margin-top:10px}
.sample-nested summary{cursor:pointer;color:var(--accent);font-size:13px;margin-bottom:8px}
.sample-nested pre{margin-top:8px}
.manage-card{margin-top:20px}
.manage-head{display:flex;align-items:flex-start;justify-content:space-between;gap:16px;margin-bottom:16px}
.manage-head h2{margin:0}
.manage-actions{display:flex;gap:8px;flex-wrap:wrap;justify-content:flex-end}
.manage-grid{display:grid;grid-template-columns:1fr 1fr;gap:12px}
.manage-form{display:grid;grid-template-columns:minmax(0,1fr) auto;gap:10px;align-items:end}
.manage-form.wide{grid-column:1/-1}
.manage-form label{display:flex;flex-direction:column;gap:6px;font-size:12.5px;color:var(--ink-3);font-weight:600}
.manage-form input,.manage-form select{min-width:0}
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
@media(max-width:1100px){.overview-grid{grid-template-columns:repeat(3,minmax(0,1fr))}.module-nav{grid-template-columns:repeat(2,minmax(0,1fr))}.health-grid{grid-template-columns:repeat(2,minmax(0,1fr))}.module-split,.module-split.wide,.preference-compare{grid-template-columns:1fr}}
@media(max-width:900px){.hero-line{flex-direction:column}.facet-grid{grid-template-columns:1fr}.crash-head{display:none}.crash-item,.crash-list.compact .crash-item{grid-template-columns:1fr;gap:10px}
.crash-health{justify-content:flex-start}.crash-count{text-align:left}.filter-head,.module-head{align-items:flex-start;flex-direction:column;gap:8px}.module-actions{justify-content:flex-start}
.group-metrics{grid-template-columns:1fr 1fr}.sample summary{grid-template-columns:1fr}.sample-time{text-align:left}.manage-head{flex-direction:column}.manage-actions{justify-content:flex-start}.manage-grid{grid-template-columns:1fr}.manage-form.wide{grid-column:auto}}
@media(max-width:760px){header{height:auto;min-height:68px;flex-wrap:wrap;padding:14px 0}.lang-switch{margin-left:0}.nav{width:100%;flex-wrap:wrap;gap:8px}.site-nav{overflow-x:auto;padding-bottom:2px}.site-nav a{white-space:nowrap}.chip{min-width:0;max-width:100%;overflow:hidden;text-overflow:ellipsis}.grid{grid-template-columns:1fr}.overview-grid,.module-nav,.health-grid{grid-template-columns:1fr}.metrics,.pref-metrics{grid-template-columns:1fr}.row{grid-template-columns:minmax(0,1fr) minmax(70px,.8fr) 3.2em}.card-title-row{align-items:flex-start;flex-direction:column}.filter-card{position:static}.wrap{padding:0 16px 48px}}
`;

const LANG_SWITCH = `<span class="lang-switch" role="group" aria-label="Language"><button type="button" data-lang-option="zh">中文</button><button type="button" data-lang-option="en">EN</button></span>`;

const LANG_SCRIPT = `<script>(()=>{const k="reasonix.stats.lang";const b=[...document.querySelectorAll("[data-lang-option]")];const pick=()=>{try{return localStorage.getItem(k)}catch{return""}};const fallback=()=>((navigator.language||"").toLowerCase().startsWith("zh")?"zh":"en");const set=(v)=>{const lang=v==="en"?"en":"zh";document.body.dataset.lang=lang;b.forEach((x)=>{const on=x.dataset.langOption===lang;x.classList.toggle("active",on);x.setAttribute("aria-pressed",String(on))});try{localStorage.setItem(k,lang)}catch{}};b.forEach((x)=>x.addEventListener("click",()=>set(x.dataset.langOption||"zh")));set(pick()||fallback())})();(()=>{const fallback=(t)=>{const a=document.createElement("textarea");a.value=t;a.setAttribute("readonly","");a.style.position="fixed";a.style.opacity="0";document.body.appendChild(a);a.select();let ok=false;try{ok=document.execCommand("copy")}catch{}a.remove();return ok};document.addEventListener("click",async(e)=>{const b=e.target.closest("[data-copy]");if(!b)return;const t=b.dataset.copy||"";let ok=false;try{if(navigator.clipboard){await navigator.clipboard.writeText(t);ok=true}}catch{}if(!ok)ok=fallback(t);b.dataset.state=ok?"copied":"failed";setTimeout(()=>{delete b.dataset.state},1400)})})()</script>`;

const STATS_ROUTE_SCRIPT = `<script>(()=>{if(location.pathname!=="/stats"||!location.hash)return;const m={diagnostics:"diagnostics",usage:"",preferences:"preferences",health:"health"};const key=location.hash.slice(1);if(m[key]===undefined)return;location.replace("/stats"+(m[key]?"/"+m[key]:"")+location.search)})()</script>`;

export function page(title: string, crumb: string, body: string, nav = ""): string {
  return `<!doctype html><html lang="en"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1"><meta name="robots" content="noindex">
<title>${esc(title)}</title><style>${CSS}</style></head><body><div class="wrap">
<header><a class="brand" href="https://reasonix.io">${LOGO}</a><a class="brand" href="https://reasonix.io">Reasonix</a><span class="crumb">/ ${esc(crumb)}</span>${LANG_SWITCH}${nav ? `<span class="nav">${nav}</span>` : ""}</header>
${body}</div>${LANG_SCRIPT}${STATS_ROUTE_SCRIPT}</body></html>`;
}

export function html(body: string): Response {
  return new Response(body, { headers: { "content-type": "text/html; charset=utf-8" } });
}

export function redirect(location: string, cookie?: string): Response {
  const headers: Record<string, string> = { location };
  if (cookie) headers["set-cookie"] = cookie;
  return new Response(null, { status: 303, headers });
}
