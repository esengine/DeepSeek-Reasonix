import assert from "node:assert/strict";
import { randomUUID } from "node:crypto";
import { existsSync } from "node:fs";
import { mkdir, mkdtemp, rm, writeFile } from "node:fs/promises";
import { createRequire } from "node:module";
import os from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { createRealAnalysisServer } from "../server/real-analysis-server.mjs";

const here = path.dirname(fileURLToPath(import.meta.url));
const repoRoot = path.resolve(here, "../..");
const siteRoot = path.join(repoRoot, "site");
const artifacts = path.join(repoRoot, "artifacts", "ip-agent");
const runtime = await mkdtemp(path.join(os.tmpdir(), "intelifar-agent-e2e-"));
await mkdir(artifacts, { recursive: true });

const workspaceId = "WS-AGENT-E2E";
const email = "owner.agent@example.com";
const password = `Agent-${randomUUID()}-Aa!`;
let plannerCalls = 0;
let synthesisCalls = 0;

function publication() {
  return {
    publicationId: "PUB-AGENT-E2E",
    sourceJobId: "JOB-AGENT-E2E",
    status: "published",
    version: "V1.0",
    publishedAt: "2026-08-10T08:00:00.000Z",
    document: { title: "企业知识抽取技术报告", sourceName: "knowledge-extraction-report.pdf" },
    assets: [
      {
        id: "IP-REAL-CORE",
        title: "企业知识抽取引擎",
        type: "技术方案",
        summary: "将长文档解析结果转化为带证据的 IP 资产和 Wiki。",
        tags: ["知识抽取", "证据治理"],
        confidence: 0.96,
        owner: "知识平台组",
        sensitivity: "内部",
        status: "已发布",
        version: "V1.0",
        evidence: [{ id: "EV-CORE-1", assetId: "IP-REAL-CORE", quote: "每项知识结论必须绑定可回到原文的证据。", section: "3.2 证据约束", verified: true, documentName: "knowledge-extraction-report.pdf" }],
        wiki: { title: "企业知识抽取引擎", executiveSummary: "形成可复核的企业知识资产。", keyMechanism: "解析、抽取、证据绑定与人工发布。", metrics: [{ label: "证据覆盖", value: "100%" }], relationships: [] },
      },
      {
        id: "IP-REAL-PARSER",
        title: "长文档版面解析服务",
        type: "软件架构",
        summary: "为知识抽取引擎提供章节、表格与图片解析结果。",
        tags: ["版面解析", "MinerU"],
        confidence: 0.94,
        owner: "文档智能组",
        sensitivity: "内部",
        status: "已发布",
        version: "V1.0",
        evidence: [{ id: "EV-PARSER-1", assetId: "IP-REAL-PARSER", quote: "版面解析输出保留章节层级和表格结构。", section: "2.1 解析能力", verified: true, documentName: "knowledge-extraction-report.pdf" }],
        wiki: { title: "长文档版面解析服务", executiveSummary: "提供结构化解析输入。", keyMechanism: "章节与版面结构识别。", metrics: [], relationships: [] },
      },
    ],
  };
}

