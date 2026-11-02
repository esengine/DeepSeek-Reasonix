import assert from "node:assert/strict";
import { chromium } from "playwright";

const BASE = process.env.PERF_URL ?? "http://localhost:4399/perf.html";
const browser = await chromium.launch();
try {
  const page = await browser.newPage({ viewport: { width: 1600, height: 1100 }, reducedMotion: "reduce" });
  const failures = [];
  page.on("pageerror", (error) => failures.push(error.message));
  for (const locale of ["en", "zh"]) {
    for (const state of ["idle", "downloading", "rollback", "verifying", "ready"]) {
      const url = new URL(BASE);
      url.search = new URLSearchParams({ pref: locale, turns: "2", versionlayout: state === "rollback" ? "downloading" : state }).toString();
      if (state === "rollback") url.searchParams.set("rollback", "1");
      await page.goto(url.href, { waitUntil: "networkidle" });
      await page.locator('[data-action="chrome.settings"]').click();
      await page.locator('.prefs-nav [data-value="versions"]').click();
      await page.waitForSelector(".vrow");
      if (state !== "idle") await page.waitForSelector(".vrow .sa[data-keep]");
      await page.evaluate(() => document.fonts.ready);
      for (const width of [390, 600, 601, 620, 760, 900]) {
        await page.locator(".vers:has(.vlist)").evaluate((el, width) => {
          el.style.width = `${width}px`;
          el.style.maxWidth = "none";
        }, width);
        await page.evaluate(() => new Promise((resolve) => requestAnimationFrame(() => requestAnimationFrame(resolve))));
        for (const open of [false, true]) {
          if (open) {
            await page.locator('.vrow [data-action="versions.notes"]').first().click();
            await page.waitForSelector(".vnotes h1");
          }
          await page.mouse.move(0, 0);
          const errors = await page.locator(".vlist").evaluate((list) => {
            const errors = [];
            const overlap = (a, b) => a.left < b.right - 0.5 && b.left < a.right - 0.5 && a.top < b.bottom - 0.5 && b.top < a.bottom - 0.5;
            for (const row of list.querySelectorAll(".vrow")) {
              const children = [...row.children].filter((el) => {
                const style = getComputedStyle(el);
                return el.matches(".nm, .ds, .vn, .sc, .sa") && style.visibility !== "hidden" && style.opacity !== "0";
              });
              for (const [i, a] of children.entries()) {
                const rect = a.getBoundingClientRect();
                const bounds = row.getBoundingClientRect();
                if (rect.left < bounds.left - 0.5 || rect.right > bounds.right + 0.5) errors.push(`${a.className} outside row`);
                for (const b of children.slice(i + 1)) {
                  if (overlap(rect, b.getBoundingClientRect())) errors.push(`${a.className} overlaps ${b.className}`);
                }
              }
              const notes = row.querySelector(".vn");
              if (notes && list.closest(".vers").getBoundingClientRect().width > 600) {
                const label = row.querySelector(".nm").getBoundingClientRect();
                const button = notes.getBoundingClientRect();
                if (button.top >= label.bottom) errors.push("notes moved off the first line");
              }
            }
            return errors;
          });
          const name = `${locale}/${state}/${width}px/notes=${open}`;
          if (errors.length) failures.push(`${name}: ${errors.join(", ")}`);
          console.log(`${errors.length ? "FAIL" : "ok"} ${name}`);
          if (open) {
            await page.locator('.vrow [aria-expanded="true"]').click();
            await page.waitForSelector(".vnotes", { state: "detached" });
          }
        }
      }
    }
  }
  assert.deepEqual(failures, []);
} finally {
  await browser.close();
}
