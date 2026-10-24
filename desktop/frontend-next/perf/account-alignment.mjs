import { chromium } from "playwright";

const browser = await chromium.launch();
const base = process.env.PERF_URL ?? "http://localhost:4399/perf.html";
const failures = [];
try {
  for (const scheme of ["light", "dark"]) {
    const page = await browser.newPage({ viewport: { width: 1280, height: 800 }, colorScheme: scheme });
    await page.addInitScript(() => {
      localStorage.setItem("rx-nav-rail", "off");
      localStorage.setItem("reasonix.mock.account", "1");
    });
    await page.goto(`${base}?pref=zh&turns=3`, { waitUntil: "networkidle" });
    await page.waitForSelector(".studio-user-foot .ini");
    const centers = await page.evaluate(() => {
      const center = (selector) => {
        const range = document.createRange();
        range.selectNodeContents(document.querySelector(selector));
        const box = range.getBoundingClientRect();
        return box.top + box.height / 2;
      };
      return [center(".studio-user-foot .ini"), center(".studio-workspace-kind")];
    });
    const aligned = Math.abs(centers[0] - centers[1]) < 0.5;
    console.log(`${aligned ? "ok" : "FAIL"} ${scheme}: avatar/workspace text centers ${centers.join(" / ")}`);
    if (!aligned) failures.push(scheme);
    await page.locator('[data-action="chrome.account"]').click();
    await page.waitForSelector(".acct");
    console.log(`ok ${scheme}: avatar opens account settings`);
    await page.close();
  }
} finally {
  await browser.close();
}
if (failures.length) process.exitCode = 1;
