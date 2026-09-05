// 并行窗格的代价：两个会话同时流式，比一个贵多少。
import { chromium } from "playwright";

const URL = process.env.PERF_URL ?? "http://localhost:4399/perf.html";
const CPU = Number(process.env.PERF_CPU ?? 1);
const ROUNDS = 150;
const DELTAS = 240;

const browser = await chromium.launch();
const rows = [];

for (const panes of [1, 2]) {
// 判据里拿中文比 DOM，所以语种要钉住 —— 不钉就跟着运行环境走，在英文机器
// 上红的是台架没说自己要哪一种语言。locale.mjs 一直是这么开页面的。
  const ctx = await browser.newContext({ locale: "zh-CN", viewport: { width: 1600, height: 900 } });
  const page = await ctx.newPage();
  const cdp = await page.context().newCDPSession(page);
  if (CPU > 1) await cdp.send("Emulation.setCPUThrottlingRate", { rate: CPU });
  await cdp.send("Performance.enable");
  await page.goto(URL, { waitUntil: "networkidle" });
  await page.waitForSelector(".app", { timeout: 15000 });
  await page.waitForTimeout(700);

  if (panes === 2) {
    await page.getByText("上一次的会话").first().click();
    await page.waitForTimeout(900);
  }
  const open = await page.evaluate(() => window.__panes());

  await page.evaluate(async (n) => {
    const yieldFrame = () => new Promise((r) => requestAnimationFrame(r));
    for (let i = 0; i < n; i++) {
      const id = `t${i}`;
      window.__feed({ kind: "turn_started" });
      window.__feed({ kind: "tool_dispatch", tool: { id, name: "edit_file", args: JSON.stringify({ path: `pkg/f${i}.go` }) } });
      window.__feed({ kind: "tool_result", tool: { id, name: "edit_file", args: JSON.stringify({ path: `pkg/f${i}.go` }), output: "写入完成", durationMs: 210, added: 4, removed: 2 } });
      window.__feed({ kind: "text", text: `第 ${i} 段回答。\n\n- 要点一\n- 要点二\n\n` });
      window.__feed({ kind: "message" });
      window.__feed({ kind: "turn_done" });
      if (i % 20 === 19) await yieldFrame();
    }
    await yieldFrame();
  }, ROUNDS);
  await page.waitForTimeout(400);

  const before = Object.fromEntries((await cdp.send("Performance.getMetrics")).metrics.map((m) => [m.name, m.value]));
  const r = await page.evaluate(async (k) => {
    const frames = [];
    window.__feed({ kind: "turn_started" });
    const t0 = performance.now();
    let prev = t0;
    for (let i = 0; i < k; i++) {
      window.__feed({ kind: "text", text: "回答的下一小段文字。" });
      await new Promise((res) => requestAnimationFrame(res));
      const now = performance.now();
      frames.push(now - prev);
      prev = now;
    }
    const total = performance.now() - t0;
    window.__feed({ kind: "turn_done" });
    frames.sort((a, b) => a - b);
    return { total, fps: (k / total) * 1000, p50: frames[Math.floor(frames.length / 2)], nodes: document.querySelectorAll("*").length };
  }, DELTAS);
  const after = Object.fromEntries((await cdp.send("Performance.getMetrics")).metrics.map((m) => [m.name, m.value]));
  const d = (k) => ((after[k] ?? 0) - (before[k] ?? 0)) * 1000;

  rows.push({
    窗格: open,
    DOM节点: r.nodes,
    "帧p50/ms": r.p50.toFixed(1),
    FPS: r.fps.toFixed(1),
    "脚本/ms": d("ScriptDuration").toFixed(0),
    "样式/ms": d("RecalcStyleDuration").toFixed(0),
    "布局/ms": d("LayoutDuration").toFixed(0),
  });
  await page.close();
}
await browser.close();
console.log(`每个窗格 ${ROUNDS} 轮会话，全部窗格同时收 ${DELTAS} 个流式增量\n`);
console.table(rows);
