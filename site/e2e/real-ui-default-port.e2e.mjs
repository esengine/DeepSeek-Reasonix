import assert from "node:assert/strict";
import { spawn } from "node:child_process";
import { existsSync } from "node:fs";
import { mkdir, writeFile } from "node:fs/promises";
import { createRequire } from "node:module";
import path from "node:path";
import { fileURLToPath } from "node:url";

const here = path.dirname(fileURLToPath(import.meta.url));
const siteRoot = path.resolve(here, "..");
const repoRoot = path.resolve(siteRoot, "..");
const baseURL = "http://127.0.0.1:4388/";
const listenURL = baseURL.slice(0, -1);
const artifacts = path.join(repoRoot, "artifacts", "real-ui-port");
await mkdir(artifacts, { recursive: true });
const workspaceRequire = createRequire(path.join(repoRoot, "..", "e2e-runner.cjs"));
const { chromium } = workspaceRequire("playwright");
const chromeCandidates = [
  "C:\\Program Files\\Google\\Chrome\\Application\\chrome.exe",
  "C:\\Program Files (x86)\\Microsoft\\Edge\\Application\\msedge.exe",
];

function waitForStartup(child, expected, timeoutMs = 15_000) {
  return new Promise((resolve, reject) => {
    let stdout = "";
    let stderr = "";
    const timer = setTimeout(() => finish(new Error(`Gateway startup timed out. stdout=${stdout} stderr=${stderr}`)), timeoutMs);
    const onStdout = (chunk) => {
      stdout += chunk;
      if (stdout.includes(expected)) finish();
    };
    const onStderr = (chunk) => { stderr += chunk; };
    const onExit = (code) => finish(new Error(`Gateway exited before startup with code ${code}. stderr=${stderr}`));
    function finish(error) {
      clearTimeout(timer);
      child.stdout.off("data", onStdout);
      child.stderr.off("data", onStderr);
      child.off("exit", onExit);
      error ? reject(error) : resolve();
    }
    child.stdout.on("data", onStdout);
    child.stderr.on("data", onStderr);
    child.once("exit", onExit);
  });
}

async function stopChild(child) {
  if (!child || child.exitCode !== null) return;
  const exited = new Promise((resolve) => child.once("exit", resolve));
  child.kill();
  await Promise.race([
    exited,
    new Promise((_, reject) => setTimeout(() => reject(new Error("Gateway process did not stop")), 5_000)),
  ]);
}

const childEnv = {
  ...process.env,
  MINERU_API_KEY: "e2e-mineru-placeholder",
  DEEPSEEK_API_KEY: "e2e-deepseek-placeholder",
  NODE_ENV: "development",
};
delete childEnv.PORT;
delete childEnv.INTELIFAR_BOOTSTRAP_PASSWORD;

const gateway = spawn(process.execPath, ["server/real-analysis-server.mjs"], {
  cwd: siteRoot,
  env: childEnv,
  stdio: ["ignore", "pipe", "pipe"],
  windowsHide: true,
});

