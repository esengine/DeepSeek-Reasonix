import assert from "node:assert/strict";
import { mkdir } from "node:fs/promises";
import { chromium } from "playwright";

const base = process.env.PERF_URL ?? "http://localhost:4399/perf.html";
const shots = process.env.SURFACE_SCREENSHOTS;
if (shots) await mkdir(shots, { recursive: true });
const browser = await chromium.launch();
const errors = [];
const surfaces = ["nav", "rail", "chrome", "side", "compose"];
const opacity = {
  dark: { nav: 0.45, rail: 0.46, chrome: 0.8, side: 0.81, compose: 0.88 },
  light: { nav: 0.6, rail: 0.61, chrome: 0.86, side: 0.87, compose: 0.89 },
};
try {
  for (const scheme of ["light", "dark"]) for (const width of [1600, 390]) {
    const context = await browser.newContext({ viewport: { width, height: width === 390 ? 844 : 1000 }, reducedMotion: "reduce" });
    await context.addInitScript((scheme) => localStorage.setItem("rx-theme", scheme), scheme);
    const page = await context.newPage();
    page.on("pageerror", (error) => errors.push(error.message));
    await page.route("**/themes/bench-photo/background", (route) => route.fulfill({ contentType: "image/svg+xml", body: '<svg xmlns="http://www.w3.org/2000/svg" width="1600" height="1000"><rect width="1600" height="1000" fill="#638c99"/><circle cx="1100" cy="400" r="400" fill="#ddbf88"/></svg>' }));
    await page.goto(`${base}?pack=photo&ws=1&sess=2`, { waitUntil: "networkidle" });
    await page.waitForSelector(".compose");
    await page.evaluate(() => document.fonts.ready);
    await page.addStyleTag({ content: "* { transition: none !important; animation: none !important; caret-color: transparent !important }" });
    // Details no longer mount .side in the dock layout; phone omits .nav.
    // Exercise their actual stylesheet selectors without changing visible UI.
    await page.evaluate((surfaces) => {
      for (const name of surfaces) if (!document.querySelector(`.${name}`)) {
        const probe = document.createElement("aside");
        probe.className = name;
        probe.style.cssText = "position:fixed;left:-9999px;width:10px;height:10px";
        document.body.append(probe);
      }
    }, surfaces);
    const styles = () => page.evaluate((surfaces) => Object.fromEntries(surfaces.map((name) => {
      const style = getComputedStyle(document.querySelector(`.${name}`));
      const canvas = document.createElement("canvas");
      canvas.width = canvas.height = 1;
      const ctx = canvas.getContext("2d");
      ctx.fillStyle = style.backgroundColor;
      ctx.fillRect(0, 0, 1, 1);
      return [name, { alpha: ctx.getImageData(0, 0, 1, 1).data[3] / 255, background: style.backgroundColor, blur: style.backdropFilter, shadow: style.textShadow }];
    })), surfaces);
    const paint = async (surfaceOpacity, backdrop = "photo") => page.evaluate(async ({ surfaceOpacity, backdrop, scheme }) => {
      const { apply } = await import("/src/ui/theme.ts");
      const tokens = {
        light: { bg: "#F2F6F8", bgSoft: "#F9FCFD", panel: "#FFFFFF", border: "#CBD9E0", fg: "#12191C", fgDim: "#4E5D65", accent: "#0E6E82" },
        dark: { bg: "#080D10", bgSoft: "#0C1316", panel: "#131C21", border: "#23333A", fg: "#E4EEF2", fgDim: "#8298A2", accent: "#4FB6CE" },
      };
      apply({ id: "bench-photo", name: "Surface fixture", tokens, background: { image: backdrop === "photo", focusX: 0.5, focusY: 0.5, homeOpacity: 0.9, taskOpacity: 0.5, overlayStrength: 0.4, surfaceOpacity }, ...(backdrop === "sky" ? { sky: { rayAlpha: 0.85, cloudAlpha: 0.4 } } : {}) }, scheme);
    }, { surfaceOpacity, backdrop, scheme });
    const baseline = await styles();
    await paint({});
    assert.deepEqual(await styles(), baseline, "omission keeps every computed paint exactly");
    const empty = await page.screenshot({ animations: "disabled" });
    await paint(undefined);
    assert.deepEqual(await page.screenshot({ animations: "disabled" }), empty, "omission keeps pixels exactly");
    await paint(opacity);
    const custom = await styles();
    for (const name of surfaces) {
      assert.ok(Math.abs(custom[name].alpha - opacity[scheme][name]) <= 1 / 255, `${scheme} ${name}: ${custom[name].background}`);
      assert.equal(custom[name].blur, baseline[name].blur);
      assert.equal(custom[name].shadow, baseline[name].shadow);
    }
    if (shots) await page.screenshot({ path: `${shots}/${scheme}-${width}-after.png`, animations: "disabled" });
    await paint(opacity, "sky");
    for (const [name, value] of Object.entries(await styles())) assert.ok(Math.abs(value.alpha - opacity[scheme][name]) <= 1 / 255, `sky ${name}`);
    await paint(opacity, "none");
    const withoutBackdrop = await styles();
    await paint(undefined, "none");
    assert.deepEqual(await styles(), withoutBackdrop, "data-bg off ignores surface map");
    await paint({ [scheme]: { nav: 0, compose: 1 } });
    const boundaries = await styles();
    assert.equal(boundaries.nav.alpha, 0);
    assert.equal(boundaries.compose.alpha, 1);
    for (const name of ["rail", "chrome", "side"]) assert.equal(boundaries[name].background, baseline[name].background, `omitted ${name}`);
    await paint(undefined);
    assert.deepEqual(await styles(), baseline, "switching to an old pack clears custom paint");
    await page.evaluate(async () => (await import("/src/ui/theme.ts")).apply(null, "light"));
    assert.equal(await page.locator("#pack-theme").count(), 0);
    assert.equal(await page.evaluate(() => document.documentElement.dataset.bg), undefined);
    console.log(`ok ${scheme} ${width}: five computed alphas; exact omitted pixels; sky, backdrop removal, boundaries and pack clearing`);
    await context.close();
  }
  assert.deepEqual(errors, []);
} finally {
  await browser.close();
}
