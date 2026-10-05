// 在真实 Chromium 里量化 Studio 长会话的流式渲染成本。
import { chromium } from "playwright";

const URL = process.env.PERF_URL ?? "http://localhost:4399/perf.html";
const DELTAS = Number(process.env.PERF_DELTAS ?? 240);
const SCALES = (process.env.PERF_SCALES ?? "0,40,150,400").split(",").map(Number);
// 低端机这一档：开发机的读数说明不了用户机上的卡顿，而报上来的正是后者。
// 1 是不降频；4~6 大致是这台机器降到入门级笔记本的量级。
const CPU = Number(process.env.PERF_CPU ?? 1);

const page_init = async (page) => {
  await page.goto(URL, { waitUntil: "networkidle" });
  await page.waitForSelector(".app", { timeout: 15000 });
  await page.waitForTimeout(600);
};

// 灌入 n 轮「工具调用 + 一段回答」，也就是 n 组工具卡片。
async function build(page, n) {
  await page.evaluate(async (n) => {
    const yieldFrame = () => new Promise((r) => requestAnimationFrame(r));
    for (let i = 0; i < n; i++) {
      const name = i % 4 === 0 ? "bash" : i % 4 === 1 ? "read_file" : i % 4 === 2 ? "edit_file" : "grep";
      const id = `t${i}`;
      const args = JSON.stringify({ path: `internal/pkg/mod${i}/file${i}.go`, pattern: "func" });
      window.__feed({ kind: "turn_started" });
      window.__feed({ kind: "tool_dispatch", tool: { id, name, args } });
      window.__feed({
        kind: "tool_result",
        tool: { id, name, args, output: `第 ${i} 次调用的输出\n`.repeat(6), durationMs: 300 + i, added: 4, removed: 2 },
      });
      window.__feed({ kind: "text", text: `这是第 ${i} 段回答，说明刚才那一步做了什么。\n\n- 要点一\n- 要点二\n\n` });
      window.__feed({ kind: "message" });
      window.__feed({ kind: "turn_done" });
      if (i % 20 === 19) await yieldFrame();
    }
    await yieldFrame();
  }, n);
  await page.waitForTimeout(400);
}

// 逐帧灌流式增量，测每一帧真正花掉多少时间。
async function stream(page, k) {
  return await page.evaluate(async (k) => {
    const frames = [];
    let long = 0;
    let longMs = 0;
    const po = new PerformanceObserver((list) => {
      for (const e of list.getEntries()) {
        long++;
        longMs += e.duration;
      }
    });
    try {
      po.observe({ type: "longtask", buffered: false });
    } catch {
      /* 不支持就只看帧间隔 */
    }

    window.__feed({ kind: "turn_started" });
    const t0 = performance.now();
    let prev = t0;
    for (let i = 0; i < k; i++) {
      window.__feed({ kind: "text", text: "回答的下一小段文字。" });
      await new Promise((r) => requestAnimationFrame(r));
      const now = performance.now();
      frames.push(now - prev);
      prev = now;
    }
    const total = performance.now() - t0;
    po.disconnect();
    window.__feed({ kind: "turn_done" });

    frames.sort((a, b) => a - b);
    const at = (p) => frames[Math.min(frames.length - 1, Math.floor(frames.length * p))];
    return {
      total,
      fps: (k / total) * 1000,
      p50: at(0.5),
      p90: at(0.9),
      p99: at(0.99),
      max: frames[frames.length - 1],
      long,
      longMs,
      nodes: document.querySelectorAll("*").length,
      items: document.querySelectorAll('[data-pane="flow"] .call, [data-pane="flow"] .find').length,
      trajRows: document.querySelectorAll("table.traj tbody tr").length,
    };
  }, k);
}

const browser = await chromium.launch();
const rows = [];
for (const n of SCALES) {
  const page = await browser.newPage({ viewport: { width: 1440, height: 900 } });
  const cdp = await page.context().newCDPSession(page);
  await cdp.send("Performance.enable");
  if (CPU > 1) await cdp.send("Emulation.setCPUThrottlingRate", { rate: CPU });
  await page_init(page);
  if (n > 0) await build(page, n);

  const before = Object.fromEntries((await cdp.send("Performance.getMetrics")).metrics.map((m) => [m.name, m.value]));
  const r = await stream(page, DELTAS);
  const after = Object.fromEntries((await cdp.send("Performance.getMetrics")).metrics.map((m) => [m.name, m.value]));
  const d = (k) => ((after[k] ?? 0) - (before[k] ?? 0)) * 1000;

  rows.push({
    轮次: n,
    卡片: r.items,
    轨迹行: r.trajRows,
    DOM节点: r.nodes,
    "帧p50/ms": r.p50.toFixed(1),
    "帧p90/ms": r.p90.toFixed(1),
    "帧max/ms": r.max.toFixed(0),
    FPS: r.fps.toFixed(1),
    "脚本/ms": d("ScriptDuration").toFixed(0),
    "样式/ms": d("RecalcStyleDuration").toFixed(0),
    "布局/ms": d("LayoutDuration").toFixed(0),
    长任务: r.long,
    "长任务/ms": r.longMs.toFixed(0),
  });
  await page.close();
}
await browser.close();
console.table(rows);
console.log(`
每个场景灌入 ${DELTAS} 个流式增量，逐帧对齐。CPU 降频 ${CPU}x。`);
