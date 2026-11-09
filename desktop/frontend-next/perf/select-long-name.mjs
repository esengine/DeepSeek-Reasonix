import assert from "node:assert/strict";
import { chromium } from "playwright";

const PAGE = process.env.PERF_URL ?? "http://localhost:4399/perf.html?pref=zh";
const browser = await chromium.launch();
try {
  const page = await browser.newPage({ viewport: { width: 1500, height: 900 }, deviceScaleFactor: 2 });
  await page.goto(PAGE, { waitUntil: "networkidle" });
  await page.evaluate(() => document.querySelector('[data-action="chrome.settings"]')?.click());
  await page.waitForSelector(".prefs-nav button");
  await page.locator(".prefs-nav button", { hasText: /^模型/ }).first().click();
  await page.waitForTimeout(400);
  await page.getByRole("button", { name: "按用途设置" }).click();
  await page.waitForTimeout(500);
  const fit = await page.evaluate(() => {
    const select = document.querySelector("select.usage-pick");
    const option = document.createElement("option");
    option.value = "long";
    option.textContent = "deepseek-v4.1-flash-1017-preview-extended · 读图";
    select.appendChild(option);
    select.value = "long";
    select.dispatchEvent(new Event("change", { bubbles: true }));
    return new Promise((resolve) => requestAnimationFrame(() => {
      const box = select.getBoundingClientRect();
      resolve({ height: Math.round(box.height), overflow: select.scrollHeight - select.clientHeight, whiteSpace: getComputedStyle(select).whiteSpace });
    }));
  });
  assert.equal(fit.whiteSpace, "nowrap", "a select never wraps the name it shows");
  assert.ok(fit.overflow <= 2, `a long chosen name does not spill out of its box (${JSON.stringify(fit)})`);
  console.log("PASS a long chosen name stays on one line inside its select");
} finally {
  await browser.close();
}
