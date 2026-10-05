// 切一次长会话，对这一下做 CPU 采样，按自耗时列热点。profile.mjs 量的是流式
// 那一段，这里量的是点下去之后、什么都还没出现之前的那一下 —— 两者的热点
// 不是同一批。产物要带 sourcemap 构建，否则热点只会是压缩后的名字。
import { chromium } from "playwright";
import { loadMap } from "./sourcemap.mjs";
import { readdirSync } from "node:fs";
const BASE = process.env.PERF_URL ?? "http://localhost:4399/perf.html";
const CPU = Number(process.env.PERF_CPU ?? 6);
const TURNS = Number(process.env.PERF_TURNS ?? 2000);
const MAPFILE = readdirSync("dist-perf/assets").find((f) => f.startsWith("perf-") && f.endsWith(".js.map"));
const MAP = MAPFILE ? loadMap(`dist-perf/assets/${MAPFILE}`) : null;
const browser = await chromium.launch();
const page = await browser.newPage({ viewport: { width: 1440, height: 900 } });
const cdp = await page.context().newCDPSession(page);
if (CPU > 1) await cdp.send("Emulation.setCPUThrottlingRate", { rate: CPU });
await page.goto(`${BASE}?ws=2&sess=8&turns=${TURNS}`, { waitUntil: "networkidle" });
await page.waitForSelector(".sessrow", { timeout: 60000 });
await page.waitForTimeout(800);

await cdp.send("Profiler.enable");
await cdp.send("Profiler.setSamplingInterval", { interval: 80 });
await cdp.send("Profiler.start");
const t0 = Date.now();
await page.locator(".sessrow").nth(1).click();
await page.waitForFunction(() => {
  const flows = Array.from(document.querySelectorAll(".flow"));
  return flows.length >= 2 && flows.every((f) => f.querySelectorAll(".chunk").length > 0);
}, null, { timeout: 120000 }).catch(() => {});
await page.evaluate(() => new Promise((r) => requestAnimationFrame(() => requestAnimationFrame(r))));
const took = Date.now() - t0;
const { profile } = await cdp.send("Profiler.stop");

const byId = new Map(profile.nodes.map((n) => [n.id, n]));
const self = new Map();
const total = profile.endTime - profile.startTime;
const dt = profile.timeDeltas ?? [];
profile.samples.forEach((id, i) => {
  const n = byId.get(id);
  if (!n) return;
  const f = n.callFrame;
  let where = `${(f.url || "(native)").split("/").pop()}:${f.lineNumber}`;
  if (f.url && f.url.includes("/assets/")) {
    const src = MAP?.at(f.lineNumber, f.columnNumber);
    if (src) where = `${src.src.replace(/^.*\/src\//, "src/")}:${src.line}${src.name ? " " + src.name : ""}`;
  }
  const key = `${f.functionName || "(anonymous)"}  ${where}`;
  self.set(key, (self.get(key) ?? 0) + (dt[i] ?? 0) / 1000);
});
const rows = [...self.entries()].sort((a, b) => b[1] - a[1]).slice(0, 18)
  .map(([k, v]) => ({ 热点: k, "自耗时/ms": v.toFixed(0), "占比%": ((v / (total / 1000)) * 100).toFixed(1) }));
console.log(`切一次 ${TURNS} 轮会话，CPU ${CPU}x，落屏耗时 ${took}ms，采样窗口 ${(total / 1000).toFixed(0)}ms\n`);
console.table(rows);
await browser.close();
