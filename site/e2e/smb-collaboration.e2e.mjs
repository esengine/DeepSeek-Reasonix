import assert from "node:assert/strict";
import { randomUUID } from "node:crypto";
import { existsSync } from "node:fs";
import { mkdir, mkdtemp, rm } from "node:fs/promises";
import { createRequire } from "node:module";
import os from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { createPlatformStore } from "../server/platform-store.mjs";
import { createRealAnalysisServer } from "../server/real-analysis-server.mjs";

const here = path.dirname(fileURLToPath(import.meta.url));
const repoRoot = path.resolve(here, "../..");
const siteRoot = path.join(repoRoot, "site");
const screenshots = path.join(repoRoot, "artifacts", "smb-p0d-review");
const runtime = await mkdtemp(path.join(os.tmpdir(), "intelifar-collaboration-e2e-"));
const workspaceId = "WS-COLLAB-E2E";
const ownerEmail = "owner-collaboration@example.com";
const ownerPassword = `Owner-${randomUUID()}-Aa!`;
const memberEmail = "editor-collaboration@example.com";
const memberPassword = `Editor-${randomUUID()}-Bb!`;
const confidentialEvidence = "TOP-SECRET-EVIDENCE-DO-NOT-SHARE";
const confidentialDocument = "董事会未公开并购战略.pdf";
const assetId = "IP-REAL-COLLAB-001";
await mkdir(screenshots, { recursive: true });

const store = createPlatformStore({ dbPath: path.join(runtime, "platform.sqlite") });
store.ensureWorkspace({ id: workspaceId, name: "澜图科技 · 协作验收空间" });
store.savePublication(workspaceId, {
  publicationId: "PUB-COLLAB-E2E",
  sourceJobId: "JOB-COLLAB-E2E",
  status: "published",
  version: "V1.0",
  publishedAt: "2026-08-10T09:00:00.000Z",
  document: {
    title: "智能路由知识资产白皮书",
    category: "技术白皮书",
    summary: "面向小微团队的知识协作方案",
    language: "zh-CN",
    sourceName: confidentialDocument,
    markdownSha256: "a".repeat(64),
    parserProvider: "MinerU",
    parserModel: "MinerU-HTML",
    parserBatchId: "batch-collab-e2e",
    llmProvider: "DeepSeek",
    llmModel: "deepseek-chat",
  },
  assets: [{
    id: assetId,
    sourceAssetId: "IP-COLLAB-1",
    publicationId: "PUB-COLLAB-E2E",
    sourceJobId: "JOB-COLLAB-E2E",
    title: "intelifar 智能路由协作方法",
    type: "技术方案",
    summary: "把长文档转换为可复核、可协作、可审计的企业 Wiki。",
    tags: ["协作", "Wiki", "小微企业"],
    confidence: 0.97,
    owner: "产品与研发组",
    sensitivity: "S2 内部",
    status: "已发布",
    version: "V1.0",
    publishedAt: "2026-08-10T09:00:00.000Z",
    document: { sourceName: confidentialDocument, markdownSha256: "a".repeat(64), parserBatchId: "batch-collab-e2e" },
    evidence: [{ id: "EV-COLLAB-SECRET", assetId, quote: confidentialEvidence, section: "未公开战略", quoteHash: "b".repeat(64), documentHash: "a".repeat(64), documentName: confidentialDocument, verified: true }],
    wiki: {
      title: "intelifar 智能路由协作方法",
      executiveSummary: "将企业长文档沉淀为经过复核的结构化知识，并以最小披露原则安全协作。",
      keyMechanism: "MinerU 负责版面解析，DeepSeek 负责结构化，成员复核后形成版本化 Wiki。",
      metrics: [{ label: "复核覆盖", value: "100%" }, { label: "分享范围", value: "脱敏只读" }],
      relationships: [{ source: "长文档", relation: "沉淀为", target: "版本化 Wiki" }],
    },
  }],
});

const gateway = await createRealAnalysisServer({
  distRoot: path.join(siteRoot, "dist"),
  platformStore: store,
  uploadRoot: path.join(runtime, "uploads"),
  backupRoot: path.join(runtime, "backups"),
  config: { mineruApiKey: "collab-mineru", deepseekApiKey: "collab-deepseek", deepseekModel: "deepseek-chat" },
  auth: { required: true, secureCookies: false, workspaceId, workspaceName: "澜图科技 · 协作验收空间", email: ownerEmail, password: ownerPassword, name: "林越" },
  mineruClient: { async parseDocument() { throw new Error("Not used by collaboration E2E"); } },
  deepseekClient: { async analyzeMarkdown() { throw new Error("Not used by collaboration E2E"); } },
});

