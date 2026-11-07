// Full-screen browser: the page takes the whole pane, the conversation is folded away but stays mounted,
// and Escape (or the same button) brings the split back.
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
    const page = await browser.newPage({ colorScheme: scheme, viewport: { width: 1400, height: 900 }, reducedMotion: "reduce" });
    await page.addInitScript(() => localStorage.setItem("rx-nav-rail", "on"));
    await page.goto(BASE, { waitUntil: "networkidle" });
    await page.waitForSelector(".app");
    await page.locator('.navbtn[aria-label="文件"]').first().click();
    await page.waitForSelector(".workbench-filelist button");
    const read = () => page.evaluate(() => {
      const pane = document.querySelector(".pane");
      const dock = pane.querySelector(':scope > .scroll[data-pane="browser"]');
      const shown = (el) => !!el && getComputedStyle(el).display !== "none";
      return {
        size: pane.getAttribute("data-browser-size"),
        conversation: shown(pane.querySelector(":scope > .pbody")),
        composer: shown(pane.querySelector(":scope > .compose")),
        dockWidth: dock.getBoundingClientRect().width,
        paneWidth: pane.getBoundingClientRect().width,
      };
    });
    await page.waitForTimeout(1200);
    await page.mouse.move(1390, 890);
    await page.waitForTimeout(300);
    const split = await read();
    check(`${scheme}: starts split with the conversation shown`, split.size === "split" && split.conversation && split.composer);
    if (SHOTS) await page.screenshot({ path: `${SHOTS}/browser-${scheme}-split.png` });
    await page.locator('[data-action="browser.full"]').first().click();
    await page.waitForTimeout(500);
    const full = await read();
    check(`${scheme}: full hides the conversation and the composer`, full.size === "full" && !full.conversation && !full.composer);
    check(`${scheme}: full gives the page the whole pane`, full.dockWidth >= full.paneWidth - 1, `${full.dockWidth}px of ${full.paneWidth}px`);
    await page.mouse.move(1390, 890);
    await page.waitForTimeout(300);
    if (SHOTS) await page.screenshot({ path: `${SHOTS}/browser-${scheme}-full.png` });
    await page.keyboard.press("Escape");
    await page.waitForTimeout(500);
    const back = await read();
    check(`${scheme}: Escape brings the split and the conversation back`, back.size === "split" && back.conversation && back.composer);
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
