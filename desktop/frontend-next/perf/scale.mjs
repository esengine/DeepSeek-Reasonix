// 会话开到极长时会怎样：节点数、堆内存、累积成本与冷启动一次性挂载。
import { chromium } from "playwright";

const PAGE = process.env.PERF_URL ?? "http://localhost:4399/perf.html";
const CPU = Number(process.env.PERF_CPU ?? 1);
const SCALES = (process.env.PERF_SCALES ?? "500,1000,2000,4000").split(",").map(Number);
const DELTAS = Number(process.env.PERF_DELTAS ?? 120);

const browser = await chromium.launch({ args: ["--js-flags=--expose-gc"] });
const rows = [];

for (const n of SCALES) {
  const page = await browser.newPage({ viewport: { width: 1440, height: 900 } });
  const cdp = await page.context().newCDPSession(page);
  if (CPU > 1) await cdp.send("Emulation.setCPUThrottlingRate", { rate: CPU });
  await cdp.send("Performance.enable");
  await page.goto(PAGE, { waitUntil: "networkidle" });
  await page.waitForSelector(".app", { timeout: 20000 });
  await page.waitForTimeout(600);

  // 累积路径：一轮一轮把会话开长，量它总共花掉多少。
  const grow = await page.evaluate(async (n) => {
    const yieldFrame = () => new Promise((r) => requestAnimationFrame(r));
    const t0 = performance.now();
    for (let i = 0; i < n; i++) {
      const id = `t${i}`;
      const args = JSON.stringify({ path: `internal/pkg/mod${i}/file${i}.go` });
      window.__feed({ kind: "turn_started" });
      window.__feed({ kind: "tool_dispatch", tool: { id, name: "edit_file", args } });
      window.__feed({ kind: "tool_result", tool: { id, name: "edit_file", args, output: `第 ${i} 次输出\n`.repeat(6), durationMs: 210, added: 4, removed: 2 } });
      window.__feed({ kind: "text", text: `第 ${i} 段回答，说明刚才那一步做了什么。\n\n- 要点一\n- 要点二\n\n` });
      window.__feed({ kind: "message" });
      window.__feed({ kind: "turn_done" });
      if (i % 20 === 19) await yieldFrame();
    }
    await yieldFrame();
    return performance.now() - t0;
  }, n);
  await page.waitForTimeout(500);

  // 长会话稳定后再量流式帧，看跟随一个回答还跟不跟得上。
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

  const mem = await cdp.send("Runtime.evaluate", { expression: "performance.memory ? performance.memory.usedJSHeapSize : 0", returnByValue: true });
  rows.push({
    轮次: n,
    卡片: await page.locator('[data-pane="flow"] .call').count(),
    DOM节点: Math.round(after.Nodes ?? 0),
    "JS堆/MB": (mem.result.value / 1048576).toFixed(0),
    "开到这么长/s": (grow / 1000).toFixed(1),
    "流式p50/ms": live.p50.toFixed(1),
    "流式p90/ms": live.p90.toFixed(1),
    "脚本/ms": (((after.ScriptDuration ?? 0) - (before.ScriptDuration ?? 0)) * 1000).toFixed(0),
    "布局/ms": (((after.LayoutDuration ?? 0) - (before.LayoutDuration ?? 0)) * 1000).toFixed(0),
  });
  console.log(`  ${n} 轮完成`);
  await page.close();
}
await browser.close();
console.log(`\n每档在会话开到该长度后，再灌 ${DELTAS} 个流式增量\n`);
console.table(rows);
