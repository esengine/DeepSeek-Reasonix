import assert from "node:assert/strict";
import { mkdir } from "node:fs/promises";
import path from "node:path";
import { chromium } from "playwright";

const base = process.env.PERF_URL ?? "http://localhost:4399/perf.html";
const shots = process.env.AGENT_SETTINGS_SHOTS;
if (shots) await mkdir(shots, { recursive: true });
const browser = await chromium.launch();
const errors = [];
try {
  for (const language of ["zh", "en"]) {
    for (const scheme of ["light", "dark"]) {
      for (const width of [390, 736, 1024, 1600]) {
        const page = await browser.newPage({ viewport: { width, height: 900 }, colorScheme: scheme, reducedMotion: "reduce" });
        page.setDefaultTimeout(10000);
        page.on("pageerror", (error) => errors.push(error.message));
        const url = new URL(base);
        url.searchParams.set("pref", language);
        url.searchParams.set("turns", "2");
        await page.goto(url.href, { waitUntil: "networkidle" });
        await page.evaluate(() => document.querySelector('[data-action="chrome.settings"]')?.click());
        await page.locator('[data-action="settings.section"][data-value="session"]').click();
        await page.locator('#set-plan-approval [role="radio"]').first().waitFor();
        await page.evaluate(() => document.fonts.ready);
        assert.equal(await page.locator(".page-index").count(), 0);
        assert.equal(await page.locator(".prefs-col > .prefs-card").count(), 5);
        assert.equal(await page.locator("#set-session-ref").isVisible(), true);
        assert.equal(await page.locator(".prefs-col > details").count(), 0);
        const layout = await page.evaluate(() => {
          const col = document.querySelector(".prefs-col");
          const rows = [...col.querySelectorAll(".srow")].filter((el) => el.checkVisibility());
          const cards = [...col.querySelectorAll(".prefs-card")].filter((el) => el.checkVisibility());
          return {
            columns: getComputedStyle(col).columnCount,
            lefts: cards.map((el) => Math.round(el.getBoundingClientRect().left)),
            clipped: rows.filter((el) => {
              const desc = el.querySelector(".srow-desc");
              return desc && (getComputedStyle(desc).whiteSpace === "nowrap" || desc.scrollWidth > desc.clientWidth + 1);
            }).length,
            overflow: col.scrollWidth - col.clientWidth,
            note: getComputedStyle(document.querySelector("#set-persona .apply")).display,
          };
        });
        assert.notEqual(layout.columns, "2");
        assert.equal(new Set(layout.lefts).size, 1);
        assert.equal(layout.clipped, 0, JSON.stringify(layout));
        assert.ok(layout.overflow <= 1, JSON.stringify(layout));
        assert.notEqual(layout.note, "none");
        if (shots && language === "zh" && [390, 1024].includes(width)) {
          await page.screenshot({ path: path.join(shots, `agent-settings-${width}-${scheme}.png`) });
        }
        assert.ok(await page.locator(".prefs-col").evaluate((el) => el.scrollWidth <= el.clientWidth + 1));
        await page.locator('#set-peers [data-action="settings.open-page"]').click();
        await page.locator('.deep-page[data-setting="peers"]').waitFor({ state: "visible" });
        assert.equal(await page.locator(".page-index").count(), 0);
        await page.locator('[data-action="settings.close-page"]').click();
        await page.locator("#set-plan-approval").waitFor({ state: "visible" });
        await page.locator('[data-action-change="settings.search"]').fill("session-ref");
        await page.locator('.prefs-found [data-target="session-ref"]').click();
        await page.locator("#set-session-ref").waitFor({ state: "visible" });
        await page.locator('[data-action-change="settings.search"]').fill("persona");
        await page.locator('.prefs-found [data-target="persona"]').click();
        await page.locator('.deep-page #set-persona[data-landed]').waitFor({ state: "visible" });
        assert.equal(await page.locator("#set-persona").count(), 1);
        console.log(`PASS agent settings: ${language} ${scheme} ${width}px`);
        await page.close();
      }
    }
  }
  assert.deepEqual(errors, []);
} finally {
  await browser.close();
}
