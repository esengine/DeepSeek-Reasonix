// 对长会话的流式帧做 CPU 采样，按自耗时列出热点。
import { chromium } from "playwright";
import { loadMap } from "./sourcemap.mjs";
import { readdirSync } from "node:fs";

const URL = process.env.PERF_URL ?? "http://localhost:4399/perf.html";
const ROUNDS = Number(process.env.PERF_ROUNDS ?? 400);
const DELTAS = Number(process.env.PERF_DELTAS ?? 240);
const CPU = Number(process.env.PERF_CPU ?? 1);
const MAPFILE = readdirSync("dist-perf/assets").find((f) => f.startsWith("perf-") && f.endsWith(".js.map"));
const MAP = MAPFILE ? loadMap(`dist-perf/assets/${MAPFILE}`) : null;

const browser = await chromium.launch();
const page = await browser.newPage({ viewport: { width: 1440, height: 900 } });
const cdp = await page.context().newCDPSession(page);
if (CPU > 1) await cdp.send("Emulation.setCPUThrottlingRate", { rate: CPU });
await page.goto(URL, { waitUntil: "networkidle" });
await page.waitForSelector(".app", { timeout: 15000 });
await page.waitForTimeout(600);

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
}, ROUNDS);
await page.waitForTimeout(400);

await cdp.send("Profiler.enable");
await cdp.send("Profiler.setSamplingInterval", { interval: 100 });
await cdp.send("Profiler.start");
await page.evaluate(async (k) => {
  window.__feed({ kind: "turn_started" });
  for (let i = 0; i < k; i++) {
    window.__feed({ kind: "text", text: "回答的下一小段文字。" });
    await new Promise((r) => requestAnimationFrame(r));
  }
  window.__feed({ kind: "turn_done" });
}, DELTAS);
const { profile } = await cdp.send("Profiler.stop");
await browser.close();

// 把采样按节点归并成自耗时。
const self = new Map();
const byId = new Map(profile.nodes.map((n) => [n.id, n]));
const total = profile.samples.length;
for (const id of profile.samples) {
  const n = byId.get(id);
  if (!n) continue;
  const f = n.callFrame;
  let where = `${(f.url || "").split("/").pop()?.split("?")[0] || "(native)"}:${f.lineNumber + 1}`;
  // Minified names say nothing: the first read of this table listed `h` and
  // `Gv`. Resolve through the bundle's own map when the build carries one.
  if (MAP && f.url?.includes("/assets/")) {
    const src = MAP.at(f.lineNumber, f.columnNumber);
    if (src) where = `${src.src.replace(/^.*\/src\//, "src/")}:${src.line}${src.name ? " " + src.name : ""}`;
  }
  const key = `${f.functionName || "(anonymous)"}  ${where}`;
  self.set(key, (self.get(key) ?? 0) + 1);
}
const ms = (profile.endTime - profile.startTime) / 1000;
const rows = [...self.entries()]
  .sort((a, b) => b[1] - a[1])
  .slice(0, 26)
  .map(([k, c]) => ({ 热点: k, "自耗时/ms": ((c / total) * ms).toFixed(0), "占比%": ((c / total) * 100).toFixed(1) }));
console.log(`采样窗口 ${ms.toFixed(0)}ms，${ROUNDS} 轮会话 · ${DELTAS} 个流式增量\n`);
console.table(rows);