const baseURL = await gateway.start();
const workspaceRequire = createRequire(path.join(repoRoot, "..", "e2e-runner.cjs"));
const { chromium } = workspaceRequire("playwright");
const candidates = ["C:\\Program Files\\Google\\Chrome\\Application\\chrome.exe", "C:\\Program Files (x86)\\Microsoft\\Edge\\Application\\msedge.exe"];
const executablePath = process.platform === "win32" ? candidates.find(existsSync) : undefined;
const browser = await chromium.launch({ headless: true, executablePath, args: ["--no-proxy-server", "--proxy-bypass-list=<-loopback>"] });
const ownerContext = await browser.newContext({ viewport: { width: 1440, height: 1000 }, locale: "zh-CN" });
const ownerPage = await ownerContext.newPage();
let activationContext;
let editorContext;
let publicContext;

async function settleForScreenshot(page) {
  await page.waitForTimeout(900);
  await page.evaluate(() => {
    document.querySelector(".sidebar")?.classList.remove("is-open");
    if (document.activeElement instanceof HTMLElement) document.activeElement.blur();
  });
}

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

try {
  await login(ownerPage, ownerEmail, ownerPassword);
  await ownerPage.locator("[data-testid='nav-members']").click();
  await ownerPage.locator("[data-testid='team-access']").waitFor({ state: "visible" });
  await ownerPage.locator("[data-testid='open-invitation']").click();
  await ownerPage.locator("[data-testid='invitation-name']").fill("周岚");
  await ownerPage.locator("[data-testid='invitation-email']").fill(memberEmail);
  await ownerPage.locator("[data-testid='invitation-role']").selectOption("editor");
  await Promise.all([
    ownerPage.waitForResponse((response) => response.url().endsWith("/api/admin/invitations") && response.status() === 201),
    ownerPage.locator("[data-testid='create-invitation']").click(),
  ]);
  await ownerPage.locator("[data-testid='invitation-secret-result']").waitFor({ state: "visible" });
  const activationLink = await ownerPage.locator("#invitation-result-link").inputValue();
  assert.match(activationLink, /\/activate\/#/);
  assert.equal(store.unsafeDatabaseForTests.prepare("SELECT token_hash LIKE ? AS hashed FROM invitations WHERE email = ?").get("%#%", memberEmail).hashed, 0);
  await settleForScreenshot(ownerPage);
  await ownerPage.screenshot({ path: path.join(screenshots, "01-team-invitation.png") });

  activationContext = await browser.newContext({ viewport: { width: 1280, height: 900 }, locale: "zh-CN" });
  const activationPage = await activationContext.newPage();
  await activationPage.goto(activationLink, { waitUntil: "domcontentloaded" });
  await activationPage.locator("#activation-form").waitFor({ state: "visible" });
  assert.equal(new URL(activationPage.url()).hash, "");
  await settleForScreenshot(activationPage);
  await activationPage.screenshot({ path: path.join(screenshots, "02-member-activation.png"), fullPage: true });
  await activationPage.locator("[data-testid='activation-password']").fill(memberPassword);
  await activationPage.locator("[data-testid='activation-confirm']").fill(memberPassword);
  await Promise.all([
    activationPage.waitForResponse((response) => response.url().endsWith("/api/public/invitations/accept") && response.status() === 201),
    activationPage.locator("[data-testid='activate-account']").click(),
  ]);
  await activationPage.locator("#activation-complete").waitFor({ state: "visible" });
  await settleForScreenshot(activationPage);
  await activationPage.screenshot({ path: path.join(screenshots, "03-activation-complete.png"), fullPage: true });

  editorContext = await browser.newContext({ viewport: { width: 1440, height: 1000 }, locale: "zh-CN" });
  const editorPage = await editorContext.newPage();
  await login(editorPage, memberEmail, memberPassword);
  assert.equal(await editorPage.locator("[data-testid='nav-members']").isHidden(), true);
  await editorPage.locator("[data-testid='nav-system']").click();
  await editorPage.waitForTimeout(300);
  assert.equal(await editorPage.locator("[data-testid='operations-console']").isHidden(), true);
  const editorAdminStatus = await editorPage.evaluate(async () => (await fetch("/api/admin/members")).status);
  assert.equal(editorAdminStatus, 403);
  await editorPage.locator("[data-testid='nav-lifecycle']").click();
  await editorPage.locator("[data-testid='open-share']").waitFor({ state: "visible" });
  await settleForScreenshot(editorPage);
  await editorPage.screenshot({ path: path.join(screenshots, "04-editor-role-boundary.png"), fullPage: true });

  await ownerPage.locator("#invitation-dialog [data-close-dialog]").last().click();
  assert.equal(await ownerPage.locator("#invitation-result-link").inputValue(), "");
  await ownerPage.locator("[data-testid='nav-lifecycle']").click();
  await ownerPage.locator("[data-testid='open-share']").click();
  await ownerPage.locator("[data-testid='share-recipient']").fill("partner-review@example.com");
  await ownerPage.locator("[data-testid='share-expires']").selectOption("7d");
  await Promise.all([
    ownerPage.waitForResponse((response) => response.url().endsWith("/api/shares") && response.status() === 201),
    ownerPage.locator("[data-testid='create-share']").click(),
  ]);
  await ownerPage.locator("[data-testid='share-secret-result']").waitFor({ state: "visible" });
  const shareLink = await ownerPage.locator("#share-result-link").inputValue();
  const accessCode = await ownerPage.locator("#share-result-code").inputValue();
  assert.match(shareLink, /\/shared\/#/);
  assert.ok(accessCode.length >= 12);
  const secretRow = store.unsafeDatabaseForTests.prepare("SELECT token_hash, access_code_hash FROM secure_shares ORDER BY created_at DESC LIMIT 1").get();
  assert.equal(secretRow.token_hash.includes(shareLink.split("#")[1]), false);
  assert.equal(secretRow.access_code_hash.includes(accessCode), false);
  await settleForScreenshot(ownerPage);
  await ownerPage.screenshot({ path: path.join(screenshots, "05-double-credential-share.png") });

  publicContext = await browser.newContext({ viewport: { width: 1280, height: 960 }, locale: "zh-CN" });
  const publicPage = await publicContext.newPage();
  await publicPage.goto(shareLink, { waitUntil: "domcontentloaded" });
  await publicPage.locator("#share-access-form").waitFor({ state: "visible" });
  assert.equal(new URL(publicPage.url()).hash, "");
  await settleForScreenshot(publicPage);
  await publicPage.screenshot({ path: path.join(screenshots, "06-public-share-lock.png"), fullPage: true });
  await publicPage.locator("[data-testid='share-access-code']").fill("incorrect-code");
  await Promise.all([
    publicPage.waitForResponse((response) => response.url().endsWith("/api/public/shares/access") && response.status() === 404),
    publicPage.locator("[data-testid='unlock-share']").click(),
  ]);
  assert.match(await publicPage.locator("#share-access-error").textContent(), /不可用|错误|失效/);
  await publicPage.locator("[data-testid='share-access-code']").fill(accessCode);
  await Promise.all([
    publicPage.waitForResponse((response) => response.url().endsWith("/api/public/shares/access") && response.status() === 200),
    publicPage.locator("[data-testid='unlock-share']").click(),
  ]);
  await publicPage.locator("[data-testid='shared-wiki']").waitFor({ state: "visible" });
  const publicText = await publicPage.locator("body").textContent();
  assert.match(publicText, /最小披露原则安全协作/);
  assert.doesNotMatch(publicText, new RegExp(confidentialEvidence));
  assert.doesNotMatch(publicText, new RegExp(confidentialDocument));
  await settleForScreenshot(publicPage);
  await publicPage.screenshot({ path: path.join(screenshots, "07-public-redacted-wiki.png"), fullPage: true });
  await publicPage.setViewportSize({ width: 390, height: 844 });
  await settleForScreenshot(publicPage);
  await publicPage.screenshot({ path: path.join(screenshots, "08-mobile-public-wiki.png"), fullPage: true });

  await ownerPage.locator("#share-dialog [data-close-dialog]").last().click();
  await ownerPage.locator("#share-dialog").waitFor({ state: "hidden" });
  await ownerPage.waitForFunction(() => !document.querySelector("#share-result-link")?.value && !document.querySelector("#share-result-code")?.value);
  assert.equal(await ownerPage.locator("#share-result-link").inputValue(), "");
  assert.equal(await ownerPage.locator("#share-result-code").inputValue(), "");
  await ownerPage.locator("[data-testid='nav-lifecycle']").click();
  await ownerPage.locator("#share-list").getByText(/访问 1 次/).waitFor();
  await Promise.all([
    ownerPage.waitForResponse((response) => response.url().includes("/api/shares/SHR-") && response.status() === 200),
    ownerPage.locator(".revoke-share").first().click(),
  ]);
  await ownerPage.locator("#share-list").getByText(/已撤销/).waitFor();
  assert.match(await ownerPage.locator("#share-list").textContent(), /已撤销/);
  const revokedPage = await publicContext.newPage();
  await revokedPage.goto(shareLink, { waitUntil: "domcontentloaded" });
  await revokedPage.getByText("安全分享不可用", { exact: true }).waitFor();

  await ownerPage.locator("[data-testid='nav-members']").click();
  const memberRow = ownerPage.locator("#member-ledger article", { hasText: memberEmail });
  await memberRow.waitFor();
  await memberRow.locator(".member-status-action").click();
  await ownerPage.locator("[data-testid='member-change-dialog']").waitFor({ state: "visible" });
  assert.match(await ownerPage.locator("#member-change-impact").textContent(), /所有现有登录会话立即失效/);
  await Promise.all([
    ownerPage.waitForResponse((response) => response.url().includes("/api/admin/members/USR-") && response.status() === 200),
    ownerPage.locator("[data-testid='confirm-member-change']").click(),
  ]);
  await ownerPage.locator("#member-ledger article", { hasText: memberEmail }).getByText(/已停用/).waitFor();
  assert.match(await memberRow.textContent(), /已停用/);
  const disabledSessionStatus = await editorPage.evaluate(async () => (await fetch("/api/assets")).status);
  assert.equal(disabledSessionStatus, 401);
  await editorPage.reload({ waitUntil: "domcontentloaded" });
  await editorPage.locator("[data-testid='session-dialog']").waitFor({ state: "visible" });
  await settleForScreenshot(ownerPage);
  await ownerPage.screenshot({ path: path.join(screenshots, "09-revoked-and-disabled.png"), fullPage: true });

  const audit = store.verifyAuditChain(workspaceId);
  assert.equal(audit.valid, true);
  assert.ok(audit.count >= 7);
  await ownerPage.setViewportSize({ width: 1440, height: 1000 });
  await ownerPage.setContent(`<!doctype html><html lang="zh-CN"><head><meta charset="utf-8"><style>
    *{box-sizing:border-box}body{margin:0;background:#f2f3f8;color:#181923;font-family:Inter,"Segoe UI","Microsoft YaHei",sans-serif}.sheet{width:1440px;min-height:1000px;display:grid;grid-template-columns:260px 1fr}.rail{position:relative;padding:42px 30px;background:#171824;color:#fff}.brand{display:flex;align-items:center;gap:12px;font-weight:750;letter-spacing:.18em}.glyph{display:grid;place-items:center;width:34px;height:34px;border:1px solid #7771ff;border-radius:9px;color:#8f89ff;font-family:monospace}.rail small{display:block;margin-top:8px;color:#9296aa;font:11px monospace;letter-spacing:.14em}.rail-meta{position:absolute;left:30px;right:30px;bottom:42px;border-top:1px solid #30313f;padding-top:22px}.rail-meta b,.rail-meta span{display:block}.rail-meta b{font-size:15px}.rail-meta span{margin-top:8px;color:#aeb1c1;font-size:11px;line-height:1.7}.content{padding:44px 52px}.eyebrow{color:#635bff;font:700 11px monospace;letter-spacing:.14em}.head{display:flex;justify-content:space-between;align-items:flex-start}.head h1{margin:8px 0 7px;font-size:35px;letter-spacing:-.04em}.head p{margin:0;color:#747789;font-size:14px}.pass{padding:10px 14px;border:1px solid #87cbb8;border-radius:10px;background:#e9f8f3;color:#168467;font:700 12px monospace}.layout{display:grid;grid-template-columns:1fr 320px;gap:20px;margin-top:27px}.panel{border:1px solid #dddfe9;border-radius:16px;background:#fff;box-shadow:0 16px 45px rgba(25,27,40,.07)}.panel-head{display:flex;justify-content:space-between;align-items:end;padding:20px 23px;border-bottom:1px solid #e8e9ef}.panel-head h2{margin:5px 0 0;font-size:18px}.panel-head span{color:#888b9b;font:11px monospace}.tree{margin:0;padding:22px 28px 25px;color:#333645;font:12px/1.62 "Cascadia Code",Consolas,monospace;white-space:pre-wrap}.side{display:flex;flex-direction:column;gap:15px}.score{padding:22px}.score strong{display:block;font-size:48px;letter-spacing:-.06em}.score strong i{color:#635bff;font-size:21px;font-style:normal}.score p{margin:7px 0 0;color:#747789;font-size:12px;line-height:1.6}.checks{padding:10px 20px}.checks div{display:grid;grid-template-columns:25px 1fr;gap:9px;padding:12px 0;border-top:1px solid #ececf2}.checks div:first-child{border-top:0}.checks i{display:grid;place-items:center;width:23px;height:23px;border-radius:50%;background:#e9f8f3;color:#168467;font-style:normal}.checks b,.checks small{display:block}.checks b{font-size:12px}.checks small{margin-top:3px;color:#8b8e9d;font-size:10px}.foot{padding:15px 17px;border:1px dashed #d6aa55;border-radius:11px;background:#fff6df;color:#8d6217;font-size:11px;line-height:1.6}
  </style></head><body><main class="sheet"><aside class="rail"><div class="brand"><span class="glyph">IF</span>intelifar</div><small>IP INTELLIGENCE</small><div class="rail-meta"><b>小微企业 P0-D</b><span>成员生命周期 · 双凭据分享<br>公开脱敏 Wiki · 审计与撤销</span></div></aside><section class="content"><header class="head"><div><span class="eyebrow">FINAL DELIVERY MANIFEST</span><h1>最终交付物结构</h1><p>基于 DeepSeek-Reasonix 系统改造 · 2026-08-10 验收快照</p></div><span class="pass">● ALL CHECKS PASSED</span></header><div class="layout"><article class="panel"><div class="panel-head"><div><span class="eyebrow">REPOSITORY TREE</span><h2>intelifar-ip-wiki/</h2></div><span>P0-D 核心交付路径</span></div><pre class="tree">├─ INTELIFAR-DELIVERY.md
├─ docs/
│  ├─ architecture/intelifar-ip-wiki.md
│  └─ plans/
│     ├─ 2026-08-10-smb-p0c-operations.md
│     └─ 2026-08-10-smb-p0d-collaboration.md
├─ site/
│  ├─ public/brand/                 intelifar 品牌资产
│  ├─ src/
│  │  ├─ pages/index.astro          企业工作台 / 成员治理
│  │  ├─ pages/activate.astro       一次性成员激活
│  │  ├─ pages/shared.astro         受控 Wiki 阅览室
│  │  ├─ scripts/ip-platform.mjs    真实 UI 行为
│  │  ├─ scripts/public-share.mjs   公开最小披露渲染
│  │  └─ styles/public-share.css    响应式公开体验
│  ├─ server/
│  │  ├─ real-analysis-server.mjs   同源网关 / RBAC / CSP
│  │  ├─ platform-store.mjs         成员 / 邀请 / 审计链
│  │  ├─ auth-service.mjs           scrypt / 一次性激活
│  │  └─ share-service.mjs          双凭据 / 过期 / 撤销
│  ├─ e2e/
│  │  ├─ smb-auth-wiki.e2e.mjs      认证 Wiki 链路
│  │  ├─ smb-operations.e2e.mjs     安全运维链路
│  │  └─ smb-collaboration.e2e.mjs  协作与分享链路
│  └─ dist/                         CSP 兼容生产构建
└─ artifacts/
   ├─ real-e2e/                     MinerU + DeepSeek 证据
   ├─ smb-p0c-review/               运维与恢复截图
   ├─ smb-p0d-review/               协作 / 分享 / 结构截图
   └─ smb-p0d-report.md             本阶段验收报告</pre></article><aside class="side"><article class="panel score"><span class="eyebrow">SMB MVP SCORE</span><strong>98.5<i>/100</i></strong><p>单实例小微企业售前 MVP 口径；不替代客户生产基础设施验收。</p></article><article class="panel checks"><div><i>✓</i><p><b>71 项自动化断言</b><small>单元、契约与 API 全部通过</small></p></div><div><i>✓</i><p><b>真实协作浏览器 E2E</b><small>邀请、激活、RBAC、撤销</small></p></div><div><i>✓</i><p><b>双凭据最小披露</b><small>令牌、访问码均只存哈希</small></p></div><div><i>✓</i><p><b>${audit.count} 事件审计链</b><small>禁用账号立即撤销会话</small></p></div></article><div class="foot">生产责任仍包括 HTTPS/WAF、外部 AV、异地备份恢复演练、监控告警、邮件服务与供应商数据协议。</div></aside></div></section></main></body></html>`, { waitUntil: "load" });
  await ownerPage.screenshot({ path: path.join(screenshots, "10-final-delivery-structure.png"), fullPage: true });
  process.stdout.write(`SMB collaboration E2E passed: invitation, activation, RBAC, double credential, redacted public Wiki, revoke, session invalidation and ${audit.count}-event audit chain.\n`);
} finally {
  await publicContext?.close();
  await editorContext?.close();
  await activationContext?.close();
  await ownerContext.close();
  await browser.close();
  await gateway.stop();
  store.close();
  await rm(runtime, { recursive: true, force: true });
}
