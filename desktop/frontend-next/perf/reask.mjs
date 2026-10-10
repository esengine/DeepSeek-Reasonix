// The rewrite box of a sent message: a long paragraph with no line breaks must
// be readable in full without scrolling inside the box, at desktop and phone width.
import { chromium } from "playwright";
import { fileURLToPath } from "node:url";
import { mkdirSync } from "node:fs";

const PAGE = process.env.PERF_URL ?? "http://localhost:4399/perf.html";
const SHOTS = process.env.REASK_SHOTS ?? fileURLToPath(new URL("shots", import.meta.url));
mkdirSync(SHOTS, { recursive: true });
const TAG = process.env.REASK_TAG ?? "now";
const LONG = "请把这个模块里所有的错误处理统一改成带类型的错误，并且保证调用方可以用 errors.Is 区分每一种失败，同时补上对应的单元测试，再检查文档里提到旧行为的地方是否需要更新，最后给出一份简短的迁移说明，列出每个受影响的函数以及它们各自需要调整的调用点。".repeat(3);

const fails = [];
const check = (name, ok, detail = "") => {
  console.log(`${ok ? "  ok" : "FAIL"}  ${name}${detail ? "  — " + detail : ""}`);
  if (!ok) fails.push(name);
};

const browser = await chromium.launch();
for (const scheme of ["light", "dark"]) {
  for (const [label, width, height] of [["wide", 1440, 900], ["phone", 390, 800]]) {
    const ctx = await browser.newContext({ locale: "zh-CN", colorScheme: scheme, viewport: { width, height } });
    const page = await ctx.newPage();
    page.on("pageerror", (e) => fails.push("pageerror: " + e.message));
    await page.goto(PAGE, { waitUntil: "networkidle" });
    await page.waitForSelector(".app", { timeout: 15000 });
    await page.waitForTimeout(600);
    const composer = page.locator(".compose textarea").first();
    await composer.fill(LONG);
    await composer.press("Enter");
    await page.waitForTimeout(900);
    await page.evaluate(() => window.__feed({ kind: "turn_started", authoredTurn: 1, msgIndex: 1 }));
    await page.waitForTimeout(700);
    await page.locator(".reask-open").last().click({ force: true });
    await page.waitForTimeout(500);
    const g = await page.evaluate(() => {
      const el = document.querySelector(".reask textarea");
      if (!el) return null;
      return { h: el.getBoundingClientRect().height, scroll: el.scrollHeight, client: el.clientHeight, view: innerHeight };
    });
    check(`${scheme}/${label}: the rewrite box opened`, !!g);
    if (g) {
      const room = Math.min(g.scroll, g.view * 0.6, 448);
      check(`${scheme}/${label}: long single-line text is visible without scrolling the box`, g.client >= room - 2, `box ${Math.round(g.h)}px, content ${g.scroll}px`);
    }
    if (g) {
      const kb = Math.round(height * 0.5);
      await page.setViewportSize({ width, height: kb });
      await page.waitForTimeout(300);
      await page.locator(".reask textarea").focus();
      await page.keyboard.press("Control+End");
      await page.waitForTimeout(200);
      const k = await page.evaluate(() => {
        const el = document.querySelector(".reask textarea");
        const r = el.getBoundingClientRect();
        return { h: r.height, view: innerHeight, caretAtEnd: el.selectionStart === el.value.length };
      });
      check(`${scheme}/${label}: with the viewport shrunk to ${kb}px the box stays within 60% of it and the caret stays at the end`, k.h <= k.view * 0.6 + 2 && k.caretAtEnd, `box ${Math.round(k.h)}px of ${k.view}px`);
      await page.setViewportSize({ width, height });
      await page.waitForTimeout(300);
    }
    await page.locator(".reask").scrollIntoViewIfNeeded();
    await page.screenshot({ path: `${SHOTS}/reask-${TAG}-${scheme}-${label}.png` });
    await ctx.close();
  }
}

for (const [label, width, height] of [["wide", 1440, 900], ["phone", 390, 800]]) {
  const ctx = await browser.newContext({ locale: "zh-CN", viewport: { width, height } });
  const page = await ctx.newPage();
  await page.goto(`${PAGE}?queue=2&queuebody`, { waitUntil: "networkidle" });
  await page.waitForSelector(".queue .qi", { timeout: 15000 });
  await page.waitForTimeout(600);
  await page.locator('.queue .qi').first().locator('button:has-text("改")').click();
  await page.waitForTimeout(600);
  const q = await page.evaluate(() => {
    const el = document.querySelector(".queue .qedit");
    if (!el) return null;
    const room = el.closest(".qitems")?.clientHeight ?? Infinity;
    return { client: el.clientHeight, scroll: el.scrollHeight, cap: parseFloat(getComputedStyle(el).maxHeight), room };
  });
  check(`queue/${label}: the inline editor opened`, !!q);
  if (q) check(`queue/${label}: a long single line fills the editor up to its cap or the room the list leaves`, q.cap >= 100 && q.client >= Math.min(q.scroll, q.cap * 0.9, q.room - 24), `editor ${q.client}px, content ${q.scroll}px, cap ${q.cap}px, room ${q.room}px`);
  await ctx.close();
}
for (const [label, width, height] of [["wide", 1440, 900], ["phone", 390, 800]]) {
  for (const [what, fill] of [["long line", null], ["sixty lines", Array.from({ length: 60 }, (_, i) => `line ${i + 1}`).join("\n")]]) {
    const ctx = await browser.newContext({ locale: "zh-CN", viewport: { width, height } });
    const page = await ctx.newPage();
    await page.goto(`${PAGE}?queue=4&queuebody`, { waitUntil: "networkidle" });
    await page.waitForSelector(".queue .qi", { timeout: 15000 });
    await page.waitForTimeout(600);
    await page.locator(".queue .qi").nth(1).locator('button:has-text("改")').click();
    await page.waitForTimeout(600);
    if (fill) { await page.locator(".queue .qedit").fill(fill); await page.waitForTimeout(400); }
    const g = await page.evaluate(() => {
      const el = document.querySelector(".queue .qedit");
      if (!el) return null;
      const list = el.closest(".qitems").getBoundingClientRect();
      const row = el.closest(".qi").getBoundingClientRect();
      return { top: row.top - list.top, bottom: list.bottom - row.bottom, room: list.height, row: row.height };
    });
    check(`queue/${label}/${what}: the editor row sits whole inside the list's visible area`, !!g && g.top >= -0.5 && g.bottom >= -0.5, g ? `row ${Math.round(g.row)}px in ${Math.round(g.room)}px, ${Math.round(g.top)}px above / ${Math.round(g.bottom)}px below` : "");
    await ctx.close();
  }
}
await browser.close();
console.log(fails.length ? `\n${fails.length} failed` : "\nall passed");
process.exit(fails.length ? 1 : 0);
