// The explorer in the workbench is read at arm's length, next to a conversation set in a 14px reading size.
// Its header, search field, file rows and the icons beside the count must stay big enough to read and to hit.
import { chromium } from "playwright";

const BASE = process.env.PERF_URL ?? "http://localhost:4399/perf.html?pref=zh&ws=2&sess=6&turns=2";
const fails = [];
const check = (name, ok, detail = "") => {
  console.log(`${ok ? "  ok" : "FAIL"}  ${name}${detail ? "  — " + detail : ""}`);
  if (!ok) fails.push(name);
};

const browser = await chromium.launch();
try {
  for (const scheme of ["light", "dark"]) {
    const page = await browser.newPage({ colorScheme: scheme, viewport: { width: 1400, height: 900 } });
    await page.addInitScript(() => localStorage.setItem("rx-nav-rail", "on"));
    await page.goto(BASE, { waitUntil: "networkidle" });
    await page.waitForSelector(".app");
    await page.locator('.navbtn[aria-label="文件"]').first().click();
    await page.waitForSelector(".workbench-explorer-head");
    await page.waitForSelector(".workbench-filelist button");
    const read = await page.evaluate(() => {
      const px = (el, prop) => parseFloat(getComputedStyle(el)[prop]);
      const head = document.querySelector(".workbench-explorer-head");
      const btn = head.querySelector(".workbench-open-editor");
      const row = document.querySelector(".workbench-filelist button");
      const search = document.querySelector(".workbench-search input");
      return {
        head: px(head, "fontSize"),
        button: btn.getBoundingClientRect().width,
        icon: btn.querySelector("svg").getBoundingClientRect().width,
        row: px(row, "fontSize"),
        search: px(search, "fontSize"),
        rowIcon: row.querySelector("svg").getBoundingClientRect().width,
      };
    });
    check(`${scheme}: the header is at least 12px`, read.head >= 12, `${read.head}px`);
    check(`${scheme}: the header buttons are at least 28px with 16px icons`, read.button >= 28 && read.icon >= 16, `${read.button}px / ${read.icon}px`);
    check(`${scheme}: file rows are at least 12px with 15px icons`, read.row >= 12 && read.rowIcon >= 15, `${read.row}px / ${read.rowIcon}px`);
    check(`${scheme}: the search field is at least 12px`, read.search >= 12, `${read.search}px`);
    await page.close();
  }
} finally {
  await browser.close();
}
if (fails.length) {
  console.error(`\n${fails.length} failed: ${fails.join("; ")}`);
  process.exit(1);
}
console.log("\nall passed");
