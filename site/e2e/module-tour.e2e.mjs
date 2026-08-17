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
const memberScreenshots = path.join(repoRoot, "artifacts", "member-permissions-module-2026-08-11");
const runtime = await mkdtemp(path.join(os.tmpdir(), "intelifar-module-tour-"));
const workspaceId = "WS-MODULE-TOUR";
const ownerEmail = "owner-modules@example.com";
const ownerPassword = `Owner-${randomUUID()}-Aa!`;
const viewerEmail = "viewer-modules@example.com";
const viewerPassword = `Viewer-${randomUUID()}-Bb!`;
const assetId = "IP-REAL-FACE01";
const batchAssetId = "IP-REAL-FACE02";
const duplicateAssetId = "IP-REAL-FACE03";
await mkdir(screenshots, { recursive: true });
await mkdir(memberScreenshots, { recursive: true });

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
    owner: "待确权",
    sensitivity: "待复核",
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
  }, {
    id: batchAssetId,
    sourceAssetId: "IP-MODULE-2",
    publicationId: "PUB-MODULE-TOUR",
    sourceJobId: "JOB-MODULE-TOUR",
    title: "批量资产治理方法",
    type: "业务规则",
    summary: "在同一批处理中确认多项资产的权属和敏感级别。",
    tags: ["批量治理", "待办中心"],
    confidence: 0.96,
    owner: "待确权",
    sensitivity: "待复核",
    status: "已发布",
    version: "V1.0",
    publishedAt: "2026-08-10T10:30:00.000Z",
    document: { sourceName: "模块巡检技术白皮书.pdf", markdownSha256: "a".repeat(64), parserBatchId: "batch-module-tour" },
    evidence: [{ id: "EV-FACE02", assetId: batchAssetId, quote: "同一批资产可以统一确认权属和敏感级别。", section: "资产治理", locator: "资产治理", quoteHash: "c".repeat(64), documentHash: "a".repeat(64), documentName: "模块巡检技术白皮书.pdf", parserBatchId: "batch-module-tour", precision: "章节级", verified: true }],
    wiki: {
      title: "批量资产治理方法",
      executiveSummary: "通过一次确认完成同类待治理资产的权属和敏感级别设置。",
      keyMechanism: "先核对选择范围，再统一设置负责人和敏感级别，最后留存操作记录。",
      metrics: [{ label: "批量上限", value: "50 项" }],
      relationships: [{ source: "待治理资产", relation: "批量完善", target: "可使用资产" }],
    },
  }, {
    id: duplicateAssetId,
    sourceAssetId: "IP-MODULE-3",
    publicationId: "PUB-MODULE-TOUR",
    sourceJobId: "JOB-MODULE-TOUR",
    title: "intelifar 全模块知识治理方法",
    type: "技术方案",
    summary: "同一知识资产的较早来源记录。",
    tags: ["历史来源"],
    confidence: 0.95,
    owner: "待确权",
    sensitivity: "待复核",
    status: "已发布",
    version: "V1.0",
    publishedAt: "2026-08-09T10:30:00.000Z",
    document: { sourceName: "模块巡检技术白皮书.pdf", markdownSha256: "a".repeat(64), parserBatchId: "batch-module-tour-old" },
    evidence: [{ id: "EV-FACE03", assetId: duplicateAssetId, quote: "旧来源记录也必须随独立资产一起完成治理。", section: "历史记录", locator: "历史记录", quoteHash: "d".repeat(64), documentHash: "a".repeat(64), documentName: "模块巡检技术白皮书.pdf", parserBatchId: "batch-module-tour-old", precision: "章节级", verified: true }],
    wiki: {
      title: "intelifar 全模块知识治理方法",
      executiveSummary: "历史来源记录仍应保留并随独立资产统一治理。",
      keyMechanism: "合并展示不删除来源记录，治理操作同步全部来源。",
      metrics: [],
      relationships: [],
    },
  }],
});
store.createUser({ id: "USR-EDITOR-MODULES", workspaceId, email: "editor-modules@example.com", name: "周岚", role: "editor", passwordHash: await hashPassword(`Editor-${randomUUID()}-Cc!`) });
store.submitWikiReview(workspaceId, assetId, { baseVersion: "V1.0", title: "intelifar 全模块知识治理方法", executiveSummary: "统一完成文档、分析、资产、Wiki、风险处置、分享和操作记录。", keyMechanism: "文档读取、知识提取、人工复核并形成版本化知识。", changeNote: "改为企业用户可理解的业务表述", submittedByUserId: "USR-EDITOR-MODULES" });

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
  await ownerPage.locator("#asset-table-body .published-row").first().waitFor({ state: "attached" });
  assert.match(await ownerPage.locator("[data-testid='workspace-identity']").textContent(), /用户手册验收空间/);

  const modules = [
    ["overview", "h1", /你好|早上好/],
    ["tasks", "h1", /待办中心/],
    ["documents", "h1", /文档中心/],
    ["analysis", "h1", /智能分析/],
    ["assets", "h1", /IP 资产库/],
    ["wiki", "#wiki-dynamic-title", /稀疏专家路由|全模块知识治理/],
    ["redaction", "h1", /敏感信息与风险线索/],
    ["lifecycle", "h1", /IP 全生命周期/],
    ["audit", "h1", /操作记录/],
    ["members", "h1", /成员与权限/],
    ["system", "h1", /系统状态/],
  ];
  for (const [view, selector, heading] of modules) await navigateAndAssert(ownerPage, view, selector, heading);

  await navigateAndAssert(ownerPage, "overview", "h1", /你好|早上好/);
  assert.doesNotMatch(await ownerPage.locator("#view-overview").textContent(), /12\.4%/);
  assert.match(await ownerPage.locator("#metric-documents-total").textContent(), /正在处理 \d+/);
  await ownerPage.waitForTimeout(4_500);
  await ownerPage.screenshot({ path: path.join(screenshots, "01-module-overview.png"), fullPage: true });

  await navigateAndAssert(ownerPage, "tasks", "h1", /待办中心/);
  await ownerPage.locator("[data-workspace-action^='wiki-review:']").waitFor();
  await ownerPage.locator("[data-workspace-action='asset-governance']").waitFor();
  assert.match(await ownerPage.locator("#workspace-action-list").textContent(), /审批 Wiki 更新/);
  assert.match(await ownerPage.locator("[data-workspace-action='asset-governance']").textContent(), /负责岗位：知识编辑者/);
  assert.match(await ownerPage.locator("[data-workspace-action='asset-governance']").textContent(), /建议三天内完成/);
  assert.equal(await ownerPage.locator(".wiki-review-decision[data-decision='approved']").count(), 1);
  await ownerPage.screenshot({ path: path.join(screenshots, "04-action-center.png"), fullPage: true });
  await ownerPage.locator("[data-action-filter='governance']").click();
  assert.match(await ownerPage.locator("#action-filter-status").textContent(), /^1 项当前岗位待办$/);
  await ownerPage.locator("[data-workspace-action='asset-governance']").getByRole("button", { name: "批量处理 2 项" }).click();
  await ownerPage.locator("#asset-governance-dialog").waitFor({ state: "visible" });
  assert.match(await ownerPage.locator("#asset-governance-selection-count").textContent(), /已选择 2 项/);
  assert.equal(await ownerPage.locator("#asset-governance-selection-list input[type='checkbox']").count(), 2);
  await ownerPage.locator("#asset-governance-select-none").click();
  assert.match(await ownerPage.locator("#asset-governance-selection-count").textContent(), /已选择 0 项/);
  assert.equal(await ownerPage.locator("[data-testid='asset-governance-save']").isDisabled(), true);
  await ownerPage.locator("#asset-governance-select-all").click();
  assert.match(await ownerPage.locator("#asset-governance-selection-count").textContent(), /已选择 2 项/);
  await ownerPage.locator("#asset-governance-owner").fill("知识平台主管");
  await ownerPage.locator("#asset-governance-sensitivity").selectOption("内部");
  await ownerPage.screenshot({ path: path.join(screenshots, "08-batch-asset-governance.png") });
  await ownerPage.locator("[data-testid='asset-governance-save']").click();
  await ownerPage.locator("#asset-governance-dialog").waitFor({ state: "hidden" });
  await ownerPage.locator("[data-workspace-action='asset-governance']").waitFor({ state: "detached" });
  const governedAssets = await ownerPage.evaluate(async () => (await (await fetch("/api/assets")).json()).assets);
  assert.equal(governedAssets.length, 3);
  assert.equal(governedAssets.every((asset) => asset.owner === "知识平台主管" && asset.sensitivity === "内部"), true);
  assert.match(await ownerPage.locator("#action-filter-status").textContent(), /^0 项当前岗位待办$/);
  await ownerPage.locator("[data-action-filter='content']").click();
  assert.equal(await ownerPage.locator("#workspace-action-list [data-workspace-action]:not([hidden])").count(), 1);

  await navigateAndAssert(ownerPage, "documents", "h1", /文档中心/);
  await ownerPage.locator("#document-advanced-filter").click();
  await ownerPage.locator("#document-advanced-filters").waitFor({ state: "visible" });
  assert.equal(await ownerPage.locator("#document-advanced-filter").getAttribute("aria-expanded"), "true");
  await ownerPage.locator("#document-action-filter").check();
  assert.match(await ownerPage.locator("#document-filter-status").textContent(), /^0 \/ 1 份文档$/);
  await ownerPage.locator("#document-filter-reset").click();
  assert.match(await ownerPage.locator("#document-filter-status").textContent(), /^1 \/ 1 份文档$/);

  await navigateAndAssert(ownerPage, "analysis", "h1", /智能分析/);
  assert.match(await ownerPage.locator("#analysis-live-text").textContent(), /历史文档形成 2 项独立资产/);
  assert.equal(await ownerPage.locator("#analysis-run-log").isDisabled(), true);

  await navigateAndAssert(ownerPage, "assets", "h1", /IP 资产库/);
  await ownerPage.locator("#asset-tag-filter").click();
  await ownerPage.locator("#asset-tag-filter-panel").waitFor({ state: "visible" });
  await ownerPage.locator("#asset-tag-options [data-asset-tag]", { hasText: "用户手册" }).click();
  assert.equal(await ownerPage.locator("#asset-table-body .published-row:not([hidden])").count(), 1);
  assert.match(await ownerPage.locator("#asset-total").textContent(), /用户手册/);
  await ownerPage.locator("#asset-tag-reset").click();

  await ownerPage.locator("#global-search").fill("批量资产治理方法");
  await ownerPage.locator("#global-search-results [data-search-asset-id]", { hasText: "批量资产治理方法" }).first().waitFor();
  assert.match(await ownerPage.locator("#global-search-results [data-search-asset-id]").first().textContent(), /批量资产治理方法/);
  await ownerPage.locator("#global-search").fill("角色化浏览器巡检");
  await ownerPage.locator("#global-search-results [data-search-asset-id]", { hasText: "原文依据" }).first().waitFor();
  assert.match(await ownerPage.locator("#global-search-results").textContent(), /原文依据/);
  await ownerPage.screenshot({ path: path.join(screenshots, "05-global-search.png") });
  await ownerPage.locator("#global-search").press("Enter");
  await ownerPage.locator("#view-wiki").waitFor({ state: "visible" });
  assert.match(await ownerPage.locator("#wiki-dynamic-title").textContent(), /全模块知识治理/);
  await navigateAndAssert(ownerPage, "assets", "h1", /IP 资产库/);
  const publishedRow = ownerPage.locator("#asset-table-body .published-row", { hasText: "全模块知识治理" });
  await publishedRow.waitFor();
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

  await ownerPage.locator("#wiki-edit").click();
  await ownerPage.locator("#wiki-edit-dialog").waitFor({ state: "visible" });
  assert.equal(await ownerPage.locator("[data-testid='wiki-save']").isDisabled(), true);
  assert.match(await ownerPage.locator("#wiki-change-count").textContent(), /没有变化/);
  await ownerPage.locator("#wiki-edit-note").fill("只填写版本说明不会创建版本");
  assert.equal(await ownerPage.locator("[data-testid='wiki-save']").isDisabled(), true);
  await ownerPage.locator("#wiki-edit-summary").fill(`${await ownerPage.locator("#wiki-edit-summary").inputValue()}（验收预览）`);
  assert.equal(await ownerPage.locator("[data-testid='wiki-save']").isDisabled(), false);
  assert.match(await ownerPage.locator("#wiki-change-count").textContent(), /1 个内容区域将形成新版本/);
  assert.match(await ownerPage.locator("#wiki-change-list").textContent(), /核心摘要/);
  await ownerPage.locator("#wiki-edit-dialog").getByRole("button", { name: "取消" }).click();
  await ownerPage.locator("#wiki-edit-dialog").waitFor({ state: "hidden" });
  assert.equal(await ownerPage.locator("#wiki-focus-toggle").getAttribute("aria-pressed"), "true");
  await ownerPage.locator("#wiki-focus-toggle").click();

  const themeBefore = await ownerPage.locator("html").getAttribute("data-theme");
  await ownerPage.locator("#theme-toggle").click();
  assert.notEqual(await ownerPage.locator("html").getAttribute("data-theme"), themeBefore);
  await ownerPage.locator("#theme-toggle").click();
  await ownerPage.waitForTimeout(4_300);

  await navigateAndAssert(ownerPage, "assets", "h1", /IP 资产库/);
  await ownerPage.locator("#asset-import-from-document").click();
  await ownerPage.locator("#intake-dialog").waitFor({ state: "visible" });
  assert.match(await ownerPage.locator("#intake-dialog").textContent(), /验证并开始分析/);
  await ownerPage.locator("#intake-dialog").getByRole("button", { name: "取消" }).click();

  await navigateAndAssert(ownerPage, "redaction", "h1", /敏感信息与风险线索/);
  await ownerPage.locator("#redaction-real-workspace").waitFor({ state: "visible" });
  assert.equal(await ownerPage.locator("[data-demo-redaction]:visible").count(), 0);
  assert.match((await ownerPage.locator("#redaction-risk-total").textContent()).trim(), /^\d+$/);
  assert.ok(await ownerPage.locator("#real-risk-workspace-list > *").count() >= 1);

  await navigateAndAssert(ownerPage, "audit", "h1", /操作记录/);
  const auditTerm = (await ownerPage.locator("#audit-log [data-audit-entry] strong").first().textContent()).trim();
  await ownerPage.locator("#audit-search").fill(auditTerm);
  assert.match((await ownerPage.locator("#audit-filter-status").textContent()).trim(), /^[1-9]\d* 条匹配记录$/);
  assert.ok(await ownerPage.locator("#audit-log [data-audit-entry]:not([hidden])").count() >= 1);
  await ownerPage.locator("#audit-search").fill("");
  await ownerPage.locator("#audit-category-filter").selectOption("content");
  assert.ok(await ownerPage.locator("#audit-log [data-audit-entry]:not([hidden])").count() >= 1);
  await ownerPage.screenshot({ path: path.join(screenshots, "06-operation-records.png"), fullPage: true });

  await navigateAndAssert(ownerPage, "system", "h1", /系统状态/);
  assert.equal(await ownerPage.locator("details.technical-details").getAttribute("open"), null);
  await ownerPage.locator("[data-testid='operations-console']").waitFor({ state: "visible" });
  await ownerPage.locator("[data-testid='system-member-summary']").waitFor({ state: "visible" });
  await ownerPage.getByText("内置确定性预检", { exact: true }).waitFor();
  assert.equal(await ownerPage.evaluate(async () => (await fetch("/api/admin/operations")).status), 200);
  await ownerPage.screenshot({ path: path.join(memberScreenshots, "04-system-member-summary-desktop.png"), fullPage: true });

  await navigateAndAssert(ownerPage, "members", "h1", /成员与权限/);
  await ownerPage.locator("[data-testid='team-access']").waitFor({ state: "visible" });
  assert.match(await ownerPage.locator("#member-count").textContent(), /位成员/);
  assert.match(await ownerPage.locator("[data-testid='member-readiness']").textContent(), /第二位管理员/);
  await ownerPage.screenshot({ path: path.join(memberScreenshots, "05-member-overview-desktop.png"), fullPage: true });
  const viewerMemberRow = () => ownerPage.locator("#member-ledger article", { hasText: viewerEmail });
  assert.match(await viewerMemberRow().textContent(), /尚未登录/);
  await ownerPage.locator("#member-search").fill("不存在的成员");
  assert.match(await ownerPage.locator("#member-ledger").textContent(), /没有符合当前条件的成员/);
  await ownerPage.locator("#member-search").fill("");
  await viewerMemberRow().locator("[data-member-role]").selectOption("editor");
  await ownerPage.locator("[data-testid='member-change-dialog']").waitFor({ state: "visible" });
  assert.match(await ownerPage.locator("#member-change-impact").textContent(), /接入文档/);
  await ownerPage.screenshot({ path: path.join(memberScreenshots, "06-role-change-confirmation-desktop.png") });
  await Promise.all([
    ownerPage.waitForResponse((response) => response.url().includes("/api/admin/members/USR-VIEWER-MODULES") && response.status() === 200),
    ownerPage.locator("[data-testid='confirm-member-change']").click(),
  ]);
  await viewerMemberRow().locator("[data-member-role]").selectOption("viewer");
  await ownerPage.locator("[data-testid='member-change-dialog']").waitFor({ state: "visible" });
  assert.match(await ownerPage.locator("#member-change-impact").textContent(), /不能接入文档/);
  await Promise.all([
    ownerPage.waitForResponse((response) => response.url().includes("/api/admin/members/USR-VIEWER-MODULES") && response.status() === 200),
    ownerPage.locator("[data-testid='confirm-member-change']").click(),
  ]);

  viewerContext = await browser.newContext({ viewport: { width: 390, height: 844 }, locale: "zh-CN" });
  const viewerPage = await viewerContext.newPage();
  await login(viewerPage, viewerEmail, viewerPassword);
  assert.match(await viewerPage.locator("#profile-role").textContent(), /只读成员/);
  assert.match(await viewerPage.locator("#readonly-role-note").textContent(), /只读岗位/);
  assert.equal(await viewerPage.locator("[data-testid='top-new-analysis']").isHidden(), true);
  assert.equal(await viewerPage.locator("[data-testid='nav-audit']").isHidden(), true);
  assert.equal(await viewerPage.locator("[data-testid='nav-members']").isHidden(), true);
  assert.equal(await viewerPage.evaluate(async () => (await fetch("/api/audit")).status), 403);
  assert.equal(await viewerPage.evaluate(async () => (await fetch("/api/admin/members")).status), 403);
  await viewerPage.locator("#mobile-menu").click();
  await viewerPage.locator(".sidebar.is-open").waitFor();
  await viewerPage.locator("[data-testid='open-my-permissions']").click();
  await viewerPage.locator("[data-testid='my-permissions-dialog']").waitFor({ state: "visible" });
  assert.match(await viewerPage.locator("[data-testid='my-permissions-dialog']").textContent(), /只读成员/);
  await viewerPage.screenshot({ path: path.join(memberScreenshots, "07-viewer-my-permissions-mobile.png") });
  await viewerPage.locator("#my-permissions-dialog [data-close-dialog]").last().click();
  assert.equal(await viewerPage.evaluate(async () => {
    const formData = new FormData();
    formData.set("title", "只读成员越权接入检查");
    formData.set("category", "测试");
    formData.set("file", new File(["viewer boundary"], "viewer-boundary.txt", { type: "text/plain" }));
    return (await fetch("/api/analysis", { method: "POST", body: formData })).status;
  }), 403);
  await viewerPage.locator("[data-testid='nav-documents']").click();
  assert.match(await viewerPage.locator("#document-role-note").textContent(), /当前为只读岗位/);
  assert.equal(await viewerPage.locator("[data-testid='documents-upload']").isHidden(), true);
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

  process.stdout.write("Module tour E2E passed: 11 internal modules, role-aware action center, atomic batch asset governance, relevance-ranked cross-Wiki search, publication approval, independent member governance, provenance, business operation records, operations and viewer RBAC.\n");
} finally {
  await viewerContext?.close();
  await ownerContext.close();
  await browser.close();
  await gateway.stop();
  store.close();
  await rm(runtime, { recursive: true, force: true });
}
