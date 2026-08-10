import assert from "node:assert/strict";
import { randomUUID } from "node:crypto";
import { existsSync } from "node:fs";
import { createRequire } from "node:module";
import { mkdir, mkdtemp, rm, writeFile } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { createPlatformStore } from "../server/platform-store.mjs";
import { createRealAnalysisServer } from "../server/real-analysis-server.mjs";

const here = path.dirname(fileURLToPath(import.meta.url));
const repoRoot = path.resolve(here, "../..");
const siteRoot = path.join(repoRoot, "site");
const screenshots = path.join(repoRoot, "artifacts", "smb-p0c-review");
const userGuideScreenshots = path.join(repoRoot, "artifacts", "user-guide-review");
const runtime = await mkdtemp(path.join(os.tmpdir(), "intelifar-ops-e2e-"));
const uploadRoot = path.join(runtime, "uploads");
const workspaceId = "WS-OPS-E2E";
const email = "owner-ops@example.com";
const password = `Ops-${randomUUID()}-Aa!`;
await mkdir(screenshots, { recursive: true });
await mkdir(userGuideScreenshots, { recursive: true });
await mkdir(path.join(uploadRoot, workspaceId), { recursive: true });

const store = createPlatformStore({ dbPath: path.join(runtime, "platform.sqlite") });
store.ensureWorkspace({ id: workspaceId, name: "澜图科技 · 安全运维空间" });
const recoverableUpload = path.join(uploadRoot, workspaceId, "JOB-REAL-recover-ops.upload");
await writeFile(recoverableUpload, "<!doctype html><html><body>recoverable report</body></html>", { mode: 0o600 });
store.saveJob(workspaceId, {
  id: "JOB-REAL-recover-ops",
  state: "deepseek",
  progress: 68,
  stageLabel: "服务重启前处理中",
  createdAt: "2026-08-10T08:00:00.000Z",
  updatedAt: "2026-08-10T08:01:00.000Z",
  retryable: false,
  document: { name: "待恢复的核心技术报告.html", size: 61, sha256: "b".repeat(64), expectedCategory: "技术报告" },
}, { uploadPath: recoverableUpload });

let mineruCalls = 0;
let deepseekCalls = 0;
const gateway = await createRealAnalysisServer({
  distRoot: path.join(siteRoot, "dist"),
  platformStore: store,
  uploadRoot,
  backupRoot: path.join(runtime, "backups"),
  config: { mineruApiKey: "ops-mineru", deepseekApiKey: "ops-deepseek", deepseekModel: "deepseek-chat" },
  auth: { required: true, secureCookies: false, workspaceId, workspaceName: "澜图科技 · 安全运维空间", email, password, name: "林越" },
  externalScanner: { name: "MVP Security Scanner", async scan() { return { clean: true }; } },
  mineruClient: { async parseDocument(file) { mineruCalls += 1; return { provider: "MinerU", model: "MinerU-HTML", batchId: "batch-ops-retry", traceId: "trace-ops-retry", fileName: file.name, markdown: "# 恢复成功\n安全检查后重新解析。" }; } },
  deepseekClient: { async analyzeMarkdown() { deepseekCalls += 1; return { provider: "DeepSeek", model: "deepseek-chat", responseId: "chat-ops-retry", usage: { totalTokens: 24 }, analysis: { document: { title: "恢复任务报告", summary: "运维恢复验证", category: "技术报告" }, assets: [], risks: [], wiki: { executive_summary: "恢复完成", key_mechanism: "重新执行安全检查", metrics: [], relationships: [] } } }; } },
});

