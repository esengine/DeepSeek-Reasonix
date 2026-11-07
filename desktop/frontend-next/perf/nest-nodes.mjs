// A long session of delegated work keeps the page light: a finished sub-agent's child calls are
// not in the document until its card is opened, so node count follows what is open, not what ran.
import { chromium } from "playwright";

const BASE = process.env.PERF_URL ?? "http://localhost:4399/perf.html";
const AGENTS = 120;
const LAST = `a${AGENTS - 1}`;
const MAX_NODES = Number(process.env.PERF_NODES_MAX ?? 25000);
const fails = [];
const check = (name, ok, detail = "") => {
  console.log(`${ok ? "  ok" : "FAIL"}  ${name}${detail ? "  — " + detail : ""}`);
  if (!ok) fails.push(name);
};
const shotDir = process.env.SHOTS;

const browser = await chromium.launch();
for (const scheme of ["light", "dark"]) {
  const ctx = await browser.newContext({ locale: "zh-CN", colorScheme: scheme, viewport: { width: 1400, height: 900 }, reducedMotion: "reduce" });
  const page = await ctx.newPage();
  page.on("pageerror", (e) => fails.push("page error: " + e.message));
  await page.goto(`${BASE}?turns=3&pref=zh`, { waitUntil: "networkidle" });
  await page.waitForSelector(".compose");
  await page.waitForTimeout(600);
  const before = await page.evaluate(() => document.querySelectorAll("*").length);
  await page.evaluate((n) => {
    window.__feed({ kind: "turn_started" });
    const big = (tag) => Array.from({ length: 60 }, (_, k) => `${tag} line ${k}: matched in internal/pkg${k % 40}/file${k}.go`).join("\n");
    for (let a = 0; a < n; a++) {
      const pid = `a${a}`;
      window.__feed({ kind: "tool_dispatch", tool: { id: pid, name: "task", args: JSON.stringify({ description: `子代理 ${a}`, prompt: "查" }) } });
      for (let c = 0; c < 15; c++) {
        const id = `${pid}c${c}`, name = c % 3 === 0 ? "bash" : c % 3 === 1 ? "grep" : "read_file";
        window.__feed({ kind: "tool_dispatch", tool: { id, name, args: JSON.stringify({ command: "x" }), parentId: pid } });
        window.__feed({ kind: "tool_result", tool: { id, name, args: JSON.stringify({ command: "x" }), output: big(id), parentId: pid, durationMs: 300 } });
      }
      window.__feed({ kind: "tool_result", tool: { id: pid, name: "task", args: "", output: "report " + a, durationMs: 9000 } });
    }
  }, AGENTS);
  await page.waitForTimeout(800);
  const after = await page.evaluate(() => document.querySelectorAll("*").length);
  const kids = await page.evaluate(() => document.querySelectorAll(".nest-bd .call").length);
  check(`${scheme}: ${AGENTS} finished sub-agents add ${after - before} nodes, under the ${MAX_NODES} limit`, after < MAX_NODES, `${after} in the page`);
  check(`${scheme}: none of their child calls is in the document while the cards are closed`, kids === 0, `${kids}`);
  await page.evaluate((id) => { const g = [...document.querySelectorAll("details")].filter((d) => d.querySelector(`[data-call="${id}"]`)).pop(); if (g) g.open = true; }, LAST);
  await page.waitForTimeout(400);
  const shoot = async (name) => {
    if (!shotDir) return;
    await page.evaluate((id) => document.querySelector(`[data-call="${id}"]`)?.scrollIntoView({ block: "start" }), LAST);
    await page.waitForTimeout(300);
    await page.screenshot({ path: `${shotDir}/${scheme}-nest-${name}.png`, clip: { x: 360, y: 40, width: 1000, height: 420 } });
  };
  await shoot("closed");
  const opened = await page.evaluate((id) => {
    const d = document.querySelector(`[data-call="${id}"] details.tool-disclosure`);
    if (!d) return -1;
    d.open = true;
    d.dispatchEvent(new Event("toggle"));
    return 1;
  }, LAST);
  await page.waitForTimeout(300);
  const nowKids = await page.evaluate((id) => document.querySelectorAll(`[data-call="${id}"] .nest-bd .call`).length, LAST);
  check(`${scheme}: opening one card mounts exactly its own 15 child calls`, opened === 1 && nowKids === 15, `${nowKids}`);
  await shoot("open");
  await ctx.close();
}
await browser.close();
if (fails.length) {
  console.log(`\n${fails.length} failed:\n  ` + fails.join("\n  "));
  process.exit(1);
}
console.log("\nall passed");
