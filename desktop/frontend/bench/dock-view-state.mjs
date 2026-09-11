import assert from "node:assert/strict";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { tmpdir } from "node:os";
import { startPreviewServer } from "./vite-preview-server.mjs";

const frontendDir = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
process.env.PLAYWRIGHT_BROWSERS_PATH = !process.env.PLAYWRIGHT_BROWSERS_PATH || process.env.PLAYWRIGHT_BROWSERS_PATH === ".pw-browsers"
  ? path.join(frontendDir, ".pw-browsers")
  : process.env.PLAYWRIGHT_BROWSERS_PATH;
const { chromium } = await import("playwright");
const preview = process.env.REASONIX_DOCK_URL ? null : await startPreviewServer(frontendDir, 4663);
const url = process.env.REASONIX_DOCK_URL ?? "http://127.0.0.1:4663";
const browser = await chromium.launch({ headless: true, executablePath: process.env.CHROME_EXECUTABLE });
try {
  const context = await browser.newContext({ viewport: { width: 1440, height: 1000 }, locale: "en-US" });
  await context.addInitScript(() => localStorage.setItem("reasonix-lang", "en"));
  const page = await context.newPage();
  const errors = [];
  page.on("pageerror", error => errors.push(error.message));
  await page.goto(url + "/?mock=bench&bench=1&app-lifecycle-probe=1");
  await page.locator("textarea.composer__input:not([aria-hidden=true])").waitFor();
  await page.locator('.project-tree__topic-main:has-text("bench:small-6t")').click();
  const tabs = page.locator('.workbench-dock__tabs [role="tab"]');
  await page.locator(".tab-picker__item").filter({ hasText: /^Files$/ }).click();
  await page.locator('[data-workspace-path="README.md"]').click();
  await page.waitForFunction(() => document.querySelector(".workspace-preview__body")?.textContent?.includes("Browser-dev workspace preview."));
  await page.locator('[data-workspace-path="go.mod"]').click();
  await page.locator('[data-workspace-path="README.md"]').click();
  await page.locator(".workbench-dock__tab-add").click();
  await page.locator(".tab-add-menu__item").filter({ hasText: /^Files$/ }).click();
  assert.equal(await tabs.count(), 2);
  assert.equal(await page.locator(".workspace-panel").count(), 1, "only the active view mounts");
  assert.equal(await page.locator(".workspace-preview__body").count(), 0, "duplicate view starts empty");
  await page.locator('[data-workspace-path="go.mod"]').click();
  const selected = () => document.querySelector(".workspace-tree__row--active")?.getAttribute("data-workspace-path");
  await page.waitForFunction(selected => document.querySelector(".workspace-tree__row--active")?.getAttribute("data-workspace-path") === selected, "go.mod");
  await tabs.nth(0).click();
  assert.equal(await page.evaluate(selected), "README.md", "first view restores selection");
  await page.locator('.workspace-iconbtn:has(svg.lucide-x)').click();
  assert.equal(await page.evaluate(selected), "go.mod", "first view restores its inner open-file stack");
  await page.locator('[data-workspace-path="README.md"]').click();
  await tabs.nth(1).click();
  assert.equal(await page.evaluate(selected), "go.mod", "second view restores selection");
  await page.getByPlaceholder("Filter files…", { exact: true }).fill("mod");
  await tabs.nth(0).click();
  assert.equal(await page.getByPlaceholder("Filter files…", { exact: true }).inputValue(), "");
  await tabs.nth(1).click();
  assert.equal(await page.getByPlaceholder("Filter files…", { exact: true }).inputValue(), "mod");
  await tabs.nth(1).locator(".workbench-dock__tab-close").click();
  await page.locator(".workbench-dock__tab-overview").click();
  await page.locator(".tab-overview__row").filter({ hasText: /Closed/ }).click();
  assert.equal(await page.evaluate(selected), "go.mod", "reopen restores the original view");
  assert.equal(await page.getByPlaceholder("Filter files…", { exact: true }).inputValue(), "mod", "reopen restores filtering");
  await page.reload();
  await page.locator('[data-workspace-path="go.mod"]').waitFor();
  assert.equal(await tabs.count(), 2, "restart restores both views");
  assert.equal(await page.evaluate(selected), "go.mod", "restart restores the active view");
  assert.equal(await page.getByPlaceholder("Filter files…", { exact: true }).inputValue(), "mod", "restart restores filtering");
  await tabs.nth(0).click();
  assert.equal(await page.evaluate(selected), "README.md", "restart keeps the other view independent");
  await page.locator('.project-tree__folder-main:has(svg.lucide-cloud)').click();
  await page.locator('.project-tree__topic-main:has-text("Remote demo session")').click();
  await page.locator(".remote-surface--ready").waitFor();
  assert.equal(await tabs.count(), 0, "other project does not inherit local dock tabs");
  await page.locator('.project-tree__topic-main:has-text("bench:small-6t")').click();
  await page.locator('[data-workspace-path="README.md"]').waitFor();
  assert.equal(await tabs.count(), 2);
  assert.equal(await page.evaluate(selected), "README.md", "returning to the project restores its view");

  // Native inert behavior, including hit-testing without the management overlay.
  await page.evaluate(() => {
    window.dockBackground = document.querySelector(".topicbar__chrome-btn--workspace");
    window.dockBackgroundClicks = 0;
    window.dockBackground.addEventListener("click", () => window.dockBackgroundClicks++);
  });
  await page.locator("button:has(svg.lucide-settings)").last().click();
  await page.locator(".settings-screen").waitFor();
  assert(await page.evaluate(() => Boolean(window.dockBackground.closest("[inert]"))));
  assert(await page.evaluate(() => {
    window.dockBackground.focus();
    return document.activeElement !== window.dockBackground;
  }), "ancestor inert prevents background focus");
  for (let i = 0; i < 6; i++) {
    await page.keyboard.press("Tab");
    assert(await page.evaluate(() => !document.activeElement?.closest("[inert]")), "Tab stays outside inert controls");
  }
  const point = await page.evaluate(() => {
    document.querySelector(".settings-screen").style.pointerEvents = "none";
    const rect = window.dockBackground.getBoundingClientRect();
    return { x: rect.x + rect.width / 2, y: rect.y + rect.height / 2 };
  });
  await page.mouse.click(point.x, point.y);
  assert.equal(await page.evaluate(() => window.dockBackgroundClicks), 0, "inert prevents background interaction without relying on an overlay");
  await page.evaluate(() => { document.querySelector(".settings-screen").style.pointerEvents = ""; });
  await page.locator(".settings-screen .management-screen__back").click();
  assert(await page.evaluate(() => !window.dockBackground.closest("[inert]")));
  assert.equal(await page.evaluate(selected), "README.md", "management preserves panel state");
  await tabs.nth(0).click({ button: "right" });
  assert.equal(await page.locator('.context-menu [role="menuitem"]').count(), 3, "outer menu has only tab actions");
  await page.keyboard.press("Escape");
  await tabs.nth(0).focus();
  await page.keyboard.press("Shift+F10");
  assert.equal(await page.locator('.context-menu [role="menuitem"]').count(), 3);
  assert(await page.locator(".context-menu").evaluate(menu => {
    const r = menu.getBoundingClientRect();
    return r.left >= 0 && r.right <= innerWidth && r.top >= 0 && r.bottom <= innerHeight;
  }), "flat keyboard menu stays inside the viewport");
  await page.keyboard.press("Escape");
  await page.locator(".topicbar__chrome-btn--launcher").click();
  await page.locator(".dock-launcher").waitFor();
  await page.locator(".topicbar__chrome-btn--launcher").click();
  await page.locator(".dock-launcher").waitFor({ state: "detached" });
  await page.screenshot({ path: path.join(tmpdir(), "reasonix-dock-views-desktop.png") });
  await page.setViewportSize({ width: 960, height: 720 });
  await page.screenshot({ path: path.join(tmpdir(), "reasonix-dock-views-narrow.png") });
  assert.deepEqual(errors, []);
  console.log("PASS independent file views, close/reopen, restart, tab-only menu and management inert behavior");
} finally {
  await browser.close();
  await preview?.httpServer.close();
}