let browser;
let context;
try {
  await waitForStartup(gateway, `intelifar real analysis gateway listening on ${listenURL}`);

  const healthResponse = await fetch(`${baseURL}api/health`);
  assert.equal(healthResponse.status, 200);
  const health = await healthResponse.json();
  assert.equal(health.status, "ok");
  assert.equal(health.mode, "real");
  assert.equal(health.providers.mineru, "configured");
  assert.equal(health.providers.deepseek, "configured");
  assert.equal(health.storage.adapter, "sqlite");
  assert.equal(health.storage.agentTasks, true);
  assert.equal(health.storage.verifiedBackups, true);
  assert.equal(health.auth.mode, "loopback-persistent");

  const executablePath = process.platform === "win32" ? chromeCandidates.find(existsSync) : undefined;
  browser = await chromium.launch({
    headless: true,
    executablePath,
    args: ["--no-proxy-server", "--proxy-bypass-list=<-loopback>"],
  });
  context = await browser.newContext({ viewport: { width: 1440, height: 1000 }, locale: "zh-CN" });
  const page = await context.newPage();

  const home = await page.goto(baseURL, { waitUntil: "networkidle" });
  assert.equal(home.status(), 200);
  await page.locator("[data-testid='overview-new-analysis']").waitFor();
  assert.ok(await page.locator("img[alt*='intelifar']").count(), "intelifar brand was not rendered");
  assert.notEqual((await page.locator("#metric-assets-total").textContent()).trim(), "1,284");
  assert.notEqual((await page.locator("#nav-document-count").textContent()).trim(), "17");
  assert.notEqual((await page.locator("#nav-redaction-count").textContent()).trim(), "23");
  assert.equal(await page.locator("[data-demo-dashboard]:visible").count(), 0);
  await page.screenshot({ path: path.join(artifacts, "01-real-ui-home-4388.png"), fullPage: true });

  await page.locator("[data-testid='nav-analysis']").click();
  assert.equal(await page.locator("[data-demo-analysis]:visible").count(), 0);

  await page.locator("[data-testid='nav-lifecycle']").click();
  await page.locator("#lifecycle-real-governance").waitFor({ state: "visible" });
  assert.equal(await page.locator("[data-demo-lifecycle]:visible").count(), 0);
  assert.doesNotMatch(await page.locator("#view-lifecycle").innerText(), /华辰智算研究院|推理架构组/);

  await page.locator("[data-testid='nav-redaction']").click();
  await page.locator("#redaction-real-workspace").waitFor({ state: "visible" });
  assert.equal(await page.locator("[data-demo-redaction]:visible").count(), 0);

  await page.locator("[data-testid='nav-audit']").click();
  const auditEntryCount = await page.locator("#audit-log [data-audit-entry]").count();
  assert.equal(Number((await page.locator("#audit-total-count").textContent()).trim()), auditEntryCount);

  const assetsPage = await context.newPage();
  await assetsPage.goto(`${baseURL}#assets`, { waitUntil: "networkidle" });
  await assetsPage.locator("#view-assets:not([hidden])").waitFor();
  await assetsPage.locator("#graph-loading").waitFor({ state: "hidden" });
  assert.ok(await assetsPage.locator(".graph-node").count(), "IP panorama did not render any graph nodes");
  assert.ok(await assetsPage.locator(".graph-edge").count(), "IP panorama did not render publication-scoped relationships");
  await assetsPage.locator("#graph-stage").screenshot({ path: path.join(artifacts, "02-ip-panorama-4388.png") });

  const agentPage = await context.newPage();
  await agentPage.goto(`${baseURL}#agent`, { waitUntil: "networkidle" });
  await agentPage.locator("#view-agent:not([hidden])").waitFor();
  assert.equal(await agentPage.locator("[data-testid='nav-agent']").getAttribute("aria-current"), "page");
  assert.equal(await agentPage.locator("[data-testid='agent-submit']").isEnabled(), true);
  await agentPage.screenshot({ path: path.join(artifacts, "03-ip-agent-4388.png"), fullPage: true });

  await page.locator("[data-testid='nav-redaction']").click();
  await page.locator("#redaction-real-workspace").waitFor({ state: "visible" });
  assert.equal(await page.locator("[data-testid='redacted-token']:visible").count(), 0);
  await page.screenshot({ path: path.join(artifacts, "04-real-risk-workspace-4388.png"), fullPage: true });

  await page.locator("[data-testid='nav-system']").click();
  await page.locator("[data-testid='operations-console']").waitFor({ state: "visible" });
  await page.getByText("运行库完整", { exact: true }).waitFor();
  assert.equal(await page.locator("[data-testid='member-ledger'] > article[data-member-id='USR-DEMO']").count(), 1);
  const backupResponse = page.waitForResponse((response) => response.url().endsWith("/api/admin/backups") && response.request().method() === "POST");
  await page.locator("[data-testid='create-backup']").click();
  assert.equal((await backupResponse).status(), 201);
  await page.locator("[data-testid='backup-ledger'] article").first().waitFor();
  assert.match(await page.locator("#ops-audit-status").textContent(), /哈希链有效/);
  await page.screenshot({ path: path.join(artifacts, "05-system-persistent-4388.png"), fullPage: true });

  await writeFile(path.join(artifacts, "e2e-report.md"), [
    "# intelifar 真实 UI 默认端口 E2E",
    "",
    `- 执行时间：${new Date().toISOString()}`,
    `- 目标地址：${baseURL}`,
    "- 结果：PASS",
    "- 健康检查：real；SQLite；MinerU configured；DeepSeek configured；Agent/备份可用",
    "- 浏览器链路：首页真实指标 → IP 全景图 → 受控 IP 任务助手 → 脱敏证据上下文 → 系统备份与审计链",
    "- 进程治理：测试结束后自动回收 4388 网关",
    "",
  ].join("\n"), "utf8");

  process.stdout.write(`Real UI default-port E2E passed: ${baseURL}, health + home + IP panorama + Agent.\n`);
} finally {
  await context?.close();
  await browser?.close();
  await stopChild(gateway);
}
