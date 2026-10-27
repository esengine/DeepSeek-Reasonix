// An enlarged settings window centres its content column between the nav and
// the window edge: the room left over is shared, not piled up on the right.
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
    await page.getByRole("button", { name: "放大设置窗口" }).click();
    await page.waitForTimeout(400);
    const room = await page.evaluate(() => {
      const main = document.querySelector(".prefs-main").getBoundingClientRect();
      const col = document.querySelector(".prefs-main .prefs-col").getBoundingClientRect();
      return { left: Math.round(col.left - main.left), right: Math.round(main.right - col.right), width: Math.round(col.width) };
    });
    assert.ok(room.width <= 1200, `the column keeps its cap (${room.width})`);
    assert.ok(Math.abs(room.left - room.right) <= 2, `${width}px: the room is shared evenly (left ${room.left}, right ${room.right})`);
    assert.deepEqual(errors, []);
    console.log(`PASS ${width}px: column ${room.width}px with ${room.left}px each side`);
    await page.close();
  }
} finally {
  await browser.close();
}
