import assert from "node:assert/strict";
import { chromium } from "playwright";

const PAGE = process.env.PERF_URL ?? "http://localhost:4399/perf.html?pref=zh";
const browser = await chromium.launch();
try {
  for (const scheme of ["light", "dark"]) {
    const context = await browser.newContext({ viewport: { width: 1500, height: 900 }, colorScheme: scheme, deviceScaleFactor: 2 });
    const page = await context.newPage();
    await page.goto(PAGE, { waitUntil: "networkidle" });
    await page.evaluate(() => document.querySelector('[data-action="chrome.settings"]')?.click());
    await page.waitForSelector(".prefs-nav button");
    await page.locator(".prefs-nav button", { hasText: /^常用/ }).first().click();
    await page.waitForTimeout(500);
    const png = await page.locator("select").first().screenshot();
    const ink = await page.evaluate(async ({ image }) => {
      const img = new Image();
      img.src = image;
      await img.decode();
      const canvas = document.createElement("canvas");
      canvas.width = img.width;
      canvas.height = img.height;
      const context2d = canvas.getContext("2d");
      context2d.drawImage(img, 0, 0);
      const data = context2d.getImageData(0, 0, img.width, img.height).data;
      const at = (x, y) => [data[(y * img.width + x) * 4], data[(y * img.width + x) * 4 + 1], data[(y * img.width + x) * 4 + 2]];
      const bg = at(Math.floor(img.width / 2), 6);
      let top = img.height;
      let bottom = -1;
      for (let y = 4; y < img.height - 4; y++) {
        for (let x = 16; x < img.width * 0.7; x++) {
          const p = at(x, y);
          if (Math.abs(p[0] - bg[0]) + Math.abs(p[1] - bg[1]) + Math.abs(p[2] - bg[2]) > 150) {
            top = Math.min(top, y);
            bottom = Math.max(bottom, y);
          }
        }
      }
      return { above: top, below: img.height - 1 - bottom };
    }, { image: `data:image/png;base64,${png.toString("base64")}` });
    assert.ok(Math.abs(ink.above - ink.below) <= 4, `${scheme}: the chosen text sits in the middle of its select (${JSON.stringify(ink)})`);
    console.log(`PASS ${scheme}: select text centred (${ink.above}px above, ${ink.below}px below at 2x)`);
    await context.close();
  }
} finally {
  await browser.close();
}
