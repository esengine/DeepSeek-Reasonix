import assert from "node:assert/strict";
import { randomUUID } from "node:crypto";
import { existsSync } from "node:fs";
import { mkdir, mkdtemp, rm } from "node:fs/promises";
import { createRequire } from "node:module";
import os from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { hashPassword } from "../server/auth-service.mjs";
import { createPlatformStore } from "../server/platform-store.mjs";
import { createRealAnalysisServer } from "../server/real-analysis-server.mjs";

const here = path.dirname(fileURLToPath(import.meta.url));
const repoRoot = path.resolve(here, "../..");
const siteRoot = path.join(repoRoot, "site");
const screenshots = path.join(repoRoot, "artifacts", "user-guide-review");
const runtime = await mkdtemp(path.join(os.tmpdir(), "intelifar-module-tour-"));
const workspaceId = "WS-MODULE-TOUR";
const ownerEmail = "owner-modules@example.com";
const ownerPassword = `Owner-${randomUUID()}-Aa!`;
const viewerEmail = "viewer-modules@example.com";
const viewerPassword = `Viewer-${randomUUID()}-Bb!`;
const assetId = "IP-REAL-FACE01";
await mkdir(screenshots, { recursive: true });

const store = createPlatformStore({ dbPath: path.join(runtime, "platform.sqlite") });
store.ensureWorkspace({ id: workspaceId, name: "澜图科技 · 用户手册验收空间" });
store.createUser({ id: "USR-VIEWER-MODULES", workspaceId, email: viewerEmail, name: "许安", role: "viewer", passwordHash: await hashPassword(viewerPassword) });
store.savePublication(workspaceId, {
  publicationId: "PUB-MODULE-TOUR",
  sourceJobId: "JOB-MODULE-TOUR",
  status: "published",
  version: "V1.0",
  publishedAt: "2026-08-10T10:30:00.000Z",
  document: { title: "模块巡检技术白皮书", category: "技术白皮书", summary: "用户手册验收样本", language: "zh-CN", sourceName: "模块巡检技术白皮书.pdf", markdownSha256: "a".repeat(64), parserProvider: "MinerU", parserModel: "MinerU-HTML", parserBatchId: "batch-module-tour", llmProvider: "DeepSeek", llmModel: "deepseek-chat" },
  assets: [{
    id: assetId,
    sourceAssetId: "IP-MODULE-1",
    publicationId: "PUB-MODULE-TOUR",
    sourceJobId: "JOB-MODULE-TOUR",
    title: "intelifar 全模块知识治理方法",
    type: "技术方案",
    summary: "从文档接入到受控分享的完整知识治理操作方法。",
    tags: ["用户手册", "模块巡检", "Wiki"],
    confidence: 0.98,
    owner: "知识平台主管",
    sensitivity: "内部",
    status: "已发布",
    version: "V1.0",
    publishedAt: "2026-08-10T10:30:00.000Z",
    document: { sourceName: "模块巡检技术白皮书.pdf", markdownSha256: "a".repeat(64), parserBatchId: "batch-module-tour" },
    evidence: [{ id: "EV-FACE01", assetId, quote: "所有模块都必须通过角色化浏览器巡检。", section: "验收原则", locator: "验收原则", quoteHash: "b".repeat(64), documentHash: "a".repeat(64), documentName: "模块巡检技术白皮书.pdf", parserBatchId: "batch-module-tour", precision: "章节级", verified: true }],
    wiki: {
      title: "intelifar 全模块知识治理方法",
      executiveSummary: "统一完成文档、分析、资产、Wiki、脱敏、分享和审计。",
      keyMechanism: "MinerU 解析、DeepSeek 结构化、人工复核并形成版本化知识。",
      metrics: [{ label: "模块覆盖", value: "9/9" }, { label: "角色边界", value: "4 级" }],
      relationships: [{ source: "企业文档", relation: "沉淀为", target: "版本化 Wiki" }],
    },
  }],
});

