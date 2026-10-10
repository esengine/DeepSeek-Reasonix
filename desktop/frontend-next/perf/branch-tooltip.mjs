import { chromium } from "playwright";
import { mkdir } from "node:fs/promises";

const base = process.env.PERF_URL ?? "http://localhost:4399/perf.html";
const shots = process.env.BRANCH_TOOLTIP_SCREENSHOTS;
if (shots) await mkdir(shots, { recursive: true });
const browser = await chromium.launch({ executablePath: process.env.PLAYWRIGHT_CHROMIUM_EXECUTABLE });
const fails = [];
try {
  for (const language of ["en", "zh"]) {
    for (const zoom of [1]) {
      const page = await browser.newPage({ viewport: { width: 390, height: 844 }, reducedMotion: "reduce" });
      page.on("pageerror", (error) => fails.push(error.message));
      await page.goto(`${base}?pref=${language}&turns=2&git=none&zoom=${zoom}`, { waitUntil: "networkidle" });
      const chip = page.locator(".studio-branch[data-norepo]");
      await chip.waitFor();
      await page.evaluate(() => document.fonts.ready);
      const label = language === "en" ? "No Git repository" : "非 Git 仓库";
      const accessible = await chip.and(page.getByRole("img", { name: label, exact: true })).count() === 1;
      const hiddenLabel = await chip.locator(".lb").evaluate((element) => getComputedStyle(element).display === "none");
      const identity = `${language}/390px/zoom=${zoom}/accessible-name`;
      console.log(`${accessible && hiddenLabel ? "ok" : "FAIL"} ${identity}`, { accessible, hiddenLabel });
      if (!accessible || !hiddenLabel) fails.push(identity);
      for (const interaction of ["hover", "focus"]) {
        if (interaction === "hover") await chip.hover();
        else {
          await page.mouse.move(0, 0);
          await chip.focus();
          await page.keyboard.press("Shift+Tab");
          await page.keyboard.press("Tab");
          if (!await chip.evaluate((element) => element === document.activeElement)) {
            fails.push(`${language}/390px/keyboard-focus`);
          }
        }
        await page.waitForTimeout(160);
        const result = await page.locator(".studio-branch-card").evaluate((card) => {
          const box = card.getBoundingClientRect();
          const pane = card.closest(".pane").getBoundingClientRect();
          const ranges = [...card.children].flatMap((child) => {
            const range = document.createRange();
            range.selectNodeContents(child);
            return [...range.getClientRects()];
          });
          return {
            left: box.left, right: box.right, paneLeft: pane.left, paneRight: pane.right,
            viewport: innerWidth, visible: getComputedStyle(card).visibility === "visible",
            wrapped: getComputedStyle(card).whiteSpace === "normal",
            textFits: ranges.every((range) => range.left >= box.left && range.right <= box.right + 0.5),
            scrollFits: card.scrollWidth <= card.clientWidth,
          };
        });
        const name = `${language}/390px/zoom=${zoom}/${interaction}`;
        const okay = result.visible && result.wrapped && result.textFits && result.scrollFits &&
          result.left >= Math.max(0, result.paneLeft) && result.right <= Math.min(result.viewport, result.paneRight);
        console.log(`${okay ? "ok" : "FAIL"} ${name}`, result);
        if (!okay) fails.push(name);
        if (shots) await page.screenshot({ path: `${shots}/${language}-${zoom}-${interaction}.png` });
      }
      await page.close();
    }
  }
} finally {
  await browser.close();
}
console.log(JSON.stringify({ guard: "branch-tooltip", failures: fails }));
if (fails.length) process.exit(1);
