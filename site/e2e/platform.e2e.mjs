import { createRequire } from "node:module";
import { mkdir, readFile, stat, writeFile } from "node:fs/promises";
import { createServer } from "node:http";
import path from "node:path";
import { fileURLToPath } from "node:url";

const here = path.dirname(fileURLToPath(import.meta.url));
const repoRoot = path.resolve(here, "../..");
const workspaceRequire = createRequire(path.join(repoRoot, "..", "e2e-runner.cjs"));
const { chromium } = workspaceRequire("playwright");
const artifacts = path.join(repoRoot, "artifacts");
const screenshots = path.join(artifacts, "screenshots");
await mkdir(screenshots, { recursive: true });

const distRoot = path.join(repoRoot, "site", "dist");
const mime = { ".html": "text/html; charset=utf-8", ".js": "text/javascript; charset=utf-8", ".css": "text/css; charset=utf-8", ".png": "image/png", ".svg": "image/svg+xml", ".json": "application/json" };
const staticServer = process.env.INTELIFAR_BASE_URL ? null : createServer(async (request, response) => {
  try {
    const pathname = decodeURIComponent(new URL(request.url, "http://127.0.0.1").pathname);
    let target = path.resolve(distRoot, `.${pathname}`);
    if (!target.startsWith(distRoot)) throw new Error("invalid path");
    if ((await stat(target)).isDirectory()) target = path.join(target, "index.html");
    const body = await readFile(target);
    response.writeHead(200, { "content-type": mime[path.extname(target)] || "application/octet-stream", "cache-control": "no-store" });
    response.end(body);
  } catch {
    response.writeHead(404, { "content-type": "text/plain; charset=utf-8" });
    response.end("Not found");
  }
});
if (staticServer) await new Promise((resolve) => staticServer.listen(0, "127.0.0.1", resolve));
const address = staticServer?.address();
const baseURL = process.env.INTELIFAR_BASE_URL || `http://127.0.0.1:${address.port}`;

const chromeCandidates = [
  "C:\\Program Files\\Google\\Chrome\\Application\\chrome.exe",
  "C:\\Program Files (x86)\\Microsoft\\Edge\\Application\\msedge.exe",
];
const executablePath = process.platform === "win32" ? chromeCandidates[0] : undefined;
const browser = await chromium.launch({
  headless: true,
  executablePath,
  args: ["--no-proxy-server", "--proxy-bypass-list=<-loopback>"],
});
const results = [];

async function check(name, task) {
  const started = Date.now();
  try {
    await task();
    results.push({ name, status: "PASS", duration: Date.now() - started });
  } catch (error) {
    results.push({ name, status: "FAIL", duration: Date.now() - started, error: error.message });
    throw error;
  }
}

function assert(condition, message) {
  if (!condition) throw new Error(message);
}

const context = await browser.newContext({ viewport: { width: 1440, height: 1000 }, locale: "zh-CN", acceptDownloads: true });
const page = await context.newPage();

await check("指挥台加载与品牌识别", async () => {
  await page.goto(baseURL, { waitUntil: "networkidle" });
  await page.evaluate(() => localStorage.clear());
  await page.reload({ waitUntil: "networkidle" });
  await page.locator("[data-testid='overview-new-analysis']").waitFor();
  assert(await page.locator("img[alt*='intelifar']").count(), "未找到 intelifar 官方 Logo");
  await page.screenshot({ path: path.join(screenshots, "01-command-center.png"), fullPage: true });
});

await check("文档接入与表单校验", async () => {
  await page.locator("[data-testid='nav-documents']").click();
  await page.locator("[data-testid='documents-upload']").click();
  await page.locator("[data-testid='start-analysis']").click();
  assert(await page.locator("[data-error-for='name']").textContent() === "请输入文档名称", "空文档名称未被拦截");
  await page.locator("#use-demo-file").click();
  await page.screenshot({ path: path.join(screenshots, "02-document-intake.png") });
  await page.locator("[data-testid='start-analysis']").click();
  await page.locator("#view-analysis:not([hidden])").waitFor();
});

await check("五阶段分析流水线完成", async () => {
  for (let index = 0; index < 4; index += 1) await page.locator("[data-testid='advance-analysis']").click();
  await page.locator("#analysis-progress-number").waitFor({ state: "visible" });
  assert(await page.locator("#analysis-progress-number").textContent() === "100%", "分析未推进到 100%");
  assert(!(await page.locator("[data-testid='advance-analysis']").isEnabled()), "完成后的分析按钮仍可继续触发");
  await page.waitForTimeout(4500);
  await page.screenshot({ path: path.join(screenshots, "03-analysis-complete.png"), fullPage: true });
});

