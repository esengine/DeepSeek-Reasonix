import assert from "node:assert/strict";
import { mkdir } from "node:fs/promises";
import { chromium } from "playwright";

const base = new URL(process.env.PERF_URL ?? "http://localhost:4399/perf.html");
const browser = await chromium.launch();
try {
  await mkdir("perf/shots", { recursive: true });
  for (const colorScheme of ["light", "dark"]) for (const pack of ["default", "sky", "photo"]) {
    const page = await browser.newPage({ viewport: { width: 1300, height: 900 }, colorScheme, reducedMotion: "reduce" });
    await page.addInitScript(() => { localStorage.setItem("rx-lang", "zh"); localStorage.setItem("rx-nav-rail", "on"); });
    const url = new URL(base);
    for (const [key, value] of Object.entries({ pref: "zh", ws: "2", sess: "6", turns: "1" })) url.searchParams.set(key, value);
    if (pack !== "default") url.searchParams.set("pack", pack);
    await page.goto(url.href, { waitUntil: "networkidle" });
    const current = page.locator('.navbtn[aria-current="page"]');
    await current.waitFor();
    const label = `${colorScheme}/${pack}`;
    await page.locator(".nav").screenshot({ path: `perf/shots/nav-selection-${colorScheme}-${pack}.png` });
    const colors = await current.evaluate((button) => {
      const ink = document.createElement("canvas").getContext("2d", { willReadFrequently: true });
      const rgb = (color) => {
        ink.clearRect(0, 0, 1, 1); ink.fillStyle = color; ink.fillRect(0, 0, 1, 1);
        return [...ink.getImageData(0, 0, 1, 1).data];
      };
      const style = getComputedStyle(button);
      const bg = rgb(style.backgroundColor);
      const fg = rgb(style.color);
      const over = (top, bottom) => {
        const a = top[3] / 255, b = bottom[3] / 255, opacity = a + b * (1 - a);
        return opacity ? top.slice(0, 3).map((v, i) => (v * a + bottom[i] * b * (1 - a)) / opacity).concat(opacity * 255) : top;
      };
      let surface = [0, 0, 0, 0];
      for (let element = button; element; element = element.parentElement) surface = over(surface, rgb(getComputedStyle(element).backgroundColor));
      const anchor = document.createElement("span");
      anchor.style.backgroundColor = "var(--accent)";
      button.append(anchor);
      const accent = rgb(getComputedStyle(anchor).backgroundColor);
      anchor.remove();
      const luminance = (color) => color.slice(0, 3).map((v) => {
        const s = v / 255; return s <= .04045 ? s / 12.92 : ((s + .055) / 1.055) ** 2.4;
      }).reduce((sum, v, i) => sum + v * [.2126, .7152, .0722][i], 0);
      const [a, b] = [luminance(over(fg, surface)), luminance(surface)].sort((x, y) => y - x);
      return { bg, accent, contrast: (a + .05) / (b + .05), marker: getComputedStyle(button, "::before").width };
    });
    assert.notDeepEqual(colors.bg, colors.accent, `${label}: navigation uses a quiet selection surface`);
    assert.ok(colors.contrast >= 3, `${label}: the selected icon stays legible (${JSON.stringify(colors)})`);
    assert.ok(parseFloat(colors.marker) >= 2, `${label}: a side marker also identifies the current section`);
    await current.focus();
    assert.ok(await current.evaluate((button) => parseFloat(getComputedStyle(button).outlineWidth) >= 2), `${label}: keyboard focus stays visible`);
    assert.equal(await current.getAttribute("aria-label"), "会话");
    await page.close();
    console.log(`PASS navigation selection ${label}: quiet surface, contrast and focus`);
  }
} finally {
  await browser.close();
}
