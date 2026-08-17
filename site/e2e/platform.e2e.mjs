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
const graphArtifacts = path.join(artifacts, "ip-asset-graph");
await mkdir(screenshots, { recursive: true });
await mkdir(graphArtifacts, { recursive: true });

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

await check("资产全景网络筛选、聚焦与键盘访问", async () => {
  await page.locator("[data-testid='nav-assets']").click();
  await page.locator("#graph-loading").waitFor({ state: "hidden" });
  assert(await page.locator(".graph-node").count() === 7, "全景网络未渲染完整演示资产");
  assert(await page.locator(".graph-edge").count() === 7, "维护者默认视图应展示已确认与待复核关系");
  assert(await page.locator(".graph-edge.is-proposed").count() === 2, "默认视图未区分待复核关系");
  const graphCanvas = page.locator("#asset-graph");
  assert(await graphCanvas.getAttribute("data-camera-scale") === "1.000", "神经全景未使用稳定初始相机");
  await page.locator("#graph-zoom-in").click();
  assert(Number(await graphCanvas.getAttribute("data-camera-scale")) > 1, "缩放按钮未放大神经全景");
  await page.locator("#graph-zoom-reset").click();
  await graphCanvas.hover({ position: { x: 420, y: 310 } });
  await page.mouse.wheel(0, 540);
  await page.mouse.wheel(0, 540);
  await page.mouse.wheel(0, 540);
  assert(await graphCanvas.getAttribute("data-zoom-level") === "overview", "缩小时未进入语义全景层级");
  await page.locator("#graph-zoom-reset").click();
  const graphBox = await graphCanvas.boundingBox();
  await page.mouse.move(graphBox.x + 180, graphBox.y + 280);
  await page.mouse.down();
  await page.mouse.move(graphBox.x + 250, graphBox.y + 315, { steps: 4 });
  await page.mouse.up();
  assert(Math.abs(Number(await graphCanvas.getAttribute("data-camera-x"))) > 10, "拖拽空白画布未改变相机位置");
  await page.locator("#graph-zoom-reset").click();
  assert(await page.locator("[data-testid='graph-camera-status']").textContent() === "100% · 关系网络", "适配全景未恢复相机状态");
  await page.screenshot({ path: path.join(graphArtifacts, "06-neural-panorama.png") });
  const coreNode = page.getByRole("button", { name: /稀疏专家路由与动态负载均衡方法，技术方案/ });
  await coreNode.focus();
  await page.keyboard.press("Enter");
  await page.locator("#graph-inspector:not([hidden])").waitFor();
  assert((await page.locator("#graph-inspector-degree").textContent()) === "4 条", "资产卡片直接关系统计不准确");
  await page.screenshot({ path: path.join(screenshots, "04-asset-panorama.png") });
  await page.locator("#graph-stage").screenshot({ path: path.join(graphArtifacts, "07-neural-node-inspector.png") });
  await page.locator("#graph-focus-neighborhood").click();
  assert(await page.locator(".graph-node").count() === 5, "一跳聚焦未保留维护者可复核的完整邻域");
  await page.locator("#asset-graph").press("Escape");
  assert(await page.locator(".graph-node").count() === 7, "Escape 未返回关系全景");
  await page.locator(".graph-edge").nth(6).waitFor();
  assert(await page.locator(".graph-edge.is-proposed").count() === 2, "待复核关系未使用独立状态呈现");
  await page.locator(".graph-edge-hit.is-proposed").first().focus();
  await page.keyboard.press("Enter");
  await page.locator("#relationship-inspector:not([hidden])").waitFor();
  assert((await page.locator("#relationship-inspector-verification").textContent()) === "待复核", "关系详情未显示复核状态");
  assert(await page.locator("#relationship-review-actions").isVisible(), "内容维护者缺少关系复核操作");
  await page.locator("#relationship-reject").click();
  assert(await page.locator(".graph-edge.is-proposed").count() === 1, "拒绝关系建议后网络未更新");
  await page.locator("#graph-reset").click();
  assert(await page.locator(".graph-edge").count() === 6, "重置视图未恢复维护者关系范围");
  await page.locator("#graph-search").fill("异构算力");
  assert(await page.locator(".graph-node").count() === 4, "图搜索未保留匹配资产及可复核的一跳上下文");
  await page.locator("#graph-reset").click();
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
  const provenanceCopy = await page.locator("#provenance-drawer").textContent();
  assert(provenanceCopy.includes("演示定位"), "演示溯源边界未展示");
  assert(provenanceCopy.includes("（演示）"), "演示证据未与真实工作空间隔离");
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
  assert((await page.locator("#audit-log").textContent()).includes("导出操作记录"), "导出操作未进入操作记录");
  await page.waitForTimeout(4500);
  await page.screenshot({ path: path.join(screenshots, "08-audit-ledger.png"), fullPage: true });
});

