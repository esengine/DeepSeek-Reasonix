import assert from "node:assert/strict";
import { randomUUID } from "node:crypto";
import { existsSync } from "node:fs";
import { createRequire } from "node:module";
import { mkdir, mkdtemp, rm } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { createRealAnalysisServer } from "../server/real-analysis-server.mjs";

const here = path.dirname(fileURLToPath(import.meta.url));
const repoRoot = path.resolve(here, "../..");
const siteRoot = path.join(repoRoot, "site");
const fixture = path.join(here, "fixtures", "intelifar-real-analysis.html");
const screenshots = path.join(repoRoot, "artifacts", "smb-p0-review");
const runtime = await mkdtemp(path.join(os.tmpdir(), "intelifar-smb-ui-"));
await mkdir(screenshots, { recursive: true });
const email = "owner@example.com";
const password = `E2E-${randomUUID()}-Aa!`;

const gateway = await createRealAnalysisServer({
  distRoot: path.join(siteRoot, "dist"),
  databasePath: path.join(runtime, "platform.sqlite"),
  uploadRoot: path.join(runtime, "uploads"),
  config: { mineruApiKey: "e2e-mineru", deepseekApiKey: "e2e-deepseek", deepseekModel: "deepseek-chat" },
  auth: { required: true, secureCookies: false, workspaceId: "WS-SMB-E2E", workspaceName: "澜图科技 · 小微版", email, password, name: "林越" },
  mineruClient: { async parseDocument(file, hooks) { hooks.onProgress({ state: "running", progress: 52, batchId: "batch-smb-ui" }); return { provider: "MinerU", model: "MinerU-HTML", batchId: "batch-smb-ui", traceId: "trace-smb-ui", fileName: file.name, markdown: "# 智能路由技术\n通过工作空间证据生成可追溯 Wiki。" }; } },
  deepseekClient: { async analyzeMarkdown() { return { provider: "DeepSeek", model: "deepseek-chat", responseId: "chat-smb-ui", usage: { totalTokens: 48 }, analysis: { document: { title: "智能路由技术报告", summary: "面向小微研发团队的知识资产沉淀方案", category: "技术设计报告", language: "zh-CN" }, assets: [{ id: "IP-SMB-1", title: "工作空间智能路由方法", type: "技术方案", summary: "将文档证据沉淀为可复核、可编辑的知识资产。", confidence: 0.96, tags: ["路由", "Wiki", "小微企业"], source_quotes: [{ quote: "通过工作空间证据生成可追溯 Wiki。", section: "智能路由技术" }] }], risks: [], wiki: { executive_summary: "将长文档分析结果发布为工作空间内可追溯的 Wiki。", key_mechanism: "MinerU 负责解析，DeepSeek 负责结构化，人工复核后形成版本。", metrics: [{ label: "证据覆盖", value: "100%" }], relationships: [{ source: "智能路由技术报告", relation: "沉淀为", target: "工作空间智能路由方法" }] } } }; } },
});
const baseURL = await gateway.start();
const workspaceRequire = createRequire(path.join(repoRoot, "..", "e2e-runner.cjs"));
const { chromium } = workspaceRequire("playwright");
const candidates = ["C:\\Program Files\\Google\\Chrome\\Application\\chrome.exe", "C:\\Program Files (x86)\\Microsoft\\Edge\\Application\\msedge.exe"];
const executablePath = process.platform === "win32" ? candidates.find(existsSync) : undefined;
const browser = await chromium.launch({ headless: true, executablePath, args: ["--no-proxy-server", "--proxy-bypass-list=<-loopback>"] });
const context = await browser.newContext({ viewport: { width: 1440, height: 1000 }, locale: "zh-CN" });
const page = await context.newPage();

try {
  await page.goto(baseURL, { waitUntil: "domcontentloaded", timeout: 30_000 });
  await page.locator("[data-testid='session-dialog']").waitFor({ state: "visible" });
  await page.screenshot({ path: path.join(screenshots, "01-smb-secure-login.png"), fullPage: true });

  await page.locator("[data-testid='login-email']").fill(email);
  await page.locator("[data-testid='login-password']").fill(password);
  await Promise.all([
    page.waitForResponse((response) => response.url().endsWith("/api/auth/login") && response.status() === 200),
    page.locator("[data-testid='login-submit']").click(),
  ]);
  await page.locator("[data-testid='session-dialog']").waitFor({ state: "hidden" });
  assert.match(await page.locator("[data-testid='workspace-identity']").textContent(), /澜图科技 · 小微版/);
  await page.screenshot({ path: path.join(screenshots, "02-workspace-identity.png"), fullPage: true });

  await page.locator("[data-testid='nav-documents']").click();
  await page.locator("[data-testid='documents-upload']").click();
  await page.locator("[data-testid='real-file-input']").setInputFiles(fixture);
  await page.locator("[data-testid='intake-category']").selectOption({ label: "技术设计报告" });
  await page.locator("[data-testid='start-analysis']").click();
  await page.locator("[data-testid='publish-analysis']").waitFor({ state: "visible" });
  await page.locator("[data-testid='publish-analysis']:not([disabled])").waitFor({ timeout: 20_000 });
  await Promise.all([
    page.waitForResponse((response) => response.url().endsWith("/publish") && [200, 201].includes(response.status())),
    page.locator("[data-testid='publish-analysis']").click(),
  ]);

  await page.locator("[data-testid='nav-wiki']").click();
  await page.locator("#wiki-dynamic-title").waitFor();
  assert.equal((await page.locator("#wiki-version").textContent()).trim(), "V1.0");
  await page.locator("[data-testid='wiki-edit']").click();
  await page.locator("[data-testid='wiki-edit-dialog']").waitFor({ state: "visible" });
  await page.locator("[data-testid='wiki-edit-summary']").fill("人工复核：该方案适用于小微研发团队的知识沉淀与证据治理。");
  await page.locator("[data-testid='wiki-edit-mechanism']").fill("MinerU 解析、DeepSeek 结构化、业务负责人复核，所有修改形成不可覆盖的新版本。");
  await page.locator("#wiki-edit-note").fill("完成首次业务复核");
  await page.screenshot({ path: path.join(screenshots, "03-wiki-version-edit.png"), fullPage: true });
  await Promise.all([
    page.waitForResponse((response) => response.url().includes("/api/wiki/") && response.request().method() === "PATCH" && response.status() === 200),
    page.locator("[data-testid='wiki-save']").click(),
  ]);
  await page.locator("[data-testid='wiki-edit-dialog']").waitFor({ state: "hidden" });
  assert.equal((await page.locator("#wiki-version").textContent()).trim(), "V1.1");
  await page.screenshot({ path: path.join(screenshots, "04-wiki-reviewed-v11.png"), fullPage: true });

  await page.locator("[data-testid='wiki-version-history']").click();
  await page.locator("[data-testid='wiki-history-drawer'].is-open").waitFor();
  await page.locator(".version-card").first().waitFor();
  assert.equal(await page.locator(".version-card").count(), 2);
  await page.waitForTimeout(4_500);
  await page.screenshot({ path: path.join(screenshots, "05-wiki-version-ledger.png"), fullPage: true });

  await page.setViewportSize({ width: 390, height: 844 });
  await page.screenshot({ path: path.join(screenshots, "06-mobile-version-ledger.png") });
  process.stdout.write("SMB authenticated Wiki E2E passed: login, workspace isolation, publish, edit and V1.1 history.\n");
} finally {
  await context.close();
  await browser.close();
  await gateway.stop();
  await rm(runtime, { recursive: true, force: true });
}