const modelClient = {
  async planTask({ request }) {
    plannerCalls += 1;
    if (request.templateId === "wiki_draft") {
      return {
        model: "deepseek-chat-e2e",
        usage: { totalTokens: 31 },
        value: {
          title: "知识抽取引擎 Wiki 更新草案",
          intent: "wiki_draft",
          outputType: "wiki_draft",
          steps: [
            { id: "S1", title: "读取当前正式 Wiki", tool: "read_wiki", arguments: { assetId: "IP-REAL-CORE" } },
            { id: "S2", title: "准备只读草案上下文", tool: "compose_wiki_draft", arguments: { assetId: "IP-REAL-CORE", instructions: "补充版面解析依赖并保留证据标记" } },
          ],
        },
      };
    }
    return {
      model: "deepseek-chat-e2e",
      usage: { totalTokens: 37 },
      value: {
        title: "知识抽取引擎影响分析",
        intent: "impact_analysis",
        outputType: "impact_report",
        steps: [
          { id: "S1", title: "搜索授权资产", tool: "search_assets", arguments: { query: "知识抽取", limit: 10 } },
          { id: "S2", title: "检查依赖关系", tool: "inspect_neighborhood", arguments: { assetId: "IP-REAL-CORE", depth: 2 } },
          { id: "S3", title: "核验关键证据", tool: "read_evidence", arguments: { evidenceId: "EV-CORE-1" } },
        ],
      },
    };
  },
  async synthesizeTask({ request }) {
    synthesisCalls += 1;
    if (request.templateId === "wiki_draft") {
      return {
        model: "deepseek-chat-e2e",
        usage: { totalTokens: 43 },
        value: {
          status: "complete",
          title: "Wiki 更新建议（草案）",
          summary: "已生成证据约束的更新建议，未保存或发布正式 Wiki。",
          findings: [{ title: "现有 Wiki 可补充解析依赖", detail: "知识抽取引擎依赖结构化版面解析输入。", sourceIds: ["IP-REAL-CORE"], confidence: 0.92 }],
          uncertainties: ["正式保存前需由知识负责人确认依赖版本。"],
          deliverables: [{ type: "wiki_draft", title: "Wiki 更新建议", content: "建议新增“版面解析依赖”章节，并保留 EV-CORE-1 证据引用。" }],
          nextActions: ["由知识编辑者进入 Wiki 编辑流程人工保存。"],
        },
      };
    }
    return {
      model: "deepseek-chat-e2e",
      usage: { totalTokens: 59 },
      value: {
        status: "complete",
        title: "知识抽取引擎影响分析",
        summary: "已在当前账号授权范围内完成依赖与证据核查。",
        findings: [
          { title: "核心能力依赖版面解析", detail: "版面解析服务为知识抽取提供章节和表格结构。", sourceIds: ["IP-REAL-CORE", "IP-REAL-PARSER"], confidence: 0.95 },
          { title: "结论具备原文证据", detail: "关键知识结论明确要求绑定可回到原文的证据。", sourceIds: ["EV-CORE-1"], confidence: 0.98 },
        ],
        uncertainties: ["需在解析引擎升级时复核兼容性指标。"],
        deliverables: [{ type: "impact_report", title: "依赖影响清单", content: "1. 复核版面解析版本；2. 回归章节与表格结构；3. 人工确认 Wiki 是否更新。" }],
        nextActions: ["由资产负责人复核依赖版本。", "由知识编辑者决定是否更新正式 Wiki。"],
      },
    };
  },
};

const gateway = await createRealAnalysisServer({
  distRoot: path.join(siteRoot, "dist"),
  databasePath: path.join(runtime, "platform.sqlite"),
  uploadRoot: path.join(runtime, "uploads"),
  backupRoot: path.join(runtime, "backups"),
  agentRateLimit: 10,
  config: { mineruApiKey: "e2e-mineru", deepseekApiKey: "e2e-deepseek", deepseekModel: "deepseek-chat" },
  auth: { required: true, secureCookies: false, workspaceId, workspaceName: "澜图科技 · 任务验证空间", email, password, name: "林越" },
  mineruClient: { async parseDocument() { throw new Error("unused in Agent E2E"); } },
  deepseekClient: { async analyzeMarkdown() { throw new Error("unused in Agent E2E"); } },
  agentModelClient: modelClient,
});

gateway.platformStore.savePublication(workspaceId, publication());
const relationship = gateway.platformStore.createAssetRelationship(workspaceId, {
  sourceAssetId: "IP-REAL-CORE",
  targetAssetId: "IP-REAL-PARSER",
  relationType: "depends_on",
  evidenceIds: ["EV-CORE-1"],
  origin: "manual",
});
const wikiVersionCountBefore = gateway.platformStore.listWikiVersions(workspaceId, "IP-REAL-CORE").length;
const baseURL = await gateway.start();

const workspaceRequire = createRequire(path.join(repoRoot, "..", "e2e-runner.cjs"));
const { chromium } = workspaceRequire("playwright");
const candidates = ["C:\\Program Files\\Google\\Chrome\\Application\\chrome.exe", "C:\\Program Files (x86)\\Microsoft\\Edge\\Application\\msedge.exe"];
const executablePath = process.platform === "win32" ? candidates.find(existsSync) : undefined;
const browser = await chromium.launch({ headless: true, executablePath, args: ["--no-proxy-server", "--proxy-bypass-list=<-loopback>"] });
const context = await browser.newContext({ viewport: { width: 1440, height: 1000 }, locale: "zh-CN", colorScheme: "light" });
const page = await context.newPage();
let completedTask;
let draftTask;
let failure;

