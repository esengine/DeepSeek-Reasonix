// Refuses every write the settings panels make, and asks what the window did
// about it. A refused write has four things to answer and they are independent:
//
//   rolledback  the control shows what the kernel holds, not what was asked
//   said        the window told someone, in words, that it did not go through
//   stuck       nothing is left disabled or spinning with no way out
//   uncaught    no unhandled rejection; a refusal is an answer, not a crash
//
// Optimistic state that never rolls back is the worst of these: the window then
// disagrees with the kernel until something reloads it, and nothing says so.
//
// It covers the toggles on the settings panels and nothing else. The writes the
// workbench makes — sending a turn, answering an approval, cancelling a run —
// are a separate walk this does not take.
//
// Run it with a kernel and the dev server up, and a headless Chrome on 9333 —
// the same stack the other sweeps take:
//
//   /path/to/reasonix serve -addr 127.0.0.1:8791 -auth none
//   REASONIX_SERVE=http://127.0.0.1:8791 npx vite --port 5177 --strictPort
//   chrome-headless-shell --remote-debugging-port=9333 --headless
//   node tools/fault-sweep.mjs http://localhost:5177/
//
// Two judgements here had to be fixed before they were worth reading, and both
// failure modes are the same shape — a probe that answers for something other
// than what it names:
//
//   A panel that sends no write at all was being scored on whether its control
//   rolled back. Nothing was injected, so of course it did not, and two
//   local-only settings read as defects. Panels with no write are now named as
//   such and judged on nothing.
//
//   Every injection carried one sentence, so the previous panel's error — still
//   on screen — was part of the next panel's baseline, and four panels that did
//   speak read as silent. Each panel now gets a sentence of its own.
const url = process.argv[2] || "http://localhost:5177/";
const { SETTINGS_OPEN, SETTINGS_COUNT, settingsTab } = await import("./steps.mjs");
const list = await (await fetch("http://127.0.0.1:9333/json/list")).json();
const target = list.find((x) => x.type === "page");
const ws = new WebSocket(target.webSocketDebuggerUrl);
let id = 0; const pend = new Map();
const send = (m, p = {}) => { const i = ++id; ws.send(JSON.stringify({ id: i, method: m, params: p })); return new Promise((r) => pend.set(i, r)); };
await new Promise((r) => (ws.onopen = r));

const rt = { exc: [] };
const hit = [];
let injecting = false;
let mark = "injected failure";
const b64 = (s) => Buffer.from(s, "utf8").toString("base64");
ws.onmessage = async (e) => {
  const m = JSON.parse(e.data);
  if (m.id && pend.has(m.id)) { pend.get(m.id)(m.result); pend.delete(m.id); return; }
  if (m.method === "Runtime.exceptionThrown") rt.exc.push((m.params.exceptionDetails.exception?.description || m.params.exceptionDetails.text || "").slice(0, 160));
  if (m.method === "Fetch.requestPaused") {
    const { requestId, request } = m.params;
    if (injecting && request.method !== "GET") {
      hit.push(request.method + " " + request.url.replace(/^https?:\/\/[^/]+/, ""));
      await send("Fetch.fulfillRequest", {
        requestId, responseCode: 500,
        responseHeaders: [{ name: "content-type", value: "application/json" }],
        body: b64(JSON.stringify({ code: "injected.failure", error: mark })),
      });
      return;
    }
    await send("Fetch.continueRequest", { requestId });
  }
};
const ev = async (x) => {
  const r = await send("Runtime.evaluate", { expression: x, returnByValue: true, awaitPromise: true });
  if (r?.exceptionDetails) throw new Error("probe failed: " + (r.exceptionDetails.exception?.description || r.exceptionDetails.text));
  return r?.result?.value;
};
const wait = (ms) => new Promise((r) => setTimeout(r, ms));

await send("Runtime.enable"); await send("Page.enable");
await send("Emulation.setDeviceMetricsOverride", { width: 1440, height: 1000, deviceScaleFactor: 1, mobile: false });
await send("Page.navigate", { url }); await wait(2500);
await ev(`localStorage.setItem("rx-lang","en")`);
await send("Page.navigate", { url }); await wait(5000);
await ev(SETTINGS_OPEN); await wait(1500);
const panels = await ev(SETTINGS_COUNT);
if (!panels) throw new Error("settings did not open; the sweep would prove nothing");
await send("Fetch.enable", { patterns: [{ urlPattern: "*", requestStage: "Request" }] });

const TOGGLES = ".prefs button[aria-pressed], .prefs button[role=switch], .prefs input[type=checkbox]";
const MAX_PER_PANEL = Number(process.env.MAX_PER_PANEL || 4);
const state = (i) => `(() => {
  const c = [...document.querySelectorAll('${TOGGLES}')].filter((e) => !e.disabled && e.offsetParent)[${i}];
  if (!c) return "";
  window.__c = c;
  return JSON.stringify({
    on: c.getAttribute("aria-pressed") ?? c.getAttribute("aria-checked") ?? (c.checked === undefined ? null : String(c.checked)),
    disabled: !!c.disabled,
    label: (c.getAttribute("aria-label") || c.textContent || "").trim().slice(0, 30),
  });
})()`;

let bad = 0, tried = 0, quiet = 0;
for (let p = 0; p < panels; p++) {
  const name = await ev(settingsTab(p));
  await wait(900);
  for (let i = 0; i < MAX_PER_PANEL; i++) {
    const raw = await ev(state(i));
    if (!raw) break;
    const before = JSON.parse(raw);
    mark = `injected failure in ${name}#${i}`;
    const said = new Set((await ev(`document.body.innerText`)).split("\n").map((l) => l.trim()));
    hit.length = 0; rt.exc.length = 0;
    injecting = true;
    await ev(`window.__c.click()`);
    await wait(1800);
    injecting = false;
    if (!hit.length) {
      console.log(`${name.padEnd(18)} ${before.label.padEnd(30)} — no write left the window`);
      continue;
    }
    tried++;
    const after = JSON.parse(await ev(state(i)));
    const now = (await ev(`document.body.innerText`)).split("\n").map((l) => l.trim());
    const spoke = now.some((l) => l && !said.has(l));
    const rolled = before.on === after.on;
    if (!rolled || !spoke || after.disabled || rt.exc.length) bad++;
    if (!spoke) quiet++;
    console.log(
      `${name.padEnd(18)} ${before.label.padEnd(30)} rolledback=${rolled ? "yes" : "NO "} said=${spoke ? "yes" : "NO "}` +
      ` stuck=${after.disabled ? "YES" : "no "} uncaught=${rt.exc.length}  ${hit[0]}`,
    );
    for (const x of [...new Set(rt.exc)].slice(0, 2)) console.log("      EXC " + x);
    await wait(300);
  }
}
await send("Fetch.disable");
console.log(`\npanels ${panels}; writes refused ${tried}; failed a judgement ${bad}; silent ${quiet}`);
ws.close();
