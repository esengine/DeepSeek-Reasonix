import assert from "node:assert/strict";
import { chromium } from "playwright";

const PAGE = process.env.PERF_URL ?? "http://localhost:4399/perf.html?pref=zh";
const browser = await chromium.launch();
try {
  const page = await browser.newPage({ viewport: { width: 1300, height: 900 } });
  await page.goto(PAGE, { waitUntil: "networkidle" });
  await page.waitForSelector(".app");
  const heights = await page.evaluate(() => {
    const long = Array.from({ length: 30 }, (_, i) => `line ${i}`).join("\n");
    const host = document.querySelector(".app");
    const make = (attrs) => {
      const box = document.createElement("div");
      box.innerHTML = `<details class="think" open ${attrs}><summary>t</summary><div class="tk"><div class="tk-in" style="white-space:pre-wrap">${long}</div></div></details>`;
      host.appendChild(box);
      return box.querySelector(".tk").getBoundingClientRect().height;
    };
    return { windowed: make("data-live"), full: make("data-live data-full") };
  });
  assert.ok(heights.windowed < 200, `a live thought is a window of a few lines (${heights.windowed}px)`);
  assert.ok(heights.full > heights.windowed * 2, `a thought the reader asked to keep open grows with its text (${heights.full}px)`);
  console.log(`PASS live thinking window ${Math.round(heights.windowed)}px, always-open ${Math.round(heights.full)}px`);
} finally {
  await browser.close();
}