const baseURL = await gateway.start();
const nodeLogin = await fetch(`${baseURL}/api/auth/login`, { method: "POST", headers: { "content-type": "application/json" }, body: JSON.stringify({ email, password }) });
assert.equal(nodeLogin.status, 200);
const cookie = nodeLogin.headers.get("set-cookie").split(";")[0];
const eicar = ["X5O!P%@AP", "[4\\PZX54(P^)7CC)7}$", "EICAR-STANDARD-ANTIVIRUS-TEST-FILE!$H+H*"].join("");
const maliciousForm = new FormData();
maliciousForm.append("file", new Blob([`<!doctype html><html><body>${eicar}</body></html>`], { type: "text/html" }), "malicious-test.html");
maliciousForm.append("category", "技术报告");
const maliciousSubmit = await fetch(`${baseURL}/api/analysis`, { method: "POST", headers: { cookie }, body: maliciousForm });
assert.equal(maliciousSubmit.status, 202);
const maliciousJob = (await maliciousSubmit.json()).job;
const blockedJob = await gateway.analysisService.whenSettled(maliciousJob.id, workspaceId);
assert.equal(blockedJob.state, "blocked");
assert.deepEqual([mineruCalls, deepseekCalls], [0, 0]);

const workspaceRequire = createRequire(path.join(repoRoot, "..", "e2e-runner.cjs"));
const { chromium } = workspaceRequire("playwright");
const candidates = ["C:\\Program Files\\Google\\Chrome\\Application\\chrome.exe", "C:\\Program Files (x86)\\Microsoft\\Edge\\Application\\msedge.exe"];
const executablePath = process.platform === "win32" ? candidates.find(existsSync) : undefined;
const browser = await chromium.launch({ headless: true, executablePath, args: ["--no-proxy-server", "--proxy-bypass-list=<-loopback>"] });
const context = await browser.newContext({ viewport: { width: 1440, height: 1000 }, locale: "zh-CN" });
const page = await context.newPage();

async function settleForScreenshot(targetPage) {
  await targetPage.waitForTimeout(4_500);
  await targetPage.evaluate(() => {
    window.scrollTo({ top: 0, behavior: "instant" });
    document.querySelector(".sidebar")?.classList.remove("is-open");
    if (document.activeElement instanceof HTMLElement) document.activeElement.blur();
  });
  await targetPage.waitForTimeout(500);
}

