import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import { chromium } from "playwright";

// OFFICE_SAMPLES names a folder holding report.docx and sales.xlsx; OUT where the shots go.
const dir = process.env.OFFICE_SAMPLES;
const out = process.env.OUT ?? "/tmp";
assert.ok(dir, "set OFFICE_SAMPLES");
const files = {
  "report.docx": { kind: "office", base64: readFileSync(join(dir, "report.docx")).toString("base64") },
  "sales.xlsx": { kind: "office", base64: readFileSync(join(dir, "sales.xlsx")).toString("base64") },
};
const browser = await chromium.launch(process.env.CHROME ? { executablePath: process.env.CHROME } : {});
try {
  for (const [scheme, width] of [["light", 1440], ["dark", 1440], ["light", 390]]) {
    const page = await browser.newPage({ viewport: { width, height: 900 }, colorScheme: scheme });
    const errors = [];
    const requests = [];
    page.on("pageerror", (err) => errors.push(err.message));
    page.on("request", (req) => requests.push(req.url()));
    await page.goto(process.env.PERF_URL ?? "http://127.0.0.1:4399/perf.html?pref=zh", { waitUntil: "networkidle" });
    await page.waitForFunction(() => window.__panes?.() > 0);
    await page.evaluate((files) => {
      window.__previewFiles(files);
      window.__feed({ kind: "turn_started", authoredTurn: 1, msgIndex: 0, text: "整理季度报告" });
      window.__feed({ kind: "tool_dispatch", tool: { id: "present", name: "present_files", readOnly: true } });
      window.__feed({ kind: "tool_result", tool: { id: "present", name: "present_files", readOnly: true,
        output: "Presented files", deliveries: Object.keys(files).map((path) => ({ path, description: "季度材料" })) } });
      window.__feed({ kind: "text", text: "报告和表格已经生成。" });
      window.__feed({ kind: "turn_done" });
    }, files);
    for (const name of width < 600 ? ["sales.xlsx"] : ["report.docx", "sales.xlsx"]) {
      await page.locator(".delivery-open").filter({ hasText: name }).click();
      const frame = page.frameLocator('iframe[title="Office 文档预览"]');
      await frame.locator(name.endsWith("docx") ? "section.docx" : "table").first().waitFor({ timeout: 15000 });
      assert.equal(await page.locator('iframe[title="Office 文档预览"]').getAttribute("sandbox"), "");
      await page.waitForTimeout(300);
      await page.screenshot({ path: join(out, `office-${name.split(".")[1]}-${scheme}-${width}.png`) });
    }
    assert.deepEqual(errors, []);
    assert.deepEqual(requests.filter((u) => !u.startsWith("http://127.0.0.1") && !u.startsWith("http://localhost") && !u.startsWith("data:") && !u.startsWith("blob:")), []);
    await page.close();
  }
} finally {
  await browser.close();
}
