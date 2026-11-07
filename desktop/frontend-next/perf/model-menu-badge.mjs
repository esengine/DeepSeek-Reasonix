import assert from "node:assert/strict";
import { chromium } from "playwright";

const PAGE = process.env.PERF_URL ?? "http://localhost:4399/perf.html?pref=zh";
const browser = await chromium.launch();
try {
  const page = await browser.newPage({ viewport: { width: 1300, height: 900 } });
  const errors = [];
  page.on("pageerror", (error) => errors.push(error.message));
  await page.goto(PAGE, { waitUntil: "networkidle" });
  await page.waitForSelector(".model-picker");
  await page.locator(".model-picker").first().click();
  await page.waitForSelector(".studio-model-menu .studio-item-line");
  const sizes = await page.evaluate(() => {
    const lines = [...document.querySelectorAll(".studio-model-menu .studio-item-line")].filter((line) => line.querySelector(".studio-item-badge"));
    for (const line of lines) line.querySelector(".lb").textContent = "workbuddy/deepseek-v4.1-flash-preview-extended";
    return lines.map((line) => {
      const badge = line.querySelector(".studio-item-badge").getBoundingClientRect();
      return { width: Math.round(badge.width), height: Math.round(badge.height) };
    });
  });
  assert.ok(sizes.length > 0, "the menu has a model that reads images");
  for (const size of sizes) assert.ok(size.height <= 20, `a long model name leaves the badge on one line (${JSON.stringify(size)})`);
  assert.deepEqual(errors, []);
  console.log(`PASS model menu badge stays on one line beside a long name (${sizes.length} rows)`);
} finally {
  await browser.close();
}
