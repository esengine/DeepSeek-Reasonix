import assert from "node:assert/strict";
import { mkdir } from "node:fs/promises";
import { join } from "node:path";
import { chromium } from "playwright";

const base = process.env.PERF_URL ?? "http://localhost:4399/perf.html";
const shots = process.env.IMAGE_PREVIEW_SHOTS;
const before = process.env.IMAGE_PREVIEW_BEFORE === "1";
const svg = '<svg xmlns="http://www.w3.org/2000/svg" width="1600" height="1000" viewBox="0 0 1600 1000"><rect width="1600" height="1000" fill="#102a43"/><rect x="80" y="80" width="1440" height="840" rx="32" fill="#173f5f"/><text x="140" y="220" font-family="sans-serif" font-size="72" fill="#f0f4f8">Original image · 1600 × 1000</text><path d="M150 750 450 550 700 660 1000 330 1450 440" fill="none" stroke="#67e8c0" stroke-width="20"/><text x="140" y="860" font-family="sans-serif" font-size="38" fill="#bcccdc">Reasonix Studio image preview fixture</text></svg>';
const path = ".reasonix/attachments/preview.svg";
const data = `data:image/svg+xml;base64,${Buffer.from(svg).toString("base64")}`;
if (shots) await mkdir(shots, { recursive: true });
const browser = await chromium.launch();
try {
  for (const theme of ["light", "dark"]) {
    for (const [width, height, zoom] of [[1100, 760, 1], [390, 844, 1], [1100, 760, 1.5]]) {
      if (before && zoom !== 1) continue;
      const page = await browser.newPage({ viewport: { width, height }, colorScheme: theme });
      const errors = [];
      page.on("pageerror", error => errors.push(error.message));
      await page.route("**/fixture/**", route => route.fulfill({ contentType: "image/svg+xml", body: svg }));
      await page.goto(`${base}?pref=zh&zoom=${zoom}`, { waitUntil: "networkidle" });
      await page.waitForFunction(() => window.__panes?.() > 0);
      await page.evaluate(({ path, data }) => {
        window.__feed({ kind: "turn_started", authoredTurn: 1, msgIndex: 0, text: `@${path} 请查看这张图片`, attachments: [{ path }] });
        window.__feed({ kind: "tool_dispatch", tool: { id: "image-shot", name: "computer_read", readOnly: true } });
        window.__feed({ kind: "tool_result", tool: { id: "image-shot", name: "computer_read", output: "Screenshot captured", images: [data], readOnly: true } });
        window.__feed({ kind: "turn_done" });
      }, { path, data });
      await page.locator('.compose input[type="file"]').setInputFiles({ name: "draft.svg", mimeType: "image/svg+xml", buffer: Buffer.from(svg) });
      await page.locator(".shot[data-state=ready]").waitFor();
      const user = page.locator(".user-image img");
      await user.waitFor();
      if (before) {
        await user.dblclick();
        assert.equal(await page.getByRole("dialog").count(), 0);
        if (shots) await page.screenshot({ path: join(shots, `before-${theme}-${width}.png`) });
        await page.close();
        continue;
      }
      await user.dblclick();
      const dialog = page.getByRole("dialog", { name: "preview.svg", exact: true });
      await dialog.waitFor();
      assert.equal(await dialog.locator("img").getAttribute("src"), `/fixture/${path}`);
      const geometry = await dialog.locator("img").evaluate(img => {
        const box = img.getBoundingClientRect();
        return { x: box.x, y: box.y, right: box.right, bottom: box.bottom, width: box.width, height: box.height, natural: img.naturalWidth, portaled: img.closest(".image-preview").parentElement === document.body };
      });
      assert.equal(geometry.natural, 1600);
      assert.equal(geometry.portaled, true);
      assert.ok(geometry.width > 240 && geometry.height > 160);
      assert.ok(Math.abs(geometry.width / geometry.height - 1.6) < 0.03);
      assert.ok(geometry.x >= 0 && geometry.y >= 0 && geometry.right <= width + 1 && geometry.bottom <= height + 1, JSON.stringify(geometry));
      const close = dialog.getByRole("button", { name: "关闭图片预览" });
      assert.equal(await close.evaluate(el => el === document.activeElement), true);
      await page.keyboard.press("Tab");
      assert.equal(await close.evaluate(el => el === document.activeElement), true);
      if (shots && zoom === 1) await page.screenshot({ path: join(shots, `after-${theme}-${width}.png`) });
      await page.keyboard.press("Escape");
      await dialog.waitFor({ state: "detached" });
      assert.equal(await page.locator(".user-image button").evaluate(el => el === document.activeElement), true);
      await page.locator(".activity-group > summary").click();
      const tool = page.locator('[data-call="image-shot"] .tool-disclosure');
      await tool.locator("summary .nm").click();
      await page.getByRole("button", { name: "查看图片 工具截图 1", exact: true }).click();
      assert.equal(await page.getByRole("dialog").locator("img").getAttribute("src"), data);
      await page.getByRole("button", { name: "关闭图片预览" }).click();
      assert.equal(await tool.evaluate(el => el.open), true);
      await page.getByRole("button", { name: "查看图片 draft.svg", exact: true }).click();
      assert.match(await page.getByRole("dialog").locator("img").getAttribute("src"), /^blob:/);
      await page.getByRole("dialog").click({ position: { x: 3, y: 3 } });
      await page.getByRole("dialog").waitFor({ state: "detached" });
      assert.equal(await page.locator(".shot[data-state=ready]").count(), 1);
      assert.equal(await page.evaluate(() => document.body.style.overflow), "");
      assert.deepEqual(errors, []);
      console.log(`PASS ${theme} ${width}×${height} zoom ${zoom}: original image, double click, geometry, focus, tool and composer`);
      await page.close();
    }
  }
} finally {
  await browser.close();
}
