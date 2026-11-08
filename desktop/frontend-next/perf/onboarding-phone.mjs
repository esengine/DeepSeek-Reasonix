import assert from "node:assert/strict";
import { chromium } from "playwright";

const browser = await chromium.launch();
const base = process.env.PERF_URL ?? "http://127.0.0.1:4399/perf.html";
try {
  for (const language of ["zh", "en"]) {
    for (const [width, height] of [[320, 640], [390, 844], [430, 932], [782, 390], [1044, 884]]) {
      const page = await browser.newPage({ viewport: { width, height }, hasTouch: width < 900, reducedMotion: "reduce" });
      const url = new URL(base);
      url.searchParams.set("onboarding", "1");
      url.searchParams.set("pref", language);
      await page.goto(url.href);
      await page.locator('[data-action="onboarding.begin"]').click();
      await page.evaluate(() => document.fonts.ready);
      const check = async (step) => {
        const clipped = await page.locator(".onb").evaluate((root) => {
          const shell = root.parentElement.getBoundingClientRect();
          return [...root.querySelectorAll("input, button, h1, .onb-steps, .onb-field, .onb-model")]
            .filter((el) => el.checkVisibility()).flatMap((el) => {
              const r = el.getBoundingClientRect();
              return r.left < shell.left - 1 || r.right > shell.right + 1 || (el.tagName !== "INPUT" && el.scrollWidth > el.clientWidth + 2)
                ? [{ tag: el.tagName, class: el.className, left: r.left, right: r.right, overflow: el.scrollWidth - el.clientWidth }] : [];
            });
        });
        assert.deepEqual(clipped, [], `${language} ${width}x${height} ${step}: ${JSON.stringify(clipped)}`);
      };
      await check("connect");
      await page.locator("#onb-key").fill("sk-layout-test");
      if (width < 600) {
        await page.setViewportSize({ width, height: 360 });
        await check("keyboard");
      }
      await page.locator(".onb-acts .onb-go").click();
      await page.locator('.onb-card[data-step="model"]').waitFor();
      await check("model");
      await page.setViewportSize({ width, height });
      await page.locator('[data-action="onboarding.back"]').click();
      assert.equal(await page.locator("#onb-key").inputValue(), "sk-layout-test");
      await page.locator('[data-action="onboarding.service"]').last().click();
      await page.locator("#onb-url").fill("https://relay.example/a/long/service/address/v1");
      await check("custom service");
      await page.locator(".onb-acts .onb-go").click();
      await page.locator('.onb-card[data-step="model"]').waitFor();
      await page.locator('[data-action="onboarding.start"]').click();
      await page.locator('.onb-card[data-step="project"]').waitFor();
      await check("project");
      if (width < 900) {
        assert.equal(await page.locator('[data-action="onboarding.start"]').isEnabled(), true);
        assert.equal(await page.getByText(language === "zh" ? "把文件夹拖到这里" : "Drop a folder here").count(), 0);
      }
      await page.close();
    }
  }
  console.log("Onboarding: both languages, phone, keyboard, landscape and desktop passed.");
} finally {
  await browser.close();
}
