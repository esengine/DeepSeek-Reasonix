import assert from "node:assert/strict";
import { chromium } from "playwright";

// A long run (a wait on a build) keeps the running indicators moving for minutes.
// Counts the style recalculations they cost over five seconds, by default and with
// the power-saving effects mode or the OS reduced-motion preference.
const browser = await chromium.launch(process.env.CHROME ? { executablePath: process.env.CHROME } : {});
const measure = async (motion, reduced) => {
  const page = await browser.newPage({ viewport: { width: 1440, height: 900 }, reducedMotion: motion });
  const cdp = await page.context().newCDPSession(page);
  await cdp.send("Performance.enable");
  await page.goto(process.env.PERF_URL ?? "http://127.0.0.1:4399/perf.html?pref=zh", { waitUntil: "networkidle" });
  await page.waitForFunction(() => window.__panes?.() > 0);
  await page.evaluate((reduced) => {
    if (reduced) document.documentElement.dataset.effects = "reduced";
    window.__feed({ kind: "turn_started", authoredTurn: 1, msgIndex: 0, text: "wait for the build" });
    window.__feed({ kind: "tool_dispatch", tool: { id: "w1", name: "wait", readOnly: true, args: "{}" } });
  }, reduced);
  await page.waitForTimeout(800);
  const read = async () => Object.fromEntries((await cdp.send("Performance.getMetrics")).metrics.map((m) => [m.name, m.value]));
  const a = await read();
  await page.waitForTimeout(5000);
  const b = await read();
  const running = await page.evaluate(() => document.getAnimations().filter((x) => x.playState === "running" && x.effect?.getTiming?.().iterations === Infinity).length);
  await page.close();
  return { style: b.RecalcStyleCount - a.RecalcStyleCount, loops: running };
};
try {
  const normal = await measure("no-preference", false);
  const saving = await measure("no-preference", true);
  const motion = await measure("reduce", false);
  console.log(JSON.stringify({ normal, saving, motion }));
  assert.ok(normal.loops > 0, "the running state shows its indicators by default");
  for (const [name, r] of Object.entries({ saving, motion })) {
    assert.equal(r.loops, 0, `${name}: a looping animation still runs`);
    assert.ok(r.style < normal.style / 5, `${name}: ${r.style} style recalcs against ${normal.style} by default`);
  }
} finally {
  await browser.close();
}
