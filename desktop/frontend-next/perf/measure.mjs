import assert from "node:assert/strict";
import { chromium } from "playwright";

const browser = await chromium.launch();
try {
  const page = await browser.newPage({ viewport: { width: 2400, height: 1200 }, locale: "en-US" });
  const errors = [];
  page.on("pageerror", (error) => errors.push(error.message));
  const url = new URL(process.env.PERF_URL ?? "http://localhost:4399/perf.html");
  url.searchParams.set("pref", "en");
  await page.goto(url.href, { waitUntil: "networkidle" });
  await page.locator(".compose textarea").waitFor();
  await page.evaluate(() => {
    window.__feed({ kind: "turn_started" });
    window.__feed({ kind: "text", text: "A populated conversation must remain readable when a custom width meets a narrow window." });
    window.__feed({ kind: "message" });
    window.__feed({ kind: "turn_done" });
  });
  await page.locator(".flow .out .txt").waitFor();
  await page.getByRole("button", { name: "Settings", exact: true }).click();
  await page.locator("#prefs-appearance").click();
  await page.getByRole("button", { name: "Custom percentage", exact: true }).click();
  const slider = page.getByRole("slider", { name: "Conversation width", exact: true });
  await slider.fill("67");
  await page.getByRole("button", { name: "Full width", exact: true }).click();
  await page.getByRole("button", { name: "Custom percentage", exact: true }).click();
  assert.equal(await slider.inputValue(), "67", "preset switching retains the custom percentage");
  await page.getByRole("button", { name: "Close Esc", exact: true }).click();
  await page.waitForFunction(() => {
    const pane = document.querySelector(".pane").clientWidth;
    return Math.abs(document.querySelector(".compose").getBoundingClientRect().width / pane - 0.67) < 0.01;
  });
  for (const selector of [".flow", ".compose"]) {
    const ratio = await page.locator(selector).evaluate((el) => el.getBoundingClientRect().width / document.querySelector(".pane").clientWidth);
    assert.ok(Math.abs(ratio - 0.67) < 0.01, `${selector} uses 67% of the conversation pane: ${ratio}`);
  }
  for (const [name, wideFlow, wideComposer] of [
    ["Full width", true, false], ["Full width, composer too", true, true], ["Reading", false, false],
  ]) {
    await page.getByRole("button", { name: "Settings", exact: true }).click();
    await page.locator("#prefs-appearance").click();
    await page.getByRole("button", { name, exact: true }).click();
    await page.getByRole("button", { name: "Close Esc", exact: true }).click();
    await page.waitForFunction(([flow, composer]) => {
      const fits = (selector, wide) => {
        const width = document.querySelector(selector).getBoundingClientRect().width;
        return wide ? width > 1800 : Math.abs(width - 760) < 1;
      };
      return fits(".flow", flow) && fits(".compose", composer);
    }, [wideFlow, wideComposer]);
  }
  console.log("PASS existing presets: reading, wide transcript and wide composer keep their geometry");
  console.log("PASS 67%: transcript and composer resize together; presets retain the value");

  await page.getByRole("button", { name: "Settings", exact: true }).click();
  await page.locator("#prefs-appearance").click();
  await page.getByRole("button", { name: "Custom percentage", exact: true }).click();
  await slider.fill("30");
  await page.getByRole("button", { name: "Close Esc", exact: true }).click();
  await page.setViewportSize({ width: 1366, height: 900 });
  await page.waitForFunction(() => document.querySelector(".compose").getBoundingClientRect().width < 350);
  const outside = await page.locator(".compose").evaluate((el) => {
    const box = el.getBoundingClientRect();
    return [...el.querySelectorAll("button")].filter((button) => {
      const rect = button.getBoundingClientRect();
      return button.checkVisibility({ checkOpacity: true, checkVisibilityCSS: true })
        && rect.width > 0 && (rect.right > box.right + 1 || rect.left < box.left - 1);
    }).map((el) => el.textContent);
  });
  assert.deepEqual(outside, [], "toolbar actions stay within a 30% composer on a laptop");
  const workspace = page.locator(".compose .worktree-trigger").first();
  await workspace.click();
  assert.equal(await workspace.getAttribute("aria-expanded"), "true", "workspace configuration remains accessible in a 30% composer");
  await page.keyboard.press("Escape");
  console.log("PASS 30%: composer actions remain contained on a laptop");

  await page.setViewportSize({ width: 390, height: 844 });
  await page.waitForFunction(() => document.querySelector(".flow").getBoundingClientRect().width > 300);
  for (const selector of [".flow", ".compose"]) {
    const width = await page.locator(selector).evaluate((el) => el.getBoundingClientRect().width);
    assert.ok(width > 300 && width <= 390, `${selector} uses the phone layout rather than 30%: ${width}`);
  }
  assert.equal(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), true, "phone has no horizontal page overflow");
  await workspace.click();
  assert.equal(await workspace.getAttribute("aria-expanded"), "true", "workspace configuration remains accessible on a phone");
  await page.keyboard.press("Escape");
  assert.deepEqual(errors, []);
  console.log("PASS phone: populated conversation and composer use the available width");
} finally {
  await browser.close();
}
