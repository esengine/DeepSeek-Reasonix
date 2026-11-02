import { chromium } from "playwright";
import { mkdirSync } from "node:fs";

const url = process.env.PERF_URL ?? "http://localhost:4399/perf.html";
const shots = process.env.SHOTS ?? "/tmp/reasonix-14759-visibility";
mkdirSync(shots, { recursive: true });
const browser = await chromium.launch();
const page = await browser.newPage({ locale: "zh-CN" });
const errors = [];
page.on("pageerror", (error) => errors.push(error.message));
const box = page.locator(".compose textarea");
function check(ok, message) {
  if (!ok) throw new Error(message);
}
async function stable() {
  await box.waitFor();
  await page.addStyleTag({ content: "*, *::before, *::after { transition: none !important; animation: none !important; }" });
  await page.evaluate(() => document.fonts.ready);
}
async function hint(on, shot) {
  const settings = page.locator('[data-action="chrome.settings"]');
  const menu = page.locator("button.studio-menu");
  if (await menu.isVisible() && await menu.getAttribute("aria-pressed") !== "true") await menu.click();
  await settings.click();
  await page.locator('.prefs-sheet input[data-action-keydown="settings.search"]').fill("placeholder");
  await page.locator('[role="option"][data-target="composer-hint"]').click();
  const section = page.locator('[data-setting="composer-hint"]');
  await section.locator(`[data-value="${on ? "on" : "off"}"]`).click();
  check(await section.locator(`[data-value="${on ? "on" : "off"}"]`).getAttribute("aria-pressed") === "true", "Setting must reflect the selected visibility");
  check(await section.locator('[data-apply="immediate"]').count() === 1, "Visibility must be immediate");
  if (shot) {
    await section.evaluate((el) => el.scrollIntoView({ block: "center" }));
    await page.screenshot({ path: `${shots}/${shot}.png` });
  }
  await page.locator('button[data-action="settings.close"]').click();
}
async function visible(on) {
  await page.waitForFunction((on) => Boolean(document.querySelector(".compose textarea")?.placeholder) === on, on);
  check(Boolean(await box.getAttribute("aria-label")), "Hidden hint must retain the accessible input label");
  check(Boolean(await box.getAttribute("aria-describedby")), "Input instructions must remain associated");
}
try {
  for (const width of [1440, 390]) {
    await page.setViewportSize({ width, height: 900 });
    for (const theme of ["light", "dark"]) {
      await page.goto(url, { waitUntil: "networkidle" });
      await stable();
      if (await page.locator("html").getAttribute("data-theme") !== theme) await page.locator('[data-action="appearance.theme"]').click();
      await visible(true);
      check(await box.evaluate((el) => getComputedStyle(el, "::placeholder").opacity) === "1", "Visible guidance must retain full theme opacity");
      await page.screenshot({ path: `${shots}/${theme}-${width}-on.png` });
      await hint(false, `${theme}-${width}-settings-off`);
      await visible(false);
      await page.screenshot({ path: `${shots}/${theme}-${width}-off.png` });
      await page.reload({ waitUntil: "networkidle" });
      await stable();
      await visible(false);
      await page.screenshot({ path: `${shots}/${theme}-${width}-off-reloaded.png` });
      await box.fill("preserved draft");
      const ink = await box.evaluate((el) => ({ color: getComputedStyle(el).color, opacity: getComputedStyle(el).opacity }));
      check(ink.opacity === "1", "Entered text must retain full opacity");
      await hint(true);
      await visible(true);
      check(await box.inputValue() === "preserved draft", "Changing hint must not alter the draft");
      check(await box.evaluate((el) => getComputedStyle(el).color) === ink.color, "Changing hint must not alter entered ink");
      await box.fill("");
      await page.reload({ waitUntil: "networkidle" });
      await stable();
      await visible(true);
      console.log(`PASS: ${theme}/${width} default on, Appearance off/on, draft and reload persistence`);
    }
  }
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.reload({ waitUntil: "networkidle" });
  await stable();
  await hint(false);
  await box.fill("visibility smoke input");
  check(await box.evaluate((el) => document.activeElement === el), "Typing must focus the composer");
  await box.evaluate((el) => el.dispatchEvent(new CompositionEvent("compositionstart", { bubbles: true })));
  await box.press("Enter");
  check(await page.evaluate(() => !window.__sent?.length), "IME Enter must not submit");
  await box.evaluate((el) => el.dispatchEvent(new CompositionEvent("compositionend", { bubbles: true })));
  await page.waitForTimeout(120);
  await box.press("Enter");
  await page.waitForFunction(() => window.__sent?.includes("visibility smoke input"));
  await page.goto(`${url}?queue=3&running=1`, { waitUntil: "networkidle" });
  await stable();
  await page.locator(".queue .qi").first().waitFor();
  await visible(false);
  check(await page.locator(".queue .qi").count() === 3, "Hiding hint must retain queued content");
  await hint(true);
  await visible(true);
  check(await page.locator(".queue .qi").count() === 3, "Showing hint must retain queued content");
  await page.screenshot({ path: `${shots}/queue-hint-on.png` });
  check(errors.length === 0, errors.join("\n"));
  console.log(`PASS: accessible label, entered ink, focus, IME, submit and queue; screenshots: ${shots}`);
} finally {
  await browser.close();
}
