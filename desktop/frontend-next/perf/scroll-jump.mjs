// The transcript's two jump buttons: absent where nothing scrolls or within 48px
// of their end, present and clear of the composer elsewhere, taking no layout
// height, clear of the scroll rail, the dock's tab and the composer at every
// width, and landing on the end when no expanded block lies that way.
import { chromium } from "playwright";
import { mkdirSync } from "node:fs";
import { join } from "node:path";

const PAGE = process.env.PERF_URL ?? "http://localhost:4399/perf.html?turns=60";
const SHOTS = process.env.SCROLLJUMP_SHOTS;
if (SHOTS) mkdirSync(SHOTS, { recursive: true });
const fails = [];
const check = (name, ok, detail = "") => {
  console.log(`${ok ? "  ok" : "FAIL"}  ${name}${detail ? "  — " + detail : ""}`);
  if (!ok) fails.push(name);
};

const SIZES = [[1280, 800], [1000, 700], [800, 700], [640, 700]];
const browser = await chromium.launch();
for (const scheme of ["light", "dark"]) {
  for (const [width, height] of SIZES) {
    const ctx = await browser.newContext({ locale: "zh-CN", viewport: { width, height }, colorScheme: scheme });
    const page = await ctx.newPage();
    page.on("pageerror", (e) => fails.push("page error: " + e.message));
    await page.goto(PAGE, { waitUntil: "domcontentloaded" });
    await page.waitForSelector('[data-pane="flow"] .flow');
    await page.waitForTimeout(1200);
    const root = page.locator('[data-pane="flow"]').first();
    await root.evaluate((el) => el.scrollTo({ top: el.scrollHeight * 0.4 }));
    await page.waitForTimeout(600);
    const clash = await page.evaluate(() => {
      const box = (name, el) => {
        if (!el) return null;
        const r = el.getBoundingClientRect();
        return r.width > 0 && r.height > 0 ? { name, left: r.left, top: r.top, right: r.right, bottom: r.bottom } : null;
      };
      const items = [
        ...[...document.querySelectorAll(".scrolljump button")].map((b) => box("jump-" + b.dataset.value, b)),
        box("rail", document.querySelector(".srail")),
        box("tab", document.querySelector(".gutter-r .grip")),
        box("compose", document.querySelector(".compose")),
        box("send", document.querySelector(".go .send, .go button")),
      ].filter(Boolean);
      const out = [];
      for (let i = 0; i < items.length; i++) for (let j = i + 1; j < items.length; j++) {
        const a = items[i], b = items[j];
        const w = Math.min(a.right, b.right) - Math.max(a.left, b.left), h = Math.min(a.bottom, b.bottom) - Math.max(a.top, b.top);
        const inside = (x, y) => x.left >= y.left && x.right <= y.right && x.top >= y.top && x.bottom <= y.bottom;
        if (w > 0.5 && h > 0.5 && !inside(a, b) && !inside(b, a)) out.push(`${a.name} x ${b.name}: ${w.toFixed(0)}x${h.toFixed(0)}`);
      }
      return { out, names: items.map((it) => it.name) };
    });
    check(`${scheme} ${width}x${height}: the buttons, rail, dock tab and composer do not overlap`, clash.out.length === 0, clash.out.join("; "));
    check(`${scheme} ${width}x${height}: the right edge shows the rail and the buttons`, (width > 800 ? ["rail", "jump-up", "jump-down"] : ["jump-up", "jump-down"]).every((n) => clash.names.includes(n)), clash.names.join(","));
    if (SHOTS) {
      await page.screenshot({ path: join(SHOTS, `${scheme}-edge-${width}.png`), clip: { x: Math.max(0, width - 220), y: 0, width: Math.min(220, width), height: Math.min(height, 640) } });
    }
    await ctx.close();
  }

  const ctx = await browser.newContext({ locale: "zh-CN", viewport: { width: 1280, height: 800 }, colorScheme: scheme });
  const page = await ctx.newPage();
  page.on("pageerror", (e) => fails.push("page error: " + e.message));
  await page.goto(PAGE, { waitUntil: "domcontentloaded" });
  await page.waitForSelector('[data-pane="flow"] .flow');
  await page.waitForTimeout(1200);
  const root = page.locator('[data-pane="flow"]').first();
  const up = page.locator('.scrolljump button[data-value="up"]');
  const down = page.locator('.scrolljump button[data-value="down"]');
  const geo = () => root.evaluate((el) => ({ top: el.scrollTop, c: el.clientHeight, h: el.scrollHeight }));

  await root.evaluate((el) => el.scrollTo({ top: el.scrollHeight }));
  await page.waitForTimeout(400);
  check(`${scheme}: at the end only the up button is offered`, (await up.count()) === 1 && (await down.count()) === 0);

  await root.evaluate((el) => el.scrollTo({ top: 0 }));
  await page.waitForTimeout(400);
  check(`${scheme}: at the start only the down button is offered`, (await up.count()) === 0 && (await down.count()) === 1);

  await root.evaluate((el) => el.scrollTo({ top: el.scrollHeight * 0.4 }));
  await page.waitForTimeout(500);
  check(`${scheme}: mid-way both are offered`, (await up.count()) === 1 && (await down.count()) === 1);
  const box = await page.evaluate(() => {
    const host = document.querySelector(".scrolljump");
    const scroller = document.querySelector('[data-pane="flow"]').getBoundingClientRect();
    const compose = document.querySelector(".compose").getBoundingClientRect();
    const btns = [...host.querySelectorAll("button")].map((b) => b.getBoundingClientRect());
    return { host: host.getBoundingClientRect().height, scrollerBottom: scroller.bottom, composeTop: compose.top,
      right: Math.max(...btns.map((b) => b.right)), scrollerRight: scroller.right, bottom: Math.max(...btns.map((b) => b.bottom)) };
  });
  check(`${scheme}: the buttons take no height`, box.host === 0, JSON.stringify(box));
  check(`${scheme}: the buttons sit inside the transcript, above the composer`, box.bottom <= box.scrollerBottom + 0.5 && box.bottom <= box.composeTop + 0.5 && box.right <= box.scrollerRight);
  if (SHOTS) await page.screenshot({ path: join(SHOTS, `${scheme}-mid.png`) });

  await up.click();
  await page.waitForTimeout(500);
  let g = await geo();
  check(`${scheme}: up with nothing expanded lands on the start`, g.top < 60, `top=${g.top.toFixed(0)}`);
  await root.evaluate((el) => el.scrollTo({ top: el.scrollHeight * 0.4 }));
  await page.waitForTimeout(500);
  for (let n = 0; n < 8 && g.h - g.top - g.c >= 60; n++) {
    await down.click();
    await page.waitForTimeout(500);
    g = await geo();
  }
  check(`${scheme}: down reaches the end`, g.h - g.top - g.c < 60, `left=${(g.h - g.top - g.c).toFixed(0)}`);
  await ctx.close();
}
await browser.close();
if (fails.length) {
  console.error(`\n${fails.length} failed`);
  process.exit(1);
}
