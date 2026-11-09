import assert from "node:assert/strict";
import { chromium } from "playwright";

const PAGE = process.env.PERF_URL ?? "http://localhost:4399/perf.html?pref=zh";
const browser = await chromium.launch();
try {
  for (const width of [1920, 2560]) {
    const page = await browser.newPage({ viewport: { width, height: 900 } });
    const errors = [];
    page.on("pageerror", (error) => errors.push(error.message));
    await page.goto(PAGE, { waitUntil: "networkidle" });
    await page.evaluate(() => document.querySelector('[data-action="chrome.settings"]')?.click());
    await page.waitForSelector(".prefs-nav button");
    await page.waitForSelector(".prefs-main .prefs-col");
    await page.waitForTimeout(400);
    const room = await page.evaluate(() => {
      const main = document.querySelector(".prefs-main").getBoundingClientRect();
      const col = document.querySelector(".prefs-main .prefs-col").getBoundingClientRect();
      const screen = document.querySelector(".prefs").getBoundingClientRect();
      const sheet = document.querySelector(".prefs-sheet").getBoundingClientRect();
      const nav = document.querySelector(".prefs-nav").getBoundingClientRect();
      const cards = [...document.querySelectorAll(".prefs-col > .prefs-card")].map((c) => c.getBoundingClientRect());
      return { sheet: { left: sheet.left, right: sheet.right, bottom: sheet.bottom }, nav: nav.width, left: Math.round(col.left - main.left), right: Math.round(main.right - col.right), width: Math.round(col.width), screen: { x: screen.x, y: screen.y, width: screen.width, height: screen.height }, single: cards.every((c, i) => Math.abs(c.width - col.width) < 1 && (!i || c.top >= cards[i - 1].bottom)) };
    });
    assert.deepEqual(room.screen, { x: 0, y: 0, width, height: 900 }, "settings occupies the application window");
    assert.deepEqual(room.sheet, { left: 0, right: width, bottom: 900 });
    assert.equal(room.nav, 240);
    assert.ok(room.single, "all cards fill one content column");
    assert.ok(room.left >= 20 && room.left <= 32 && room.right >= 20 && room.right <= 32, `${width}px: only fixed content gutters remain (${JSON.stringify(room)})`);
    assert.ok(Math.abs(room.left - room.right) <= 2, `${width}px: the room is shared evenly (left ${room.left}, right ${room.right})`);
    assert.deepEqual(errors, []);
    console.log(`PASS ${width}px: column ${room.width}px with ${room.left}px each side`);
    await page.close();
  }
} finally {
  await browser.close();
}
