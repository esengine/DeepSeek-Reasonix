// Two servers on one host and different ports are two connections, each listed under its own address.
import { chromium } from "playwright";

const BASE = process.env.PERF_URL ?? "http://localhost:4399/perf.html";
const fails = [];
const check = (name, ok, detail = "") => {
  console.log(`${ok ? "  ok" : "FAIL"}  ${name}${detail ? "  — " + detail : ""}`);
  if (!ok) fails.push(name);
};
const shotDir = process.env.SHOTS;

const browser = await chromium.launch();
try {
  for (const scheme of ["light", "dark"]) {
    const ctx = await browser.newContext({ locale: "zh-CN", colorScheme: scheme, viewport: { width: 1400, height: 900 }, reducedMotion: "reduce" });
    const page = await ctx.newPage();
    page.on("pageerror", (e) => fails.push("页面异常: " + e.message));
    await page.goto(`${BASE}?twoports=1`, { waitUntil: "networkidle" });
    await page.waitForSelector(".app", { timeout: 20000 });
    await page.getByRole("button", { name: "设置", exact: true }).click();
    await page.locator('[role="tab"][title="模型"]').click();
    await page.locator('[data-action="settings.open-page"][data-target="providers"]').click();
    await page.waitForSelector('[data-action-click="provider.select"]');
    const text = await page.evaluate(() => [...document.querySelectorAll('[data-action-click="provider.select"]')].map((b) => b.textContent ?? ""));
    check(`${scheme}: the server on port 8756 is listed`, text.some((t) => t.includes("127.0.0.1:8756")), text.join(" | "));
    check(`${scheme}: the server on port 8788 is listed beside it`, text.some((t) => t.includes("127.0.0.1:8788")), text.join(" | "));
    await page.waitForTimeout(900);
    if (shotDir) await page.screenshot({ path: `${shotDir}/${scheme}-ports.png` });
    await ctx.close();
  }
} finally {
  await browser.close();
}
if (fails.length) {
  console.log(`\n${fails.length} failed:\n  ` + fails.join("\n  "));
  process.exit(1);
}
console.log("\nall passed");
