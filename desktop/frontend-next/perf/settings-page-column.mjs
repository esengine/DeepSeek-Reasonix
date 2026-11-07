import assert from "node:assert/strict";
import { chromium } from "playwright";

const PAGE = process.env.PERF_URL ?? "http://localhost:4399/perf.html?pref=zh";
const browser = await chromium.launch();
try {
  const page = await browser.newPage({ viewport: { width: 1600, height: 900 } });
  const errors = [];
  page.on("pageerror", (error) => errors.push(error.message));
  await page.goto(PAGE, { waitUntil: "networkidle" });
  await page.evaluate(() => document.querySelector('[data-action="chrome.settings"]')?.click());
  await page.waitForSelector(".prefs-nav button");
  const lefts = () =>
    page.evaluate(() => {
      const left = (selector) => Math.round(document.querySelector(selector).getBoundingClientRect().left);
      return { title: left(".prefs-main .prefs-pagetitle"), index: left(".prefs-main .page-index"), col: left(".prefs-main .prefs-col") };
    });
  for (const name of ["模型", "连接", "常用"]) {
    await page.locator(".prefs-nav button", { hasText: new RegExp(`^${name}`) }).first().click();
    await page.waitForTimeout(400);
    const at = await lefts();
    assert.equal(at.index, at.title, `${name}: the page index starts where the title does (${JSON.stringify(at)})`);
    assert.equal(at.col, at.title, `${name}: the cards start where the title does (${JSON.stringify(at)})`);
    console.log(`PASS ${name}: title, index and cards all start at ${at.title}px`);
  }
  await page.locator(".prefs-nav button", { hasText: /^操作与性能/ }).first().click();
  await page.waitForTimeout(400);
  await page.getByRole("button", { name: "调整导航" }).click();
  await page.waitForTimeout(600);
  const shown = await page.evaluate(() => {
    const card = document.querySelector(".prefs-col .deep-page").closest(".prefs-card");
    return [...card.children].filter((c) => !c.classList.contains("deep-page") && getComputedStyle(c).display !== "none").length;
  });
  assert.equal(shown, 0, "an open sub-page shows nothing of the page it was opened from");
  console.log("PASS sub-page: nothing of the parent page remains above it");
  assert.deepEqual(errors, []);
} finally {
  await browser.close();
}
