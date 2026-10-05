// A structural sweep of the rendered interface: what the pixels resolve to, in
// both themes, at several widths, along a walk that opens the panels a static
// read never reaches.
//
// Each judgement is a property of the render, never a guess about intent:
//   contrast  text against the surface it actually lands on (AA, 4.5 / 3 large)
//   truncated clipped text with no title to read the rest from
//   clippedY  text cut off vertically outside a line-clamp
//   spill     ink wider than its box with nobody clipping it — it lands on a neighbour
//   offscreen an element past the viewport edge, background layers excluded
//   overflowX the document itself scrolling sideways
//   unnamed   a control no assistive technology can announce
//   dupId     two elements answering to one id
//   hanCaps   Han text under Latin small-caps typography — letterspacing pulls
//             the characters apart and uppercase means nothing to them
//
// Run it with a kernel and the dev server up, and a headless Chrome on 9333:
//
//   /path/to/reasonix serve -addr 127.0.0.1:8791 -auth none
//   REASONIX_SERVE=http://127.0.0.1:8791 npx vite --port 5177 --strictPort
//   chrome-headless-shell --remote-debugging-port=9333 --headless
//   node tools/audit-sweep.mjs http://localhost:5177/ dark 1440 950
//
// A second vite with no REASONIX_SERVE boots the fixture instead, which is the
// only way to reach a transcript with tool cards, plans and approvals in it.
// That path opens on the connect card, so hand it a script that fills the form
// and clicks through: ONB=path/to/onboard.js node tools/audit-sweep.mjs ...
//
// Two things it deliberately does not do quietly: a probe that fails to
// evaluate throws rather than returning an empty list, because an empty list
// reads exactly like a clean sweep; and a step whose control was not on screen
// is named in  rather than passing as walked.
const url = process.argv[2], theme = process.argv[3] || "dark";
const W = Number(process.argv[4] || 1440), H = Number(process.argv[5] || 950);
const { readFileSync } = await import("node:fs");
const { STEPS, SETTINGS_OPEN, SETTINGS_COUNT, settingsTab } = await import("./steps.mjs");
const list = await (await fetch("http://127.0.0.1:9333/json/list")).json();
const t = list.find((x) => x.type === "page");
const ws = new WebSocket(t.webSocketDebuggerUrl);
let id = 0; const pend = new Map();
const send = (m, p = {}) => { const i = ++id; ws.send(JSON.stringify({ id: i, method: m, params: p })); return new Promise((r) => pend.set(i, r)); };
const rt = { exc: [], err: [], warn: [], net: [] };
await new Promise((r) => (ws.onopen = r));
ws.onmessage = (e) => {
  const m = JSON.parse(e.data);
  if (m.id && pend.has(m.id)) { pend.get(m.id)(m.result); pend.delete(m.id); return; }
  if (m.method === "Runtime.exceptionThrown") rt.exc.push((m.params.exceptionDetails.exception?.description || m.params.exceptionDetails.text || "").slice(0, 200));
  if (m.method === "Runtime.consoleAPICalled") {
    const s = m.params.args.map((a) => a.value ?? a.description ?? "").join(" ").slice(0, 200);
    if (m.params.type === "error") rt.err.push(s); if (m.params.type === "warning") rt.warn.push(s);
  }
  if (m.method === "Network.responseReceived" && m.params.response.status >= 400)
    rt.net.push(m.params.response.status + " " + m.params.response.url.replace(/^https?:\/\/[^/]+/, ""));
};
const ev = async (x) => {
  const r = await send("Runtime.evaluate", { expression: x, returnByValue: true, awaitPromise: true });
  if (r?.exceptionDetails) throw new Error("probe failed: " + (r.exceptionDetails.exception?.description || r.exceptionDetails.text));
  return r?.result?.value;
};
const wait = (ms) => new Promise((r) => setTimeout(r, ms));
await send("Runtime.enable"); await send("Page.enable"); await send("Network.enable");
await send("Emulation.setDeviceMetricsOverride", { width: W, height: H, deviceScaleFactor: 1, mobile: false });
await send("Page.navigate", { url }); await wait(2500);
await ev(`localStorage.setItem("rx-lang","en");localStorage.setItem("rx-theme",${JSON.stringify(theme)})`);
await send("Page.navigate", { url }); await wait(4500);
// What the entry script reached is reported, never dropped: a card it failed to
// answer leaves every step behind it unreachable, and that reads in the output
// as a dozen skipped steps rather than as the one thing that went wrong.
let onb = null;
if (process.env.ONB) { onb = await ev(readFileSync(process.env.ONB, "utf8")); await wait(3500); }

