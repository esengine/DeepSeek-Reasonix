// The first-run "start" button must be reachable at every window height.
// The body clips, so a card taller than the window is only reachable if the
// stage itself scrolls. Focus is no evidence: the browser scrolls the clipped
// body to a focused element, so the wheel is what gets asserted.
import { chromium } from "playwright";
import { mkdirSync } from "node:fs";
import { join } from "node:path";

const PAGE = process.env.PERF_URL ?? "http://localhost:4399/perf.html?onboarding=1";
const SHOTS = process.env.ONBOARDING_SHOTS;
const SIZES = [
  [1920, 700], [1600, 600], [2560, 560], [1366, 600], [1280, 540], [1440, 900], [1600, 760, 1.25],
  [760, 480], [761, 480], [760, 480, 0.8], [761, 480, 1.8], [800, 480, 1.8], [840, 560, 1.8],
  [840, 560, 1.5], [1280, 720, 1.8], [1600, 900, 1.8, [840, 560]], [840, 560, 1.8, [1600, 900]],
];
if (SHOTS) mkdirSync(SHOTS, { recursive: true });
const fails = [];
const check = (name, ok, detail = "") => {
  console.log(`${ok ? "  ok" : "FAIL"}  ${name}${detail ? "  — " + detail : ""}`);
  if (!ok) fails.push(name);
};

const browser = await chromium.launch();
for (const scheme of ["light", "dark"]) {
  for (const [width, height, zoom = 1, resize] of SIZES) {
    const ctx = await browser.newContext({ locale: "zh-CN", viewport: { width, height }, colorScheme: scheme });
    const page = await ctx.newPage();
    page.on("pageerror", (e) => fails.push("page error: " + e.message));
    const url = new URL(PAGE);
    url.searchParams.set("zoom", String(zoom));
    await page.goto(url.href, { waitUntil: "domcontentloaded" });
    await page.waitForSelector(".onb-card");
    await page.waitForFunction((z) => Number(getComputedStyle(document.documentElement).zoom) === z, zoom);
    const tag = `${scheme} ${width}x${height}${zoom !== 1 ? ` zoom${zoom}` : ""}${resize ? ` to ${resize.join("x")}` : ""}`;

    const fitsCard = async (label) => {
      const layout = await page.evaluate(() => {
        const stage = document.querySelector(".onb-stage");
        const shell = document.querySelector(".onb-shell");
        const r = shell.getBoundingClientRect();
        const fields = ["#onb-url", "#onb-key", ".onb-go", ".onb-note", ".onb-protocols", ".onb-pick"];
        const clipped = fields.flatMap((selector) => {
          const el = document.querySelector(selector);
          if (!el) return [];
          const box = el.getBoundingClientRect();
          return box.left < Math.max(0, r.left) - 0.5 || box.right > Math.min(innerWidth, r.right) + 0.5
            ? [{ selector, left: box.left, right: box.right, shellRight: r.right }] : [];
        });
        return { clipped, columns: getComputedStyle(shell).gridTemplateColumns.split(" ").length,
          expectedColumns: stage.clientWidth <= 760 ? 1 : 2, width: stage.scrollWidth, client: stage.clientWidth };
      });
      check(`${tag} ${label}: form stays within the card and viewport`, layout.clipped.length === 0, JSON.stringify(layout.clipped));
      check(`${tag} ${label}: columns follow zoom-adjusted room`, layout.columns === layout.expectedColumns, JSON.stringify(layout));
      check(`${tag} ${label}: stage does not scroll sideways`, layout.width <= layout.client + 1);
    };

    const reachable = async (label, enabled) => {
      const viewport = page.viewportSize();
      const go = page.locator(".onb-go");
      await page.evaluate(() => document.activeElement?.blur());
      await page.mouse.move(viewport.width / 2, viewport.height / 2);
      await page.mouse.wheel(0, 4000);
      await page.waitForTimeout(250);
      const box = await go.boundingBox();
      check(`${tag} ${label}: after wheeling to the bottom the button is in the viewport`, !!box && box.y >= 0 && box.y + box.height <= viewport.height + 0.5, JSON.stringify(box));
      const bar = await page.locator(".onb-brandbar").boundingBox();
      check(`${tag} ${label}: the title bar stays in the viewport`, !!bar && bar.y >= -0.5 && bar.y + bar.height <= viewport.height + 0.5, JSON.stringify(bar));
      if (!enabled) return;
      const hit = await page.evaluate(() => {
        const r = document.querySelector(".onb-go").getBoundingClientRect();
        const el = document.elementFromPoint(r.x + r.width / 2, r.y + r.height / 2);
        return !!el?.closest(".onb-go");
      });
      check(`${tag} ${label}: the point under its centre hits it`, hit);
      if (SHOTS) await page.screenshot({ path: join(SHOTS, `${tag.replaceAll(" ", "-")}-${label.replaceAll(" ", "-")}.png`) });
      const first = page.locator(".onb-chip").first();
      await page.evaluate(() => document.activeElement?.blur());
      await first.focus();
      await page.waitForTimeout(250);
      const [fb, bb] = [await first.boundingBox(), await page.locator(".onb-brandbar").boundingBox()];
      check(`${tag} ${label}: a focused top-of-card element sits below the title bar`, !!fb && !!bb && fb.y >= bb.y + bb.height - 0.5, JSON.stringify({ fb, bb }));
    };

    await fitsCard("before connect");
    await reachable("before connect", false);
    await page.fill('input[placeholder="sk-…"]', "sk-test");
    await page.locator(".onb-go").click();
    await page.waitForSelector(".onb-found");
    await fitsCard("after connect");
    await reachable("after connect", true);
    if (resize) {
      await page.setViewportSize({ width: resize[0], height: resize[1] });
      await page.waitForFunction(() => {
        const stage = document.querySelector(".onb-stage");
        return Math.abs(stage.getBoundingClientRect().width - innerWidth) < 1;
      });
      await fitsCard("after resize");
      await reachable("after resize", true);
    }
    await page.locator(".onb-go").click();
    await page.waitForSelector(".app .chrome");
    check(`${tag}: Start finishes setup`, await page.locator(".onb-stage").count() === 0);
    await ctx.close();
  }
}
await browser.close();
console.log(fails.length ? `\n${fails.length} failed` : "\nall passed");
process.exit(fails.length ? 1 : 0);
