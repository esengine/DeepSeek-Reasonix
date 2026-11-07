// With the icon rail hidden, the sidebar's shortcut buttons are one family: the same colour at rest, so none of
// them reads as heavier than its neighbours, and the search row below the new-session button keeps a quiet ink.
import { chromium } from "playwright";

const BASE = process.env.PERF_URL ?? "http://localhost:4399/perf.html?pref=zh&ws=3&sess=3&turns=2";
const fails = [];
const check = (name, ok, detail = "") => {
  console.log(`${ok ? "  ok" : "FAIL"}  ${name}${detail ? "  — " + detail : ""}`);
  if (!ok) fails.push(name);
};

const browser = await chromium.launch();
try {
  for (const scheme of ["light", "dark"]) {
    const ctx = await browser.newContext({ locale: "zh-CN", colorScheme: scheme, viewport: { width: 1280, height: 900 }, reducedMotion: "reduce" });
    await ctx.addInitScript(() => { try { localStorage.setItem("rx-nav-rail", "off"); } catch {} });
    const page = await ctx.newPage();
    await page.goto(BASE, { waitUntil: "networkidle" });
    await page.waitForSelector(".studio-quicknav > button");
    await page.mouse.move(1270, 890);
    const looks = await page.evaluate(() => [...document.querySelectorAll(".studio-quicknav > button")].map((b) => {
      const c = getComputedStyle(b);
      return `${c.color} / ${c.backgroundColor} / ${c.fontSize}`;
    }));
    const distinct = new Set(looks);
    check(`${scheme}: the shortcut buttons share one ink and fill at rest`, distinct.size === 1, [...distinct].join(" | "));
    const [search, first] = await Promise.all([page.locator(".studio-search").first().boundingBox(), page.locator(".studio-new-task").first().boundingBox()]);
    const gap = search.y - (first.y + first.height);
    check(`${scheme}: the search row follows the new-session button without a blank band`, gap >= 0 && gap <= 10, `${gap.toFixed(1)}px`);
    await ctx.close();
  }
} finally {
  await browser.close();
}
if (fails.length) {
  console.error(`\n${fails.length} failed: ${fails.join("; ")}`);
  process.exit(1);
}
console.log("\nall passed");