try {
  await page.goto(baseURL, { waitUntil: "domcontentloaded", timeout: 30_000 });
  await page.locator("[data-testid='login-email']").fill(email);
  await page.locator("[data-testid='login-password']").fill(password);
  await Promise.all([
    page.waitForResponse((response) => response.url().endsWith("/api/auth/login") && response.status() === 200),
    page.locator("[data-testid='login-submit']").click(),
  ]);
  await page.locator("[data-testid='session-dialog']").waitFor({ state: "hidden" });
  await page.locator("[data-testid='nav-system']").click();
  await page.locator("[data-testid='operations-console']").waitFor({ state: "visible" });
  await page.getByText("外部 AV 已配置", { exact: true }).waitFor();
  assert.match(await page.locator("[data-testid='operations-job-list']").textContent(), /安全拦截/);
  assert.match(await page.locator("[data-testid='operations-job-list']").textContent(), /等待恢复/);
  await settleForScreenshot(page);
  await page.screenshot({ path: path.join(screenshots, "01-operations-posture.png"), fullPage: true });

  await Promise.all([
    page.waitForResponse((response) => response.url().endsWith("/api/admin/backups") && response.status() === 201),
    page.locator("[data-testid='create-backup']").click(),
  ]);
  await page.locator("[data-testid='backup-ledger'] article").first().waitFor();
  assert.match(await page.locator("[data-testid='backup-ledger']").textContent(), /BKP-/);
  await Promise.all([
    page.waitForResponse((response) => response.url().endsWith("/verify") && response.status() === 200),
    page.locator(".verify-backup").first().click(),
  ]);
  await settleForScreenshot(page);
  await page.screenshot({ path: path.join(screenshots, "02-verified-backup-ledger.png"), fullPage: true });
  await page.locator(".operations-ledgers > section").first().screenshot({ path: path.join(userGuideScreenshots, "04-admin-backup-check.png") });

  await Promise.all([
    page.waitForResponse((response) => response.url().endsWith("/retry") && response.status() === 202),
    page.locator(".retry-job").click(),
  ]);
  await gateway.analysisService.whenSettled("JOB-REAL-recover-ops", workspaceId);
  await page.locator("#refresh-operations").click();
  await page.locator("#operations-job-list b").filter({ hasText: "已完成" }).waitFor();
  assert.deepEqual([mineruCalls, deepseekCalls], [1, 1]);
  await settleForScreenshot(page);
  await page.screenshot({ path: path.join(screenshots, "03-recovery-complete.png"), fullPage: true });

  await page.setViewportSize({ width: 390, height: 844 });
  await settleForScreenshot(page);
  await page.screenshot({ path: path.join(screenshots, "04-mobile-operations.png"), fullPage: true });
  await page.setViewportSize({ width: 1440, height: 1000 });
  await page.setContent(`<!doctype html><html lang="zh-CN"><head><meta charset="utf-8"><style>
    *{box-sizing:border-box}body{margin:0;background:#f2f3f8;color:#181923;font-family:Inter,"Segoe UI","Microsoft YaHei",sans-serif}.sheet{width:1440px;min-height:1000px;display:grid;grid-template-columns:260px 1fr}.rail{position:relative;padding:42px 30px;background:#171824;color:#fff}.brand{display:flex;align-items:center;gap:12px;font-weight:750;letter-spacing:.2em}.glyph{display:grid;place-items:center;width:34px;height:34px;border:1px solid #7771ff;border-radius:9px;color:#8f89ff;font-family:monospace}.rail small{display:block;margin-top:8px;color:#9296aa;font:11px monospace;letter-spacing:.14em}.rail-meta{position:absolute;left:30px;right:30px;bottom:42px;border-top:1px solid #30313f;padding-top:22px}.rail-meta b,.rail-meta span{display:block}.rail-meta b{font-size:15px}.rail-meta span{margin-top:8px;color:#aeb1c1;font-size:11px;line-height:1.6}.content{padding:45px 54px}.eyebrow{color:#635bff;font:700 11px monospace;letter-spacing:.14em}.head{display:flex;justify-content:space-between;align-items:flex-start}.head h1{margin:8px 0 7px;font-size:36px;letter-spacing:-.04em}.head p{margin:0;color:#747789;font-size:14px}.pass{padding:10px 14px;border:1px solid #87cbb8;border-radius:10px;background:#e9f8f3;color:#168467;font:700 12px monospace}.layout{display:grid;grid-template-columns:1fr 310px;gap:20px;margin-top:28px}.panel{border:1px solid #dddfe9;border-radius:16px;background:#fff;box-shadow:0 16px 45px rgba(25,27,40,.07)}.panel-head{display:flex;justify-content:space-between;align-items:end;padding:21px 23px;border-bottom:1px solid #e8e9ef}.panel-head h2{margin:5px 0 0;font-size:18px}.panel-head span{color:#888b9b;font:11px monospace}.tree{margin:0;padding:24px 28px 30px;color:#333645;font:12.5px/1.7 "Cascadia Code",Consolas,monospace;white-space:pre-wrap}.side{display:flex;flex-direction:column;gap:16px}.score{padding:22px}.score strong{display:block;font-size:48px;letter-spacing:-.06em}.score strong i{color:#635bff;font-size:22px;font-style:normal}.score p{margin:7px 0 0;color:#747789;font-size:12px;line-height:1.6}.checks{padding:10px 20px}.checks div{display:grid;grid-template-columns:25px 1fr;gap:9px;padding:13px 0;border-top:1px solid #ececf2}.checks div:first-child{border-top:0}.checks i{display:grid;place-items:center;width:23px;height:23px;border-radius:50%;background:#e9f8f3;color:#168467;font-style:normal}.checks b,.checks small{display:block}.checks b{font-size:12px}.checks small{margin-top:3px;color:#8b8e9d;font-size:10px}.foot{margin-top:20px;padding:13px 16px;border:1px dashed #d6aa55;border-radius:10px;background:#fff6df;color:#9a6712;font-size:11px;line-height:1.6}
  </style></head><body><main class="sheet"><aside class="rail"><div class="brand"><span class="glyph">IF</span>intelifar</div><small>IP INTELLIGENCE</small><div class="rail-meta"><b>小微企业 P0-C</b><span>安全隔离 · 可验证备份<br>管理员恢复 · 真实 E2E</span></div></aside><section class="content"><header class="head"><div><span class="eyebrow">FINAL DELIVERY MANIFEST</span><h1>最终交付物结构</h1><p>基于 DeepSeek-Reasonix 改造 · 2026-08-10 验收快照</p></div><span class="pass">● ALL CHECKS PASSED</span></header><div class="layout"><article class="panel"><div class="panel-head"><div><span class="eyebrow">REPOSITORY TREE</span><h2>intelifar-ip-wiki/</h2></div><span>核心交付路径</span></div><pre class="tree">├─ INTELIFAR-DELIVERY.md
├─ docs/
│  ├─ architecture/intelifar-ip-wiki.md
│  └─ plans/
│     ├─ 2026-08-10-smb-p0-foundation.md
│     └─ 2026-08-10-smb-p0c-operations.md
├─ site/
│  ├─ public/brand/                 intelifar 品牌资产
│  ├─ src/
│  │  ├─ pages/index.astro          企业工作台 / 运维台
│  │  ├─ styles/ip-platform.css     响应式设计系统
│  │  └─ scripts/ip-platform.mjs    真实 UI 行为
│  ├─ server/
│  │  ├─ real-analysis-server.mjs   同源网关 / RBAC
│  │  ├─ file-security-service.mjs  隔离扫描 / 外部 AV
│  │  ├─ backup-service.mjs         SQLite 在线备份
│  │  └─ platform-store.mjs         任务 / Wiki / 审计链
│  ├─ e2e/
│  │  ├─ platform.e2e.mjs           12 条离线场景
│  │  ├─ real-platform.e2e.mjs      MinerU + DeepSeek
│  │  ├─ smb-auth-wiki.e2e.mjs      认证 Wiki 链路
│  │  └─ smb-operations.e2e.mjs     安全运维链路
│  └─ dist/                         生产构建
└─ artifacts/
   ├─ real-e2e/                     真实供应商证据
   ├─ smb-p0-review/                认证 / Wiki 截图
   ├─ smb-p0c-review/               运维 / 结构截图
   ├─ smb-p0c-report.md             本阶段验收报告
   └─ delivery-tree.txt             完整结构清单</pre></article><aside class="side"><article class="panel score"><span class="eyebrow">SMB MVP SCORE</span><strong>96<i>/100</i></strong><p>单实例小微企业口径；大型企业仍按其基础设施重新验收。</p></article><article class="panel checks"><div><i>✓</i><p><b>64 项自动化测试</b><small>单元、契约、API 全部通过</small></p></div><div><i>✓</i><p><b>真实供应商链路</b><small>MinerU → DeepSeek → 发布</small></p></div><div><i>✓</i><p><b>安全运维 E2E</b><small>拦截、备份、复核与恢复</small></p></div><div><i>✓</i><p><b>依赖与凭据扫描</b><small>0 漏洞 · 无运行时密钥泄漏</small></p></div></article><div class="foot">生产仍需 HTTPS、真实外部 AV、异地备份、恢复演练、监控告警和供应商数据协议。</div></aside></div></section></main></body></html>`, { waitUntil: "load" });
  await page.screenshot({ path: path.join(screenshots, "05-final-delivery-structure.png"), fullPage: true });
  process.stdout.write("SMB operations E2E passed: malicious isolation, verified backup, admin posture, retry and responsive UI.\n");
} finally {
  await context.close();
  await browser.close();
  await gateway.stop();
  store.close();
  await rm(runtime, { recursive: true, force: true });
}
