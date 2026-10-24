// With the icon rail hidden, the sidebar's own shortcut rows are one family: the same size, weight and colour,
// so none of them reads as bigger or bolder than its neighbours. The new-session button and the feedback row are
// emphasis by design and are not part of the family.
import { chromium } from "playwright";

const BASE = process.env.PERF_URL ?? "http://localhost:4399/perf.html?pref=zh&ws=3&sess=3&turns=2";
const FAMILY = ["搜索与快捷操作", "文件", "工具与权限", "扩展", "记忆", "远程", "钱包与用量"];
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
    await page.waitForSelector(".app");
    const looks = {};
    for (const name of FAMILY) {
      const el = page.getByText(name, { exact: true }).first();
      await el.waitFor();
      looks[name] = await el.evaluate((e) => { const c = getComputedStyle(e); return `${c.fontSize} / ${c.fontWeight} / ${c.color}`; });
    }
    const distinct = new Set(Object.values(looks));
    check(`${scheme}: the sidebar's shortcut rows share one size, weight and colour`, distinct.size === 1,
      distinct.size === 1 ? [...distinct][0] : Object.entries(looks).map(([k, v]) => `${k}: ${v}`).join("; "));
    const [search, first] = await Promise.all([page.locator(".studio-search").first().boundingBox(), page.locator(".studio-quicknav > button").first().boundingBox()]);
    const gap = first.y - (search.y + search.height);
    check(`${scheme}: the shortcut rows follow the search row without a blank band`, gap <= 4, `${gap.toFixed(1)}px`);
    await ctx.close();
  }
} finally {
  await browser.close();
}
if (fails.length) {
  console.error(`\n${fails.length} failed: ${fails.join("; ")}`);
  process.exit(1);
}