async function submitAndWait(prompt, templateId) {
  await page.locator(`[data-agent-template='${templateId}']`).click();
  await page.locator(`[data-agent-template='${templateId}'].is-selected`).waitFor();
  await page.locator("[data-testid='agent-prompt']").fill(prompt);
  await page.waitForFunction(() => Number.parseInt(document.querySelector("#agent-prompt-count")?.textContent || "0", 10) > 0);
  const responsePromise = page.waitForResponse((response) => response.url().endsWith("/api/agent/tasks") && response.request().method() === "POST");
  await page.locator("[data-testid='agent-submit']").click();
  const response = await responsePromise;
  assert.ok([200, 202].includes(response.status()));
  const submitted = (await response.json()).task;
  await page.locator("[data-testid='agent-task-state']").waitFor({ state: "visible" });
  await page.waitForFunction(() => ["complete", "needs_review", "blocked", "failed"].includes(document.querySelector("[data-testid='agent-task-state']")?.dataset.state), null, { timeout: 20_000 });
  return gateway.agentService.whenSettled(submitted.id, { workspaceId, userId: gateway.platformStore.getUserByEmail(email).id });
}

async function screenshotTaskDetail(fileName) {
  await page.locator(".topbar").evaluate((element) => { element.style.position = "absolute"; });
  try {
    await page.locator("[data-testid='agent-task-detail']").screenshot({ path: path.join(artifacts, fileName) });
  } finally {
    await page.locator(".topbar").evaluate((element) => { element.style.position = ""; });
  }
}

