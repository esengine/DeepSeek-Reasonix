import assert from "node:assert/strict";
import { mkdir } from "node:fs/promises";
import { chromium } from "playwright";

const base = process.env.PERF_URL ?? "http://localhost:4399/perf.html";
const browser = await chromium.launch();
try {
  await mkdir("perf/shots", { recursive: true });
  for (const colorScheme of ["light", "dark"]) for (const width of [1600, 390]) for (const scale of [1, 1.3]) {
    const page = await browser.newPage({ viewport: { width: 1300, height: 1000 }, colorScheme, reducedMotion: "reduce" });
    page.setDefaultTimeout(10000);
    const errors = [];
    page.on("pageerror", (error) => errors.push(error.message));
    await page.goto(`${base}?pref=zh&turns=1`, { waitUntil: "networkidle" });
    await page.locator('[data-action="chrome.settings"]').click();
    await page.locator('.prefs-nav').waitFor();
    await page.setViewportSize({ width, height: 1000 });
    await page.evaluate((size) => document.documentElement.style.setProperty('--fs-scale', String(size)), scale);
    const sections = await page.locator('.prefs-nav [role="tab"]').evaluateAll((tabs) => tabs.map((tab) => tab.dataset.value));
    for (const section of sections) {
      await page.locator(`.prefs-nav [data-value="${section}"]`).click();
      await page.waitForTimeout(200);
      const geometry = await page.locator('.prefs-screen').evaluate((root) => {
        const sheet = root.querySelector('.prefs-sheet').getBoundingClientRect();
        const panel = root.querySelector('.prefs-main');
        const col = panel.querySelector('.prefs-col').getBoundingClientRect();
        const cards = [...panel.querySelectorAll('.prefs-col > .prefs-card')].map((card) => card.getBoundingClientRect());
        const nav = root.querySelector('.prefs-nav').getBoundingClientRect();
        return {
          sheet: { left: sheet.left, right: sheet.right, bottom: sheet.bottom }, nav: nav.width,
          gutters: [col.left - panel.getBoundingClientRect().left, panel.getBoundingClientRect().right - col.right],
          spill: panel.scrollWidth - panel.clientWidth,
          logsFit: [...panel.querySelectorAll('#set-logs .acct-note')].every((note) => note.scrollWidth <= note.clientWidth + 1),
          single: cards.every((card, i) => Math.abs(card.width - col.width) < 1 && (!i || card.top >= cards[i - 1].bottom)),
        };
      });
      const label = `${colorScheme} ${width}px ${scale} ${section}`;
      if (geometry.spill > 1 || !geometry.logsFit) {
        console.log(label, await page.locator('.prefs-main').evaluate((panel) => [...panel.querySelectorAll('*')]
          .filter((node) => node.clientWidth > 1 && node.scrollWidth > node.clientWidth + 1)
          .map((node) => ({ tag: node.tagName, class: node.className, width: node.clientWidth, scroll: node.scrollWidth }))));
        await page.screenshot({ path: `perf/shots/settings-full-failed-${colorScheme}-${width}-${scale}-${section}.png` });
      }
      assert.deepEqual(geometry.sheet, { left: 0, right: width, bottom: 1000 }, label);
      assert.ok(geometry.single, `${label}: full-width cards in one column`);
      assert.ok(geometry.spill <= 1, `${label}: horizontal overflow ${geometry.spill}px`);
      assert.ok(geometry.logsFit, `${label}: log paths wrap within their rows`);
      if (width > 640) {
        assert.equal(geometry.nav, 240, label);
        assert.deepEqual(geometry.gutters, [24, 24], label);
      }
      if (section === 'common' && scale === 1) await page.screenshot({ path: `perf/shots/settings-full-${colorScheme}-${width}.png` });
    }
    assert.deepEqual(errors, []);
    console.log(`PASS full-screen settings ${colorScheme} ${width}px scale ${scale}: ${sections.length} categories`);
    await page.close();
  }
} finally {
  await browser.close();
}
