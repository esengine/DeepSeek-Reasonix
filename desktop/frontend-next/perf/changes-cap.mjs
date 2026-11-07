// A workspace with a hundred thousand changed files keeps the page light: the Git panel draws a page of rows and
// says how many it left out, and the rest comes on request.
import { chromium } from "playwright";

const BASE = process.env.PERF_URL ?? "http://localhost:4399/perf.html";
const CHANGES = Number(process.env.PERF_CHANGES ?? 100000);
const NODES_MAX = Number(process.env.PERF_NODES_MAX ?? 20000);
const shotDir = process.env.SHOTS;
const fails = [];
const check = (name, ok, detail = "") => {
  console.log(`${ok ? "  ok" : "FAIL"}  ${name}${detail ? "  — " + detail : ""}`);
  if (!ok) fails.push(name);
};

const browser = await chromium.launch();
try {
  for (const scheme of ["light", "dark"]) {
    const ctx = await browser.newContext({ locale: "zh-CN", colorScheme: scheme, viewport: { width: 1400, height: 900 }, reducedMotion: "reduce" });
    const page = await ctx.newPage();
    page.on("pageerror", (e) => fails.push("page error: " + e.message));
    await ctx.addInitScript(() => localStorage.setItem("rx-nav-rail", "on"));
    await page.goto(`${BASE}?pref=zh&ws=2&sess=3&turns=2&changes=${CHANGES}`, { waitUntil: "networkidle" });
    await page.waitForSelector(".app");
    await page.locator('.navbtn[aria-label="文件"]').first().click();
    await page.waitForSelector(".workbench-changes");
    const nodes = await page.evaluate(() => document.getElementsByTagName("*").length);
    const rows = await page.locator(".workbench-change-row").count();
    check(`${scheme}: ${CHANGES} changes leave the page under ${NODES_MAX} nodes`, nodes < NODES_MAX, `${nodes} nodes, ${rows} rows`);
    check(`${scheme}: the list says how many it left out`, (await page.locator(".workbench-change-more").textContent())?.includes("没有显示") === true);
    check(`${scheme}: the count in the head is every change`, (await page.locator(".workbench-changes .workbench-explorer-head small").first().textContent())?.replace(/\D/g, "") === String(CHANGES));
    await page.locator(".workbench-change-more").scrollIntoViewIfNeeded();
    if (shotDir) await page.screenshot({ path: `${shotDir}/${scheme}-changes-cap.png` });
    await page.locator('[data-action="workbench.more-changes"]').click();
    check(`${scheme}: more rows come on request`, (await page.locator(".workbench-change-row").count()) > rows);
    await ctx.close();
  }
} finally {
  await browser.close();
}
if (fails.length) {
  console.error("\n失败:", fails.join("; "));
  process.exit(1);
}