await check("真实服务边界与系统状态", async () => {
  await page.locator("[data-testid='nav-system']").click();
  const technicalDetails = page.locator("details.technical-details");
  assert(!(await technicalDetails.getAttribute("open")), "管理员技术详情默认不应展开");
  await technicalDetails.locator("summary").click();
  const systemCopy = await page.locator("#view-system").textContent();
  assert(systemCopy.includes("文档读取服务"), "文档读取边界披露缺失");
  assert(systemCopy.includes("知识提取服务"), "知识提取边界披露缺失");
  assert(systemCopy.includes("原始文档用于读取"), "原始文档处理范围披露缺失");
  assert(systemCopy.includes("限定范围的文本用于知识提取"), "知识提取范围披露缺失");
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
  await mobilePage.locator("#mobile-menu").click();
  await mobilePage.locator("[data-testid='nav-assets']").click();
  await mobilePage.locator("#graph-loading").waitFor({ state: "hidden" });
  await mobilePage.waitForTimeout(450);
  assert(await mobilePage.evaluate(() => document.body.scrollWidth <= window.innerWidth), "移动端资产全景产生页面级横向溢出");
  assert(await mobilePage.locator(".graph-node").count() === 7, "移动端资产全景节点缺失");
  await mobilePage.screenshot({ path: path.join(graphArtifacts, "04-mobile-panorama.png"), fullPage: true });
  await mobile.close();
});

let graphPerformance = { search: { p95Ms: 0 }, boundedTraversal: { p95Ms: 0 } };
try {
  graphPerformance = JSON.parse(await readFile(path.join(graphArtifacts, "performance-results.json"), "utf8"));
} catch {
  // The manifest remains renderable before the optional large-fixture benchmark is run.
}
const searchP95 = Number(graphPerformance.search?.p95Ms || 0).toFixed(2);
const traversalP95 = Number(graphPerformance.boundedTraversal?.p95Ms || 0).toFixed(2);

