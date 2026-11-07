// The code editor's text and its search field follow the interface text size, like the rest of the page.
import { chromium } from "playwright";

const BASE = process.env.PERF_URL ?? "http://localhost:4399/perf.html?pref=zh&ws=2&sess=6&turns=2";
const SHOTS = process.env.SHOTS_DIR;
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
    await page.waitForSelector(".workbench-filelist button");
    await page.getByText("README.md").first().click();
    await page.getByRole("button", { name: "编辑", exact: true }).click();
    await page.waitForSelector(".cm-content");
    await page.locator(".cm-content").click();
    for (const chord of ["Control+f", "Meta+f"]) {
      if (await page.locator(".cm-textfield").count()) break;
      await page.keyboard.press(chord);
      await page.waitForTimeout(300);
    }
    await page.waitForSelector(".cm-textfield");
    for (const scale of [0.85, 1, 1.6]) {
      await page.evaluate((v) => document.documentElement.style.setProperty("--fs-scale", String(v)), scale);
      await page.waitForTimeout(1000);
      const read = await page.evaluate(() => {
        const px = (el) => parseFloat(getComputedStyle(el).fontSize);
        return { text: px(document.querySelector(".cm-content")), field: px(document.querySelector(".cm-textfield")) };
      });
      check(`${scheme} x${scale}: the editor text is ${12 * scale}px`, Math.abs(read.text - 12 * scale) < 0.1, `${read.text}px`);
      check(`${scheme} x${scale}: the search field is ${11 * scale}px`, Math.abs(read.field - 11 * scale) < 0.1, `${read.field}px`);
      if (SHOTS) await page.screenshot({ path: `${SHOTS}/editor-${scheme}-${scale}.png`, fullPage: false });
    }
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
