package main

// The dashboard page: dark flight-recorder instrument panel, all-monospace
// type, recessed trace screens. The renderer is incremental (keyed DOM,
// in-place updates) so cells animate, numbers tween, and hover survives
// polling; a replay engine scrubs the run by wall-clock timestamp. Win/break
// polarity is carried by geometry, never by the green/red pair alone.
const serveHTML = `<!doctype html>
<html lang="en"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="theme-color" content="#0d0d0d">
<title>e2ebench · live</title>
<style>
:root {
  color-scheme: dark;
  --page: #0d0d0d; --panel: #1a1a19; --screen: #141413;
  --ink: #f2f1ec; --ink-2: #c3c2b7; --muted: #898781; --faint: #55544f;
  --hairline: rgba(255,255,255,0.08); --tick: rgba(255,255,255,0.05);
  --grid: #2c2c2a;
  --explore: #3987e5; --attempt: #d95926;
  --win: #0ca30c; --break: #d03b3b;
  --accent: #86b6ef;
}
* { box-sizing: border-box; margin: 0; }
html { background: var(--page); scroll-behavior: smooth; }
body { color: var(--ink);
  font: 13px/1.55 ui-monospace, "SF Mono", SFMono-Regular, Menlo, Consolas, monospace;
  padding: 34px clamp(18px, 4.5vw, 56px) 80px; max-width: 1280px; margin: 0 auto;
  font-variant-numeric: tabular-nums; }
section, .rule, header, #runseg, #deck, #cluster { animation: rise .5s ease-out backwards; }
#runseg { animation-delay: .05s; } #deck { animation-delay: .1s; }
#cluster { animation-delay: .15s; }
@keyframes rise { from { opacity: 0; transform: translateY(8px); } }

.rule { border: 0; border-top: 1px solid var(--hairline); margin: 40px 0 0; }
.eyebrow { font-size: 10px; letter-spacing: 0.22em; text-transform: uppercase;
  color: var(--muted); margin: 10px 0 14px; }
.eyebrow b { color: var(--ink-2); font-weight: 600; }
.caption { color: var(--muted); font-size: 12px; max-width: 72ch; margin: -6px 0 18px; }
.caption b { color: var(--ink-2); font-weight: 600; }

header { display: flex; align-items: baseline; }
.wordmark { font-size: 13px; font-weight: 700; letter-spacing: 0.08em; }
.wordmark em { font-style: normal; color: var(--accent); }
.wordmark span { color: var(--muted); font-weight: 400; }
#clock { margin-left: auto; color: var(--muted); font-size: 12px; }

#runseg { display: flex; gap: 3px; margin-top: 22px; }
#runseg i { flex: 1 1 0; max-width: 22px; height: 14px; border-radius: 2px;
  background: var(--grid); cursor: pointer;
  transition: background .3s, box-shadow .3s, transform .12s; }
#runseg i:hover { transform: translateY(-2px); }
#runseg i.done { background: var(--accent); opacity: 0.85; }
#runseg i.live { background: var(--attempt);
  box-shadow: 0 0 10px color-mix(in srgb, var(--attempt) 55%, transparent);
  animation: pulse 1.1s ease-in-out infinite; }

#deck { display: flex; align-items: center; gap: 14px; margin-top: 16px;
  padding: 10px 14px; background: var(--panel); border: 1px solid var(--hairline);
  border-radius: 10px; }
#deck button, #deck select { font: inherit; color: var(--ink-2); background: transparent;
  border: 1px solid var(--hairline); border-radius: 6px; padding: 4px 12px;
  cursor: pointer; transition: color .15s, border-color .15s, background .15s; }
#deck button:hover { color: var(--ink); border-color: var(--muted); }
#deck button.hot { color: var(--attempt); border-color: var(--attempt); }
#ptime { color: var(--muted); font-size: 12px; min-width: 15ch; }
#scrub { flex: 1 1 auto; -webkit-appearance: none; appearance: none; height: 4px;
  background: var(--grid); border-radius: 2px; cursor: pointer; }
#scrub::-webkit-slider-thumb { -webkit-appearance: none; width: 14px; height: 14px;
  border-radius: 50%; background: var(--ink-2); border: 2px solid var(--page);
  transition: transform .12s; }
#scrub::-webkit-slider-thumb:hover { transform: scale(1.25); }

#cluster { display: flex; gap: 48px; margin-top: 20px; flex-wrap: wrap; }
.gauge b { display: block; font-size: 30px; font-weight: 300; letter-spacing: -0.01em;
  line-height: 1.1; }
.gauge b small { font-size: 15px; color: var(--muted); font-weight: 300; }
.gauge span { display: block; font-size: 10px; letter-spacing: 0.2em;
  text-transform: uppercase; color: var(--muted); margin-top: 5px; }

#now { display: flex; gap: 36px; align-items: stretch; min-height: 118px; }
#now .main { flex: 1 1 auto; min-width: 0; }
#now .head { display: flex; align-items: baseline; gap: 16px; }
#now .name { font-size: 17px; font-weight: 700; letter-spacing: 0.02em; cursor: pointer; }
#now .name:hover { color: var(--accent); }
#now .rec { color: var(--attempt); font-size: 11px; letter-spacing: 0.2em;
  animation: pulse 1.1s ease-in-out infinite; }
#now .doing { color: var(--ink-2); font-size: 12px; transition: color .3s; }
#now .side { display: flex; flex-direction: column; gap: 14px; padding-left: 32px;
  border-left: 1px solid var(--hairline); }
#now .side .gauge b { font-size: 22px; }
#now.idle { color: var(--muted); font-size: 12px; display: block; min-height: 0; }

.screen { background: var(--screen); border: 1px solid var(--hairline);
  border-radius: 8px; padding: 0 12px; margin-top: 14px; position: relative;
  background-image: repeating-linear-gradient(90deg, var(--tick) 0 1px, transparent 1px 75px);
  background-origin: content-box; transition: border-color .3s, box-shadow .3s; }
.trace { display: flex; align-items: center; height: 56px; position: relative; }
.trace::before { content: ""; position: absolute; left: 0; right: 0; top: 50%;
  border-top: 1px solid var(--grid); }
.cells { display: flex; align-items: center; height: 100%; overflow-x: auto; flex: 1; }
.cell { flex: 0 0 12px; height: 12px; border-radius: 2px; margin-right: 3px;
  background: var(--grid); align-self: center; position: relative;
  transition: transform .12s; }
.cell:hover { transform: scale(1.4); z-index: 2; }
.cell.e { background: var(--explore); }
.cell.a { background: var(--attempt); }
.cell.o { background: var(--win); height: 44px; align-self: flex-start;
  border-radius: 3px 3px 0 0;
  box-shadow: 0 0 12px color-mix(in srgb, var(--win) 50%, transparent); }
.cell.r { background: var(--break); height: 44px; align-self: flex-end;
  border-radius: 0 0 3px 3px;
  box-shadow: 0 0 12px color-mix(in srgb, var(--break) 50%, transparent); }
.cell.new { animation: pop .3s cubic-bezier(.2,.9,.3,1.35); }
.cell.o.new, .cell.r.new { animation: pop .3s cubic-bezier(.2,.9,.3,1.35),
  flare .9s ease-out; }
@keyframes pop { from { transform: scale(.2); opacity: 0; } }
@keyframes flare { 0% { box-shadow: 0 0 2px 8px color-mix(in srgb, currentColor 40%, transparent); }
  100% { } }
.cell.cursor { background: transparent; border: 1px dashed var(--muted);
  animation: pulse 1.1s ease-in-out infinite; flex: none; }
.mini .cell { flex-basis: 7px; height: 7px; margin-right: 2px; border-radius: 1.5px; }
.mini .cell.o, .mini .cell.r { height: 21px; box-shadow: none; }
.mini.trace { height: 30px; }

#kpis { display: grid; grid-template-columns: repeat(auto-fit, minmax(240px, 1fr)); }
.kpi { padding: 4px 28px 6px 0; }
.kpi + .kpi { border-left: 1px solid var(--hairline); padding-left: 28px; }
.kpi b { font-size: 38px; font-weight: 300; line-height: 1.05; letter-spacing: -0.01em;
  transition: color .3s; }
.kpi b small { font-size: 14px; color: var(--muted); font-weight: 400; }
.kpi.win b { color: var(--win); text-shadow: 0 0 18px color-mix(in srgb, var(--win) 35%, transparent); }
.kpi.break b { color: var(--break); }
.kpi.zero b { color: var(--faint); text-shadow: none; }
.kpi .label { font-size: 10px; letter-spacing: 0.2em; text-transform: uppercase;
  color: var(--ink-2); margin-top: 8px; }
.kpi .why { color: var(--muted); font-size: 11.5px; margin-top: 6px; line-height: 1.5;
  font-family: system-ui, sans-serif; }
.kpi .bar { height: 3px; background: var(--grid); margin-top: 12px; border-radius: 2px;
  overflow: hidden; }
.kpi .bar i { display: block; height: 100%; background: var(--attempt);
  transition: width .6s ease; }

#legend { display: flex; flex-wrap: wrap; gap: 10px 30px; }
#legend span { display: inline-flex; align-items: center; gap: 9px; font-size: 12px;
  color: var(--ink-2); }
#legend i { width: 10px; height: 10px; border-radius: 2px; flex: none; }
#legend .up, #legend .down { height: 26px; }
#legend .up i { height: 20px; border-radius: 3px 3px 0 0; align-self: flex-start; }
#legend .down i { height: 20px; border-radius: 0 0 3px 3px; align-self: flex-end; }

#wall { display: grid; grid-template-columns: repeat(auto-fill, minmax(280px, 1fr));
  gap: 26px 30px; }
.task { transition: opacity .4s; }
.task .top { display: flex; align-items: baseline; gap: 8px; }
.task .name { font-size: 12px; font-weight: 700; letter-spacing: 0.02em;
  overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.task .badges { margin-left: auto; display: flex; gap: 8px; font-size: 11px; flex: none; }
.badge.win { color: var(--win); } .badge.break { color: var(--break); }
.badge.guard { color: var(--attempt); }
.task .screen { margin-top: 8px; padding: 0 8px;
  background-image: repeating-linear-gradient(90deg, var(--tick) 0 1px, transparent 1px 45px); }
.task:hover .screen { border-color: var(--muted); }
.task.live .screen { border-color: var(--attempt);
  box-shadow: 0 0 14px color-mix(in srgb, var(--attempt) 25%, transparent); }
.task.flash .screen { animation: flashring 1.2s ease-out; }
@keyframes flashring { 0% { border-color: var(--accent);
  box-shadow: 0 0 0 4px color-mix(in srgb, var(--accent) 45%, transparent); } }
.task .meta { color: var(--muted); font-size: 11px; margin-top: 7px; }
.task .meta b { color: var(--ink-2); font-weight: 600; }
.task.queued { opacity: .55; }
.task.queued .name { color: var(--faint); font-weight: 400; }
.task.queued .screen { border-style: dashed; background-image: none; }
.qnote { color: var(--faint); font-size: 11px; letter-spacing: 0.18em;
  text-transform: uppercase; margin: auto; }

#tip { position: fixed; z-index: 10; pointer-events: none; background: var(--panel);
  border: 1px solid var(--hairline); border-radius: 8px; padding: 8px 12px;
  font-size: 12px; color: var(--ink-2); max-width: 320px; line-height: 1.5;
  box-shadow: 0 8px 30px rgba(0,0,0,.5); opacity: 0; transform: translateY(4px) scale(.97);
  transition: opacity .15s, transform .15s; }
#tip.on { opacity: 1; transform: none; }
#tip b { color: var(--ink); }

@keyframes pulse { 50% { opacity: 0.3; } }
@media (prefers-reduced-motion: reduce) { * { animation: none !important; transition: none !important; }
  html { scroll-behavior: auto; } }
</style></head>
<body>
<header>
  <div class="wordmark"><em>reasonix</em> e2ebench <span>· flight recorder</span></div>
  <span id="clock"></span>
</header>
<div id="runseg"></div>
<div id="deck">
  <button id="play" title="replay the run from its trajectories">▶ replay</button>
  <input id="scrub" type="range" min="0" max="1000" value="1000">
  <span id="ptime"></span>
  <select id="speed"><option value="30">30×</option><option value="120" selected>120×</option><option value="600">600×</option></select>
  <button id="golive">● live</button>
</div>
<div id="cluster"></div>

<hr class="rule"><div class="eyebrow">now running</div>
<div id="now" class="idle">waiting for the first task…</div>

<hr class="rule"><div class="eyebrow">shadow scorer verdicts</div>
<p class="caption">A second scorer watches every tool round and only credits <b>verified</b> progress — a recognized check going from fail to pass. Everything else is motion.</p>
<section id="kpis"></section>

<hr class="rule"><div class="eyebrow">how to read a trace</div>
<div id="legend">
  <span><i style="background:var(--grid)"></i>quiet — nothing new</span>
  <span><i style="background:var(--explore)"></i>exploring — new files or commands</span>
  <span><i style="background:var(--attempt)"></i>working — edits or checks, unverified</span>
  <span class="up"><i style="background:var(--win)"></i>verified win — failing check turned green</span>
  <span class="down"><i style="background:var(--break)"></i>broke something — passing check failed</span>
</div>

<hr class="rule"><div class="eyebrow" id="walllabel">all tasks</div>
<section id="wall"></section>
<div id="tip"></div>

<script>
"use strict";
var $ = function (id) { return document.getElementById(id); };
var LIVE_MS = 15000;
var state = null, fetchedAt = 0, suite = [], byID = {};
var segs = {}, cards = {};
var R = { on: false, playing: false, t: 0, speed: 120, t0: 0, t1: 0, last: 0 };
var nowShown = null;

function fmtDur(ms) {
  if (!ms || ms < 0) return "0s";
  if (ms < 90000) return (ms / 1000).toFixed(0) + "s";
  return Math.floor(ms / 60000) + "m" + String(Math.round((ms % 60000) / 1000)).padStart(2, "0") + "s";
}
function fmtOff(ms) {
  var s = Math.max(0, Math.round(ms / 1000));
  return "T+" + String(Math.floor(s / 60)).padStart(2, "0") + ":" + String(s % 60).padStart(2, "0");
}
function catOf(r) { return r.r ? "r" : r.o ? "o" : (r.c || r.v) ? "a" : r.e ? "e" : ""; }
function nameOf(r) {
  return r.r ? "broke something" : r.o ? "verified win" :
    (r.c || r.v) ? "working" : r.e ? "exploring" : "quiet";
}
function tipOf(i, r) {
  var p = [];
  if (r.o) p.push(r.o + " check(s) turned green");
  if (r.r) p.push(r.r + " check(s) broke");
  if (r.v) p.push(r.v + " check runs");
  if (r.c) p.push(r.c + " edits");
  if (r.e) p.push(r.e + " new files/commands");
  if (!p.length) p.push("repeat of earlier work");
  return "<b>round " + (i + 1) + " — " + nameOf(r) + "</b><br>" + p.join(", ");
}
function cellNode(i, r, animate) {
  var d = document.createElement("div");
  d.className = "cell " + catOf(r) + (animate ? " new" : "");
  d.dataset.tip = tipOf(i, r);
  return d;
}
function countVis(rounds, cut) {
  var n = 0;
  while (n < rounds.length && (rounds[n].t || 0) <= cut) n++;
  return n;
}
function goTo(id) {
  var c = cards[id];
  if (!c) return;
  c.el.scrollIntoView({ behavior: "smooth", block: "center" });
  c.el.classList.remove("flash");
  void c.el.offsetWidth;
  c.el.classList.add("flash");
}

function ensure() {
  suite.forEach(function (id) {
    if (!segs[id]) {
      var i = document.createElement("i");
      i.dataset.tip = "<b>" + id + "</b><br>click to jump to its card";
      i.addEventListener("click", function () { goTo(id); });
      $("runseg").appendChild(i);
      segs[id] = i;
    }
    if (!cards[id]) {
      var el = document.createElement("div");
      el.className = "task queued";
      el.innerHTML = '<div class="top"><span class="name">' + id +
        '</span><div class="badges"></div></div>' +
        '<div class="screen"><div class="trace mini"><div class="cells"></div>' +
        '<span class="qnote">queued</span></div></div><div class="meta"></div>';
      $("wall").appendChild(el);
      cards[id] = { el: el, cells: el.querySelector(".cells"), badges: el.querySelector(".badges"),
        meta: el.querySelector(".meta"), qnote: el.querySelector(".qnote"), built: 0, queued: true };
    }
  });
}

function updateCard(id, t, cut, liveNow) {
  var c = cards[id];
  var rounds = t ? t.rounds : [];
  var vis = t ? (cut === Infinity ? rounds.length : countVis(rounds, cut)) : 0;
  if (!t || vis === 0) {
    c.el.classList.add("queued"); c.el.classList.remove("live");
    c.qnote.style.display = ""; c.cells.innerHTML = ""; c.built = 0;
    c.badges.innerHTML = ""; c.meta.textContent = "";
    return { o: 0, r: 0 };
  }
  c.el.classList.remove("queued");
  c.qnote.style.display = "none";
  if (vis < c.built) { c.cells.innerHTML = ""; c.built = 0; }
  var animate = vis - c.built <= 12;
  for (var i = c.built; i < vis; i++) c.cells.appendChild(cellNode(i, rounds[i], animate));
  c.built = vis;
  var o = 0, rg = 0;
  for (var j = 0; j < vis; j++) { o += rounds[j].o || 0; rg += rounds[j].r || 0; }
  c.badges.innerHTML =
    (o ? '<span class="badge win" data-tip="verified wins">↑' + o + "</span>" : "") +
    (rg ? '<span class="badge break" data-tip="regressions">↓' + rg + "</span>" : "") +
    (t.no_progress ? '<span class="badge guard" data-tip="progress guard interventions">g' + t.no_progress + "</span>" : "");
  var span = cut === Infinity ? t.span_ms
    : Math.max(0, (rounds[vis - 1].t || 0) - ((rounds[0].t || 0)));
  c.meta.innerHTML = "<b>" + vis + "</b> rounds · " + fmtDur(span) +
    (liveNow ? ' · <span class="badge guard">running</span>' : "");
  c.el.classList.toggle("live", liveNow);
  return { o: o, r: rg };
}

function updateNow(liveIDs, cut) {
  var el = $("now");
  if (!liveIDs.length) {
    nowShown = null;
    el.className = "idle";
    var complete = suite.length > 0 && suite.every(function (id) { return byID[id]; });
    el.textContent = R.on ? "…" :
      complete ? "run complete — press ▶ replay to watch it back" :
        "between tasks — grading the last one or starting the next…";
    return;
  }
  var id = liveIDs[liveIDs.length - 1];
  var t = byID[id];
  var rounds = t.rounds;
  var vis = cut === Infinity ? rounds.length : countVis(rounds, cut);
  if (nowShown !== id) {
    nowShown = id;
    el.className = "";
    el.innerHTML = '<div class="main"><div class="head"><span class="rec">● rec</span>' +
      '<span class="name" data-goto="' + id + '">' + id + "</span>" +
      '<span class="doing"></span></div>' +
      '<div class="screen"><div class="trace"><div class="cells"></div>' +
      '<div class="cell cursor" data-tip="next round"></div></div></div></div>' +
      '<div class="side"><div class="gauge"><b class="g-round"></b><span>round</span></div>' +
      '<div class="gauge"><b class="g-elapsed"></b><span>elapsed</span></div>' +
      '<div class="gauge"><b class="g-tools"></b><span>in tools</span></div></div>';
    el.querySelector(".name").addEventListener("click", function () { goTo(id); });
    el._built = 0;
  }
  var cells = el.querySelector(".cells");
  if (vis < el._built) { cells.innerHTML = ""; el._built = 0; }
  var animate = vis - el._built <= 12;
  for (var i = el._built; i < vis; i++) cells.appendChild(cellNode(i, rounds[i], animate));
  el._built = vis;
  var last = rounds[vis - 1] || {};
  el.querySelector(".doing").textContent = nameOf(last) + "…";
  el.querySelector(".g-round").textContent = vis;
  var elapsed = cut === Infinity
    ? t.span_ms + (state ? Date.now() - fetchedAt : 0)
    : Math.max(0, (last.t || 0) - (rounds[0] ? rounds[0].t || 0 : 0));
  el.querySelector(".g-elapsed").textContent = fmtDur(elapsed);
  el.querySelector(".g-tools").textContent = fmtDur(t.tool_ms);
}

function gauge(v, label) {
  return '<div class="gauge"><b>' + v + "</b><span>" + label + "</span></div>";
}
function kpiHTML(cls, value, unit, label, why, ratio) {
  return '<div class="kpi ' + cls + '"><b>' + value + (unit ? "<small> " + unit + "</small>" : "") +
    '</b><div class="label">' + label + '</div><div class="why">' + why + "</div>" +
    (ratio !== undefined ? '<div class="bar"><i style="width:' + Math.min(100, ratio) + '%"></i></div>' : "") +
    "</div>";
}

function applyAll() {
  if (!state) return;
  var cut = R.on ? R.t : Infinity;
  ensure();
  var doneN = 0, liveIDs = [], obj = 0, reg = 0;
  suite.forEach(function (id) {
    var t = byID[id];
    var liveNow = false, isDone = false;
    if (t) {
      if (R.on) {
        var vis = countVis(t.rounds, cut);
        liveNow = vis > 0 && vis < t.rounds.length;
        isDone = t.rounds.length > 0 && vis >= t.rounds.length;
      } else {
        liveNow = t.ago_ms >= 0 && t.ago_ms < LIVE_MS;
        isDone = !liveNow;
      }
    }
    if (isDone) doneN++;
    if (liveNow) liveIDs.push(id);
    segs[id].className = liveNow ? "live" : isDone ? "done" : "";
    var c = updateCard(id, t, cut, liveNow);
    obj += c.o; reg += c.r;
  });

  var spans = [];
  suite.forEach(function (id) {
    var t = byID[id];
    if (t && t.span_ms && (!R.on || countVis(t.rounds, cut) >= t.rounds.length)) spans.push(t.span_ms);
  });
  var avg = spans.length ? spans.reduce(function (a, b) { return a + b; }, 0) / spans.length : 0;
  var left = suite.length - doneN - liveIDs.length;
  $("cluster").innerHTML =
    gauge(doneN + "<small> / " + suite.length + "</small>", "tasks recorded") +
    (avg ? gauge(fmtDur(avg), "per task") : "") +
    (R.on ? gauge(fmtOff(R.t - R.t0), "playhead") :
      left + liveIDs.length > 0 && avg
        ? gauge("~" + fmtDur(avg * (left + liveIDs.length * 0.5)), "time left")
        : gauge("done", "status")) +
    (liveIDs.length ? gauge(liveIDs.length, R.on ? "on screen" : "running now") : "") +
    (left > 0 ? gauge(left, "queued") : "");

  updateNow(liveIDs, cut);

  var atEnd = !R.on || R.t >= R.t1;
  var fp = 0, pr = 0, stall = 0, regressed = 0;
  state.tasks.forEach(function (t) {
    var o = t.outcome; if (!o) return;
    fp += o.false_progress_rounds || 0; pr += o.progress_rounds || 0;
    stall = Math.max(stall, o.solution_stall_max || 0);
    if (o.regressed_from_best) regressed++;
  });
  $("kpis").innerHTML =
    kpiHTML(obj ? "win" : "zero", obj, "", "verified wins",
      "Rounds where a failing check turned green — the only progress the shadow scorer trusts.") +
    kpiHTML(reg ? "break" : "zero", reg + (regressed && atEnd ? " / " + regressed : ""), "",
      "regressions" + (regressed && atEnd ? " / peaked early" : ""),
      "A previously passing check broke" + (regressed && atEnd ? "; some runs ended below their best state." : ".")) +
    kpiHTML("", atEnd && pr ? Math.round(100 * fp / pr) + "%" : "–", atEnd && pr ? fp + " of " + pr : "",
      "false progress",
      "Rounds the current scorer counted as progress that never turned into a verified win.",
      atEnd && pr ? 100 * fp / pr : 0) +
    kpiHTML("", atEnd && stall ? stall : "–", atEnd && stall ? "rounds" : "", "longest stall",
      "Most consecutive rounds of work with no verified win — where an agent burns time.");

  $("walllabel").innerHTML = "all tasks · <b>" + suite.length + "</b>";
  $("scrub").value = R.on && R.t1 > R.t0 ? Math.round(1000 * (R.t - R.t0) / (R.t1 - R.t0)) : 1000;
  $("ptime").textContent = R.on
    ? fmtOff(R.t - R.t0) + " / " + fmtOff(R.t1 - R.t0)
    : "live tail";
  $("play").textContent = R.playing ? "⏸ pause" : "▶ replay";
  $("play").classList.toggle("hot", R.playing);
}

function bounds() {
  var t0 = Infinity, t1 = 0;
  state.tasks.forEach(function (t) {
    if (t.rounds.length) {
      t0 = Math.min(t0, t.rounds[0].t || Infinity);
      t1 = Math.max(t1, t.rounds[t.rounds.length - 1].t || 0);
    }
  });
  R.t0 = t0 === Infinity ? 0 : t0;
  R.t1 = t1;
}

function frame(now) {
  if (!R.playing) return;
  var dt = R.last ? now - R.last : 16;
  R.last = now;
  R.t += dt * R.speed;
  if (R.t >= R.t1) { R.t = R.t1; R.playing = false; }
  applyAll();
  if (R.playing) requestAnimationFrame(frame);
}
$("play").addEventListener("click", function () {
  if (!state) return;
  bounds();
  if (R.playing) { R.playing = false; applyAll(); return; }
  if (!R.on || R.t >= R.t1) { R.on = true; R.t = R.t0; resetBuilt(); }
  R.playing = true; R.last = 0;
  requestAnimationFrame(frame);
});
$("golive").addEventListener("click", function () {
  R.on = false; R.playing = false; resetBuilt(); applyAll();
});
$("scrub").addEventListener("input", function () {
  if (!state) return;
  bounds();
  R.on = true;
  R.t = R.t0 + (R.t1 - R.t0) * (this.value / 1000);
  applyAll();
});
$("speed").addEventListener("change", function () { R.speed = +this.value; });
function resetBuilt() {
  Object.keys(cards).forEach(function (id) { cards[id].cells.innerHTML = ""; cards[id].built = 0; });
  nowShown = null;
}

var tip = $("tip");
document.addEventListener("mouseover", function (e) {
  var n = e.target.closest ? e.target.closest("[data-tip]") : null;
  if (n) { tip.innerHTML = n.dataset.tip; tip.classList.add("on"); }
  else tip.classList.remove("on");
});
document.addEventListener("mousemove", function (e) {
  if (!tip.classList.contains("on")) return;
  var x = Math.min(e.clientX + 14, window.innerWidth - tip.offsetWidth - 10);
  var y = e.clientY + 16;
  if (y + tip.offsetHeight > window.innerHeight - 8) y = e.clientY - tip.offsetHeight - 10;
  tip.style.left = x + "px"; tip.style.top = y + "px";
});

setInterval(function () {
  if (state && !R.on) applyAll();
}, 1000);

function tick() {
  fetch("/api/state").then(function (r) { return r.json(); }).then(function (st) {
    state = st; fetchedAt = Date.now();
    byID = {};
    st.tasks.forEach(function (t) { byID[t.id] = t; });
    suite = st.suite && st.suite.length ? st.suite : st.tasks.map(function (t) { return t.id; });
    $("clock").textContent = new Date(st.now).toLocaleTimeString();
    if (!R.on) applyAll();
    if (location.hash === "#replay" && !R.on) { location.hash = ""; $("play").click(); }
  }).catch(function () {
    $("clock").textContent = "poll failed — is the server still up?";
  });
}
tick();
setInterval(tick, 2000);
</script>
</body></html>
`
