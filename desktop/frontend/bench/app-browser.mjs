#!/usr/bin/env node

import path from "node:path";
import { fileURLToPath } from "node:url";
import { chromium } from "playwright";
import { startPreviewServer } from "./vite-preview-server.mjs";

const frontendDir = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
process.env.PLAYWRIGHT_BROWSERS_PATH ||= path.join(frontendDir, ".pw-browsers");
const port = Number(process.env.REASONIX_APP_BROWSER_PORT ?? 4657);
const preview = await startPreviewServer(frontendDir, port);
const browser = await chromium.launch({ headless: true });

function assert(condition, message) {
  if (!condition) throw new Error(message);
  process.stdout.write(`  PASS ${message}\n`);
}

async function settle(page, frames = 5) {
  await page.evaluate((count) => new Promise((resolve) => {
    const tick = () => --count <= 0 ? resolve() : requestAnimationFrame(tick);
    requestAnimationFrame(tick);
  }), frames);
}

async function openSettings(page) {
  await page.locator("button:has(svg.lucide-settings)").last().click();
  await page.locator(".settings-modal").waitFor();
}

async function chooseLayout(page, label, className) {
  await openSettings(page);
  await page.locator(".settings-modal .set-seg__btn").filter({ hasText: new RegExp(`^${label}$`) }).click();
  await page.locator(`.app.${className}`).waitFor();
  await page.locator(".settings-modal .modal-close-button").click();
  await page.locator(".settings-modal").waitFor({ state: "detached" });
  await settle(page);
}