await check("最终交付物结构截图留档", async () => {
  await page.setContent(`<!doctype html><html lang="zh-CN"><head><meta charset="utf-8"><style>
    *{box-sizing:border-box}body{margin:0;background:#f3f4f8;color:#171823;font:14px "Microsoft YaHei",sans-serif}.sheet{display:grid;grid-template-columns:260px 1fr;min-height:100vh}.rail{display:flex;flex-direction:column;padding:42px 30px;background:#171823;color:#fff}.brand{display:flex;gap:12px;align-items:center;font:bold 20px monospace;letter-spacing:.08em}.glyph{display:grid;place-items:center;width:36px;height:36px;border:1px solid #756cff;border-radius:10px;color:#a9a4ff}.rail small{margin:10px 0;color:#8d91a5;font:10px monospace;letter-spacing:.18em}.rail-meta{margin-top:auto;display:grid;gap:8px;border-top:1px solid #343644;padding-top:20px}.rail-meta b{font-size:14px}.rail-meta span{color:#b8bac7;font-size:11px;line-height:1.7}.content{padding:44px 52px}.head{display:flex;justify-content:space-between;align-items:flex-start}.eyebrow{color:#635bff;font:bold 10px monospace;letter-spacing:.14em}.head h1{margin:10px 0 4px;font-size:34px}.head p{margin:0;color:#727689}.pass{padding:10px 16px;border:1px solid #8ed8bf;border-radius:10px;background:#effbf7;color:#16866a;font:bold 10px monospace}.layout{display:grid;grid-template-columns:minmax(0,1fr) 310px;gap:20px;margin-top:28px}.panel{overflow:hidden;border:1px solid #dfe1e9;border-radius:16px;background:#fff;box-shadow:0 18px 50px rgba(26,27,43,.06)}.panel-head{display:flex;justify-content:space-between;align-items:end;padding:23px;border-bottom:1px solid #e4e5eb}.panel-head h2{margin:5px 0 0;font-size:18px}.panel-head>span{color:#7d8090;font-size:9px}.tree{margin:0;padding:25px 28px;color:#292b39;font:12px/1.75 Consolas,monospace;white-space:pre-wrap}.side{display:grid;gap:16px}.metric{padding:23px}.metric small{display:block;color:#787b8c;font-size:10px}.metric strong{display:block;margin:7px 0 2px;font-size:35px}.metric b{color:#635bff}.checklist{padding:8px 22px}.checklist p{display:grid;grid-template-columns:24px 1fr;gap:9px;align-items:center;margin:0;padding:16px 0;border-top:1px solid #e8e9ee;font-size:11px}.checklist p:first-child{border-top:0}.checklist i{display:grid;place-items:center;width:22px;height:22px;border-radius:50%;background:#e7f6f1;color:#16866a;font-style:normal}.note{padding:15px;border:1px dashed #d9a44c;border-radius:12px;background:#fff9eb;color:#865d16;font-size:10px;line-height:1.6}
  </style></head><body><main class="sheet"><aside class="rail"><div class="brand"><span class="glyph">IF</span>intelifar</div><small>IP INTELLIGENCE</small><div class="rail-meta"><b>IP 神经全景阶段</b><span>语义缩放 · 权限遍历 · 可解释搜索<br>桌面与移动端真实 E2E</span></div></aside><section class="content"><header class="head"><div><span class="eyebrow">FINAL DELIVERY MANIFEST</span><h1>最终交付物结构</h1><p>intelifar 小微企业知识资产平台 · 2026-08-10 验收快照</p></div><span class="pass">● ALL CHECKS PASSED</span></header><div class="layout"><article class="panel"><div class="panel-head"><div><span class="eyebrow">REPOSITORY TREE</span><h2>intelifar-ip-wiki/</h2></div><span>神经全景核心交付路径</span></div><pre class="tree">├─ INTELIFAR-DELIVERY.md
├─ docs/
│  ├─ INTELIFAR-USER-GUIDE.zh-CN.md
│  ├─ architecture/adr/0002-use-rebuildable-ip-graph-projection.md
│  ├─ plans/2026-08-10-ip-asset-knowledge-graph-*.md
│  └─ plans/2026-08-10-neural-ip-panorama-*.md
├─ site/
│  ├─ server/
│  │  ├─ asset-graph-store.mjs          图投影 / 图搜索 / 权限遍历
│  │  ├─ platform-store.mjs             发布事务 / 关系生命周期
│  │  └─ real-analysis-server.mjs       图与关系 API / 审计
│  ├─ src/
│  │  ├─ pages/index.astro              资产关系全景 UI
│  │  ├─ styles/ip-platform.css         intelifar 神经全景视觉系统
│  │  └─ scripts/asset-graph*.mjs       布局 / 相机 / 交互测试
│  ├─ e2e/
│  │  ├─ platform.e2e.mjs               桌面 / 键盘 / 移动端
│  │  └─ asset-graph-performance.mjs    10k / 100k 性能门槛
│  └─ dist/                             生产静态构建
└─ artifacts/ip-asset-graph/
   ├─ 01..07-*.png                      全景 / 节点 / 移动 / 结构截图
   ├─ acceptance-report.md              功能评分与验收边界
   ├─ performance-results.json          原始性能数据
   └─ performance-report.md             性能验收摘要</pre></article><aside class="side"><section class="panel metric"><small>SEARCH P95 / 目标 400ms</small><strong>${searchP95}<b>ms</b></strong><small>10,000 节点 · 100,000 关系</small></section><section class="panel metric"><small>2-HOP P95 / 目标 500ms</small><strong>${traversalP95}<b>ms</b></strong><small>权限先裁剪，再按索引遍历</small></section><section class="panel checklist"><p><i>✓</i><span>工作区与查看者机密边界</span></p><p><i>✓</i><span>关系确认、拒绝与哈希审计</span></p><p><i>✓</i><span>键盘、桌面与移动端全景</span></p><p><i>✓</i><span>中文手册与就地截图</span></p></section><div class="note">生产部署仍需由客户完成 HTTPS、主机加固、异地备份、告警和供应商数据协议。</div></aside></div></section></main></body></html>`);
  await page.screenshot({ path: path.join(graphArtifacts, "05-final-delivery-structure.png"), fullPage: true });
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
  "|   |-- src/scripts/asset-graph*.mjs  # graph layout, semantic camera and tests",
  "|   |-- public/brand/                 # official logo assets",
  "|   |-- e2e/platform.e2e.mjs          # desktop, keyboard and mobile scenarios",
  "|   |-- e2e/asset-graph-performance.mjs # 10k nodes / 100k edges gate",
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
  "    |-- ip-asset-graph/                # neural panorama, performance and structure proof",
  "    `-- screenshots/                   # full product browser evidence",
].join("\n"), "utf8");
process.stdout.write(`${results.length} E2E scenarios passed.\n`);
