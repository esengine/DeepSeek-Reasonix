import assert from "node:assert/strict";
import { mkdirSync } from "node:fs";
import { join } from "node:path";
import { chromium } from "playwright";

const PAGE = process.env.PERF_URL ?? "http://localhost:4399/perf.html";
const SHOTS = process.env.ONBOARDING_SHOTS;
const cases = [
  { width: 2560, height: 720 },
  { width: 1920, height: 600 },
  { width: 1366, height: 768 },
  { width: 1280, height: 480 },
  { width: 1440, height: 1080 },
  { width: 640, height: 480 },
  { width: 390, height: 640 },
  { width: 1366, height: 768, zoom: 1.3 },
  { width: 1280, height: 480, zoom: 0.9 },
  { width: 1280, height: 720, zoom: 1.5 },
  { width: 390, height: 640, zoom: 1.3 },
  { width: 1440, height: 1080, resize: { width: 1920, height: 480 } },
];
if (SHOTS) mkdirSync(SHOTS, { recursive: true });

async function reach(page, selector) {
  const target = page.locator(selector);
  await target.scrollIntoViewIfNeeded({ timeout: 3000 });
  const bounds = await target.boundingBox();
  const viewport = page.viewportSize();
  assert(bounds && viewport, `${selector} has a rendered box`);
  assert(bounds.y >= -1 && bounds.y + bounds.height <= viewport.height + 1,
    `${selector} fits vertically: ${JSON.stringify(bounds)}`);
  assert(bounds.x >= -1 && bounds.x + bounds.width <= viewport.width + 1,
    `${selector} fits horizontally: ${JSON.stringify(bounds)}`);
  const hits = await target.evaluate((el) => {
    const r = el.getBoundingClientRect();
    const hit = document.elementFromPoint(r.x + r.width / 2, r.y + r.height / 2);
    return hit === el || el.contains(hit);
  });
  assert(hits, `${selector} receives pointer input`);
  return target;
}

const browser = await chromium.launch();
const failures = [];
try {
  for (const entry of cases) {
    const { width, height, zoom = 1, resize } = entry;
    const label = `${width}x${height}@${zoom}${resize ? " resized" : ""}`;
    const page = await browser.newPage({ viewport: { width, height }, locale: "zh-CN", reducedMotion: "reduce" });
    const errors = [];
    page.on("pageerror", (error) => errors.push(error.message));
    try {
      const url = new URL(PAGE);
      url.searchParams.set("onboarding", "1");
      url.searchParams.set("pref", "zh");
      url.searchParams.set("zoom", String(zoom));
      await page.goto(url.href, { waitUntil: "networkidle" });
      await page.waitForSelector(".onb-stage");
      await page.locator("#onb-key").fill("sk-onboarding-fixture");
      await (await reach(page, ".onb-go")).click();
      await page.waitForSelector(".onb-found");
      await page.waitForFunction(() => {
        const button = document.querySelector(".onb-go");
        return button && !button.disabled && button.textContent.trim() === "开始";
      });
      if (resize) {
        await page.setViewportSize(resize);
        await page.waitForFunction(() =>
          document.querySelector(".onb-stage").getBoundingClientRect().height <= innerHeight + 1,
          undefined, { timeout: 3000 });
      }

      const layout = await page.locator(".onb-stage").evaluate((stage) => ({
        height: stage.getBoundingClientRect().height,
        viewport: innerHeight,
        width: stage.scrollWidth,
        clientWidth: stage.clientWidth,
      }));
      assert(layout.height <= layout.viewport + 1, `stage stays inside the viewport: ${JSON.stringify(layout)}`);
      assert(layout.width <= layout.clientWidth + 1, "onboarding does not overflow horizontally");

      const viewport = page.viewportSize();
      await page.mouse.move(viewport.width / 2, viewport.height / 2);
      await page.mouse.wheel(0, 10000);
      await page.waitForFunction(() => {
        const stage = document.querySelector(".onb-stage");
        return stage.scrollHeight - stage.clientHeight - stage.scrollTop <= 1;
      }, undefined, { timeout: 3000 });
      await reach(page, ".onb-foot");
      await reach(page, "#onb-url");
      const start = await reach(page, ".onb-go");
      if (SHOTS) await page.screenshot({ path: join(SHOTS, `${label.replaceAll(" ", "-")}.png`) });
      await start.click();
      await page.waitForSelector(".app .chrome");
      assert.equal(await page.locator(".onb-stage").count(), 0, "Start exits onboarding");
      assert.deepEqual(errors, [], "no browser errors");
      console.log(`PASS ${label}`);
    } catch (error) {
      failures.push(label);
      console.error(`FAIL ${label}: ${error.message}`);
      if (SHOTS) await page.screenshot({ path: join(SHOTS, `failed-${label.replaceAll(" ", "-")}.png`) });
    } finally {
      await page.close();
    }
  }
} finally {
  await browser.close();
}
assert.deepEqual(failures, [], "all onboarding viewports pass");