await check("资产详情与证据数量", async () => {
  await page.locator("[data-testid='nav-assets']").click();
  await page.locator("[data-testid='first-asset']").click();
  await page.locator("#asset-drawer.is-open").waitFor();
  assert((await page.locator("#asset-drawer").textContent()).includes("14 处"), "资产证据覆盖信息缺失");
  await page.waitForTimeout(4500);
  await page.screenshot({ path: path.join(screenshots, "04-asset-profile.png") });
  await page.locator("#asset-drawer [data-close-drawer]").click();
});

await check("Wiki 引用精准溯源", async () => {
  await page.locator("[data-testid='nav-wiki']").click();
  await page.locator("[data-testid='wiki-citation']").click();
  await page.locator("#provenance-drawer.is-open").waitFor();
  assert((await page.locator("#provenance-drawer").textContent()).includes("页 49 · 内容块 01"), "溯源定位精度未展示");
  await page.waitForTimeout(400);
  await page.screenshot({ path: path.join(screenshots, "05-wiki-provenance.png") });
  await page.locator("#provenance-drawer [data-close-drawer]").first().click();
});

await check("Wiki 全文定位与专注阅读", async () => {
  await page.locator("#wiki-search").fill("拥塞");
  assert((await page.locator("#wiki-search-status").textContent()).includes("匹配章节"), "Wiki 搜索未返回章节数量");
  assert(await page.locator("[data-wiki-search-section].is-search-match").count() >= 1, "Wiki 搜索未标记匹配章节");
  await page.locator("#wiki-search").fill("不存在的企业术语");
  assert((await page.locator("#wiki-search-status").textContent()).includes("0 个"), "Wiki 空结果状态缺失");
  await page.locator("#wiki-search").fill("");
  await page.locator("#wiki-focus-toggle").click();
  assert(await page.locator("#view-wiki.is-focus-mode").count() === 1, "Wiki 专注阅读未生效");
  await page.screenshot({ path: path.join(screenshots, "11-wiki-focus-search.png"), fullPage: true });
  await page.locator("#wiki-focus-toggle").click();
});

await check("资产检索空状态与键盘抽屉", async () => {
  await page.locator("[data-testid='nav-assets']").click();
  await page.locator("#asset-search").fill("完全不存在的资产");
  assert(await page.locator("#asset-empty-state").isVisible(), "资产检索空状态未显示");
  await page.locator("#asset-search").fill("");
  await page.locator("[data-testid='first-asset']").focus();
  await page.keyboard.press("Enter");
  await page.locator("#asset-drawer.is-open").waitFor();
  await page.keyboard.press("Escape");
  assert(await page.locator("#asset-drawer.is-open").count() === 0, "Escape 未关闭资产抽屉");
  assert(await page.evaluate(() => document.activeElement?.dataset?.testid === "first-asset"), "关闭抽屉后未恢复触发点焦点");
  await page.screenshot({ path: path.join(screenshots, "12-asset-empty-state.png") });
});

await check("涂黑内容权限化查看", async () => {
  await page.locator("[data-testid='nav-redaction']").click();
  await page.locator("[data-testid='redacted-token']").click();
  await page.locator("#provenance-drawer.is-open").waitFor();
  assert((await page.locator("#toast-region").textContent()).includes("权限校验通过"), "敏感溯源未反馈权限校验");
  await page.locator("#provenance-drawer [data-close-drawer]").first().click();
  await page.waitForTimeout(4500);
  await page.screenshot({ path: path.join(screenshots, "06-redaction-workbench.png"), fullPage: true });
});

await check("生命周期安全分享", async () => {
  await page.locator("[data-testid='nav-lifecycle']").click();
  await page.locator("[data-testid='open-share']").click();
  await page.locator("[data-testid='share-recipient']").fill("partner@huachen.example");
  await page.locator("[data-testid='share-expires']").selectOption("7d");
  await page.locator("[data-testid='create-share']").click();
  await page.getByText("partner@huachen.example", { exact: true }).waitFor();
  await page.waitForTimeout(4500);
  await page.screenshot({ path: path.join(screenshots, "07-lifecycle-governance.png"), fullPage: true });
});

