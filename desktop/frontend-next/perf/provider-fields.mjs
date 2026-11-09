import assert from "node:assert/strict";
import { mkdir } from "node:fs/promises";
import { chromium } from "playwright";

const base = process.env.PERF_URL ?? "http://localhost:4399/perf.html";
const browser = await chromium.launch();
try {
  await mkdir("perf/shots", { recursive: true });
  for (const colorScheme of ["light", "dark"]) for (const language of ["zh", "en"]) for (const width of [1800, 1400, 1000, 390]) for (const scale of [1, 1.3]) {
    const page = await browser.newPage({ viewport: { width: 1400, height: 1000 }, colorScheme, reducedMotion: "reduce" });
    page.setDefaultTimeout(10000);
    const errors = [];
    page.on("pageerror", (error) => errors.push(error.message));
    page.on("requestfailed", (request) => errors.push(`${request.url()}: ${request.failure()?.errorText}`));
    await page.addInitScript((value) => localStorage.setItem("rx-lang", value), language);
    await page.goto(`${base}?pref=${language}&turns=1`, { waitUntil: "networkidle" });
    assert.deepEqual(errors, [], "the browser fixture and its styles load successfully");
    await page.locator('[data-action="chrome.settings"]').evaluate((button) => button.click());
    await page.locator('.prefs-nav [data-value="model"]').click();
    await page.getByText("myrelay", { exact: true }).first().click();
    const form = page.locator(".addp[data-edit]");
    await form.locator('.token-limits input').first().fill("1600000");
    await form.locator('[data-value="model-window"]').last().fill("1000000");
    await form.locator('[data-value="model-max-output"]').last().fill("1000000");
    await page.setViewportSize({ width, height: 1000 });
    await page.evaluate(async (value) => {
      document.documentElement.style.setProperty("--fs-scale", String(value));
      await document.fonts.ready;
    }, scale);
    const label = `${colorScheme}-${language}-${width}-${scale}`;
    const billing = form.locator('[data-value="billing-mode"]');
    const currency = form.locator('[data-value="model-price-currency"]').last();
    await currency.selectOption("USD");
    await billing.selectOption("subscription_equivalent");
    const measure = () => form.evaluate((root) => {
      const context = document.createElement("canvas").getContext("2d");
      const fields = [...root.querySelectorAll('.ml-row > .unit-field input, .ml-row > select, [data-value="billing-mode"]')];
      const clipped = fields.flatMap((field) => {
        const style = getComputedStyle(field);
        context.font = style.font;
        const text = field.tagName === "SELECT" ? field.selectedOptions[0].textContent : field.value || field.placeholder;
        const available = field.clientWidth - parseFloat(style.paddingLeft) - parseFloat(style.paddingRight);
        const needed = context.measureText(text).width;
        return needed > available + 1 ? [{ field: field.getAttribute("aria-label"), text, needed, available }] : [];
      });
      const rows = [...root.querySelectorAll(".ml-row")];
      const uneven = rows.flatMap((row) => {
        const heights = [...row.querySelectorAll(":scope > .unit-field, :scope > select")].map((field) => field.getBoundingClientRect().height);
        return Math.max(...heights) - Math.min(...heights) > 1 ? [heights] : [];
      });
      const outside = fields.filter((field) => {
        const box = field.getBoundingClientRect(), parent = field.closest(".ml-row, label").getBoundingClientRect();
        return box.right > parent.right + 1 || box.left < parent.left - 1;
      }).map((field) => field.getAttribute("aria-label"));
      for (const row of rows) {
        const controls = row.querySelectorAll(":scope > .unit-field, :scope > select");
        if (row.getBoundingClientRect().right - controls[controls.length - 1].getBoundingClientRect().right > 1) outside.push("unfilled row");
      }
      return { clipped, uneven, outside };
    });
    const selected = await measure();
    await form.locator(".ml-group").first().scrollIntoViewIfNeeded();
    await page.screenshot({ path: `perf/shots/provider-fields-${label}.png` });
    assert.deepEqual(selected, { clipped: [], uneven: [], outside: [] }, `${label}: values, inherited hints and the full billing choice fit aligned controls`);
    await billing.selectOption("payg");
    assert.deepEqual(await measure(), { clipped: [], uneven: [], outside: [] }, `${label}: the default billing choice fits too`);
    await currency.focus();
    await page.keyboard.press("Space");
    await page.waitForFunction(() => document.querySelector('[data-value="model-price-currency"]:open'));
    await page.keyboard.press("Enter");
    assert.equal(await currency.inputValue(), "USD", `${label}: keyboard confirmation preserves the currency`);
    await form.locator('[data-value="model-window"]').last().fill("");
    assert.deepEqual(await measure(), { clipped: [], uneven: [], outside: [] }, `${label}: clearing a value reveals its complete inheritance hint`);
    await form.locator('.token-limits input').first().fill("");
    assert.deepEqual(await measure(), { clipped: [], uneven: [], outside: [] }, `${label}: an undeclared shared window also fits`);
    await billing.scrollIntoViewIfNeeded();
    await page.screenshot({ path: `perf/shots/provider-fields-prices-${label}.png` });
    assert.deepEqual(errors, []);
    await page.close();
    console.log(`PASS provider fields ${label}: text fit, aligned heights, currency, billing and inheritance`);
  }
} finally {
  await browser.close();
}
