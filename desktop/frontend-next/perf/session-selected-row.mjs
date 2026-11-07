// The current session's row must read as current at a glance: its fill is stronger than a hovered row's
// and a plain row's, and hovering it does not change that fill.
import assert from "node:assert/strict";
import { chromium } from "playwright";

const BASE = process.env.PERF_URL ?? "http://localhost:4399/perf.html";
const MIN_SELECTED_OVER_REST = 1.05;
const MIN_SELECTED_OVER_HOVER = 1.02;
const luminance = ([r, g, b]) => [r, g, b].map((c) => { c /= 255; return c <= 0.03928 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4; }).reduce((s, v, i) => s + v * [0.2126, 0.7152, 0.0722][i], 0);
const contrast = (a, b) => { const [hi, lo] = [luminance(a), luminance(b)].sort((x, y) => y - x); return (hi + 0.05) / (lo + 0.05); };

const browser = await chromium.launch();
try {
  for (const theme of ["light", "dark"]) {
    const page = await (await browser.newContext({ locale: "zh-CN", viewport: { width: 1280, height: 800 }, colorScheme: theme, reducedMotion: "reduce" })).newPage();
    await page.goto(BASE, { waitUntil: "networkidle" });
    await page.waitForSelector(".sessrow[data-on]");
    const selectedRow = page.locator(".sessrow[data-on]").first();
    const plainRow = page.locator(".sessrow:not([data-on]):not([data-run]):not([data-unread])").first();
    const sample = async (row, hover) => {
      await row.scrollIntoViewIfNeeded();
      await (hover ? row.hover() : page.mouse.move(1270, 790));
      await page.waitForTimeout(250);
      const box = await row.boundingBox();
      const png = await page.screenshot({ clip: { x: box.x + 5, y: box.y + box.height / 2 - 2, width: 4, height: 4 } });
      return page.evaluate(async (b64) => {
        const img = new Image();
        img.src = `data:image/png;base64,${b64}`;
        await img.decode();
        const c = document.createElement("canvas");
        c.width = c.height = 4;
        const ctx = c.getContext("2d");
        ctx.drawImage(img, 0, 0);
        return [...ctx.getImageData(1, 1, 1, 1).data].slice(0, 3);
      }, png.toString("base64"));
    };
    const rest = await sample(plainRow, false), selected = await sample(selectedRow, false), hovered = await sample(plainRow, true), selectedHovered = await sample(selectedRow, true);
    assert.ok(contrast(selected, rest) >= MIN_SELECTED_OVER_REST, `${theme}: selected over a plain row is ${contrast(selected, rest).toFixed(3)}`);
    assert.ok(contrast(selected, hovered) >= MIN_SELECTED_OVER_HOVER, `${theme}: selected over a hovered row is ${contrast(selected, hovered).toFixed(3)}`);
    assert.deepEqual(selectedHovered, selected, `${theme}: hovering the selected row changes its fill`);
    await page.close();
  }
} finally {
  await browser.close();
}
console.log("the selected session row is distinct from hover and plain rows, in light and dark");
