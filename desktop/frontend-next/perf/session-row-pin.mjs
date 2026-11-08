import assert from "node:assert/strict";
import { chromium } from "playwright";

const PAGE = process.env.PERF_URL ?? "http://localhost:4399/perf.html?pref=zh";
const browser = await chromium.launch();
try {
  const page = await browser.newPage({ viewport: { width: 1300, height: 900 } });
  const errors = [];
  page.on("pageerror", (error) => errors.push(error.message));
  await page.addInitScript(() => localStorage.setItem("rx-session-quick-actions", "on"));
  await page.goto(PAGE, { waitUntil: "networkidle" });
  await page.waitForSelector(".ds-row.sessrow");
  await page.waitForTimeout(500);
  const shown = (row, selector) =>
    row.evaluate((el, sel) => {
      const node = el.querySelector(sel);
      return !!node && getComputedStyle(node).display !== "none" && getComputedStyle(node).opacity !== "0" && node.getBoundingClientRect().width > 2;
    }, selector);
  const rows = page.locator(".ds-row.sessrow");
  await page.mouse.move(700, 450);
  for (let i = 0; i < Math.min(await rows.count(), 5); i++) {
    assert.equal(await shown(rows.nth(i), ".sesspin-action"), false, `row ${i}: a resting row shows no pin button`);
    assert.equal(await shown(rows.nth(i), ".sesspin-marker"), false, `row ${i}: an unpinned row shows no pin marker`);
  }
  const first = rows.first();
  await first.hover();
  assert.equal(await shown(first, ".sesspin-action"), true, "hovering a row offers the pin button");
  await first.locator(".sesspin-action").click();
  await page.waitForTimeout(400);
  await page.evaluate(() => document.activeElement?.blur());
  await page.mouse.move(700, 450);
  await page.waitForTimeout(200);
  const pinned = page.locator('.ds-row.sessrow:has(.sesspin-marker)').first();
  assert.equal(await shown(pinned, ".sesspin-marker"), true, "a pinned row keeps its marker while the pointer is away");
  assert.equal(await shown(pinned, ".sesspin-action"), false, "and still hides the button");
  await pinned.hover();
  assert.equal(await shown(pinned, ".sesspin-marker"), false, "the marker gives way to the button under the pointer");
  assert.equal(await shown(pinned, ".sesspin-action"), true);
  assert.deepEqual(errors, []);
  console.log("PASS session row pin button and marker");
} finally {
  await browser.close();
}
