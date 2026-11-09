// The model list in a provider's settings sits inside a page that already scrolls. A scroller of its own
// nests a second scrollbar in the first, and an alias field that keeps the browser's default look is a
// white box in the dark theme. Both are questions for a real browser.
import { chromium } from "playwright";

const BASE = process.env.PERF_URL ?? "http://localhost:4399/perf.html";
const fails = [];
const check = (name, ok, detail = "") => {
  console.log(`${ok ? "  ok" : "FAIL"}  ${name}${detail ? "  — " + detail : ""}`);
  if (!ok) fails.push(name);
};

const browser = await chromium.launch();
try {
  for (const scheme of ["light", "dark"]) {
    const ctx = await browser.newContext({ locale: "zh-CN", colorScheme: scheme, viewport: { width: 1400, height: 900 } });
    const page = await ctx.newPage();
    await page.goto(BASE, { waitUntil: "networkidle" });
    await page.waitForSelector(".app", { timeout: 20000 });
    await page.getByRole("button", { name: "设置", exact: true }).click();
    await page.locator('[role="tab"][title="模型"]').click();
    await page.locator('[data-action="settings.page-index"][data-target="providers"]').click();
    await page.getByText("myrelay", { exact: true }).first().click();
    await page.getByText("从服务商读取可用模型").first().click();
    await page.waitForSelector(".mrows .mline");
    const read = await page.evaluate(() => {
      const list = document.querySelector(".mrows");
      const alias = document.querySelector(".mline .malias");
      return {
        scrolls: list.scrollHeight > list.clientHeight + 1,
        overflowY: getComputedStyle(list).overflowY,
        aliasBg: getComputedStyle(alias).backgroundColor,
        aliasBorder: getComputedStyle(alias).borderTopLeftRadius,
      };
    });
    check(`${scheme}: the model list has no scrollbar of its own`, !read.scrolls && read.overflowY === "visible", `overflow-y ${read.overflowY}`);
    check(`${scheme}: the alias field is styled like the other fields`, read.aliasBorder !== "0px", read.aliasBorder);
    if (scheme === "dark") check("dark: the alias field is not a white box", read.aliasBg !== "rgb(255, 255, 255)", read.aliasBg);
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