try {
  const page = await browser.newPage({ viewport: { width: 1440, height: 1000 } });
  const pageErrors = [];
  page.on("pageerror", (error) => pageErrors.push(error.message));
  await page.goto(`http://127.0.0.1:${port}/?mock=bench&bench=1&app-lifecycle-probe=1`, { waitUntil: "domcontentloaded" });
  await page.locator("textarea.composer__input:not([aria-hidden=true])").waitFor();
  await page.locator(".project-tree").waitFor();
  await page.evaluate(() => {
    window.__appBrowserIdentity = {
      composer: document.querySelector("textarea.composer__input:not([aria-hidden=true])"),
      projectTree: document.querySelector(".project-tree"),
      sidebar: document.querySelector(".sidebar"),
    };
  });
  const composer = page.locator("textarea.composer__input:not([aria-hidden=true])");
  await composer.fill("layout-owned draft");

  assert(await page.locator(".app.app--workbench").count() === 1, "workbench layout renders from the authoritative startup snapshot");
  await chooseLayout(page, "Creation", "app--creation");
  assert(await composer.inputValue() === "layout-owned draft", "creation layout preserves the Composer draft and mount");
  await chooseLayout(page, "Workbench", "app--workbench");
  assert(await composer.inputValue() === "layout-owned draft", "workbench layout preserves the Composer draft and mount");

  const identities = await page.evaluate(() => ({
    composer: window.__appBrowserIdentity.composer === document.querySelector("textarea.composer__input:not([aria-hidden=true])"),
    projectTree: window.__appBrowserIdentity.projectTree === document.querySelector(".project-tree"),
    sidebar: window.__appBrowserIdentity.sidebar === document.querySelector(".sidebar"),
  }));
  assert(Object.values(identities).every(Boolean), "layout variants reuse the shared sidebar, project tree, and Composer DOM identity");

  const terminalToggle = page.getByRole("button", { name: "Terminal", exact: true }).first();
  await terminalToggle.click();
  await page.locator('.terminal-drawer[aria-hidden="false"]').waitFor();
  assert(await page.locator('.terminal-drawer-resizer[tabindex="0"]').count() === 1, "open terminal drawer exposes one keyboard resizer");
  assert(await page.locator(".footer.footer--compact").count() === 1, "open terminal compacts the shared footer without remounting Composer");
  assert(await composer.inputValue() === "layout-owned draft", "terminal drawer lifecycle preserves the Composer draft");
  await terminalToggle.click();
  await page.locator('.terminal-drawer[aria-hidden="true"][inert]').waitFor();
  assert(await page.locator('.terminal-drawer-resizer[tabindex="-1"]').count() === 1, "closed warm terminal is inert and leaves keyboard navigation");

  await page.locator('.project-tree__topic-main:has-text("bench:geometry")').click();
  await page.waitForFunction(() => document.querySelector(".transcript")?.textContent?.includes("Geometry contract fixture complete."));
  await page.locator('.project-tree__topic-main:has-text("bench:small-6t")').click();
  await page.waitForFunction(() => document.querySelector(".transcript")?.textContent?.includes("ASYNC LAYOUT EXPANSION COMPLETE"));
  const afterSwitch = await page.evaluate(() => ({
    projectTree: window.__appBrowserIdentity.projectTree === document.querySelector(".project-tree"),
    subscriptions: window.__reasonixAppLifecycle?.snapshot().activeSubscriptions,
    operations: window.__reasonixAppLifecycle?.snapshot().activeOperations,
  }));
  assert(afterSwitch.projectTree, "same-project session switching preserves the Sidebar project tree (not WorkspacePanel)");
  assert(afterSwitch.subscriptions === 6, `the six AppRuntimeEffects subscriptions remain singular (${afterSwitch.subscriptions})`);
  assert(afterSwitch.operations === 0, "instrumented operation owners report zero active operations (not yet all App operations)");

  await page.locator('.project-tree__folder-main:has(svg.lucide-cloud)').click();
  await page.locator('.project-tree__topic-main:has-text("Remote demo session")').click();
  await page.locator('.remote-surface--ready').waitFor();
  await page.waitForFunction(() => document.querySelector('textarea.composer__input:not([aria-hidden=true])')?.disabled === false);
  assert((await page.locator('.topicbar').textContent()).includes('Remote demo session'), "remote project selection adopts its source workspace and authoritative hydrated surface");
  await page.locator('.sidebar__quick-action').click();
  await page.waitForFunction(() => document.querySelector('.topicbar')?.textContent?.includes('New session'));
  await page.locator('.remote-surface--ready').waitFor();
  await page.waitForFunction(() => document.querySelector('textarea.composer__input:not([aria-hidden=true])')?.disabled === false);
  assert(await page.locator('.remote-surface').count() === 1, "global New Session stays on the remote workspace instead of opening a local blank");
  assert(await page.evaluate(() => window.__appBrowserIdentity.composer === document.querySelector('textarea.composer__input:not([aria-hidden=true])')), "local/remote navigation and remote New Session preserve the Composer DOM identity");
  await page.locator('.project-tree__topic-main:has-text("bench:geometry")').click();
  await page.waitForFunction(() => document.querySelector('.transcript')?.textContent?.includes('Geometry contract fixture complete.'));
  assert(await page.locator('.remote-surface').count() === 0, "subsequent local navigation owns the surface; remote events do not reclaim it");
  assert(pageErrors.length === 0, `three-layout replay emits no page errors (${pageErrors.length})`);

  const classicPage = await browser.newPage({ viewport: { width: 1440, height: 1000 } });
  const classicErrors = [];
  classicPage.on("pageerror", (error) => classicErrors.push(error.message));
  await classicPage.goto(`http://127.0.0.1:${port}/?mock=bench&bench=1&layout=classic`, { waitUntil: "domcontentloaded" });
  await classicPage.locator(".app").waitFor();
  process.stdout.write(`  INFO classic fixture class: ${await classicPage.locator(".app").getAttribute("class")}\n`);
  await classicPage.locator(".app.app--classic textarea.composer__input:not([aria-hidden=true])").waitFor();
  await classicPage.locator(".app.app--classic .project-tree").waitFor();
  assert(classicErrors.length === 0, "classic compatibility snapshot renders the shared Composer and project tree without errors");
  await classicPage.close();
  process.stdout.write("app browser lifecycle gate passed\n");
} finally {
  await browser.close();
  await preview.close();
}
