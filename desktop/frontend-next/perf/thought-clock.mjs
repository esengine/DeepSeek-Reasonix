import assert from "node:assert/strict";
import { mkdir } from "node:fs/promises";
import { chromium } from "playwright";

const base = process.env.PERF_URL ?? "http://localhost:4399/perf.html";
const browser = await chromium.launch();
try {
  await mkdir("perf/shots", { recursive: true });
  for (const colorScheme of ["light", "dark"]) {
    const page = await browser.newPage({ viewport: { width: 1400, height: 1000 }, colorScheme, reducedMotion: "reduce" });
    page.setDefaultTimeout(10000);
    await page.addInitScript(() => localStorage.setItem("rx-lang", "zh"));
    await page.goto(`${base}?pref=zh&turns=1`, { waitUntil: "networkidle" });
    const pane = page.locator(".pane[data-active]");
    await pane.waitFor();
    const before = await pane.locator(".think").count();
    const now = 1_700_000_000_000;
    await page.clock.setFixedTime(now);
    const feed = (event) => page.evaluate((ev) => window.__feed(ev), event);
    await feed({ kind: "turn_started" });
    await feed({ kind: "reasoning", text: "计时回归" });
    await pane.locator(".think[data-live]").waitFor();
    await page.clock.setFixedTime(now + 1200);
    await page.waitForFunction(() => document.querySelector('.pane[data-active] .think[data-live] .think-label')?.textContent.includes("1.2"));
    const tool = { id: "clock-call", name: "bash", readOnly: true, issuer: "model" };
    await feed({ kind: "tool_dispatch", tool: { ...tool, partial: true } });
    await page.waitForFunction(() => !document.querySelector('.pane[data-active] .think[data-live]'));
    const labels = () => pane.locator(".think-label:not(.think-out)").allTextContents();
    assert.equal((await labels()).at(-1), "思考 1.2 秒 · 4 字");
    await page.clock.setFixedTime(now + 1_800_000);
    await page.waitForTimeout(200);
    assert.equal((await labels()).at(-1), "思考 1.2 秒 · 4 字", "a tool's running time never becomes thinking time");
    await feed({ kind: "text", text: "检查工具输出。" });
    await feed({ kind: "message", text: "检查工具输出。", reasoning: "计时回归", thoughtMs: 1100 });
    await feed({ kind: "tool_dispatch", tool });
    await feed({ kind: "tool_result", tool: { ...tool, output: "done" } });
    assert.equal(await pane.locator(".think").count(), before + 1, "interleaved tool events keep one response card");
    await feed({ kind: "reasoning", text: "第二轮" });
    await page.clock.setFixedTime(now + 1_800_500);
    await feed({ kind: "message", text: "完成。", reasoning: "第二轮", thoughtMs: 500 });
    await feed({ kind: "turn_done" });
    await page.clock.setFixedTime(now + 3_600_000);
    await page.waitForTimeout(300);
    assert.equal(await pane.locator(".think[data-live]").count(), 0);
    assert.deepEqual((await labels()).slice(-2), ["思考 1.1 秒 · 4 字", "思考 0.5 秒 · 3 字"]);
    await pane.locator(".think").last().scrollIntoViewIfNeeded();
    await page.screenshot({ path: `perf/shots/thought-clock-${colorScheme}.png` });
    await page.close();
    console.log(`PASS ${colorScheme}: completed thoughts stay still after tools, later rounds and another 30 minutes`);
  }
} finally {
  await browser.close();
}
