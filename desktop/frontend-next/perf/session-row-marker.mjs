// A running, halted or finished session row marks its state once, in the slot at its left edge. The row
// draws a bar there (::before) and also owns a status dot (.pip); showing both puts two shapes on top of
// each other. Guard: for every state, at most one of the two is rendered, in light and dark.
import { chromium } from "playwright";

const BASE = process.env.PERF_URL ?? "http://localhost:4399/perf.html";
const STATES = ["running", "halt", "done"];
const browser = await chromium.launch();
const fails = [];
for (const theme of ["light", "dark"]) {
  const page = await (await browser.newContext({ locale: "zh-CN", viewport: { width: 1280, height: 800 }, colorScheme: theme })).newPage();
  await page.goto(BASE, { waitUntil: "networkidle" });
  await page.getByText("稍后再说").first().click().catch(() => {});
  await page.waitForSelector('.rail .sessrow[aria-selected="true"]');
  for (const state of STATES) {
    const seen = await page.evaluate((s) => {
      const row = document.querySelector('.rail .sessrow[aria-selected="true"]');
      row.setAttribute("data-run", s);
      const pip = row.querySelector(":scope > .ds-lead > .ds-dot");
      const bar = getComputedStyle(row, "::before");
      return {
        dot: !!pip && getComputedStyle(pip).display !== "none",
        bar: bar.content !== "none" && bar.display !== "none",
      };
    }, state);
    if (seen.dot && seen.bar) fails.push(`${theme} ${state}: the status dot and the bar are both drawn at the row's left edge`);
    if (!seen.dot && !seen.bar) fails.push(`${theme} ${state}: the row shows no state marker at all`);
  }
  await page.close();
}
await browser.close();
if (fails.length) {
  console.error(fails.join("\n"));
  process.exit(1);
}
console.log("one state marker per row, in light and dark, for", STATES.join(", "));