const P = `(() => {
  const vis = (el) => { const r = el.getBoundingClientRect(); const s = getComputedStyle(el);
    return r.width > 0 && r.height > 0 && s.visibility !== "hidden" && s.display !== "none" && s.opacity !== "0"; };
  const path = (el) => { const p = []; let c = el;
    while (c && c !== document.body && p.length < 3) { p.push(c.tagName.toLowerCase() + (typeof c.className === "string" && c.className.trim() ? "." + c.className.trim().split(/\\s+/).slice(0,2).join(".") : "")); c = c.parentElement; }
    return p.reverse().join(">"); };
  // A box past the viewport edge inside a scroller is reachable by scrolling to
  // it, which is what .prefs-nav does at the narrowest window — an edge is only
  // an edge when nothing between here and it scrolls.
  const scrolled = (el) => { let c = el.parentElement; while (c && c !== document.body) {
    const s = getComputedStyle(c); if (/auto|scroll/.test(s.overflowX) && c.scrollWidth > c.clientWidth + 1) return true; c = c.parentElement; } return false; };
  const behind = (el) => { let c = el; while (c && c !== document.body) { if (parseFloat(getComputedStyle(c).opacity) < .9) return true; c = c.parentElement; } return false; };
  const nm = (el) => (el.textContent||"").trim() || el.getAttribute("aria-label") || el.getAttribute("title") || (el.getAttribute("aria-labelledby") ? (document.getElementById(el.getAttribute("aria-labelledby"))?.textContent||"").trim() : "") || el.getAttribute("placeholder") || "";
  const out = { truncated: [], clippedY: [], offscreen: [], overflowX: [], unnamed: [], dupId: [], imgNoAlt: [], spill: [], contrast: [], hanCaps: [] };
  // What the pixels resolve to, not what the token table promises: a colour is
  // checked against the surface it was designed for, then used on another one.
  const lum = (c) => { const [r,g,b] = c.map((v) => { v/=255; return v<=0.03928 ? v/12.92 : Math.pow((v+0.055)/1.055,2.4); }); return 0.2126*r+0.7152*g+0.0722*b; };
  // getComputedStyle now hands back oklch()/color-mix() verbatim, and digging the
  // numbers out of that reads three components as if they were rgb — every
  // judgement then compares two colours nobody painted. Paint one pixel and read
  // it back instead: whatever the browser resolved is what gets measured.
  const _cx = Object.assign(document.createElement("canvas"), { width: 1, height: 1 }).getContext("2d", { willReadFrequently: true });
  const parseC = (x) => { _cx.clearRect(0,0,1,1); _cx.fillStyle = "#000"; _cx.fillStyle = x; _cx.fillRect(0,0,1,1);
    const d = _cx.getImageData(0,0,1,1).data; return [d[0], d[1], d[2], d[3]/255]; };
  const over = (f,b) => { const a = f.length>3?f[3]:1; return [0,1,2].map((i)=>f[i]*a+b[i]*(1-a)); };
  const bgOf = (el) => { const st=[]; let c=el;
    while (c && c !== document.documentElement) { const b=parseC(getComputedStyle(c).backgroundColor); if(b.length&&(b.length<4||b[3]>0)) st.push(b); c=c.parentElement; }
    let acc=[255,255,255]; const rb=parseC(getComputedStyle(document.documentElement).backgroundColor); if(rb[3]>0) acc=rb.slice(0,3);
    for (let i=st.length-1;i>=0;i--) acc=over(st[i],acc); return acc; };
  const hex = (c) => "#" + c.map((v)=>Math.round(v).toString(16).padStart(2,"0")).join("").toUpperCase();
  for (const el of document.querySelectorAll("*")) {
    if (el.children.length || !(el.textContent||"").trim() || !vis(el)) continue;
    const s2 = getComputedStyle(el);
    // Text painted through a gradient has no single colour to weigh: its own
    // is transparent by construction, which reads here as ink identical to the
    // surface. Say nothing rather than report 1.00 against a heading nobody
    // can see the problem in.
    if (/text/.test(s2.webkitBackgroundClip || s2.backgroundClip || "")) continue;
    // WCAG exempts an inactive component: a disabled control is greyed to say
    // it cannot be used, and holding it to body-text contrast buries the rest.
    if (el.closest("[disabled], [aria-disabled=true]")) continue;
    const px = parseFloat(s2.fontSize), bold = parseInt(s2.fontWeight,10) >= 700;
    const large = px >= 24 || (px >= 18.66 && bold);
    const need = large ? 3 : 4.5;
    const bg = bgOf(el); const fg0 = parseC(s2.color); const fg = fg0.length>3 ? over(fg0,bg) : fg0.slice(0,3);
    const L1 = lum(fg), L2 = lum(bg);
    const ratio = (Math.max(L1,L2)+0.05)/(Math.min(L1,L2)+0.05);
    if (ratio < need) out.contrast.push(ratio.toFixed(2) + " (need " + need + ") " + hex(fg) + " on " + hex(bg) + "  " + path(el) + " | " + (el.textContent||"").trim().slice(0,22));
  }
  // The box is not the ink: a name squeezed to nothing still paints its letters,
  // over whatever sits beside it. Clipped elements are truncation, not spill.
  for (const el of document.querySelectorAll("*")) {
    if (el.children.length || !(el.textContent || "").trim() || !vis(el)) continue;
    if (/hidden|clip|scroll|auto/.test(getComputedStyle(el).overflowX)) continue;
    const box = el.getBoundingClientRect();
    const rg = document.createRange(); rg.selectNodeContents(el);
    const b = rg.getBoundingClientRect();
    if (b.width > box.width + 2) out.spill.push(path(el) + " ink=" + Math.round(b.width) + " box=" + Math.round(box.width) + " | " + (el.textContent||"").trim().slice(0, 28));
  }
  for (const el of document.querySelectorAll("*")) {
    if (!vis(el)) continue;
    const s = getComputedStyle(el), txt = (el.textContent||"").trim();
    if (el.children.length === 0 && txt.length > 4 && !el.closest("[title]")) {
      if (el.scrollWidth > el.clientWidth + 1 && /hidden|clip/.test(s.overflowX)) out.truncated.push(path(el) + " | " + txt.slice(0, 44));
      if (el.scrollHeight > el.clientHeight + 1 && /hidden|clip/.test(s.overflowY) && s.webkitLineClamp === "none") out.clippedY.push(path(el) + " | " + txt.slice(0, 44));
    }
    const r = el.getBoundingClientRect();
    if (s.position !== "fixed" && r.width > 4 && !behind(el) && !scrolled(el)) {
      if (r.right > window.innerWidth + 1) out.offscreen.push(path(el) + " right=" + Math.round(r.right));
      if (r.left < -1) out.offscreen.push(path(el) + " left=" + Math.round(r.left));
    }
  }
  if (document.documentElement.scrollWidth > window.innerWidth + 1) out.overflowX.push("scrollWidth=" + document.documentElement.scrollWidth);
  for (const el of document.querySelectorAll('button,a[href],[role="button"],[role="tab"],input:not([type=hidden]),select,textarea'))
    // aria-hidden takes an element out of the accessibility tree on purpose: the
    // row is the control and the twist beside it is decoration.
    if (vis(el) && !el.closest('[aria-hidden="true"],[inert]') && !nm(el)) out.unnamed.push(path(el));
  const seen = new Map();
  for (const el of document.querySelectorAll("[id]")) seen.set(el.id, (seen.get(el.id)||0)+1);
  for (const [i, n] of seen) if (n > 1) out.dupId.push(i + " x" + n);
  for (const el of document.querySelectorAll("img")) if (vis(el) && el.getAttribute("alt") === null) out.imgNoAlt.push(path(el));
  // Small-caps typography is Latin lettering. Tracking separates words there and
  // pulls Han characters apart instead; uppercase has nothing to act on. The
  // judgement reads the resolved style, so it holds whatever rule set it.
  for (const el of document.querySelectorAll("*")) {
    if (!vis(el)) continue;
    const own = [...el.childNodes].filter((n) => n.nodeType === 3 && n.textContent.trim()).map((n) => n.textContent.trim()).join("");
    if (!own || !/[\u4e00-\u9fff]/.test(own)) continue;
    const s = getComputedStyle(el), ls = parseFloat(s.letterSpacing);
    if ((!isNaN(ls) && Math.abs(ls) > 0.4) || s.textTransform === "uppercase")
      out.hanCaps.push(path(el) + " ls=" + s.letterSpacing + " " + s.textTransform + " | " + own.slice(0, 24));
  }
  return out;
})()`;