await check("审计事件与 CSV 导出", async () => {
  await page.locator("[data-testid='nav-audit']").click();
  const downloadPromise = page.waitForEvent("download");
  await page.locator("[data-testid='export-audit']").click();
  const download = await downloadPromise;
  await download.saveAs(path.join(artifacts, "intelifar-audit-sample.csv"));
  assert((await page.locator("#audit-log").textContent()).includes("导出审计日志"), "导出操作未进入审计日志");
  await page.waitForTimeout(4500);
  await page.screenshot({ path: path.join(screenshots, "08-audit-ledger.png"), fullPage: true });
});

await check("真实服务边界与系统状态", async () => {
  await page.locator("[data-testid='nav-system']").click();
  const systemCopy = await page.locator("#view-system").textContent();
  assert(systemCopy.includes("外部处理器"), "系统拓扑未披露外部处理器");
  assert(systemCopy.includes("原始文档发送 MinerU"), "MinerU 数据边界披露缺失");
  assert(systemCopy.includes("解析文本发送 DeepSeek"), "DeepSeek 数据边界披露缺失");
  assert((await page.locator("#system-live-status").textContent()).includes("离线演示"), "静态模式未准确标识离线状态");
  await page.screenshot({ path: path.join(screenshots, "13-system-data-boundary.png"), fullPage: true });
});

await check("移动端响应式导航与指挥台", async () => {
  const mobile = await browser.newContext({ viewport: { width: 390, height: 844 }, locale: "zh-CN" });
  const mobilePage = await mobile.newPage();
  await mobilePage.goto(`${baseURL}/#overview`, { waitUntil: "networkidle" });
  await mobilePage.locator("#mobile-menu").click();
  assert(await mobilePage.locator(".sidebar.is-open").isVisible(), "移动端导航未打开");
  await mobilePage.locator("[data-testid='nav-overview']").click();
  await mobilePage.waitForTimeout(400);
  await mobilePage.screenshot({ path: path.join(screenshots, "09-mobile-command-center.png"), fullPage: true });
  await mobile.close();
});

await context.close();
await browser.close();
if (staticServer) await new Promise((resolve) => staticServer.close(resolve));

const lines = [
  "# intelifar IP Wiki E2E Report",
  "",
  `- Run: ${new Date().toISOString()}`,
  `- Target: ${baseURL}`,
  `- Browser: Chromium / installed Chrome`,
  `- Result: ${results.every((result) => result.status === "PASS") ? "PASS" : "FAIL"}`,
  "",
  "| Scenario | Status | Duration |",
  "| --- | --- | ---: |",
  ...results.map((result) => `| ${result.name} | ${result.status} | ${result.duration} ms |`),
  "",
  "Screenshots are stored in `artifacts/screenshots/`.",
];
await writeFile(path.join(artifacts, "e2e-report.md"), `\uFEFF${lines.join("\n")}`, "utf8");
await writeFile(path.join(artifacts, "delivery-tree.txt"), [
  "intelifar-ip-wiki/",
  "|-- README.md / README.zh-CN.md       # intelifar fork entry",
  "|-- INTELIFAR-DELIVERY.md             # acceptance and runbook",
  "|-- LICENSE                           # upstream MIT license",
  "|-- docs/",
  "|   |-- architecture/intelifar-ip-wiki.md",
  "|   `-- plans/2026-08-09-intelifar-ip-wiki-*.md",
  "|-- internal/                         # preserved Reasonix Go kernel",
  "|-- site/",
  "|   |-- src/pages/index.astro         # enterprise application shell",
  "|   |-- src/styles/ip-platform.css    # intelifar design system",
  "|   |-- src/scripts/ip-platform*.mjs  # behavior, state, contracts",
  "|   |-- public/brand/                 # official logo assets",
  "|   |-- e2e/platform.e2e.mjs          # twelve browser scenarios",
  "|   |-- e2e/real-platform.e2e.mjs     # MinerU + DeepSeek live scenario",
  "|   |-- server/                       # gateway, providers and publication registry",
  "|   `-- dist/                         # production static build",
  "`-- artifacts/",
  "    |-- e2e-report.md",
  "    |-- intelifar-audit-sample.csv",
  "    |-- delivery-tree.txt",
  "    |-- enterprise-95-scorecard.md",
  "    |-- enterprise-95-review/          # final desktop/mobile visual review",
  "    |-- real-e2e/                      # sanitized live-provider publication evidence",
  "    `-- screenshots/01..14-*.png",
].join("\n"), "utf8");
process.stdout.write(`${results.length} E2E scenarios passed.\n`);
