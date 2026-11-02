import assert from "node:assert/strict";
import { mkdir } from "node:fs/promises";
import { chromium } from "playwright";

const base = process.env.PERF_URL ?? "http://localhost:4399/perf.html";
const before = process.env.RECENT_BEFORE_URL;
const shots = process.env.RECENT_SHOTS;
if (shots) await mkdir(shots, { recursive: true });
const browser = await chromium.launch();
const errors = [];
async function settle(page, selector) {
  await page.locator(selector).evaluateAll((elements) => Promise.all(elements.flatMap((element) => {
    element.getBoundingClientRect();
    return element.getAnimations().map((animation) => animation.finished.catch(() => {}));
  })));
}
async function showRail(page) {
  if (await page.locator(".rail[inert]").count()) {
    await page.locator('[data-action="chrome.rail"]:not(.railveil)').filter({ visible: true }).first().click();
  }
  await page.locator(".rail:not([inert]) .projects-header").waitFor();
  await settle(page, ".app, .rail");
}
try {
  for (const scheme of ["light", "dark"]) {
    for (const [size, width] of [["wide", 1440], ["phone", 390]]) {
      const page = await browser.newPage({ viewport: { width, height: 900 }, colorScheme: scheme, reducedMotion: "reduce" });
      page.on("pageerror", (error) => errors.push(error.message));
      const tag = `${scheme}-${size}`;
      if (before && shots) {
        await page.goto(`${before}?pref=en&rows=1&ws=2&sess=180&turns=2`, { waitUntil: "networkidle" });
        await showRail(page);
        await page.screenshot({ path: `${shots}/before-${tag}.png` });
      }
      await page.goto(`${base}?pref=en&rows=1&ws=2&sess=180&turns=2`, { waitUntil: "networkidle" });
      await showRail(page);
      if (shots) await page.screenshot({ path: `${shots}/after-heading-${tag}.png` });
      await page.locator('.wsrow[data-target="~/projects/workspace-1"]').click();
      await page.locator('.studio-session-segments [data-value="live"]').click();
      const recent = page.getByRole("button", { name: "Recent sessions", exact: true });
      const hit = await recent.evaluate((element) => {
        const r = element.getBoundingClientRect();
        return r.left >= 0 && r.right <= innerWidth && element.contains(document.elementFromPoint(r.x + r.width / 2, r.y + r.height / 2));
      });
      assert.ok(hit, `${tag}: Recent is reachable in the project heading`);
      await recent.click();
      const dialog = page.getByRole("dialog", { name: "Recent sessions", exact: true });
      await dialog.locator(".recent-session").first().waitFor();
      await settle(page, ".pickveil");
      assert.equal(await dialog.evaluate((element) => getComputedStyle(element).opacity), "1", "the dialog is actually painted");
      assert.equal(await dialog.locator(".recent-session").count(), 50);
      const bounds = await dialog.locator(".pickcard").boundingBox();
      assert.ok(bounds && bounds.x >= 0 && bounds.x + bounds.width <= width && bounds.y >= 0 && bounds.y + bounds.height <= 900, `${tag}: dialog stays inside the viewport`);
      if (shots) await page.screenshot({ path: `${shots}/after-recent-${tag}.png` });
      for (let i = 1; i <= 6; i++) {
        await dialog.getByRole("button", { name: "Next page", exact: true }).click();
        await dialog.getByText(`Page ${i + 1}`, { exact: true }).waitFor();
        await dialog.locator('.picklist[aria-busy="false"] .recent-session').first().waitFor();
      }
      assert.equal(await dialog.locator(".recent-session").count(), 50, "history past 300 remains paged rather than hidden");
      const selected = dialog.locator('.recent-session[data-target="/sessions/w1-s150.jsonl"]');
      await selected.click();
      await dialog.waitFor({ state: "detached" });
      const located = page.locator('.rail .sessrow[data-target="/sessions/w1-s150.jsonl"]');
      await located.waitFor();
      await page.waitForFunction(() => document.querySelector('.rail .sessrow[data-target="/sessions/w1-s150.jsonl"]')?.getAttribute("aria-selected") === "true");
      assert.equal(await page.locator('.studio-session-segments [data-value="all"]').getAttribute("aria-pressed"), "true");
      assert.equal(await page.locator('.wsrow[data-target="~/projects/workspace-1"]').getAttribute("aria-expanded"), "true");
      if (size === "wide") {
        assert.ok(await located.evaluate((element) => element === document.activeElement), `${tag}: selection focuses the session in its project`);
      } else {
        assert.equal(await page.locator(".app").getAttribute("data-rail"), "off", "phone navigation reveals the conversation");
        await showRail(page);
        assert.ok(await located.isVisible(), "reopening the phone rail reveals the selected session in its expanded project");
        const selectedBounds = await located.boundingBox();
        assert.ok(selectedBounds && selectedBounds.x >= 0 && selectedBounds.x + selectedBounds.width <= width, "the selected phone row is on screen, not covered by a closing rail");
      }
      if (shots) await page.screenshot({ path: `${shots}/after-located-${tag}.png` });
      console.log(`${tag}: heading hit target, 50-row global pages beyond 300, viewport containment, finished-session project location`);
      await page.close();
    }
  }
  const page = await browser.newPage({ viewport: { width: 1440, height: 900 } });
  await page.goto(`${base}?pref=en&turns=2`, { waitUntil: "networkidle" });
  await showRail(page);
  await page.evaluate(() => window.__feed({ kind: "turn_started" }));
  await page.locator('.studio-session-segments [data-value="live"]').click();
  await page.locator('.rail .sessrow[data-target="/sessions/mock.jsonl"]').waitFor();
  await page.evaluate(() => window.__feed({ kind: "turn_done" }));
  await page.locator('.rail .sessrow[data-target="/sessions/mock.jsonl"]').waitFor({ state: "detached" });
  await page.getByRole("button", { name: "Recent sessions", exact: true }).click();
  await page.locator('.recent-session[data-target="/sessions/mock.jsonl"]').click();
  await page.locator('.rail .sessrow[data-target="/sessions/mock.jsonl"]').waitFor();
  console.log("running -> finished -> Recent -> existing pane: passed");
  await page.close();
  assert.deepEqual(errors, [], "no page errors");
} finally {
  await browser.close();
}
