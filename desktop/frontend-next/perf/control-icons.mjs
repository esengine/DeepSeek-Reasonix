import assert from "node:assert/strict";
import { mkdir } from "node:fs/promises";
import { chromium } from "playwright";

const base = new URL(process.env.PERF_URL ?? "http://localhost:4399/perf.html");
const browser = await chromium.launch();
const measure = (buttons) => buttons.map((button) => {
  const icon = button.querySelector("svg");
  if (!icon) return { missing: true };
  const box = button.getBoundingClientRect();
  const svg = icon.getBoundingClientRect();
  const glyph = icon.getBBox();
  const unit = svg.width / icon.viewBox.baseVal.width;
  return {
    missing: false,
    glyphWidth: glyph.width * unit,
    glyphHeight: glyph.height * unit,
    offsetX: Math.abs(svg.x + svg.width / 2 - box.x - box.width / 2),
    offsetY: Math.abs(svg.y + svg.height / 2 - box.y - box.height / 2),
    fits: svg.left >= box.left && svg.right <= box.right && svg.top >= box.top && svg.bottom <= box.bottom,
    accessible: !!button.getAttribute("aria-label") && icon.getAttribute("aria-hidden") === "true",
  };
});
const check = (sizes, label, centered = true) => {
  assert.ok(sizes.length > 0, `${label}: controls exist`);
  for (const size of sizes) {
    assert.ok(!size.missing && size.accessible && size.fits, `${label}: named SVG controls fit (${JSON.stringify(size)})`);
    assert.ok(size.glyphWidth >= 9 && size.glyphHeight >= 9, `${label}: symbols remain legible (${JSON.stringify(size)})`);
    if (centered) assert.ok(size.offsetX <= 1 && size.offsetY <= 1, `${label}: symbols are centered (${JSON.stringify(size)})`);
  }
};
try {
  await mkdir("perf/shots", { recursive: true });
  for (const colorScheme of ["light", "dark"]) for (const width of [1300, 390]) for (const scale of [1, 1.3]) {
    const page = await browser.newPage({ viewport: { width, height: 900 }, colorScheme, reducedMotion: "reduce" });
    const errors = [];
    page.on("pageerror", (error) => errors.push(error.message));
    await page.addInitScript(() => localStorage.setItem("rx-lang", "zh"));
    const url = new URL(base);
    url.searchParams.set("running", "1");
    await page.goto(url.href, { waitUntil: "networkidle" });
    await page.locator('.compose [data-action="session.stop"]').waitFor();
    await page.evaluate((value) => document.documentElement.style.setProperty("--fs-scale", String(value)), scale);
    const actions = page.locator(".compose .go");
    const send = actions.locator('[data-action="session.send"]');
    const stop = actions.locator('[data-action="session.stop"]');
    const label = `${colorScheme}/${width}/${scale}`;
    assert.equal(await send.isDisabled(), true, `${label}: an empty running draft cannot send`);
    const disabledBackground = await send.evaluate((button) => getComputedStyle(button).backgroundColor);
    check(await actions.locator("button").evaluateAll(measure), label);
    await actions.screenshot({ path: `perf/shots/control-icons-${colorScheme}-${width}-${scale}.png` });
    await page.locator(".compose textarea").fill("继续检查");
    assert.equal(await send.isEnabled(), true, `${label}: a running draft can steer`);
    await send.evaluate(async (button) => {
      getComputedStyle(button).backgroundColor;
      await Promise.all(button.getAnimations().map((animation) => animation.finished.catch(() => {})));
    });
    assert.notEqual(await send.evaluate((button) => getComputedStyle(button).backgroundColor), disabledBackground, `${label}: a disabled send looks inactive`);
    check(await send.evaluateAll(measure), `${label}/steer`, false);
    await stop.focus();
    assert.ok(await stop.evaluate((button) => parseFloat(getComputedStyle(button).outlineWidth) >= 2), `${label}: keyboard focus is visible`);
    await stop.click();
    await page.waitForFunction(() => document.querySelector('[data-action="session.stop"]')?.getAttribute("aria-busy") === "true");
    assert.equal(await stop.isDisabled(), true, `${label}: a pending stop cannot be repeated`);
    check(await stop.evaluateAll(measure), `${label}/stopping`);
    assert.deepEqual(errors, []);
    await page.close();
    console.log(`PASS composer icons ${label}: legibility, alignment, focus and running states`);
  }
  for (const colorScheme of ["light", "dark"]) {
    const page = await browser.newPage({ viewport: { width: 1300, height: 900 }, colorScheme, reducedMotion: "reduce" });
    const errors = [];
    page.on("pageerror", (error) => errors.push(error.message));
    await page.addInitScript(() => {
      localStorage.setItem("rx-lang", "zh"); localStorage.setItem("rx-nav-rail", "on");
      window.reasonixHost = {
        shell: "electron", platform: "windows", titleBar: false,
        isWindowMaximised: async () => false, setUnreadBadge: async () => {},
        showBrowserView: async () => {}, hideBrowserView: async () => {}, freezeBrowserView: async () => "",
        controlBrowserView: async () => {}, navigateBrowserView: async () => "", onBrowserLoadState: () => () => {},
      };
    });
    const url = new URL(base);
    url.searchParams.set("pref", "zh");
    url.searchParams.set("ws", "2");
    url.searchParams.set("sess", "6");
    url.searchParams.set("agentpage", "web");
    await page.goto(url.href, { waitUntil: "networkidle" });
    assert.deepEqual(errors, []);
    await page.locator('.navbtn[aria-label="文件"]').first().click();
    await page.locator('[data-action-click="workbench.tab"][data-target="browser:view-a"]').click();
    const buttons = page.locator(".bnav .bbtn");
    await buttons.first().waitFor();
    assert.equal(await buttons.count(), 4, "the browser exposes four navigation actions");
    check(await buttons.evaluateAll(measure), `${colorScheme}/browser`);
    for (const button of await buttons.all()) assert.equal(await button.getAttribute("title"), await button.getAttribute("aria-label"));
    await page.locator(".bnav").screenshot({ path: `perf/shots/control-icons-browser-${colorScheme}.png` });
    assert.deepEqual(errors, []);
    await page.close();
    console.log(`PASS browser icons ${colorScheme}: legibility, alignment and action labels`);
  }
  for (const colorScheme of ["light", "dark"]) for (const width of [1300, 390]) {
    const page = await browser.newPage({ viewport: { width: 1300, height: 900 }, colorScheme, reducedMotion: "reduce" });
    const errors = [];
    page.on("pageerror", (error) => errors.push(error.message));
    await page.goto(`${base}?pref=zh&ws=2&sess=6&turns=2`, { waitUntil: "networkidle" });
    const label = `${colorScheme}/${width}/library`;
    const toolIcons = page.locator(".sym svg");
    assert.ok(await toolIcons.count() > 0, `${label}: transcript icons exist`);
    assert.ok(await toolIcons.evaluateAll((icons) => icons.every((icon) => icon.classList.contains("lucide"))), `${label}: tool cards use library icons`);
    await page.locator('.model-picker').click();
    assert.ok(await page.locator(".studio-model-menu .mi-mono .lucide-server").count() > 0, `${label}: custom providers use service icons`);
    assert.equal(await page.locator(".studio-model-menu .mi-mono").first().textContent(), "");
    assert.ok(await page.locator(".studio-model-menu .menu-current.lucide-check").count() > 0, `${label}: selection uses a library check`);
    await page.keyboard.press("Escape");
    await page.locator('[data-action="chrome.settings"]').evaluate((button) => button.click());
    await page.locator('.prefs-nav [data-value="controls"]').click();
    await page.setViewportSize({ width, height: 900 });
    const order = page.locator('[data-action="appearance.nav-entry-move"]');
    await order.first().scrollIntoViewIfNeeded();
    check(await order.evaluateAll(measure), `${label}/reorder`);
    const down = order.locator('.lucide-arrow-down').first().locator('..');
    const value = await down.getAttribute("data-value");
    await down.focus();
    await page.keyboard.press("Enter");
    const moved = page.locator(`[data-action="appearance.nav-entry-move"][data-value="${value.replace(/:down$/, ":up")}"]`);
    assert.equal(await moved.isEnabled(), true, `${label}: reordering remains keyboard operable`);
    await page.locator("#set-naventries").screenshot({ path: `perf/shots/control-icons-library-${colorScheme}-${width}.png` });
    assert.deepEqual(errors, []);
    await page.close();
    console.log(`PASS semantic icons ${label}: tools, providers, selection and keyboard reordering`);
  }
} finally {
  await browser.close();
}
