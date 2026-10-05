// 左栏的会话树开得很大时会怎样：首屏、节点数、以及它是否拖累正在流式的会话。
import { chromium } from "playwright";

const BASE = process.env.PERF_URL ?? "http://localhost:4399/perf.html";
const CPU = Number(process.env.PERF_CPU ?? 1);
// 工作区数 × 每个工作区的会话数
const SHAPES = (process.env.PERF_TREE ?? "2x8,10x50,20x200,40x500").split(",").map((s) => s.split("x").map(Number));
const DELTAS = Number(process.env.PERF_DELTAS ?? 120);

const browser = await chromium.launch();
const rows = [];

for (const [ws, sess] of SHAPES) {
  const page = await browser.newPage({ viewport: { width: 1440, height: 900 } });
  const cdp = await page.context().newCDPSession(page);
  if (CPU > 1) await cdp.send("Emulation.setCPUThrottlingRate", { rate: CPU });
  await cdp.send("Performance.enable");

  const t0 = Date.now();
  await page.goto(`${BASE}?ws=${ws}&sess=${sess}`, { waitUntil: "networkidle" });
  await page.waitForSelector(".app", { timeout: 30000 });
  await page.waitForSelector(".wsnode", { timeout: 30000 });
  const firstPaint = Date.now() - t0;
  await page.waitForTimeout(700);

  // 一个中等长度的会话在跑，同时左栏挂着这么大一棵树。
  await page.evaluate(async () => {
    const y = () => new Promise((r) => requestAnimationFrame(r));
    for (let i = 0; i < 150; i++) {
      const id = `t${i}`, args = JSON.stringify({ path: `pkg/f${i}.go` });
      window.__feed({ kind: "turn_started" });
      window.__feed({ kind: "tool_dispatch", tool: { id, name: "edit_file", args } });
      window.__feed({ kind: "tool_result", tool: { id, name: "edit_file", args, output: "写入完成\n", durationMs: 210, added: 4, removed: 2 } });
      window.__feed({ kind: "text", text: `第 ${i} 段回答。\n\n- 一\n- 二\n\n` });
      window.__feed({ kind: "message" });
      window.__feed({ kind: "turn_done" });
      if (i % 20 === 19) await y();
    }
    await y();
  });
  await page.waitForTimeout(400);

  const before = Object.fromEntries((await cdp.send("Performance.getMetrics")).metrics.map((m) => [m.name, m.value]));
  const live = await page.evaluate(async (k) => {
    const frames = [];
    window.__feed({ kind: "turn_started" });
    let prev = performance.now();
    for (let i = 0; i < k; i++) {
      window.__feed({ kind: "text", text: "回答的下一小段文字。" });
      await new Promise((r) => requestAnimationFrame(r));
      const now = performance.now();
      frames.push(now - prev);
      prev = now;
    }
    window.__feed({ kind: "turn_done" });
    frames.sort((a, b) => a - b);
    return { p50: frames[Math.floor(frames.length / 2)], p90: frames[Math.floor(frames.length * 0.9)] };
  }, DELTAS);
  const after = Object.fromEntries((await cdp.send("Performance.getMetrics")).metrics.map((m) => [m.name, m.value]));

  // 展开一个折叠的工作区，量这一下点击有多久才落地。
  const twist = page.locator(".wsnode").nth(1).locator("button.twist");
  const tExpand = Date.now();
  await twist.click();
  await page.waitForTimeout(50);
  await page.evaluate(() => new Promise((r) => requestAnimationFrame(() => requestAnimationFrame(r))));
  const expand = Date.now() - tExpand;

  const dom = await page.evaluate(() => ({
    全文档: document.querySelectorAll("*").length,
    左栏: document.querySelector(".rail")?.querySelectorAll("*").length ?? 0,
    会话行: document.querySelectorAll(".sessrow").length,
  }));

  rows.push({
    "树形": `${ws} 区 × ${sess} 会话`,
    会话总数: ws * sess,
    会话行: dom.会话行,
    左栏节点: dom.左栏,
    全文档节点: dom.全文档,
    "首屏/ms": firstPaint,
    "展开一个/ms": expand,
    "流式p50/ms": live.p50.toFixed(1),
    "脚本/ms": (((after.ScriptDuration ?? 0) - (before.ScriptDuration ?? 0)) * 1000).toFixed(0),
  });
  console.log(`  ${ws}x${sess} 完成`);
  await page.close();
}
await browser.close();
console.log(`\n左栏挂着这棵树，同时一个 150 轮的会话灌 ${DELTAS} 个流式增量\n`);
console.table(rows);
