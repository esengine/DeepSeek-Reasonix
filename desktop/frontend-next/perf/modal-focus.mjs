// A dialog that declares itself modal keeps the keyboard inside it: Tab from the last control goes back to the first, and
// never reaches the sidebar, the transcript or the composer the sheet is covering.
import { chromium } from "playwright";

const BASE = process.env.PERF_URL ?? "http://localhost:4399/perf.html";
const fails = [];
const check = (name, ok, detail = "") => {
  console.log(`${ok ? "  ok" : "FAIL"}  ${name}${detail ? "  — " + detail : ""}`);
  if (!ok) fails.push(name);
};

const browser = await chromium.launch();
for (const scheme of ["light", "dark"]) {
  const ctx = await browser.newContext({ locale: "zh-CN", viewport: { width: 1280, height: 900 }, colorScheme: scheme });
  const page = await ctx.newPage();
  page.setDefaultTimeout(8000);
  page.on("pageerror", (e) => fails.push("page error: " + e.message));
  await page.goto(BASE, { waitUntil: "networkidle" });
  await page.waitForSelector(".app");
  await page.waitForTimeout(600);

  const outside = async (selector, presses) => {
    const out = [];
    for (let i = 0; i < presses; i++) {
      await page.keyboard.press("Tab");
      const where = await page.evaluate((sel) => {
        const el = document.activeElement;
        return el && el !== document.body && !el.closest(sel) ? (el.getAttribute("aria-label") || el.textContent || el.tagName).trim().slice(0, 30) : "";
      }, selector);
      if (where) out.push(where);
    }
    return out;
  };

  await page.evaluate(() => document.querySelector('[data-action="chrome.settings"]')?.click());
  await page.waitForSelector(".prefs-sheet");
  await page.waitForTimeout(400);
  await page.evaluate(() => [...document.querySelectorAll("button")].find((x) => /备份原文件并修复/.test(x.textContent))?.click());
  await page.waitForTimeout(300);
  await page.evaluate(() => document.activeElement?.blur());
  const settingsOut = await outside(".prefs-screen", 60);
  check(`${scheme}: Tab never leaves the full-screen settings surface`, settingsOut.length === 0, settingsOut.slice(0, 3).join("; "));
  await page.keyboard.press("Escape");
  await page.waitForTimeout(400);

  await page.evaluate(() => document.querySelector('[data-action="workspace.search"]')?.click());
  await page.waitForSelector(".palette");
  await page.waitForTimeout(300);
  const paletteOut = await outside(".palette", 30);
  check(`${scheme}: Tab never leaves the command palette`, paletteOut.length === 0, paletteOut.slice(0, 3).join("; "));
  await ctx.close();
}
await browser.close();
if (fails.length) {
  console.log(`\n${fails.length} failed`);
  process.exit(1);
}
console.log("\nall ok");
