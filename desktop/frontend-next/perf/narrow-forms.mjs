// The connection form is the widest thing in settings. At phone widths its headings stay on one line, rows that cannot fit
// wrap instead of overlapping, and nothing passes the right edge of the window.
import { chromium } from "playwright";

const BASE = process.env.PERF_URL ?? "http://localhost:4399/perf.html";
const fails = [];
const check = (name, ok, detail = "") => {
  console.log(`${ok ? "  ok" : "FAIL"}  ${name}${detail ? "  — " + detail : ""}`);
  if (!ok) fails.push(name);
};

const browser = await chromium.launch();
for (const scheme of ["light", "dark"]) {
  for (const width of [360, 390, 430]) {
    const ctx = await browser.newContext({ locale: "zh-CN", viewport: { width, height: 844 }, colorScheme: scheme, reducedMotion: "reduce" });
    const page = await ctx.newPage();
    page.setDefaultTimeout(8000);
    page.on("pageerror", (e) => fails.push("page error: " + e.message));
    await page.goto(BASE, { waitUntil: "networkidle" });
    await page.waitForSelector(".app");
    await page.waitForTimeout(600);
    await page.evaluate(() => document.querySelector('[data-action="chrome.settings"]')?.click());
    await page.waitForTimeout(500);
    await page.evaluate(() => [...document.querySelectorAll("button")].find((x) => /备份原文件并修复/.test(x.textContent))?.click());
    await page.waitForTimeout(400);
    await page.evaluate(() => document.querySelector('[role="tab"][title="模型"]')?.click());
    await page.evaluate(() => document.querySelector('[data-action="settings.open-page"][data-target="providers"]')?.click());
    await page.waitForSelector(".pdetail", { state: "attached" });
    await page.evaluate(() => [...document.querySelectorAll("button,li,a")].find((x) => /myrelay-work/.test(x.textContent) && x.textContent.length < 80)?.click());
    await page.waitForSelector(".pdetail .mlhead", { state: "attached" });
    const m = await page.evaluate(() => {
      const live = (el) => {
        const r = el.getBoundingClientRect();
        return r.width > 0 && r.height > 0 && getComputedStyle(el).visibility !== "hidden" ? r : null;
      };
      const head = document.querySelector(".pdetail .mlhead");
      const kids = [...head.children].map((el) => [el, live(el)]).filter(([, r]) => r);
      const overlaps = [];
      for (let i = 0; i < kids.length; i++)
        for (let j = i + 1; j < kids.length; j++) {
          const a = kids[i][1], b = kids[j][1];
          if (Math.min(a.right, b.right) - Math.max(a.left, b.left) > 1 && Math.min(a.bottom, b.bottom) - Math.max(a.top, b.top) > 1) overlaps.push(i + "/" + j);
        }
      const ttl = head.querySelector(".ttl");
      const lineH = parseFloat(getComputedStyle(ttl).lineHeight) || parseFloat(getComputedStyle(ttl).fontSize) * 1.4;
      const out = [...document.querySelectorAll(".pdetail *")].filter((el) => live(el) && live(el).right > innerWidth + 1 && !el.closest("[hidden]")).map((el) => String(el.className).split(" ")[0] || el.tagName).slice(0, 4);
      return { ttlLines: Math.round(ttl.getBoundingClientRect().height / lineH), overlaps, out, hscroll: document.documentElement.scrollWidth > innerWidth + 1 };
    });
    check(`${scheme} ${width}px: the model list heading stays on one line`, m.ttlLines === 1, JSON.stringify(m));
    check(`${scheme} ${width}px: the model list header wraps instead of overlapping`, m.overlaps.length === 0, m.overlaps.join(", "));
    check(`${scheme} ${width}px: nothing in the connection form passes the window edge`, m.out.length === 0 && !m.hscroll, m.out.join(", "));
    await ctx.close();
  }
}
await browser.close();
if (fails.length) {
  console.log(`\n${fails.length} failed`);
  process.exit(1);
}
console.log("\nall ok");
