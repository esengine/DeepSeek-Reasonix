import assert from "node:assert/strict";
import { mkdirSync } from "node:fs";
import { chromium } from "playwright";

const url = process.env.PERF_URL ?? "http://localhost:4399/perf.html";
const shots = process.env.SCREENSHOT_DIR;
if (shots) mkdirSync(shots, { recursive: true });
const browser = await chromium.launch();
const failures = [];
try {
  for (const theme of ["light", "dark"]) {
    for (const width of [1440, 390]) {
      const context = await browser.newContext({ locale: "en-US", viewport: { width, height: 900 }, permissions: ["clipboard-read", "clipboard-write"] });
      const page = await context.newPage();
      try {
        await page.goto(url);
        await page.waitForFunction(() => typeof window.__feed === "function" && window.__panes() > 0);
        await page.evaluate((theme) => {
          document.documentElement.dataset.theme = theme;
          window.__feed({ kind: "turn_started" });
          window.__feed({ kind: "text", text: "First chosen sentence last." });
          window.__feed({ kind: "message" });
          window.__feed({ kind: "turn_done" });
        }, theme);
        const answer = page.locator('.call[data-k="say"] .txt p');
        await answer.waitFor();
        const open = async () => {
          const point = await answer.evaluate((p) => {
            const range = document.createRange();
            range.setStart(p.firstChild, 6);
            range.setEnd(p.firstChild, 21);
            getSelection().removeAllRanges();
            getSelection().addRange(range);
            const rect = range.getBoundingClientRect();
            return { x: rect.x + rect.width / 2, y: rect.y + rect.height / 2 };
          });
          await page.mouse.click(point.x, point.y, { button: "right" });
          await page.getByRole("menuitem", { name: "Quote selected text", exact: true }).waitFor();
        };
        const typography = () => page.locator('.acts-pop > button').evaluateAll((buttons) => buttons.map((button) => {
          const style = getComputedStyle(button);
          return { font: style.font, tracking: style.letterSpacing, transform: style.textTransform };
        }));
        await open();
        await page.evaluate(() => document.fonts.ready);
        await page.locator(".acts-pop").evaluate((menu) => Promise.all(menu.getAnimations().map((animation) => animation.finished)));
        if (shots) await page.screenshot({ path: `${shots}/${theme}-${width}.png` });
        const [quoteStyle, copyStyle] = await typography();
        assert.deepEqual(copyStyle, quoteStyle, "copy and quote menu typography differ");
        await page.keyboard.press("ArrowDown");
        const copy = page.getByRole("menuitem", { name: "Copy selected text", exact: true });
        assert.equal(await copy.evaluate((button) => button === document.activeElement), true);
        await page.keyboard.press("Enter");
        await page.getByText("Copied", { exact: true }).waitFor();
        assert.equal(await page.evaluate(() => navigator.clipboard.readText()), "chosen sentence");
        assert.deepEqual((await typography())[1], quoteStyle, "copy feedback changes typography");
        await page.keyboard.press("Escape");
        assert.equal(await page.getByRole("menu").count(), 0);
        await open();
        await page.getByRole("menuitem", { name: "Quote selected text", exact: true }).click();
        await page.locator('.compose .nm[title="chosen sentence"]').waitFor();
        assert.equal(await page.getByRole("menu").count(), 0);
        console.log(`PASS ${theme} ${width}: typography, copy, feedback, keyboard, dismissal, quote`);
      } catch (error) {
        failures.push(`${theme} ${width}: ${error.message}`);
        console.error(failures.at(-1));
      } finally {
        await context.close();
      }
    }
  }
} finally {
  await browser.close();
}
assert.deepEqual(failures, []);
