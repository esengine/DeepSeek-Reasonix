import { chromium } from "playwright";

const BASE = process.env.PERF_URL ?? "http://localhost:4399/perf.html";
const fails = [];
const check = (name, ok, detail = "") => {
  console.log(`${ok ? "  ok" : "FAIL"}  ${name}${detail ? "  — " + detail : ""}`);
  if (!ok) fails.push(name);
};
const box = (page, sel) =>
  page.evaluate((s) => {
    const r = document.querySelector(s)?.getBoundingClientRect();
    return r ? { top: Math.round(r.top), bottom: Math.round(innerHeight - r.bottom), height: Math.round(r.height) } : null;
  }, sel);

const browser = await chromium.launch();
for (const [w, h] of [[1600, 1000], [1280, 760], [390, 800]]) {
  const page = await browser.newPage({ viewport: { width: w, height: h }, reducedMotion: "reduce" });
  page.on("pageerror", (e) => fails.push("page error: " + e.message));
  await page.goto(`${BASE}?turns=5&pref=en`, { waitUntil: "networkidle" });
  await page.waitForSelector(".app");
  await page.evaluate(() => document.querySelector('[data-action="chrome.settings"]')?.click());
  await page.waitForSelector(".prefs-sheet");
  await page.waitForTimeout(300);
  const prefs = await box(page, ".prefs-screen");
  await page.keyboard.press("Escape");
  await page.waitForSelector(".prefs-sheet", { state: "detached" });
  await page.evaluate(() => document.querySelector('[data-action="feedback.open"]')?.click());
  await page.waitForSelector(".fbk");
  await page.waitForTimeout(300);
  const fbk = await box(page, ".fbk");
  const tag = `${w}x${h}`;
  if (w > 760) {
    check(`${tag}: settings fills the window`, prefs.top === 0 && prefs.bottom === 0, JSON.stringify(prefs));
    check(`${tag}: feedback sits in the window's vertical middle`, Math.abs(fbk.top - fbk.bottom) <= 2, JSON.stringify(fbk));
  } else {
    check(`${tag}: settings still reaches the bottom of a phone-width window`, prefs.bottom === 0, JSON.stringify(prefs));
  }
  await page.close();
}
await browser.close();
if (fails.length) {
  console.log(`\n${fails.length} failed`);
  process.exit(1);
}
console.log("\nall ok");
