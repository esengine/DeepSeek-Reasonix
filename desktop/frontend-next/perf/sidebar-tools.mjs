import assert from "node:assert/strict";
import { mkdir } from "node:fs/promises";
import { chromium } from "playwright";

const base = process.env.PERF_URL ?? "http://localhost:4399/perf.html";
const browser = await chromium.launch();
try {
  await mkdir("perf/shots", { recursive: true });
  for (const colorScheme of ["light", "dark"]) for (const width of [1440, 390]) for (const hidden of [false, true]) {
    const page = await browser.newPage({ viewport: { width, height: 900 }, colorScheme, reducedMotion: "reduce" });
    page.setDefaultTimeout(10000);
    const errors = [];
    page.on("pageerror", (error) => errors.push(error.message));
    await page.addInitScript((hide) => {
      localStorage.setItem("rx-nav-rail", "off");
      localStorage.setItem("rx-lang", "zh");
      localStorage.setItem("rx-nav-entries", JSON.stringify({ order: ["remote", "usage"], hidden: hide ? ["usage", "files", "tools", "ext", "memory", "remote"] : [] }));
    }, hidden);
    await page.goto(`${base}?pref=zh&turns=1`, { waitUntil: "networkidle" });
    if (width === 390) await page.locator('.studio-menu').click();
    const root = page.locator(".studio-tools");
    const settings = root.locator('[data-action="chrome.settings"]');
    assert.equal(await settings.innerText(), "设置");
    assert.equal(await page.locator('.studio-feedback').count(), 0, 'the retired feedback row is not drawn');
    if (hidden) {
      assert.equal(await root.locator('.studio-tools-toggle').count(), 0);
      await root.hover();
      await page.waitForTimeout(250);
      assert.equal(await page.getByRole('menu', { name: '侧栏入口' }).count(), 0);
    } else {
      await root.hover();
      const menu = page.getByRole('menu', { name: '侧栏入口' });
      await menu.waitFor();
      assert.deepEqual(await menu.locator('button > span').allTextContents(), ['远程', '用量', '文件', '工具与权限', '扩展', '记忆']);
      const item = menu.getByRole('menuitem', { name: '文件' });
      await item.hover();
      await page.waitForTimeout(220);
      assert.ok(await menu.isVisible(), 'pointer can cross the gap into the card');
      const bounds = await menu.boundingBox();
      assert.ok(bounds.x >= 0 && bounds.x + bounds.width <= width && bounds.y >= 0 && bounds.y + bounds.height <= 900);
      assert.equal(await menu.locator('button svg.lucide').count(), 6);
      await page.screenshot({ path: `perf/shots/sidebar-tools-${colorScheme}-${width}.png` });
      await page.mouse.move(width - 5, 10);
      await menu.waitFor({ state: 'hidden' });
      const toggle = root.locator('.studio-tools-toggle');
      await toggle.focus();
      await page.keyboard.press('Enter');
      await page.waitForFunction(() => document.activeElement?.getAttribute('role') === 'menuitem');
      await page.keyboard.press('End');
      assert.equal(await page.locator(':focus > span').innerText(), '记忆');
      await page.keyboard.press('Escape');
      assert.ok(await toggle.evaluate((node) => node === document.activeElement));
      await toggle.click();
      await item.click();
      await menu.waitFor({ state: 'hidden' });
      assert.equal(await page.locator('.app').getAttribute('data-browser'), 'on');
      if (width === 390) await page.locator('.studio-menu').click();
    }
    await settings.click();
    await page.locator('.prefs-screen').waitFor();
    assert.equal(await page.getByRole('menu', { name: '侧栏入口' }).count(), 0);
    assert.deepEqual(errors, []);
    console.log(`PASS sidebar ${colorScheme} ${width}px ${hidden ? 'all hidden' : 'named hover/click/keyboard card'}`);
    await page.close();
  }
} finally {
  await browser.close();
}