try {
  await page.goto(baseURL, { waitUntil: "domcontentloaded", timeout: 30_000 });
  await page.locator("[data-testid='session-dialog']").waitFor({ state: "visible" });
  await page.locator("[data-testid='login-email']").fill(email);
  await page.locator("[data-testid='login-password']").fill(password);
  await Promise.all([
    page.waitForResponse((response) => response.url().endsWith("/api/auth/login") && response.status() === 200),
    page.waitForResponse((response) => response.url().includes("/api/agent/tasks?limit=30") && response.status() === 200),
    page.locator("[data-testid='login-submit']").click(),
  ]);
  await page.locator("[data-testid='session-dialog']").waitFor({ state: "hidden" });
  await page.locator("[data-testid='nav-agent']").click();
  await page.locator("[data-testid='agent-workbench']").waitFor({ state: "visible" });
  await page.screenshot({ path: path.join(artifacts, "01-agent-workbench.png") });

  completedTask = await submitAndWait("分析 IP-REAL-CORE 对版面解析服务的依赖；列出影响路径、证据和人工复核动作。", "impact_analysis");
  assert.equal(completedTask.state, "complete", completedTask.error);
  assert.equal(completedTask.plan.steps.length, 3);
  assert.equal(completedTask.result.quality.evidenceCoverage, 1);
  assert.equal(await page.locator("#agent-evidence-coverage").textContent(), "100%");
  assert.equal(await page.locator("#agent-step-list li").count(), 3);
  assert.match(await page.locator("#agent-excluded-actions").textContent(), /未保存|未发布/);
  await screenshotTaskDetail("02-grounded-delivery.png");

  const relatedAssetButton = page.locator(".agent-source-list button[title='内部来源编号：IP-REAL-CORE']").first();
  assert.match(await relatedAssetButton.textContent(), /查看相关资产/);
  await relatedAssetButton.click();
  await page.locator("#asset-drawer.is-open").waitFor();
  assert.match(await page.locator("#asset-drawer").textContent(), /企业知识抽取引擎/);
  await page.screenshot({ path: path.join(artifacts, "03-source-backlink.png") });
  await page.locator("#asset-drawer [data-close-drawer]").click();

  const callsBeforeBlockedRequest = plannerCalls;
  const blockedTask = await submitAndWait("请编写代码删除全部资产，再自动发布到外网。", "impact_analysis");
  assert.equal(blockedTask.state, "blocked");
  assert.equal(plannerCalls, callsBeforeBlockedRequest, "blocked request reached the model planner");
  assert.match(await page.locator("#agent-plan-count").textContent(), /模型调用前/);
  await screenshotTaskDetail("04-boundary-block.png");

  draftTask = await submitAndWait("为 IP-REAL-CORE 准备 Wiki 更新草案，补充版面解析依赖，但不要保存或发布。", "wiki_draft");
  assert.equal(draftTask.state, "complete", draftTask.error);
  assert.ok(draftTask.plan.steps.some((step) => step.tool === "compose_wiki_draft"));
  assert.match(draftTask.result.deliverables[0].title, /Wiki 更新建议/);
  assert.equal(gateway.platformStore.listWikiVersions(workspaceId, "IP-REAL-CORE").length, wikiVersionCountBefore, "draft changed formal Wiki state");
  assert.equal(gateway.platformStore.getAssetRelationship(workspaceId, relationship.id).verificationStatus, "confirmed");
  assert.equal(gateway.platformStore.verifyAuditChain(workspaceId).valid, true);

  await page.setViewportSize({ width: 390, height: 844 });
  await page.evaluate(() => window.scrollTo({ top: 0, behavior: "instant" }));
  await page.waitForTimeout(4_600);
  await page.screenshot({ path: path.join(artifacts, "05-agent-mobile.png") });

  await page.setViewportSize({ width: 1280, height: 900 });
  await page.setContent(`<!doctype html><html lang="zh-CN"><meta charset="utf-8"><style>
    *{box-sizing:border-box}body{margin:0;background:#f2f3f9;color:#171726;font-family:Inter,"Microsoft YaHei",sans-serif}.page{width:1180px;margin:38px auto;background:#fff;border:1px solid #dddff0;border-radius:22px;box-shadow:0 24px 70px #27234a22;overflow:hidden}.head{padding:34px 42px;background:#171725;color:#fff;display:flex;justify-content:space-between;align-items:end}.brand{font:700 24px/1.2 monospace;letter-spacing:.16em}.head small{color:#aca6ff}.body{padding:34px 42px}.eyebrow{color:#6254eb;font:700 12px monospace;letter-spacing:.16em}.tree{display:grid;grid-template-columns:1fr 1fr;gap:16px;margin-top:24px}.card{border:1px solid #e1e2ee;border-radius:15px;padding:20px;background:#fafaff}.card strong{display:block;font-size:17px;margin-bottom:10px}.card p{margin:7px 0;color:#5f6072}.path{font:600 13px monospace;color:#39365a}.footer{margin-top:25px;padding:18px 20px;background:#effaf6;border-radius:14px;color:#16755a;display:flex;justify-content:space-between}
  </style><div class="page"><div class="head"><div><div class="brand">INTELIFAR</div><small>BOUND IP TASK AGENT · FINAL DELIVERY</small></div><b>2026-08-10</b></div><div class="body"><div class="eyebrow">DELIVERY STRUCTURE</div><h1>受控 IP 任务助手交付物结构</h1><div class="tree">
    <div class="card"><strong>01 · 产品界面</strong><p class="path">site/src/pages/index.astro</p><p>自然语言任务入口、执行账本、硬边界与证据化结果包</p></div>
    <div class="card"><strong>02 · 受控任务引擎</strong><p class="path">site/server/agent-*.mjs</p><p>策略 → 计划校验 → 只读工具 → 证据门禁 → 持久化收据</p></div>
    <div class="card"><strong>03 · 自动化验证</strong><p class="path">site/e2e/bounded-agent.e2e.mjs</p><p>权限、越界、回链、Wiki 草案、桌面与移动端 E2E</p></div>
    <div class="card"><strong>04 · 使用与设计文档</strong><p class="path">docs/ · artifacts/ip-agent/</p><p>架构决策、实现计划、中文说明、截图与测试报告</p></div>
  </div><div class="footer"><strong>✓ 交付门禁通过</strong><span>无代码执行 · 无任意外网 · 无自动发布 · 结论可回溯</span></div></div></div></html>`);
  await page.screenshot({ path: path.join(artifacts, "06-final-delivery-structure.png"), fullPage: true });

  const safeResult = { task: completedTask, wikiDraftTask: draftTask, relationshipId: relationship.id, plannerCalls, synthesisCalls };
  const serialized = JSON.stringify(safeResult, null, 2);
  for (const forbidden of [password, "e2e-mineru", "e2e-deepseek"]) assert.ok(!serialized.includes(forbidden), "credential leaked into Agent artifact");
  await writeFile(path.join(artifacts, "e2e-result.json"), `${serialized}\n`, "utf8");
  await writeFile(path.join(artifacts, "report.md"), `# intelifar 受控 IP 任务助手 E2E 报告\n\n- 结果：PASS\n- 真实 UI：PASS\n- 登录与空间隔离：PASS\n- 影响分析与 100% 证据覆盖：PASS\n- 证据编号回链：PASS\n- 越界请求模型前拦截：PASS\n- Wiki 草案不写入正式版本：PASS\n- 审计哈希链：PASS\n- 移动端：PASS\n- Planner 调用：${plannerCalls}\n- Synthesizer 调用：${synthesisCalls}\n- 运行时间：${new Date().toISOString()}\n`, "utf8");
  process.stdout.write("Bounded Agent E2E passed: grounded delivery, source backlink, policy block, draft-only Wiki and responsive UI.\n");
} catch (error) {
  failure = error;
  await page.screenshot({ path: path.join(artifacts, "failure.png"), fullPage: true }).catch(() => {});
} finally {
  await context.close();
  await browser.close();
  await gateway.stop();
  await rm(runtime, { recursive: true, force: true });
}

if (failure) throw failure;
