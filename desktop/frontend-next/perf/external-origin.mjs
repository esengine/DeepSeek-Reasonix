// The ask-on-external-text switch has its own settings block under tools, starts off, turns on with one click, and says what it leaves out.
import { chromium } from "playwright";

const BASE = process.env.PERF_URL ?? "http://localhost:4399/perf.html";
const shotDir = process.env.SHOTS;
const fails = [];
const check = (name, ok, detail = "") => {
  console.log(`${ok ? "  ok" : "FAIL"}  ${name}${detail ? "  — " + detail : ""}`);
  if (!ok) fails.push(name);
};

const browser = await chromium.launch();
for (const scheme of ["light", "dark"]) {
  for (const [pref, label] of [["zh", "外部文字"], ["en", "Outside text"]]) {
    for (const width of [1280, 420]) {
      const tag = `${scheme} ${pref} ${width}px`;
      const page = await browser.newPage({ viewport: { width, height: 1000 }, locale: pref === "zh" ? "zh-CN" : "en-US", colorScheme: scheme, reducedMotion: "reduce" });
      page.on("pageerror", (e) => fails.push("页面异常: " + e.message));
      await page.goto(`${BASE}?pref=${pref}&turns=2`, { waitUntil: "networkidle" });
      await page.waitForSelector(".app");
      await page.evaluate(() => document.querySelector('[data-action="chrome.settings"]')?.click());
      await page.waitForSelector('[data-action="settings.section"][data-value="tools"]');
      await page.evaluate(() => document.querySelector('[data-action="settings.section"][data-value="tools"]')?.click());
      const sw = page.getByRole("switch", { name: label });
      await sw.waitFor();
      check(`${tag}: it starts off`, (await sw.getAttribute("aria-checked")) === "false");
      await sw.scrollIntoViewIfNeeded();
      const said = await page.evaluate(() => document.body.textContent + [...document.querySelectorAll("#set-external-origin [title]")].map((el) => el.title).join(" "));
      check(`${tag}: it says what it does not cover and that a headless run refuses`, pref === "zh" ? said.includes("改写过或编码过的文字不在其内") && said.includes("会被拒绝而不是执行") : said.includes("reworded or encoded") && said.includes("refused instead of run"));
      if (shotDir) { await page.mouse.move(width - 3, 995); await page.waitForTimeout(250); await page.locator("#set-external-origin").first().screenshot({ path: `${shotDir}/${scheme}-${pref}-${width}-off.png` }).catch(() => page.screenshot({ path: `${shotDir}/${scheme}-${pref}-${width}-off.png` })); }
      await sw.click();
      await page.waitForFunction((l) => document.querySelector(`[role="switch"][aria-label="${l}"]`)?.getAttribute("aria-checked") === "true", label);
      check(`${tag}: one click turns it on`, true);
      if (shotDir) { await page.mouse.move(width - 3, 995); await page.waitForTimeout(250); await page.locator("#set-external-origin").first().screenshot({ path: `${shotDir}/${scheme}-${pref}-${width}-on.png` }).catch(() => {}); }
      await page.close();
    }
  }
}
await browser.close();
if (fails.length) { console.log(`\n${fails.length} failed`); process.exit(1); }
console.log("\nall passed");