const gateway = await createRealAnalysisServer({
  distRoot: path.join(siteRoot, "dist"),
  platformStore: store,
  uploadRoot: path.join(runtime, "uploads"),
  backupRoot: path.join(runtime, "backups"),
  config: { mineruApiKey: "module-mineru", deepseekApiKey: "module-deepseek", deepseekModel: "deepseek-chat" },
  auth: { required: true, secureCookies: false, workspaceId, workspaceName: "澜图科技 · 用户手册验收空间", email: ownerEmail, password: ownerPassword, name: "林越" },
  mineruClient: { async parseDocument() { throw new Error("Not used by module tour"); } },
  deepseekClient: { async analyzeMarkdown() { throw new Error("Not used by module tour"); } },
});

const baseURL = await gateway.start();
const workspaceRequire = createRequire(path.join(repoRoot, "..", "e2e-runner.cjs"));
const { chromium } = workspaceRequire("playwright");
const candidates = ["C:\\Program Files\\Google\\Chrome\\Application\\chrome.exe", "C:\\Program Files (x86)\\Microsoft\\Edge\\Application\\msedge.exe"];
const executablePath = process.platform === "win32" ? candidates.find(existsSync) : undefined;
const browser = await chromium.launch({ headless: true, executablePath, args: ["--no-proxy-server", "--proxy-bypass-list=<-loopback>"] });
const ownerContext = await browser.newContext({ viewport: { width: 1440, height: 1000 }, locale: "zh-CN", acceptDownloads: true });
const ownerPage = await ownerContext.newPage();
let viewerContext;

async function login(page, email, password) {
  await page.goto(baseURL, { waitUntil: "domcontentloaded", timeout: 30_000 });
  await page.locator("[data-testid='login-email']").fill(email);
  await page.locator("[data-testid='login-password']").fill(password);
  await Promise.all([
    page.waitForResponse((response) => response.url().endsWith("/api/auth/login") && response.status() === 200),
    page.locator("[data-testid='login-submit']").click(),
  ]);
  await page.locator("[data-testid='session-dialog']").waitFor({ state: "hidden" });
}

async function navigateAndAssert(page, view, selector, expectedText) {
  await page.locator(`[data-testid='nav-${view}']`).click();
  const target = page.locator(`#view-${view}`);
  await target.waitFor({ state: "visible" });
  assert.equal(await page.locator("section.view:not([hidden])").count(), 1);
  assert.match((await target.locator(selector).first().textContent()).trim(), expectedText);
}

