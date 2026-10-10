// 滚轮轨迹的探针：在一份很高的两轮转录里一路向上滚，scrollTop 在滚轮向上时
// 不许往下走、单步不许比滚轮多走、scrollHeight 不许缩水。
//
// 目前只报告、不判红：`.call` 的 `content-visibility: auto` 配上
// `contain-intrinsic-size: auto 216px`，会在块第一次进入视口时把预估高度改成
// 实测高度，scrollHeight 随之缩水（12975 → 11217），内容在手指下跳动。修复
// 落地后把 `WHEEL_STRICT` 的默认值翻成 1，这条探针就成为那次修复的验收。
import { chromium } from "playwright";

const PAGE = process.env.PERF_URL ?? "http://localhost:4399/perf.html";
const STEP = Number(process.env.WHEEL_STEP ?? 100);
const TURNS = Number(process.env.WHEEL_TURNS ?? 2);
const CALLS = Number(process.env.WHEEL_CALLS ?? 6);
const WAIT = Number(process.env.WHEEL_WAIT ?? 120);
const STRICT = (process.env.WHEEL_STRICT ?? "0") === "1";

const browser = await chromium.launch();
const ctx = await browser.newContext({ locale: "zh-CN", viewport: { width: 1440, height: 900 } });
const page = await ctx.newPage();
const findings = [];
const pageErrors = [];
page.on("pageerror", (e) => pageErrors.push("page error: " + e.message));

await page.goto(PAGE + (PAGE.includes("?") ? "&" : "?") + "queue=1", { waitUntil: "networkidle" });
await page.waitForSelector(".app", { timeout: 15000 });
await page.waitForTimeout(700);

const code = "```go\n" + Array.from({ length: 40 }, (_, i) => `func f${i}(x int) int { return x*${i} + 1 } // line ${i}`).join("\n") + "\n```\n\n";
const table = "| a | b | c |\n|---|---|---|\n" + Array.from({ length: 12 }, (_, i) => `| ${i} | row ${i} | value ${i * 7} |`).join("\n") + "\n\n";
const prose = Array.from({ length: 14 }, (_, i) => `Paragraph ${i}: ` + "the quick brown fox jumps over the lazy dog, ".repeat(9)).join("\n\n") + "\n\n";
const reply = "# Heading\n\n" + prose + code + table + prose + code + "- one\n- two\n- three\n\n" + prose;

await page.evaluate(async ({ n, reply, calls }) => {
  const frame = () => new Promise((r) => requestAnimationFrame(r));
  for (let i = 0; i < n; i++) {
    window.__feed({ kind: "turn_started", authoredTurn: i + 1, msgIndex: i * 2, text: `Question ${i}` });
    for (let k = 0; k < (i === 0 ? calls : 6); k++) {
      const id = `t${i}-${k}`;
      const args = JSON.stringify({ path: `pkg/f${k}.go` });
      window.__feed({ kind: "tool_dispatch", tool: { id, name: "edit_file", args } });
      window.__feed({ kind: "tool_result", tool: { id, name: "edit_file", args, output: "ok", durationMs: 210, added: 4, removed: 2 } });
    }
    window.__feed({ kind: "text", text: reply });
    window.__feed({ kind: "message" });
    window.__feed({ kind: "turn_done" });
    await frame();
  }
  await frame();
}, { n: TURNS, reply, calls: CALLS });
await page.waitForTimeout(800);

const flow = page.locator('[data-pane="flow"]').first();
const box = await flow.boundingBox();
const cdp = await ctx.newCDPSession(page);
const read = () => flow.evaluate((el) => ({ top: el.scrollTop, h: el.scrollHeight }));

const start = await read();
const rows = [];
let prev = start;
for (let i = 0; i < 400; i++) {
  await cdp.send("Input.dispatchMouseEvent", { type: "mouseWheel", x: box.x + box.width / 2, y: box.y + box.height / 2, deltaX: 0, deltaY: -STEP });
  await page.waitForTimeout(WAIT);
  const p = await read();
  rows.push({ i, d: p.top - prev.top, ...p });
  prev = p;
  if (p.top <= 0) break;
}

const check = (name, ok, detail = "") => {
  console.log(`${ok ? "  ok" : STRICT ? "FAIL" : "NOTE"}  ${name}${detail ? "  — " + detail : ""}`);
  if (!ok) findings.push(name);
};
const down = rows.filter((r) => r.d > 2);
const jump = rows.filter((r) => r.d < -STEP * 1.5);
const minH = Math.min(start.h, ...rows.map((r) => r.h));
console.log(`start top=${start.top} scrollHeight=${start.h}; ${rows.length} wheel steps; scrollHeight min=${minH} end=${prev.h}`);
check("never moves down while the wheel goes up", down.length === 0, down.slice(0, 6).map((r) => `#${r.i}:${r.d}`).join(" "));
check("no step exceeds the wheel", jump.length === 0, jump.slice(0, 6).map((r) => `#${r.i}:${r.d}`).join(" "));
check("scrollHeight does not shrink", minH >= start.h - 4, `${start.h} -> ${minH}`);

await browser.close();
for (const e of pageErrors) console.log(e);
process.exit(pageErrors.length || (STRICT && findings.length) ? 1 : 0);
