import assert from "node:assert/strict";
import { chromium } from "playwright";

import { pdf } from "./document-fixtures.cjs";

const browser = await chromium.launch();
let page;
try {
  page = await browser.newPage({ viewport: { width: 1440, height: 960 } });
  const errors = [];
  const requests = [];
  page.on("pageerror", (err) => errors.push(err.message));
  page.on("console", (msg) => { if (msg.type() === "error") console.error(msg.text()); });
  page.on("request", (req) => requests.push(req.url()));
  await page.goto(process.env.PERF_URL ?? "http://127.0.0.1:4399/perf.html?pref=zh", { waitUntil: "networkidle" });
  await page.waitForFunction(() => window.__panes?.() > 0);
  await page.evaluate(async (pdf) => {
    const data = {
      "report.pdf": pdf,
      "broken.pdf": "%PDF-1.7\ninvalid document",
      "table.csv": 'id,value\n001,"hello\nworld"\n002,"=SUM(A1:A2)"\n' + Array.from({ length: 500 }, (_, n) => `${n},row ${n}`).join("\n"),
      "page.html": '<style>h1{color:#385b80}</style><h1>Static report</h1><script>parent.__unsafe=true</script><img src="https://preview-test.invalid/leak"><p>Preview stays in the workbench.</p>',
    };
    window.__previewFiles(Object.fromEntries(Object.entries(data).map(([path, text]) => [
      path, { text, kind: path.endsWith("pdf") ? "pdf" : path.endsWith("csv") ? "table" : "html" },
    ])));
    window.__feed({ kind: "turn_started", authoredTurn: 1, msgIndex: 0, text: "Create reports" });
    window.__feed({ kind: "tool_dispatch", tool: { id: "present", name: "present_files", readOnly: true } });
    window.__feed({ kind: "tool_result", tool: { id: "present", name: "present_files", readOnly: true,
      output: "Presented files", deliveries: Object.keys(data).map((path) => ({ path, description: "Preview fixture" })) } });
    window.__feed({ kind: "text", text: "Reports are ready." });
    window.__feed({ kind: "turn_done" });
  }, pdf());
  await page.locator(".delivery-open").filter({ hasText: "report.pdf" }).click();
  await page.locator(".textLayer").getByText("Report preview", { exact: true }).waitFor();
  assert.equal(await page.locator(".document-pdf-page canvas").evaluate((el) => el.width > 0 && el.height > 0), true);
  for (const [selector, label] of [
    ['[data-action="share.card"]', "设备访问"],
    ['[data-action="transcript.find"]', "在这段对话里查找"],
    ['[data-action="appearance.theme"]', "切换深色主题"],
    ['.chrome [data-action="settings.section"][data-value="tools"]', "沙盒与运行边界"],
    ['[data-action="pane.view"][data-value="analysis"]', "打开运行分析"],
    ['[data-action="pane.view"][data-value="browser"]', "关闭工作台"],
    ['[data-action="browser.full"]', "全屏工作台"],
    ['[data-action="workbench.files"]', "展开文件树"],
    ['[data-action="workbench.new"]', "新建标签"],
    [".acts .copy", "复制回复"],
    ['[data-action="reply.quote"]', "引用到输入框"],
    ['[data-action="reply.more"]', "更多"],
  ]) {
    const button = page.locator(selector).last();
    await button.hover();
    const tip = page.locator(".icon-tooltip");
    await tip.filter({ hasText: label }).waitFor();
    assert.equal(await button.getAttribute("title"), null);
    const box = await tip.boundingBox();
    assert.ok(box.x >= 0 && box.y >= 0 && box.x + box.width <= 1440 && box.y + box.height <= 960);
    if (selector.includes("browser.full")) await page.screenshot({ path: "/tmp/reasonix-tooltip-workbench.png" });
    if (selector.includes("reply.quote")) await page.screenshot({ path: "/tmp/reasonix-tooltip-reply.png" });
    await page.mouse.move(1, 1);
    await tip.waitFor({ state: "detached" });
  }
  await page.locator('[data-action="browser.full"]').focus();
  await page.getByRole("tooltip").filter({ hasText: "全屏工作台" }).waitFor();
  await page.keyboard.press("Escape");
  assert.equal(await page.locator(".icon-tooltip").count(), 0);
  await page.getByRole("button", { name: "下一页", exact: true }).click();
  await page.locator(".textLayer").getByText("Second page", { exact: true }).waitFor();
  await page.screenshot({ path: "/tmp/reasonix-preview-pdf.png" });
  await page.locator(".delivery-open").filter({ hasText: "table.csv" }).click();
  await page.getByRole("table", { name: "table.csv" }).waitFor();
  assert.equal(await page.locator(".document-table td").filter({ hasText: "=SUM(A1:A2)" }).count(), 1);
  assert.ok(await page.locator(".document-table tr").count() < 100);
  await page.locator(".document-table").evaluate((el) => { el.scrollTop = 6000; });
  await page.waitForFunction(() => document.querySelector(".document-table th")?.textContent !== "1");
  await page.screenshot({ path: "/tmp/reasonix-preview-csv.png" });
  await page.locator(".delivery-open").filter({ hasText: "page.html" }).click();
  const frame = page.frameLocator('iframe[title="HTML 静态预览"]');
  await frame.getByRole("heading", { name: "Static report" }).waitFor();
  assert.equal(await page.evaluate(() => window.__unsafe), undefined);
  assert.equal(requests.some((url) => url.includes("preview-test.invalid")), false);
  await page.screenshot({ path: "/tmp/reasonix-preview-html.png" });
  await page.locator(".delivery-open").filter({ hasText: "report.pdf" }).click();
  await page.locator(".textLayer").getByText("Second page", { exact: true }).waitFor();
  assert.equal(await page.locator('[role="tab"][data-target="file:report.pdf"]').count(), 1);
  await page.locator(".delivery-open").filter({ hasText: "broken.pdf" }).click();
  await page.getByRole("alert").filter({ hasText: "PDF 无法显示" }).waitFor();
  assert.equal(await page.locator(".document-pdf-page").isVisible(), false);
  assert.deepEqual(errors, []);
  console.log("Document previews: PDF text/navigation/tab reuse/corruption, virtual CSV, isolated HTML and delivery clicks passed.");
} catch (error) {
  if (page) {
    await page.screenshot({ path: "/tmp/reasonix-preview-failure.png" });
    console.error(await page.locator(".document-preview").innerText().catch(() => "No preview"));
  }
  throw error;
} finally {
  await browser.close();
}
