// Frame cost of folding the left rail with 1/3/6 panes that each hold a long transcript. The rail width is
// animated per frame, so every mounted pane is laid out again unless a hidden pane is skipped. Guard: the
// median frame with PERF_GUARD panes stays within FRAME_RATIO_MAX of the one-pane median, a pane that
// was hidden comes back at the scroll position it left, and the transcript does not inherit the animated
// column widths: with a long transcript the fold costs style recalculation of every element under it.
import assert from "node:assert/strict";
import { mkdir } from "node:fs/promises";
import { chromium } from "playwright";

const CPU = Number(process.env.PERF_CPU ?? 4);
const TURNS = Number(process.env.PERF_TURNS ?? 60);
const REPS = Number(process.env.PERF_REPS ?? 6);
const COUNTS = (process.env.PERF_N ?? "1,3,6").split(",").map(Number);
const GUARD = Number(process.env.PERF_GUARD ?? 3);
const FRAME_RATIO_MAX = Number(process.env.PERF_FRAME_RATIO ?? 2.5);
const URL = process.env.PERF_URL ?? "http://localhost:4399/perf.html";
const SHOTS = process.env.RAIL_TOGGLE_SHOTS;
if (SHOTS) await mkdir(SHOTS, { recursive: true });
const pct = (a, p) => [...a].sort((x, y) => x - y)[Math.min(a.length - 1, Math.floor(a.length * p))];
const browser = await chromium.launch();
const rows = [];
let kept = null;
for (const n of COUNTS) {
  const ctx = await browser.newContext({ locale: "zh-CN", viewport: { width: 1440, height: 900 } });
  const page = await ctx.newPage();
  const cdp = await ctx.newCDPSession(page);
  await page.goto(`${URL}?panes=${n}`, { waitUntil: "networkidle" });
  await page.waitForSelector(".app");
  await page.waitForTimeout(800);
  await page.evaluate(async (k) => {
    const frame = () => new Promise((r) => requestAnimationFrame(r));
    for (let i = 0; i < k; i++) {
      const id = `t${i}`, args = JSON.stringify({ command: `go test ./pkg${i}/...` });
      window.__feed({ kind: "turn_started" });
      window.__feed({ kind: "tool_dispatch", tool: { id, name: "bash", args } });
      window.__feed({ kind: "tool_result", tool: { id, name: "bash", args, output: "ok\n".repeat(20) } });
      window.__feed({ kind: "text", text: `第 ${i} 段回答。\n\n- 要点一\n- 要点二\n\n` });
      window.__feed({ kind: "message" });
      window.__feed({ kind: "turn_done" });
      if (i % 10 === 9) await frame();
    }
    await frame();
  }, TURNS);
  await page.waitForTimeout(800);
  if (CPU > 1) await cdp.send("Emulation.setCPUThrottlingRate", { rate: CPU });
  const frames = [];
  for (let r = 0; r < REPS; r++) {
    const got = await page.evaluate(async () => {
      const out = [];
      let prev = performance.now();
      const end = prev + 450;
      const before = document.querySelector(".app")?.dataset.rail;
      const mac = /Mac/.test(navigator.platform);
      window.dispatchEvent(new KeyboardEvent("keydown", { key: "\\", ctrlKey: !mac, metaKey: mac, bubbles: true, cancelable: true }));
      while (performance.now() < end) {
        await new Promise((res) => requestAnimationFrame(res));
        const now = performance.now();
        out.push(now - prev);
        prev = now;
      }
      if (document.querySelector(".app")?.dataset.rail === before) throw new Error("the rail did not toggle");
      return out;
    });
    frames.push(...got);
    await page.waitForTimeout(150);
  }
  rows.push({ panes: n, frames: frames.length, p50: pct(frames, 0.5).toFixed(1), p95: pct(frames, 0.95).toFixed(1), worst: Math.max(...frames).toFixed(1), over50: frames.filter((f) => f > 50).length });
  if (n === GUARD) {
    const first = page.locator('.sessrow[data-target="/sessions/pane-1.jsonl"]');
    const second = page.locator('.sessrow[data-target="/sessions/pane-2.jsonl"]');
    assert.equal(await first.getAttribute("data-live"), "", "the first row must identify an existing pane");
    assert.equal(await second.getAttribute("data-live"), "", "the second row must identify another existing pane");
    assert.equal(await first.getAttribute("aria-selected"), "true", "the measured pane must be the first session");
    const scroller = await page.locator('.pane:not([data-off]) .scroll[data-pane="flow"]').elementHandle();
    assert.ok(scroller);
    await scroller.evaluate((root) => { root.scrollTop = 400; });
    await page.waitForTimeout(200);
    assert.equal(await scroller.evaluate((root) => root.scrollTop), 400, "the initial reading position must be reachable");
    if (SHOTS) await page.screenshot({ path: `${SHOTS}/before-hidden.png` });
    await second.click();
    await page.waitForFunction(() => document.querySelector('.sessrow[data-target="/sessions/pane-2.jsonl"]')?.getAttribute("aria-selected") === "true");
    assert.ok(await scroller.evaluate((root) => root.closest(".pane").hasAttribute("data-off")), "the original pane must actually be hidden");
    assert.equal(await page.locator(".pane").count(), n, "switching existing conversations must not open a new pane");
    await page.evaluate(() => {
      window.__feed({ kind: "turn_started" });
      window.__feed({ kind: "text", text: "Background scroll preservation probe." });
      window.__feed({ kind: "message" });
      window.__feed({ kind: "turn_done" });
    });
    await page.waitForFunction(() => document.querySelector(".pane:not([data-off]) .flow")?.textContent.includes("Background scroll preservation probe."));
    if (SHOTS) await page.screenshot({ path: `${SHOTS}/while-hidden.png` });
    await first.click();
    await page.waitForFunction(() => document.querySelector('.sessrow[data-target="/sessions/pane-1.jsonl"]')?.getAttribute("aria-selected") === "true");
    assert.ok(await scroller.evaluate((root) => root === document.querySelector('.pane:not([data-off]) .scroll[data-pane="flow"]')), "scroll-retention precondition: inspect the originally hidden transcript");
    kept = await scroller.evaluate((root) => root.scrollTop);
    if (SHOTS) await page.screenshot({ path: `${SHOTS}/after-return.png` });
    await second.click();
    await page.waitForFunction(() => {
      if (document.querySelector('.sessrow[data-target="/sessions/pane-2.jsonl"]')?.getAttribute("aria-selected") !== "true") return false;
      const root = document.querySelector('.pane:not([data-off]) .scroll[data-pane="flow"]');
      return root && root.scrollHeight - root.clientHeight - root.scrollTop <= 48;
    });
    console.log(`existing pane round trip: scrollTop 400 -> hidden with new output -> ${kept}; following neighbour remains at latest`);
  }
  await ctx.close();
}
const inherited = await (async () => {
  const ctx = await browser.newContext({ locale: "zh-CN", viewport: { width: 1440, height: 900 } });
  const page = await ctx.newPage();
  await page.goto(`${URL}?panes=1`, { waitUntil: "networkidle" });
  await page.waitForSelector(".flow");
  const seen = await page.evaluate(async () => {
    const read = () => ["--rail-w", "--nav-w", "--side-w"].map((v) => getComputedStyle(document.querySelector(".flow")).getPropertyValue(v)).join(" ");
    const app = () => getComputedStyle(document.querySelector(".app")).getPropertyValue("--rail-w");
    const out = { flow: [read()], app: [app()] };
    const mac = /Mac/.test(navigator.platform);
    window.dispatchEvent(new KeyboardEvent("keydown", { key: "\\", ctrlKey: !mac, metaKey: mac, bubbles: true, cancelable: true }));
    for (const wait of [90, 400]) {
      await new Promise((r) => setTimeout(r, wait));
      out.flow.push(read());
      out.app.push(app());
    }
    return out;
  });
  await ctx.close();
  return seen;
})();
await browser.close();
console.table(rows);
const one = rows.find((r) => r.panes === 1), many = rows.find((r) => r.panes === GUARD);
const fails = [];
if (one && many && Number(many.p50) > Number(one.p50) * FRAME_RATIO_MAX) {
  fails.push(`median frame with ${GUARD} panes is ${many.p50} ms against ${one.p50} ms with one (limit x${FRAME_RATIO_MAX})`);
}
if (new Set(inherited.app).size < 2) fails.push(`the rail width did not change during the fold (${inherited.app.join(" -> ")}), so the guard measured nothing`);
else if (new Set(inherited.flow).size > 1) fails.push(`the transcript inherits the animated column widths (${inherited.flow.join(" -> ")}); every element under it is restyled on each frame of the fold`);
if (kept !== null && Math.abs(kept - 400) > 2) fails.push(`a pane hidden at scrollTop 400 came back at ${kept}`);
if (fails.length) {
  console.error(fails.join("\n"));
  process.exit(1);
}
