import assert from "node:assert/strict";
import { mkdir } from "node:fs/promises";
import { chromium } from "playwright";

const PAGE = new URL(process.env.PERF_URL ?? "http://localhost:4399/perf.html");
PAGE.searchParams.set("modelhealth", "1");
const browser = await chromium.launch();
try {
  await mkdir("perf/shots", { recursive: true });
  for (const lang of ["zh", "en"]) for (const width of [1300, 760, 390]) for (const scale of [1, 1.3]) {
    const page = await browser.newPage({ viewport: { width, height: 900 }, colorScheme: lang === "zh" ? "light" : "dark" });
    const errors = [];
    page.on("pageerror", (error) => errors.push(error.message));
    await page.addInitScript((language) => localStorage.setItem("rx-lang", language), lang);
    await page.goto(PAGE.href, { waitUntil: "networkidle" });
    await page.waitForSelector(".model-picker");
    await page.evaluate((value) => document.documentElement.style.setProperty("--fs-scale", String(value)), scale);
    await page.locator(".model-picker").first().click();
    const menu = page.locator(".studio-model-menu:not([hidden])");
    await menu.locator('.menu-item-status[data-tone="positive"]').first().waitFor();
    const effortWrapped = await menu.locator(".menu-aside .studio-item-line").evaluateAll((lines) => lines.some((line) => {
      const meta = line.querySelector(".studio-item-meta");
      return meta && meta.getBoundingClientRect().top >= line.querySelector(".lb").getBoundingClientRect().bottom;
    }));
    assert.ok(!effortWrapped, `${lang}/${width}/${scale}: reasoning choices keep their label and tier together`);
    const vision = menu.locator(".menu-rows button.mi:has(.studio-item-badge)").first();
    const visionRef = await vision.getAttribute("data-value");
    await vision.click();
    await page.waitForFunction((value) => document.querySelector('.studio-model-menu:not([hidden]) button[data-on]')?.dataset.value === value, visionRef);
    await page.keyboard.press("Escape");
    await page.locator(".model-picker").first().click();
    await menu.locator('.menu-item-status[data-tone="positive"]').first().waitFor();
    await menu.evaluate((element) => {
      for (const line of element.querySelectorAll(".menu-rows .studio-item-line")) {
        line.querySelector(".lb").textContent = "deepseek-v4.1-flash-preview-extended";
      }
      for (const head of element.querySelectorAll(".menu-rows .mi.head")) {
        head.querySelector(".lb").textContent = "Shanghai_AI_Lab";
        head.querySelector(".rt").textContent = "discovery-api.intern-ai.org.cn/compatible-mode/v1";
      }
    });
    const measured = await menu.evaluate((element) => {
      const rows = element.querySelector(".menu-rows");
      const sizes = [...rows.querySelectorAll(".studio-item-line")].map((line) => {
        const label = line.querySelector(".lb");
        const badge = line.querySelector(".studio-item-badge");
        const status = line.closest("button").querySelector(".menu-item-status");
        const labelBox = label.getBoundingClientRect();
        const badgeBox = badge?.getBoundingClientRect();
        return {
          labelWidth: labelBox.width,
          badgeHeight: badgeBox?.height,
          badgeLine: badge ? parseFloat(getComputedStyle(badge).lineHeight) + 4 : 0,
          badgeClipped: badge ? badge.scrollWidth > badge.clientWidth + 1 : false,
          badgeOverlap: badgeBox ? badgeBox.right > status.getBoundingClientRect().left : false,
        };
      });
      return { sizes, overflow: rows.scrollWidth - rows.clientWidth, menuOverflow: element.scrollWidth - element.clientWidth };
    });
    const tag = `${lang}/${width}/${scale}`;
    await menu.screenshot({ path: `perf/shots/model-menu-${lang}-${width}-${scale}.png` });
    assert.ok(measured.sizes.some((size) => size.badgeHeight), `${tag}: a model reads images`);
    for (const size of measured.sizes) {
      assert.ok(size.labelWidth >= 96 * scale, `${tag}: the model name remains readable (${JSON.stringify(size)})`);
      if (size.badgeHeight) assert.ok(size.badgeHeight <= size.badgeLine + 1 && !size.badgeClipped && !size.badgeOverlap, `${tag}: the image badge stays whole (${JSON.stringify(size)})`);
    }
    assert.ok(measured.overflow <= 1 && measured.menuOverflow <= 1, `${tag}: long endpoints do not create horizontal scrolling (${JSON.stringify(measured)})`);
    await menu.locator(".mfind").fill("kimi");
    const choice = menu.locator(".menu-rows button.mi:not(.plain)").first();
    const picked = await choice.getAttribute("data-value");
    assert.ok(picked?.includes("kimi"), `${tag}: search still finds the model`);
    await choice.click();
    await page.waitForFunction((value) => document.querySelector('.studio-model-menu:not([hidden]) button[data-on]')?.dataset.value === value, picked);
    await page.keyboard.press("Escape");
    await menu.waitFor({ state: "hidden" });
    await page.locator(".model-picker").first().click();
    await page.waitForFunction((value) => document.querySelector('.studio-model-menu:not([hidden]) button[data-on]')?.dataset.value === value, picked);
    assert.equal(await menu.locator(".mfind").inputValue(), "", `${tag}: reopening clears the search`);
    await page.keyboard.press("Escape");
    await menu.waitFor({ state: "hidden" });
    assert.deepEqual(errors, []);
    console.log(`PASS model menu ${tag}: names, badges, connection status and endpoints fit`);
    await page.close();
  }
} finally {
  await browser.close();
}