try {
  await login(ownerPage, ownerEmail, ownerPassword);
  await ownerPage.locator("#asset-table-body .published-row").waitFor({ state: "attached" });
  assert.match(await ownerPage.locator("[data-testid='workspace-identity']").textContent(), /用户手册验收空间/);

  const modules = [
    ["overview", "h1", /早上好/],
    ["documents", "h1", /文档中心/],
    ["analysis", "h1", /智能分析工作室/],
    ["assets", "h1", /IP 资产库/],
    ["wiki", "#wiki-dynamic-title", /稀疏专家路由|全模块知识治理/],
    ["redaction", "h1", /脱敏与溯源工作台/],
    ["lifecycle", "h1", /IP 全生命周期/],
    ["audit", "h1", /审计日志/],
    ["system", "h1", /系统状态/],
  ];
  for (const [view, selector, heading] of modules) await navigateAndAssert(ownerPage, view, selector, heading);

  await navigateAndAssert(ownerPage, "overview", "h1", /早上好/);
  await ownerPage.waitForTimeout(4_500);
  await ownerPage.screenshot({ path: path.join(screenshots, "01-module-overview.png"), fullPage: true });

  await ownerPage.locator("#global-search").fill("全模块知识治理");
  await ownerPage.locator("#global-search").press("Enter");
  await ownerPage.locator("#view-assets").waitFor({ state: "visible" });
  const publishedRow = ownerPage.locator("#asset-table-body .published-row", { hasText: "全模块知识治理" });
  await publishedRow.waitFor();
  assert.equal(await ownerPage.locator("#asset-table-body tr:not([hidden])").count(), 1);
  await publishedRow.click();
  await ownerPage.locator("#asset-drawer.is-open").waitFor();
  assert.match(await ownerPage.locator("#asset-drawer-title").textContent(), /全模块知识治理/);
  await ownerPage.locator("#asset-view-evidence").click();
  await ownerPage.locator("#provenance-drawer.is-open").waitFor();
  assert.match(await ownerPage.locator("#evidence-quote").textContent(), /角色化浏览器巡检/);
  await ownerPage.locator("#asset-drawer:not(.is-open)").waitFor();
  await ownerPage.waitForTimeout(600);
  await ownerPage.screenshot({ path: path.join(screenshots, "02-asset-provenance.png") });
  await ownerPage.keyboard.press("Escape");

  await navigateAndAssert(ownerPage, "wiki", "#wiki-dynamic-title", /全模块知识治理/);
  await ownerPage.locator("#wiki-search").fill("MinerU");
  assert.match(await ownerPage.locator("#wiki-search-status").textContent(), /找到 [1-9]\d* 个匹配章节/);
  assert.ok(await ownerPage.locator("[data-wiki-search-section]:not([hidden])").count() >= 1);
  await ownerPage.locator("#wiki-focus-toggle").click();
  assert.equal(await ownerPage.locator("#wiki-focus-toggle").getAttribute("aria-pressed"), "true");
  await ownerPage.locator("#wiki-focus-toggle").click();

  const themeBefore = await ownerPage.locator("html").getAttribute("data-theme");
  await ownerPage.locator("#theme-toggle").click();
  assert.notEqual(await ownerPage.locator("html").getAttribute("data-theme"), themeBefore);
  await ownerPage.locator("#theme-toggle").click();

  await navigateAndAssert(ownerPage, "redaction", "h1", /脱敏与溯源工作台/);
  await ownerPage.locator("[data-testid='redacted-token']").click();
  await ownerPage.locator("#provenance-drawer.is-open").waitFor();
  await ownerPage.keyboard.press("Escape");

  await navigateAndAssert(ownerPage, "audit", "h1", /审计日志/);
  await ownerPage.locator("#audit-search").fill("越权");
  assert.equal((await ownerPage.locator("#audit-filter-status").textContent()).trim(), "1 条匹配事件");
  assert.equal(await ownerPage.locator("#audit-log [data-audit-entry]:not([hidden])").count(), 1);

  await navigateAndAssert(ownerPage, "system", "h1", /系统状态/);
  await ownerPage.locator("[data-testid='operations-console']").waitFor({ state: "visible" });
  await ownerPage.locator("[data-testid='team-access']").waitFor({ state: "visible" });
  await ownerPage.getByText("内置确定性预检", { exact: true }).waitFor();
  assert.equal(await ownerPage.evaluate(async () => (await fetch("/api/admin/operations")).status), 200);

  viewerContext = await browser.newContext({ viewport: { width: 390, height: 844 }, locale: "zh-CN" });
  const viewerPage = await viewerContext.newPage();
  await login(viewerPage, viewerEmail, viewerPassword);
  assert.match(await viewerPage.locator("#profile-role").textContent(), /只读成员/);
  await viewerPage.locator("#mobile-menu").click();
  await viewerPage.locator("[data-testid='nav-wiki']").click();
  assert.equal(await viewerPage.locator("[data-testid='wiki-edit']").isDisabled(), true);
  assert.equal(await viewerPage.locator("#share-wiki").isDisabled(), true);
  await viewerPage.locator("#mobile-menu").click();
  await viewerPage.locator("[data-testid='nav-lifecycle']").click();
  assert.equal(await viewerPage.locator("[data-testid='open-share']").isDisabled(), true);
  assert.equal(await viewerPage.evaluate(async () => (await fetch("/api/shares")).status), 403);
  await viewerPage.waitForTimeout(4_500);
  if (!String(await viewerPage.locator(".sidebar").getAttribute("class")).includes("is-open")) {
    await viewerPage.locator("#mobile-menu").click();
  }
  await viewerPage.locator(".sidebar.is-open").waitFor();
  await viewerPage.waitForTimeout(400);
  await viewerPage.screenshot({ path: path.join(screenshots, "03-mobile-viewer-boundary.png") });

  process.stdout.write("Module tour E2E passed: 9 internal modules, search, asset/provenance, Wiki reading, redaction, audit, operations, theme and viewer RBAC.\n");
} finally {
  await viewerContext?.close();
  await ownerContext.close();
  await browser.close();
  await gateway.stop();
  store.close();
  await rm(runtime, { recursive: true, force: true });
}
