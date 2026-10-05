// 切到一个已经很长的会话要多久：读回整份记录、折成条目、把窗格重挂一遍。
// 与 scale.mjs 的分工：那边量的是会话在眼前一轮轮变长，这边量的是一次点击
// 之后、什么都还没发生之前，用户要等的那一下。
import { chromium } from "playwright";

const BASE = process.env.PERF_URL ?? "http://localhost:4399/perf.html";
const CPU = Number(process.env.PERF_CPU ?? 1);
const TURNS = (process.env.PERF_TURNS ?? "0,100,400,1000,2000").split(",").map(Number);
// 内核那边 /status 声明了钱包端点时是一次网络往返；这里把那笔账摆进来。
const STATUS_MS = Number(process.env.PERF_STATUS_MS ?? 0);

const browser = await chromium.launch();
const rows = [];

for (const turns of TURNS) {
  const page = await browser.newPage({ viewport: { width: 1440, height: 900 } });
  const cdp = await page.context().newCDPSession(page);
  if (CPU > 1) await cdp.send("Emulation.setCPUThrottlingRate", { rate: CPU });
  await cdp.send("Performance.enable");
  await page.goto(`${BASE}?ws=2&sess=8&turns=${turns}&statusms=${STATUS_MS}`, { waitUntil: "networkidle" });
  await page.waitForSelector(".app", { timeout: 30000 });
  await page.waitForSelector(".sessrow", { timeout: 30000 });
  await page.waitForTimeout(600);

  // 折条目这步是纯计算，单独量一次：它在点击之后、任何一帧之前发生。
  const fold = await page.evaluate(() => window.__foldCost?.() ?? null);

  const before = Object.fromEntries((await cdp.send("Performance.getMetrics")).metrics.map((m) => [m.name, m.value]));
  const longBefore = await page.evaluate(() => {
    window.__long = [];
    if (!window.__lobs) {
      window.__lobs = new PerformanceObserver((l) => window.__long.push(...l.getEntries().map((e) => e.duration)));
      window.__lobs.observe({ entryTypes: ["longtask"] });
    }
    return 0;
  });
  void longBefore;

  // 点开左栏里的一个会话——真机上用户就是这么切的。
  const t0 = Date.now();
  await page.locator(".sessrow").nth(1).click();
  // 判据是新开的那栏自己有了内容。等「有块」是不够的：切换时旧栏还在屏幕上，
  // 它的块会让判据在新栏还空着的时候就成立。
  const settled = await page
    .waitForFunction(
      (n) => {
        const flows = Array.from(document.querySelectorAll(".flow"));
        if (flows.length < 2) return false;
        return n === 0 || flows.every((f) => f.querySelectorAll(".chunk").length > 0);
      },
      turns,
      { timeout: 60000 },
    )
    .then(() => Date.now() - t0)
    .catch(() => -1);
  await page.evaluate(() => new Promise((r) => requestAnimationFrame(() => requestAnimationFrame(r))));
  const painted = Date.now() - t0;
  const after = Object.fromEntries((await cdp.send("Performance.getMetrics")).metrics.map((m) => [m.name, m.value]));
  const long = await page.evaluate(() => (window.__long ?? []).slice().sort((a, b) => b - a));

  const dom = await page.evaluate(() => ({
    全文档: document.querySelectorAll("*").length,
    转录: document.querySelector(".flow")?.querySelectorAll("*").length ?? 0,
    块: document.querySelectorAll(".flow .chunk").length,
  }));

  rows.push({
    轮次: turns,
    "折条目/ms": fold === null ? "-" : fold.toFixed(1),
    "到内容/ms": settled,
    "到落屏/ms": painted,
    "脚本/ms": (((after.ScriptDuration ?? 0) - (before.ScriptDuration ?? 0)) * 1000).toFixed(0),
    "布局/ms": (((after.LayoutDuration ?? 0) - (before.LayoutDuration ?? 0)) * 1000).toFixed(0),
    长任务数: long.length,
    "最长任务/ms": long.length ? long[0].toFixed(0) : 0,
    块: dom.块,
    转录节点: dom.转录,
    全文档节点: dom.全文档,
  });
  console.log(`  ${turns} 轮完成`);
  await page.close();
}
await browser.close();
console.log("\n点开左栏里另一个会话，量到那份记录真的出现在眼前\n");
console.table(rows);
