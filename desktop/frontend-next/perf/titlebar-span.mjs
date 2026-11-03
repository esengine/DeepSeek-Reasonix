import { chromium } from "playwright";
import assert from "node:assert/strict";

const browser = await chromium.launch();
try {
  for (const platform of ["darwin", "win32", "linux"]) {
    const page = await browser.newPage({ viewport: { width: 1440, height: 900 }, reducedMotion: "reduce" });
    await page.goto(`${process.env.PERF_URL ?? "http://localhost:4399/perf.html"}?pref=en&ws=2&sess=6&turns=2`);
    await page.waitForSelector(".rail");
    await page.evaluate((platform) => {
      document.documentElement.dataset.platform = platform;
      document.documentElement.dataset.titlebar = "app";
    }, platform);
    await page.evaluate(() => Promise.all(document.getAnimations().map((animation) => animation.finished)));
    const check = async () => {
      const bounds = await page.evaluate(() => {
        const rect = (selector) => {
          const element = document.querySelector(selector);
          if (!element && selector === ".nav" && document.querySelector(".app").dataset.nav === "off") return null;
          const r = element.getBoundingClientRect();
          return { left: r.left, right: r.right, top: r.top, bottom: r.bottom };
        };
        const nav = document.querySelector(".nav");
        return { app: rect(".app"), bar: rect(".chrome"), rail: rect(".rail"), nav: rect(".nav"), crumb: rect(".crumb"),
          navBorder: nav && getComputedStyle(nav).borderRightWidth, navVisible: document.querySelector(".app").dataset.nav === "on" };
      });
      assert.equal(bounds.bar.left, bounds.app.left, `${platform}: titlebar starts at window edge`);
      assert.equal(bounds.bar.right, bounds.app.right, `${platform}: titlebar spans window`);
      assert.ok(bounds.rail.top >= bounds.bar.bottom, `${platform}: sidebar stays below titlebar`);
      if (bounds.nav) {
        assert.ok(bounds.nav.top >= bounds.bar.bottom, `${platform}: navigation stays below titlebar`);
        if (bounds.navVisible) assert.equal(bounds.navBorder, "1px", `${platform}: navigation divider starts at its top edge`);
      }
      if (platform === "darwin") assert.ok(bounds.crumb.left >= 96, "session identity clears traffic lights");
    };
    await check();
    const divider = page.locator('.gutter-l[role="separator"]').first();
    await divider.press("Enter");
    await page.waitForFunction(() => document.querySelector(".app").dataset.rail === "off");
    await check();
    await divider.press("Enter");
    await page.waitForFunction(() => document.querySelector(".app").dataset.rail === "on");
    await check();
    await divider.press("ArrowRight");
    await check();
    await page.setViewportSize({ width: 600, height: 800 });
    await check();
    await page.close();
    console.log(`${platform}: full-width titlebar, sidebar toggle, resize and narrow layout passed`);
  }
} finally {
  await browser.close();
}
