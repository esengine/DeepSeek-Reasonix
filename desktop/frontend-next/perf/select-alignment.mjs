import assert from "node:assert/strict";
import { mkdir } from "node:fs/promises";
import { chromium } from "playwright";

const base = process.env.PERF_URL ?? "http://localhost:4399/perf.html";
const browser = await chromium.launch();
try {
  await mkdir("perf/shots", { recursive: true });
  for (const colorScheme of ["light", "dark"]) for (const width of [1300, 390]) for (const language of ["zh", "en"]) for (const scale of [1, 1.3]) {
    const page = await browser.newPage({ viewport: { width: 1300, height: 900 }, colorScheme, reducedMotion: "reduce" });
    page.setDefaultTimeout(10000);
    const errors = [];
    page.on("pageerror", (error) => errors.push(error.message));
    await page.addInitScript((value) => localStorage.setItem("rx-lang", value), language);
    await page.goto(`${base}?pref=${language}&turns=1`, { waitUntil: "networkidle" });
    await page.locator('[data-action="chrome.settings"]').click();
    await page.locator('.prefs-nav [data-value="common"]').click();
    const select = page.locator('[data-action="appearance.language"]');
    await select.waitFor();
    await page.setViewportSize({ width, height: 900 });
    await page.evaluate(async (value) => {
      document.documentElement.style.setProperty("--fs-scale", String(value));
      await document.fonts.ready;
    }, scale);
    const label = `${colorScheme}-${width}-${language}-${scale}`;
    const colors = await select.evaluate((field) => ({ ink: getComputedStyle(field).color, ground: getComputedStyle(field).backgroundColor }));
    const shot = await select.screenshot({ path: `perf/shots/select-alignment-${label}.png` });
    const bounds = await page.evaluate(async ({ data, ink, ground }) => {
      const picture = new Image();
      picture.src = `data:image/png;base64,${data}`;
      await picture.decode();
      const canvas = document.createElement("canvas");
      canvas.width = picture.width; canvas.height = picture.height;
      const ctx = canvas.getContext("2d");
      const rgb = (color) => { ctx.fillStyle = color; ctx.fillRect(0, 0, 1, 1); return [...ctx.getImageData(0, 0, 1, 1).data].slice(0, 3); };
      const fg = rgb(ink), bg = rgb(ground);
      ctx.drawImage(picture, 0, 0);
      const pixels = ctx.getImageData(0, 0, canvas.width, canvas.height).data;
      const distance = (pixel, color) => color.reduce((sum, channel, i) => sum + (channel - pixel[i]) ** 2, 0);
      let top = canvas.height, bottom = -1;
      for (let y = 1; y < canvas.height - 1; y++) for (let x = 12; x < canvas.width - 34; x++) {
        const pixel = pixels.slice((y * canvas.width + x) * 4, (y * canvas.width + x) * 4 + 3);
        if (distance(pixel, fg) < distance(pixel, bg)) { top = Math.min(top, y); bottom = Math.max(bottom, y); }
      }
      return { top, bottom, height: canvas.height, offset: Math.abs((top + bottom + 1 - canvas.height) / 2) };
    }, { data: shot.toString("base64"), ...colors });
    assert.ok(bounds.bottom > bounds.top, `${label}: the selected text is visible`);
    assert.ok(bounds.offset <= 2, `${label}: text is vertically centered (${JSON.stringify(bounds)})`);
    assert.deepEqual(await select.locator("option").allTextContents(), language === "zh" ? ["跟随系统", "简体中文", "英文"] : ["Follow the system", "简体中文", "English"]);
    await select.focus();
    await page.keyboard.press("Space");
    await page.waitForFunction(() => document.querySelector('[data-action="appearance.language"]').matches(":open"));
    await page.keyboard.press("Escape");
    assert.equal(await page.evaluate(() => localStorage.getItem("rx-lang")), language, `${label}: dismissing the picker preserves the language`);
    assert.deepEqual(errors, []);
    await page.close();
    console.log(`PASS select ${label}: centered text, language names and keyboard dismissal`);
  }
} finally {
  await browser.close();
}
