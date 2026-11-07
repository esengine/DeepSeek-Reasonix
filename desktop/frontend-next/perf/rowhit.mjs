// A project row swaps its count for its actions while the row is hovered.
// A faded element still takes the pointer, so a count left clickable sits over the middle of the buttons and only
// their corners answer. Hit-testing is a layout question, so it is asked of a real browser.
import { chromium } from "playwright";

const BASE = process.env.PERF_URL ?? "http://localhost:4399/perf.html";
const fails = [];
const check = (name, ok, detail = "") => {
  console.log(`${ok ? "  ok" : "FAIL"}  ${name}${detail ? "  — " + detail : ""}`);
  if (!ok) fails.push(name);
};

const browser = await chromium.launch();
try {
  const page = await (await browser.newContext({ locale: "zh-CN", viewport: { width: 1600, height: 900 } })).newPage();
  await page.goto(BASE, { waitUntil: "networkidle" });
  await page.waitForSelector(".app", { timeout: 20000 });
  for (const action of ["session.new", "workspace.menu"]) {
    const row = page.locator(".ds-proj").first();
    const button = row.locator(`[data-action="${action}"]`).first();
    await row.scrollIntoViewIfNeeded();
    const rowBox = await row.boundingBox();
    await page.mouse.move(rowBox.x + rowBox.width / 2, rowBox.y + rowBox.height / 2);
    await page.waitForTimeout(250);
    const box = await button.boundingBox();
    await page.mouse.move(box.x + 3, box.y + box.height - 3);
    await page.waitForTimeout(250);
    const misses = await button.evaluate((el) => {
      const r = el.getBoundingClientRect();
      const out = [];
      for (const [fx, fy] of [[0.5, 0.5], [0.25, 0.25], [0.75, 0.25], [0.25, 0.75], [0.75, 0.75]]) {
        const hit = document.elementFromPoint(r.left + r.width * fx, r.top + r.height * fy);
        if (hit !== el && !el.contains(hit)) out.push(`${fx},${fy}:${hit?.className || hit?.tagName}`);
      }
      return out;
    });
    check(`${action}: every part of the button takes the pointer on a hovered row`, misses.length === 0, misses.join(" "));
  }
} finally {
  await browser.close();
}
if (fails.length) {
  console.error(`\n${fails.length} failed: ${fails.join("; ")}`);
  process.exit(1);
}
console.log("\nall passed");
