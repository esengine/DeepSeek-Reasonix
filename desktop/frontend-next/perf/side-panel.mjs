// The side question opens from the button in the composer, above the composer
// and inside the conversation's own column — never over what sits beside or
// above it, whether the file panel is open or not and at any width.
import { chromium } from "playwright";
import { mkdir } from "node:fs/promises";

const PAGE = process.env.PERF_URL ?? "http://localhost:4399/perf.html?pref=zh&ws=2&sess=6&turns=3";
const fails = [];
const check = (name, ok, detail = "") => {
  console.log(`${ok ? "  ok" : "FAIL"}  ${name}${detail ? "  — " + detail : ""}`);
  if (!ok) fails.push(name);
};
const shotDir = process.env.SHOTS;
const box = (el) => (el ? (({ left, top, right, bottom }) => ({ left, top, right, bottom }))(el.getBoundingClientRect()) : null);

if (shotDir) await mkdir(shotDir, { recursive: true });
const browser = await chromium.launch({ channel: process.env.PLAYWRIGHT_CHANNEL });
for (const scheme of ["light", "dark"]) {
  for (const [width, files, height = 800, zoom = 1] of [[1400, false], [1400, true], [760, false], [760, true], [390, false, 700], [900, false, 480], [760, false, 800, 1.3]]) {
    const label = `${scheme} ${width}x${height} zoom ${zoom}${files ? " with the file panel" : ""}`;
    const page = await browser.newPage({ viewport: { width, height }, locale: "zh-CN", colorScheme: scheme });
    page.on("pageerror", (e) => fails.push("页面异常: " + e.message));
    await page.addInitScript(() => localStorage.setItem("rx-nav-rail", "on"));
    await page.goto(`${PAGE}&zoom=${zoom}`, { waitUntil: "networkidle" });
    await page.waitForSelector(".compose");
    if (files) {
      await page.locator('.navbtn[aria-label="文件"]').first().click();
      await page.waitForSelector(".workbench-filelist button");
    }
    const more = page.locator(".compose .turn-more");
    if (await more.isVisible()) await more.click();
    await page.locator('[data-action="side.toggle"]').click();
    await page.waitForSelector(".sidepanel");
    await page.waitForTimeout(250);
    const r = await page.evaluate(() => {
      const box = (el) => (el ? (({ left, top, right, bottom }) => ({ left, top, right, bottom }))(el.getBoundingClientRect()) : null);
      const panel = document.querySelector(".sidepanel");
      const compose = panel.closest(".compose");
      const pane = compose.closest(".pane");
      const beside = [...document.querySelectorAll(".workbench-filelist, .workbench-explorer-head")].filter((el) => !pane.contains(el)).map(box);
      return { panel: box(panel), compose: box(compose), pane: box(pane), beside, vw: innerWidth, vh: innerHeight, button: box(document.querySelector('[data-action="side.toggle"]')) };
    });
    const p = r.panel;
    check(`${label}: it sits above the composer`, p.bottom <= r.compose.top + 1 && r.compose.top - p.bottom < 24, `bottom ${Math.round(p.bottom)}, composer top ${Math.round(r.compose.top)}`);
    check(`${label}: it stays inside its pane`, p.left >= r.pane.left - 1 && p.right <= r.pane.right + 1 && p.top >= r.pane.top - 1, `panel ${Math.round(p.left)}–${Math.round(p.right)}, pane ${Math.round(r.pane.left)}–${Math.round(r.pane.right)}`);
    check(`${label}: it stays inside the window`, p.left >= 0 && p.right <= r.vw && p.top >= 0 && p.bottom <= r.vh);
    check(`${label}: it does not cover the file panel`, r.beside.every((b) => p.right <= b.left || p.left >= b.right || p.bottom <= b.top || p.top >= b.bottom), files ? `${r.beside.length} file panel parts` : "no file panel");
    check(`${label}: it opens above the button that asked`, p.bottom <= r.button.top && p.left <= r.button.left + 1, `button ${Math.round(r.button.left)}`);
    const draft = page.locator('[data-action-change="side.draft"]');
    await draft.fill("解释这个函数如何处理错误。".repeat(50));
    await page.locator('[data-action="side.send"]').click();
    await page.locator('.sideturn[data-role="side"]').waitFor();
    await draft.fill("keep this draft");
    const collapse = page.locator('[data-action="side.collapse"]');
    await collapse.click();
    const hidden = await draft.isHidden();
    const compact = await page.locator(".sidepanel").evaluate((el) => {
      const b = el.getBoundingClientRect();
      const button = el.querySelector('[data-action="side.collapse"]');
      const c = button.getBoundingClientRect();
      const hit = document.elementFromPoint(c.x + c.width / 2, c.y + c.height / 2);
      const flow = el.closest(".pane").querySelector('[data-pane="flow"]').getBoundingClientRect();
      return { height: b.height, hit: hit === button || button.contains(hit), readable: b.top - flow.top, flowHeight: flow.height };
    });
    check(`${label}: collapse leaves a reachable header and hides inputs`, hidden && compact.hit && compact.height < 72, JSON.stringify(compact));
    check(`${label}: collapse exposes the transcript`, compact.readable > 100 && compact.readable > compact.flowHeight / 2, JSON.stringify(compact));
    await collapse.focus();
    await page.keyboard.press("Tab");
    check(`${label}: Tab skips the collapsed controls`, await page.locator('[data-action="side.close"]').evaluate((el) => el === document.activeElement));
    if (shotDir && [1400, 390].includes(width) && !files) await page.screenshot({ path: `${shotDir}/${scheme}-${width}.png` });
    await collapse.click();
    await draft.waitFor({ state: "visible" });
    check(`${label}: expansion keeps the draft`, await draft.inputValue() === "keep this draft");
    await page.keyboard.press("Escape");
    await page.locator(".sidepanel").waitFor({ state: "detached" });
    check(`${label}: Escape returns focus`, await page.locator('[data-action="side.toggle"]').evaluate((el) => el === document.activeElement));
    await page.close();
  }
}
await browser.close();
if (fails.length) {
  console.log(`\n${fails.length} failed`);
  process.exit(1);
}
