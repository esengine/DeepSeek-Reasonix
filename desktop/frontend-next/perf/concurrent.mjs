// Many sessions streaming at once (#12232): hidden panes must not be rewritten at
// stream rate, and N=PERF_GUARD must keep FPS_RATIO_MIN of the N=1 frame rate.
// PERF_N=1,3,6,9 lists more rows; the verdict compares only N=1 with PERF_GUARD.
import { chromium } from "playwright";

const URL = process.env.PERF_URL ?? "http://localhost:4399/perf.html";
const CPU = Number(process.env.PERF_CPU ?? 4);
const SECS = Number(process.env.PERF_SECS ?? 5);
const TURNS = Number(process.env.PERF_TURNS ?? 40);
const GUARD = Number(process.env.PERF_GUARD ?? 9);
const COUNTS = (process.env.PERF_N ?? `1,${GUARD}`).split(",").map(Number);
const WRITES_MAX = Number(process.env.PERF_WRITES_MAX ?? 4);
const FPS_RATIO_MIN = Number(process.env.PERF_FPS_RATIO ?? 0.7);

const THOUGHT_CHARS = Number(process.env.PERF_THOUGHT ?? 3000);
const THOUGHT = "先看一下这个文件的结构，再决定改哪里；要注意调用方、回放路径与错误状态。".repeat(Math.ceil(THOUGHT_CHARS / 34 / 40));
const browser = await chromium.launch();
const rows = [];
const fails = [];

for (const n of COUNTS) {
  const ctx = await browser.newContext({ locale: "zh-CN", viewport: { width: 1440, height: 900 } });
  const page = await ctx.newPage();
  const cdp = await ctx.newCDPSession(page);
  if (CPU > 1) await cdp.send("Emulation.setCPUThrottlingRate", { rate: CPU });
  await cdp.send("Performance.enable");
  await page.goto(`${URL}?panes=${n}`, { waitUntil: "networkidle" });
  await page.waitForSelector(".app", { timeout: 15000 });
  await page.waitForTimeout(700);

  await page.evaluate(async (k) => {
    const yieldFrame = () => new Promise((r) => requestAnimationFrame(r));
    for (let i = 0; i < k; i++) {
      window.__feed({ kind: "turn_started" });
      window.__feed({ kind: "reasoning", text: "先看文件再决定改哪里。" });
      window.__feed({ kind: "text", text: `第 ${i} 段回答。\n\n- 要点一\n- 要点二\n\n` });
      window.__feed({ kind: "message" });
      window.__feed({ kind: "turn_done" });
      if (i % 10 === 9) await yieldFrame();
    }
    await yieldFrame();
  }, TURNS);
  await page.waitForTimeout(500);
  await page.evaluate((t) => {
    window.__feed({ kind: "turn_started" });
    for (let i = 0; i < 40; i++) window.__feed({ kind: "reasoning", text: t });
  }, THOUGHT);
  await page.waitForTimeout(500);

  const read = async () => Object.fromEntries((await cdp.send("Performance.getMetrics")).metrics.map((m) => [m.name, m.value]));
  async function phase(kind) {
    const before = await read();
    const r = await page.evaluate(
      ([k, secs]) =>
        new Promise((resolve) => {
          let frames = 0;
          let worst = 0;
          const t0 = performance.now();
          let last = t0;
          const tick = () => {
            frames++;
            const now = performance.now();
            worst = Math.max(worst, now - last);
            last = now;
            if (now - t0 < secs * 1000) requestAnimationFrame(tick);
          };
          requestAnimationFrame(tick);
          let writes = 0;
          const mo = new MutationObserver((ms) => {
            for (const m of ms) {
              const at = m.target instanceof Element ? m.target : m.target.parentElement;
              if (at?.closest(".pane[data-off] .out")) writes++;
            }
          });
          mo.observe(document.body, { subtree: true, characterData: true, childList: true });
          const feed = setInterval(() => window.__feed({ kind: k, text: "继续往下想一小段文字，" }), 33);
          setTimeout(() => {
            clearInterval(feed);
            mo.disconnect();
            resolve({ wall: performance.now() - t0, frames, worst, writes });
          }, secs * 1000);
        }),
      [kind, SECS],
    );
    const after = await read();
    const d = (key) => (after[key] ?? 0) - (before[key] ?? 0);
    return {
      N: n,
      阶段: kind === "reasoning" ? "thinking" : "answering",
      "主线程%": ((d("TaskDuration") * 1000 * 100) / r.wall).toFixed(0),
      "脚本%": ((d("ScriptDuration") * 1000 * 100) / r.wall).toFixed(0),
      "样式重算/s": (d("RecalcStyleCount") / (r.wall / 1000)).toFixed(0),
      "布局/s": (d("LayoutCount") / (r.wall / 1000)).toFixed(0),
      fps: ((r.frames / r.wall) * 1000).toFixed(1),
      "最长帧/ms": r.worst.toFixed(0),
      "后台写/s/窗格": n > 1 ? (r.writes / (r.wall / 1000) / (n - 1)).toFixed(1) : "-",
      节点: after.Nodes,
    };
  }
  const a = await phase("reasoning");
  await page.evaluate(() => window.__feed({ kind: "text", text: "开始回答。" }));
  const b = await phase("text");
  rows.push(a, b);
  await ctx.close();
}
await browser.close();
console.log(`降频 ${CPU}x，每个窗格 ${TURNS} 轮历史，每阶段 ${SECS}s\n`);
console.table(rows);
for (const phase of ["thinking", "answering"]) {
  const one = rows.find((r) => r.N === 1 && r.阶段 === phase);
  const many = rows.find((r) => r.N === GUARD && r.阶段 === phase);
  if (!one || !many) {
    fails.push(`${phase} 缺 N=1 或 N=${GUARD} 的读数`);
    continue;
  }
  const writes = Number(many["后台写/s/窗格"]);
  const ratio = Number(many.fps) / Number(one.fps);
  console.log(`${phase}: 后台窗格每秒写 ${writes} 次（≤ ${WRITES_MAX}），N=${GUARD} 帧率 / N=1 帧率 = ${ratio.toFixed(2)}（≥ ${FPS_RATIO_MIN}）`);
  if (writes > WRITES_MAX) fails.push(`${phase} 后台窗格每秒写 ${writes} 次 > ${WRITES_MAX}`);
  if (ratio < FPS_RATIO_MIN) fails.push(`${phase} 帧率比 ${ratio.toFixed(2)} < ${FPS_RATIO_MIN}`);
}
console.log(fails.length ? `\n失败 ${fails.length} 项：\n- ${fails.join("\n- ")}` : "\n全部通过");
process.exit(fails.length ? 1 : 0);
