// Reports Chinese still rendered by an English interface.
//
// A catalogue's coverage cannot be decided from where a string is written: a
// constant table holding Chinese is correct when its render site calls t(),
// and `t(variable)` hides its key from every static check there is. What is on
// screen is the only judge, so this walks the real interface and reads it.
//
// It takes the same walk as audit-sweep, from tools/steps.mjs, because a screen
// neither of them opens is a screen neither of them can judge.
//
// Run it with a kernel and the dev server up, and a headless Chrome on 9333:
//
//   /path/to/reasonix serve -addr 127.0.0.1:8791 -auth none
//   REASONIX_SERVE=http://127.0.0.1:8791 npx vite --port 5177 --strictPort
//   chrome-headless-shell --remote-debugging-port=9333 --headless
//   node tools/i18n-sweep.mjs http://localhost:5177/
//
// Point REASONIX_HOME at an empty directory holding only a config.toml with a
// provider: a home with real sessions reports their titles, which are the
// user's words and not the interface's. The fixture is no use here for the
// same reason — its scripted session is written in Chinese, and this judgement
// cannot tell a window's own words from the content it is showing.
const base = "http://127.0.0.1:9333";
const url = process.argv[2];
const { readFileSync } = await import("node:fs");
const { STEPS, SETTINGS_OPEN, SETTINGS_COUNT, settingsTab } = await import("./steps.mjs");

let id = 0;
const pending = new Map();
const send = (ws, method, params = {}) => {
  const msgId = ++id;
  ws.send(JSON.stringify({ id: msgId, method, params }));
  return new Promise((r) => pending.set(msgId, r));
};

const list = await (await fetch(base + "/json/list")).json();
const target = list.find((t) => t.type === "page");
const ws = new WebSocket(target.webSocketDebuggerUrl);
await new Promise((r) => (ws.onopen = r));
ws.onmessage = (ev) => {
  const m = JSON.parse(ev.data);
  if (m.id && pending.has(m.id)) { pending.get(m.id)(m.result); pending.delete(m.id); }
};

const evaluate = async (expr) => {
  const r = await send(ws, "Runtime.evaluate", { expression: expr, returnByValue: true, awaitPromise: true });
  if (r?.exceptionDetails) throw new Error("probe failed: " + (r.exceptionDetails.exception?.description || r.exceptionDetails.text));
  return r?.result?.value;
};
const wait = (ms) => new Promise((r) => setTimeout(r, ms));

await send(ws, "Runtime.enable");
await send(ws, "Page.enable");
await send(ws, "Emulation.setDeviceMetricsOverride", { width: 1440, height: 1000, deviceScaleFactor: 1, mobile: false });
// The interface language is stored locally and fixed at boot, so set it first.
await send(ws, "Page.navigate", { url });
await wait(3000);
await evaluate(`localStorage.setItem("rx-lang", "en")`);
await send(ws, "Page.navigate", { url });
await wait(4000);
let onb = null;
if (process.env.ONB) { onb = await evaluate(readFileSync(process.env.ONB, "utf8")); await wait(3500); }

// The kernel's own language setting outranks this local one and reloads the
// window with it, so a home configured for Chinese silently turns this sweep
// into a Chinese window reporting Chinese — every string a hit, none of them a
// finding. Read back what actually rendered rather than trusting the write.
const rendered = await evaluate(`document.documentElement.lang`);
if (!/^en/.test(rendered || "")) {
  throw new Error(`the window is rendering "${rendered}", not English: set language = "en" in the kernel's config, or unset it — it outranks rx-lang`);
}

const HAN = "[\\u4e00-\\u9fff]";
const scan = (label) => evaluate(`(() => {
  const out = [];
  const walk = document.createTreeWalker(document.body, NodeFilter.SHOW_TEXT);
  let n; while ((n = walk.nextNode())) {
    const s = (n.textContent || "").trim();
    if (s && new RegExp("${HAN}").test(s)) out.push(s.slice(0, 60));
  }
  for (const el of document.querySelectorAll("[title],[aria-label],[placeholder]")) {
    for (const a of ["title", "aria-label", "placeholder"]) {
      const v = el.getAttribute(a);
      if (v && new RegExp("${HAN}").test(v)) out.push(a + "=" + v.slice(0, 50));
    }
  }
  return [...new Set(out)];
})()`).then((r) => ({ label, hits: r || [] }));

const results = [];
const skipped = [];
for (const [label, act] of STEPS) {
  if (act) {
    if (!(await evaluate(act))) { skipped.push(label); continue; }
    await wait(/^turn-/.test(label) ? 4500 : 1100);
  }
  results.push(await scan(label));
}
// A turn that failed renders the kernel's own error and the run label after
// it — text no settled screen carries. With a working key nothing here fails,
// and that half goes unwalked; say so rather than pass silently.
const ended = await evaluate(`document.querySelector(".pane")?.dataset.run ?? ""`);
if (ended !== "halt") console.log(`[failed-turn] the turn did not end in error (run=${ended}); this half went unwalked`);

// The last step leaves the account panel open over the chrome.
await evaluate(`document.body.click()`);
await wait(400);
await evaluate(SETTINGS_OPEN);
await wait(1500);
const count = await evaluate(SETTINGS_COUNT);
if (!count) throw new Error("settings did not open; the sweep would prove nothing");
for (let i = 0; i < count; i++) {
  const name = await evaluate(settingsTab(i));
  await wait(1000);
  results.push(await scan(name));
}

// One string is one finding, wherever it was first seen: an error card rides
// every section, and counting it 29 times buries the two other strings on the
// screen under it.
const seen = new Map();
for (const { label, hits } of results) for (const h of hits) if (!seen.has(h)) seen.set(h, label);
if (onb) console.log("entry:", JSON.stringify(onb));
for (const [hit, label] of seen) console.log(`  [${label}] ${hit}`);
console.log(`\nsections walked: ${results.length}; skipped=[${skipped.join(",")}]; Chinese still rendered: ${seen.size}`);
ws.close();
