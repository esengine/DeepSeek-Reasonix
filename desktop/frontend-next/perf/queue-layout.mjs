import { chromium } from "playwright";
import { mkdirSync } from "node:fs";
import assert from "node:assert/strict";

const url = process.env.PERF_URL ?? "http://127.0.0.1:4407/perf.html";
const shots = process.env.QUEUE_SHOTS ?? "/tmp/queue-14497-shots";
const baseline = process.env.QUEUE_BASELINE === "1";
mkdirSync(shots, { recursive: true });
const browser = await chromium.launch();
const failures = [];
for (const theme of ["light", "dark"]) {
  for (const width of [1440, 390]) {
    const context = await browser.newContext({ viewport: { width, height: 1000 }, locale: "zh-CN" });
    const page = await context.newPage();
    page.on("pageerror", (e) => failures.push(e.message));
    await page.goto(`${url}?queue=12&queuebody&queuestates&turns=8`, { waitUntil: "networkidle" });
    await page.locator(".qi").first().waitFor();
    await page.evaluate((theme) => { document.documentElement.dataset.theme = theme; }, theme);
    await page.waitForTimeout(500);
    const geometry = await page.evaluate(() => {
      const list = document.querySelector(".qitems");
      const row = document.querySelector(".qi");
      const style = getComputedStyle(row);
      return { border: style.borderTopWidth, height: row.getBoundingClientRect().height,
        scrolls: list.scrollHeight > list.clientHeight, visibleHeight: list.clientHeight, width: document.documentElement.scrollWidth,
        background: style.backgroundColor, gap: getComputedStyle(list).gap };
    });
    console.log(`${baseline ? "before" : "after"}/${theme}/${width}`, geometry);
    await page.screenshot({ path: `${shots}/${baseline ? "before" : "after"}-${theme}-${width}.png` });
    if (baseline) {
      assert.notEqual(geometry.border, "0px", "baseline must expose the standalone row border");
    } else {
      assert.equal(geometry.border, "0px");
      assert.ok(geometry.height >= 36 && geometry.height <= 40, JSON.stringify(geometry));
      assert.equal(geometry.gap, "2px");
      assert.ok(geometry.scrolls);
      assert.ok(geometry.width <= width);
      assert.equal(await page.getByRole("combobox", { name: "任务输入" }).getAttribute("placeholder"), "继续输入以排队后续修改");
      const row = page.locator(".qi").first();
      assert.equal(await row.locator(".qwhy").getAttribute("title"), "This queued instruction needs review before delivery. ".repeat(5));
      assert.ok(geometry.visibleHeight >= 180 && geometry.visibleHeight <= 208, JSON.stringify(geometry));
      await row.getByRole("button", { name: "改", exact: true }).focus();
      assert.equal(await row.getByRole("button", { name: "下移", exact: true }).evaluate((el) => getComputedStyle(el).opacity), "1");
      await page.screenshot({ path: `${shots}/focus-${theme}-${width}.png` });
      await row.getByRole("button", { name: "改", exact: true }).click();
      await page.locator(".qedit").waitFor();
      assert.ok(await page.locator(".qedit").inputValue());
      await page.screenshot({ path: `${shots}/edit-${theme}-${width}.png` });
      await page.locator(".qedit").press("Escape");
      assert.equal(await page.locator(".qedit").count(), 0);
      assert.equal(await row.getByRole("button", { name: "取回", exact: true }).getAttribute("title"), "取回");
      await page.locator(".qitems").evaluate((el) => { el.scrollTop = el.scrollHeight; });
      await page.locator(".qi").last().getByRole("button", { name: "取回", exact: true }).focus();
      assert.ok(await page.locator(".qi").last().getByRole("button", { name: "取回", exact: true }).isVisible());
    }
    await context.close();
  }
}
await browser.close();
assert.deepEqual(failures, []);
console.log("Queue layout browser matrix passed");
