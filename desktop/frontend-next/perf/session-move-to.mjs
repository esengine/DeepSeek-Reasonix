// A conversation can be moved to another project from its menu: the dialog says what travels and what does not,
// lists the other projects, and asks the kernel exactly once for the one chosen.
import { chromium } from "playwright";

const BASE = process.env.PERF_URL ?? "http://localhost:4399/perf.html";
const shotDir = process.env.SHOTS;
const fails = [];
const check = (name, ok, detail = "") => {
  console.log(`${ok ? "  ok" : "FAIL"}  ${name}${detail ? "  — " + detail : ""}`);
  if (!ok) fails.push(name);
};

const browser = await chromium.launch();
for (const scheme of ["light", "dark"]) {
  for (const width of [1280, 420]) {
    const tag = `${scheme} ${width}px`;
    const page = await browser.newPage({ viewport: { width, height: 900 }, locale: "zh-CN", colorScheme: scheme, reducedMotion: "reduce" });
    page.on("pageerror", (e) => fails.push("页面异常: " + e.message));
    await page.goto(`${BASE}?pref=zh&ws=3&sess=3&turns=1`, { waitUntil: "networkidle" });
    await page.waitForSelector(".app");
    const rows = '.sessrow[role="treeitem"][data-target]';
    await page.waitForSelector(rows);
    const first = await page.locator(rows).first().getAttribute("data-target");
    await page.evaluate((sel) => document.querySelector(sel).dispatchEvent(new MouseEvent("contextmenu", { bubbles: true, cancelable: true, clientX: 140, clientY: 300 })), rows);
    await page.getByRole("menuitem", { name: "移动到项目…" }).click();
    await page.waitForSelector('[role="dialog"] input[type="radio"]');
    const dlg = await page.evaluate(() => {
      const d = document.querySelector('[role="dialog"]:has(input[type="radio"])');
      const card = d.querySelector(".pickcard").getBoundingClientRect();
      return { options: [...d.querySelectorAll(".movess-row")].map((o) => o.textContent), text: d.textContent, fits: card.left >= 0 && card.right <= innerWidth + 1 && card.bottom <= innerHeight + 1, focusIn: d.contains(document.activeElement) };
    });
    check(`${tag}: it lists the other projects but not the one it is in`, dlg.options.length === 2 && !dlg.options.some((o) => o.startsWith("workspace-0")), dlg.options.join(" | "));
    check(`${tag}: it says what travels and what does not`, dlg.text.includes("原项目的权限与目标设置不会跟着走") && dlg.text.includes("路径保持原样"));
    check(`${tag}: the dialog fits the window and holds focus`, dlg.fits && dlg.focusIn, JSON.stringify({ fits: dlg.fits, focusIn: dlg.focusIn }));
    if (shotDir) await page.mouse.move(width - 3, 895), await page.waitForTimeout(250), await page.screenshot({ path: `${shotDir}/${scheme}-${width}-dialog.png` });
    await page.locator('[role="dialog"] .movess-row').nth(1).click();
    await page.getByRole("button", { name: "移动", exact: true }).click();
    await page.waitForFunction(() => (window.__moves ?? []).length === 1);
    const moves = await page.evaluate(() => window.__moves);
    check(`${tag}: the kernel is asked once, for this conversation and the chosen project`, moves.length === 1 && moves[0].path === first && moves[0].target.endsWith("workspace-2"), JSON.stringify(moves));
    await page.waitForFunction(() => !document.querySelector('[role="dialog"]:has(input[type="radio"])'));
    check(`${tag}: the dialog closes when it is done`, true);
    await page.close();
  }
}
await browser.close();
if (fails.length) {
  console.error(`\n${fails.length} failed: ${fails.join("; ")}`);
  process.exit(1);
}
console.log("\nall passed");
