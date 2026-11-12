import { chromium } from "playwright";

const BASE = process.env.PERF_URL ?? "http://localhost:4399/perf.html?pref=zh&ws=1&sess=1&turns=1";
const TOLERANCE = 1;
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
    await page.locator(".studio-tools").hover();
    await page.waitForSelector(".studio-quicknav > button");
    const strip = await page.evaluate(() => [...document.querySelectorAll(".studio-quicknav > button")].map((b) => {
      const r = b.getBoundingClientRect();
      const svg = b.querySelector("svg, .studio-icon").getBoundingClientRect();
      return { w: r.width, h: r.height, left: r.left, cy: r.y + r.height / 2, icon: [svg.width, svg.height], iconCy: svg.y + svg.height / 2 };
    }));
    const spread = (key) => Math.max(...strip.map((s) => s[key])) - Math.min(...strip.map((s) => s[key]));
    check(`${scheme}: the shortcut card has several entries`, strip.length >= 5, `${strip.length}`);
    check(`${scheme}: the shortcut buttons share one size`, spread("w") <= TOLERANCE && spread("h") <= TOLERANCE, JSON.stringify(strip.map((s) => [s.w, s.h])));
    check(`${scheme}: the shortcut rows align and center their icons`, spread("left") <= TOLERANCE && strip.every((s) => Math.abs(s.cy - s.iconCy) <= TOLERANCE), JSON.stringify(strip.map((s) => [s.left, s.cy - s.iconCy])));
    check(`${scheme}: the shortcut icons share one size`, new Set(strip.map((s) => s.icon.join("x"))).size === 1, strip.map((s) => s.icon.join("x")).join(" "));
    const rows = await page.evaluate(() => [".studio-new-task", ".studio-search", ".studio-tools-row"].map((s) => {
      const row = document.querySelector(s);
      if (!row) return null;
      const r = row.getBoundingClientRect();
      const svg = row.querySelector("svg, .studio-icon").getBoundingClientRect();
      return { left: r.left, right: r.right, icon: svg.left - r.left, visible: r.width > 0 && r.height > 0 };
    }));
    check(`${scheme}: new session, search and tools are drawn`, rows.every((r) => r?.visible), JSON.stringify(rows));
    check(`${scheme}: those rows share the sidebar's left and right edges`, rows.every((r) => r && Math.abs(r.left - rows[0].left) <= TOLERANCE && Math.abs(r.right - rows[0].right) <= TOLERANCE), JSON.stringify(rows.map((r) => [r.left, r.right])));
    check(`${scheme}: the retired feedback row is not drawn`, !(await page.locator(".studio-feedback").count()));
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
