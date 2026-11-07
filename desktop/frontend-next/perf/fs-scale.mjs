// Interface text has one size dial, --fs-scale, and the transcript has its own,
// --read. Every piece of text must answer to one of them: text that moves with
// neither is a size written into a stylesheet as a bare pixel count, which the
// text-size setting could never reach.
import { chromium } from "playwright";

const BASE = process.env.PERF_URL ?? "http://localhost:4399/perf.html";
const fails = [];
const check = (name, ok, detail = "") => {
  console.log(`${ok ? "  ok" : "FAIL"}  ${name}${detail ? "  — " + detail : ""}`);
  if (!ok) fails.push(name);
};

const SCENES = [
  { name: "conversation", q: "?pref=zh&ws=2&sess=6&turns=3" },
  { name: "files panel", q: "?pref=zh&ws=2&sess=6&turns=2", act: async (p) => { await p.locator('.navbtn[aria-label="文件"]').first().click(); await p.waitForSelector(".workbench-filelist button"); } },
  { name: "side panel", q: "?pref=zh&turns=2", act: async (p) => { await p.locator('[data-action="side.toggle"]').click(); await p.waitForSelector(".sidepanel"); } },
  { name: "narrow", q: "?pref=zh&ws=2&sess=6&turns=2", w: 390 },
  { name: "settings", q: "?pref=zh&turns=2", tabs: true },
];

const sizes = (page) => page.evaluate(() => {
  window.__texts ??= (() => {
    const out = [];
    const walker = document.createTreeWalker(document.body, NodeFilter.SHOW_ELEMENT);
    let n;
    while ((n = walker.nextNode())) {
      if (![...n.childNodes].some((c) => c.nodeType === 3 && c.textContent.trim())) continue;
      const r = n.getBoundingClientRect();
      if (r.width < 2 || r.height < 2 || !n.checkVisibility({ checkVisibilityCSS: true, contentVisibilityAuto: true })) continue;
      out.push(n);
    }
    return out;
  })();
  return window.__texts.map((el) => parseFloat(getComputedStyle(el).fontSize));
});

const browser = await chromium.launch();
for (const sc of SCENES) {
  const page = await browser.newPage({ viewport: { width: sc.w ?? 1400, height: 900 }, locale: "zh-CN", reducedMotion: "reduce" });
  page.on("pageerror", (e) => fails.push("页面异常: " + e.message));
  await page.addInitScript(() => localStorage.setItem("rx-nav-rail", "on"));
  await page.goto(BASE + sc.q, { waitUntil: "networkidle" });
  await page.waitForSelector(".app");
  await page.evaluate(() => document.fonts.ready);
  const views = [];
  if (sc.tabs) {
    await page.evaluate(() => document.querySelector('[data-action="chrome.settings"]')?.click());
    await page.waitForSelector(".prefs-nav button");
    const n = await page.evaluate(() => document.querySelectorAll(".prefs-nav button").length);
    for (let i = 0; i < n; i++) views.push(async () => { await page.evaluate((k) => document.querySelectorAll(".prefs-nav button")[k].click(), i); });
  } else views.push(async () => { await sc.act?.(page); });
  let total = 0;
  const holes = new Map();
  for (const show of views) {
    await show();
    await page.waitForTimeout(500);
    await page.evaluate(() => { window.__texts = undefined; document.documentElement.style.setProperty("--fs-scale", "1"); });
    const base = await sizes(page);
    await page.evaluate(() => document.documentElement.style.setProperty("--fs-scale", "1.25"));
    await page.waitForTimeout(80);
    const scaled = await sizes(page);
    await page.evaluate(() => {
      const root = document.documentElement.style;
      window.__read = root.getPropertyValue("--read");
      root.setProperty("--fs-scale", "1");
      root.setProperty("--read", `${parseFloat(getComputedStyle(document.documentElement).getPropertyValue("--read")) * 1.25}px`);
    });
    await page.waitForTimeout(80);
    const reading = await sizes(page);
    await page.evaluate(() => {
      const root = document.documentElement.style;
      if (window.__read) root.setProperty("--read", window.__read);
      else root.removeProperty("--read");
    });
    const names = await page.evaluate(() => window.__texts.map((el) => (el.className && typeof el.className === "string" ? el.className.trim().split(/\s+/).slice(0, 2).join(".") : el.tagName.toLowerCase()) + " " + el.textContent.trim().slice(0, 10)));
    base.forEach((b, i) => {
      if (b <= 0) return;
      total++;
      const ok = Math.abs(scaled[i] / b - 1.25) < 0.03 || Math.abs(reading[i] / b - 1.25) < 0.03;
      if (!ok) holes.set(`${names[i]} (${b}px)`, (holes.get(`${names[i]} (${b}px)`) ?? 0) + 1);
    });
  }
  check(`${sc.name}: every piece of text answers to the text size or the reading size`, holes.size === 0, `${total} text nodes${holes.size ? "; stuck: " + [...holes.keys()].slice(0, 6).join(", ") : ""}`);
  await page.close();
}
await browser.close();
if (fails.length) {
  console.log(`\n${fails.length} failed`);
  process.exit(1);
}
