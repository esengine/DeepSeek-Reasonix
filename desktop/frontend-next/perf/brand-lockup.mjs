// The product mark in the sidebar's top-left is one row: the wordmark and an outlined uppercase badge, vertically centred on
// each other. Where the badge was hidden before it stays hidden.
import { chromium } from "playwright";

const BASE = process.env.PERF_URL ?? "http://localhost:4399/perf.html";
const fails = [];
const check = (name, ok, detail = "") => {
  console.log(`${ok ? "  ok" : "FAIL"}  ${name}${detail ? "  — " + detail : ""}`);
  if (!ok) fails.push(name);
};

const browser = await chromium.launch();
let seen = null;
for (const scheme of ["light", "dark"]) {
  for (const width of [1280, 700]) {
    const ctx = await browser.newContext({ locale: "zh-CN", viewport: { width, height: 800 }, colorScheme: scheme });
    const page = await ctx.newPage();
    page.on("pageerror", (e) => fails.push("page error: " + e.message));
    await page.goto(BASE, { waitUntil: "networkidle" });
    await page.waitForSelector(".studio-brand-lockup");
    await page.waitForTimeout(500);
    const m = await page.evaluate(() => {
      const logo = document.querySelector(".studio-brand-logo").getBoundingClientRect();
      const sub = document.querySelector(".studio-brand-sub");
      const r = sub.getBoundingClientRect();
      const cs = getComputedStyle(sub);
      return {
        shown: r.width > 0,
        left: r.left,
        top: r.top,
        dy: Math.abs(logo.top + logo.height / 2 - (r.top + r.height / 2)),
        after: r.left >= logo.right - 1,
        fill: cs.backgroundColor, ink: cs.color, border: cs.borderTopWidth, radius: cs.borderTopLeftRadius, upper: cs.textTransform, text: sub.textContent,
      };
    });
    const tag = `${scheme} ${width}px`;
    if (width >= 760) {
      check(`${tag}: the badge is on the wordmark's row, within the 3px optical drop the sidebar board draws`, m.shown && m.dy <= 3.5 && m.after, JSON.stringify(m));
      check(`${tag}: it is an outlined uppercase badge in both themes and the text is untouched`, m.fill === "rgba(0, 0, 0, 0)" && m.border !== "0px" && m.radius !== "0px" && m.upper === "uppercase" && m.text === "studio", JSON.stringify(m));
      if (!seen) seen = { left: m.left, top: m.top };
      check(`${tag}: it sits in the same place whichever theme is on`, Math.abs(m.left - seen.left) <= 0.5 && Math.abs(m.top - seen.top) <= 0.5, `${m.left},${m.top} vs ${seen.left},${seen.top}`);
    } else {
      check(`${tag}: the badge stays hidden on a narrow window`, !m.shown);
    }
    await ctx.close();
  }
}
await browser.close();
if (fails.length) {
  console.log(`\n${fails.length} failed`);
  process.exit(1);
}
console.log("\nall ok");
