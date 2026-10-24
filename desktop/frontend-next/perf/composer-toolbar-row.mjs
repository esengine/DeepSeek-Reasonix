import assert from "node:assert/strict";
import { chromium } from "playwright";

const base = process.env.PERF_URL ?? "http://localhost:4399/perf.html";
const browser = await chromium.launch();
const failures = [];
try {
  for (const scheme of ["light", "dark"]) {
    for (const [pref, zoom] of [["zh", 1], ["en", 1], ["zh", 1.25], ["en", 1.25]]) {
      const page = await browser.newPage({ viewport: { width: 1500, height: 900 }, colorScheme: scheme, reducedMotion: "reduce" });
      page.setDefaultTimeout(8000);
      await page.goto(`${base}?pref=${pref}&ws=1&sess=1&turns=1&fullaccess&effort=max&modelref=workbuddy/deepseek-v4-pro-long-name&zoom=${zoom}`, { waitUntil: "networkidle" });
      await page.waitForSelector(".turntools");
      await page.evaluate(() => document.fonts.ready);
      const wrapped = [];
      for (let width = 900; width >= 400; width -= 10) {
        const rows = await page.evaluate((w) => {
          document.querySelector(".pane").style.width = `${w}px`;
          const tools = document.querySelector(".turntools");
          const centers = [...tools.children]
            .map((el) => el.getBoundingClientRect())
            .filter((box) => box.width > 0 && box.height > 0)
            .map((box) => box.top + box.height / 2)
            .sort((a, b) => a - b);
          return centers.reduce((rows, y, i) => (i && y - centers[i - 1] > 8 ? rows + 1 : rows), 1);
        }, width);
        if (rows > 1) wrapped.push(width);
      }
      console.log(`${wrapped.length === 0 ? "PASS" : "FAIL"} ${scheme} ${pref} zoom ${zoom}: toolbar stays on one row from 900 to 400 px${wrapped.length ? `; wraps at ${wrapped.join(", ")}` : ""}`);
      if (wrapped.length) failures.push(`${scheme}/${pref}/${zoom}`);
      await page.close();
    }
  }
} finally {
  await browser.close();
}
assert.deepEqual(failures, [], "the composer toolbar wrapped to a second row");
