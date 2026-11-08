import assert from "node:assert/strict";
import { chromium } from "playwright";

const url = process.env.PERF_URL ?? "http://localhost:4399/perf.html?pref=zh&turns=2";
const browser = await chromium.launch();
try {
  for (const width of [390, 1044]) {
    const page = await browser.newPage({ viewport: { width, height: 884 }, reducedMotion: "reduce" });
    await page.goto(url, { waitUntil: "networkidle" });
    const sidebar = page.getByRole("toolbar", { name: "侧栏入口" });
    for (const name of [/^用量/, /^扩展$/, /^设置$/]) {
      const drawer = page.getByRole("button", { name: "会话列表", exact: true });
      const expand = page.getByRole("button", { name: "展开工作区栏", exact: true });
      if (await drawer.isVisible() && await drawer.getAttribute("aria-pressed") !== "true") await drawer.click();
      else if (await expand.isVisible()) await expand.click();
      await sidebar.getByRole("button", { name }).click();
      await page.getByRole("dialog").waitFor();
      const measure = () => page.evaluate(() => {
        const sheet = document.querySelector(".prefs-sheet");
        const bounds = (selector) => {
          const r = sheet.querySelector(selector).getBoundingClientRect();
          return { left: r.left, top: r.top, right: r.right, bottom: r.bottom };
        };
        const button = bounds(".prefs-actions button");
        const hit = document.elementFromPoint((button.left + button.right) / 2, (button.top + button.bottom) / 2);
        return { solo: sheet.hasAttribute("data-solo"), title: bounds(".prefs-hd h1"), actions: bounds(".prefs-actions"), button, main: bounds(".prefs-main"), hitClose: !!hit?.closest('[data-action="settings.close"]') };
      });
      const check = (r) => {
        assert.ok(r.hitClose, "the close button stays visible and clickable above the content");
        assert.ok(r.actions.top >= r.title.top - 1 && r.actions.bottom <= r.title.bottom + 1, "the close area stays inside the title row");
        if (r.solo) {
          assert.ok(r.title.right >= r.button.right, "the title background covers the close area");
          assert.ok(Math.abs((r.button.top + r.button.bottom - r.title.top - r.title.bottom) / 2) <= 1, "the close button is centred in the title row");
        }
        assert.ok(r.button.bottom <= r.main.top || !r.solo, "the close button stays above the scrolling content");
      };
      const top = await measure();
      check(top);
      await page.locator(".prefs-main").evaluate((el) => { el.scrollTop = el.scrollHeight; });
      const scrolled = await measure();
      check(scrolled);
      assert.deepEqual(scrolled.button, top.button, "scrolling the body leaves the close button in place");
      await page.getByRole("button", { name: "关闭 Esc", exact: true }).click();
      await page.getByRole("dialog").waitFor({ state: "hidden" });
      console.log(`PASS ${name}: header and close button at ${width}px`);
    }
    await page.close();
  }
} finally {
  await browser.close();
}