const res = [], skipped = [];
for (const [label, act] of STEPS) {
  if (act) { const ok = await ev(act); if (!ok) { skipped.push(label); continue; } await wait(/^turn-/.test(label) ? 4500 : 1100); }
  res.push({ label, ...(await ev(P)) });
}
await ev(`document.body.click()`); await wait(400);
await ev(SETTINGS_OPEN); await wait(1400);
const n = await ev(SETTINGS_COUNT);
if (!n) skipped.push("settings"); else for (let i = 0; i < n; i++) {
  const nm2 = await ev(settingsTab(i));
  await wait(800); res.push({ label: nm2, ...(await ev(P)) });
}
const KEYS = ["contrast", "truncated", "clippedY", "offscreen", "overflowX", "spill", "unnamed", "dupId", "imgNoAlt", "hanCaps"];
const agg = Object.fromEntries(KEYS.map((k) => [k, new Map()]));
for (const r of res) for (const k of KEYS) for (const v of (r[k]||[])) if (!agg[k].has(v)) agg[k].set(v, r.label);
console.log(`\n##### ${url} theme=${theme} ${W}x${H} steps=${res.length} skipped=[${skipped.join(",")}]`);
if (onb) console.log("  entry:", JSON.stringify(onb));
for (const k of KEYS) { const m = agg[k]; if (!m.size) continue;
  console.log(`  ${k}: ${m.size}`); [...m.entries()].slice(0, 22).forEach(([v, w]) => console.log(`    [${w}] ${v}`)); }
console.log(`  runtime: exc=${rt.exc.length} err=${rt.err.length} warn=${rt.warn.length} net4xx=${new Set(rt.net).size}`);
[...new Set(rt.exc)].slice(0,8).forEach(x=>console.log("    EXC "+x));
[...new Set(rt.err)].slice(0,10).forEach(x=>console.log("    ERR "+x));
[...new Set(rt.warn)].slice(0,10).forEach(x=>console.log("    WARN "+x));
[...new Set(rt.net)].slice(0,10).forEach(x=>console.log("    NET "+x));
ws.close();

