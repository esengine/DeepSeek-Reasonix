import assert from "node:assert/strict";
import { existsSync } from "node:fs";
import { mkdir, mkdtemp, rm, writeFile } from "node:fs/promises";
import { createRequire } from "node:module";
import os from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { createPlatformStore } from "../server/platform-store.mjs";
import { createRealAnalysisServer } from "../server/real-analysis-server.mjs";

const here = path.dirname(fileURLToPath(import.meta.url));
const repoRoot = path.resolve(here, "../..");
const artifactRoot = path.join(repoRoot, "artifacts", "semantica-phase-2");
const runtime = await mkdtemp(path.join(os.tmpdir(), "intelifar-semantic-review-e2e-"));
await mkdir(artifactRoot, { recursive: true });

const store = createPlatformStore({ dbPath: path.join(runtime, "platform.sqlite") });
store.ensureWorkspace({ id: "WS-DEMO", name: "澜图科技 · 语义复核验收空间" });

function publication(suffix, sourceName, owner, publishedAt) {
  const assetId = `IP-REAL-REV${suffix}`;
  return {
    publicationId: `PUB-SEM-REV-${suffix}`,
    sourceJobId: `JOB-SEM-REV-${suffix}`,
    status: "published",
    version: "V1.0",
    publishedAt,
    document: { title: "企业知识中台建设报告", sourceName },
    assets: [{
      id: assetId,
      title: "企业知识中台治理方法",
      type: "技术方案",
      summary: "从企业文档形成可追溯、可复核的知识资产。",
      owner,
      sensitivity: "内部",
      confidence: 0.95,
      tags: ["知识治理", "Wiki"],
      status: "已发布",
      version: "V1.0",
      publishedAt,
      document: { sourceName },
      evidence: [],
      wiki: { title: "企业知识中台治理方法", executiveSummary: "形成可复核的企业知识资产。", keyMechanism: "来源定位、人工复核和版本留痕。", metrics: [], relationships: [] },
    }],
  };
}

store.savePublication("WS-DEMO", publication("A", "企业知识中台白皮书.pdf", "产品部", "2026-08-10T08:00:00.000Z"));
store.savePublication("WS-DEMO", publication("B", "知识治理实施指南.pdf", "研发部", "2026-08-11T08:00:00.000Z"));

const gateway = await createRealAnalysisServer({
  distRoot: path.join(repoRoot, "site", "dist"),
  platformStore: store,
  config: { mineruApiKey: "e2e-mineru", deepseekApiKey: "e2e-deepseek", deepseekModel: "deepseek-chat" },
  mineruClient: { async parseDocument() { throw new Error("not used"); } },
  deepseekClient: { async analyzeMarkdown() { throw new Error("not used"); } },
  semanticaClient: {
    async status() { return { state: "ready", enabled: true, engine: "Semantica", version: "0.6.0", message: "本地语义增强可用" }; },
    async enrich(assets) {
      assert.deepEqual(assets.map((asset) => asset.id).sort(), ["IP-REAL-REVA", "IP-REAL-REVB"]);
      return {
        status: "complete",
        engine: "Semantica",
        version: "0.6.0",
        checkedAssets: assets.length,
        duplicates: [{ assetIds: ["IP-REAL-REVA", "IP-REAL-REVB"], similarity: 0.97, confidence: 0.94, reasons: ["标题完全一致", "资产类型一致"] }],
        conflicts: [{ group: "enterprise-wiki", title: "企业知识中台治理方法", field: "owner", severity: "high", confidence: 0.92, values: ["产品部", "研发部"], sources: [{ assetId: "IP-REAL-REVA", document: "企业知识中台白皮书.pdf", value: "产品部" }, { assetId: "IP-REAL-REVB", document: "知识治理实施指南.pdf", value: "研发部" }] }],
        provenance: { assets: assets.length, evidence: 0, entries: [] },
      };
    },
  },
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
  const before = await (await fetch(`${baseURL}/api/assets`)).json();
  await page.goto(`${baseURL}/#system`, { waitUntil: "domcontentloaded", timeout: 30_000 });
  await page.locator("[data-testid='operations-console']").waitFor({ state: "visible" });
  await page.locator("#semantic-check-status", { hasText: "可以检查" }).waitFor();

  await Promise.all([
    page.waitForResponse((response) => response.url().endsWith("/api/admin/semantic/enrich") && response.status() === 200),
    page.locator("[data-testid='run-semantic-check']").click(),
  ]);
  await page.locator("[data-semantic-review-id]").first().waitFor();
  assert.equal(await page.locator("[data-semantic-review-id]").count(), 2);
  assert.match(await page.locator("#semantic-review-summary").textContent(), /待复核 2 条/);
  assert.equal(await page.locator(".semantic-review-decision").count(), 4);
  await page.locator("#semantic-check-panel").scrollIntoViewIfNeeded();
  await page.waitForTimeout(4_500);
  await page.locator("#semantic-check-panel").screenshot({ path: path.join(artifactRoot, "01-pending-reviews.png") });

  await page.locator("[data-testid='nav-tasks']").click();
  await page.locator("[data-workspace-action='semantic-review']").waitFor();
  assert.match(await page.locator("[data-workspace-action='semantic-review']").textContent(), /复核 2 条语义资产建议/);
  await page.screenshot({ path: path.join(artifactRoot, "02-action-center.png"), fullPage: true });
  await page.locator("[data-workspace-action='semantic-review']").getByRole("button", { name: "去处理" }).click();
  await page.locator("#semantic-check-panel").scrollIntoViewIfNeeded();

  const duplicate = page.locator("[data-semantic-review-id]", { hasText: "疑似重复" });
  await duplicate.getByRole("button", { name: "确认需治理" }).click();
  await page.locator("[data-testid='semantic-review-dialog']").waitFor({ state: "visible" });
  assert.match(await page.locator("#semantic-review-dialog-impact").textContent(), /不会自动合并资产或修改 Wiki/);
  await page.locator("#semantic-review-note").fill("由知识产权负责人继续核对来源与使用范围");
  await page.screenshot({ path: path.join(artifactRoot, "03-confirm-dialog.png") });
  await Promise.all([
    page.waitForResponse((response) => response.url().includes("/api/admin/semantic/reviews/") && response.url().endsWith("/decision") && response.status() === 200),
    page.locator("[data-testid='confirm-semantic-review']").click(),
  ]);
  await page.locator("[data-testid='semantic-review-dialog']").waitFor({ state: "hidden" });
  await duplicate.getByText("已确认 · 等待后续治理", { exact: true }).waitFor();

  const conflict = page.locator("[data-semantic-review-id]", { hasText: "责任方不一致" });
  await conflict.getByRole("button", { name: "保留独立记录" }).click();
  await page.locator("[data-testid='semantic-review-dialog']").waitFor({ state: "visible" });
  await page.locator("#semantic-review-note").fill("两个部门分别维护各自实施记录");
  await Promise.all([
    page.waitForResponse((response) => response.url().includes("/api/admin/semantic/reviews/") && response.url().endsWith("/decision") && response.status() === 200),
    page.locator("[data-testid='confirm-semantic-review']").click(),
  ]);
  await conflict.getByText("已保留为独立记录", { exact: true }).waitFor();
  assert.equal(await page.locator(".semantic-review-decision").count(), 0);
  await page.waitForTimeout(4_500);
  await page.locator("#semantic-check-panel").screenshot({ path: path.join(artifactRoot, "04-decisions-saved.png") });

  await page.reload({ waitUntil: "domcontentloaded" });
  await page.locator("[data-testid='nav-system']").click();
  await page.locator("[data-semantic-review-id]").first().waitFor();
  assert.equal(await page.getByText("已确认 · 等待后续治理", { exact: true }).count(), 1);
  assert.equal(await page.getByText("已保留为独立记录", { exact: true }).count(), 1);
  assert.equal(await page.locator(".semantic-review-decision").count(), 0);

  const after = await (await fetch(`${baseURL}/api/assets`)).json();
  assert.deepEqual(after.assets, before.assets, "semantic review decisions changed formal assets");
  const reviews = await (await fetch(`${baseURL}/api/admin/semantic/reviews`)).json();
  assert.deepEqual(reviews.reviews.map((review) => review.status).sort(), ["confirmed", "dismissed"]);
  const audit = await (await fetch(`${baseURL}/api/audit?limit=20`)).json();
  assert.ok(audit.events.some((event) => event.action === "semantic.review_confirm"));
  assert.ok(audit.events.some((event) => event.action === "semantic.review_dismiss"));

  await page.setViewportSize({ width: 390, height: 844 });
  await page.locator("#semantic-check-panel").scrollIntoViewIfNeeded();
  await page.locator("#semantic-check-panel").screenshot({ path: path.join(artifactRoot, "05-mobile-persisted-reviews.png") });

  const report = {
    executedAt: new Date().toISOString(),
    status: "PASS",
    baseURL,
    createdReviews: 2,
    decisions: ["confirmed", "dismissed"],
    persistedAfterReload: true,
    formalAssetsUnchanged: true,
    auditActions: ["semantic.review_confirm", "semantic.review_dismiss"],
  };
  await writeFile(path.join(artifactRoot, "result.json"), `${JSON.stringify(report, null, 2)}\n`, "utf8");
  await writeFile(path.join(artifactRoot, "report.md"), [
    "# Semantica 人工复核闭环 E2E",
    "",
    `- 执行时间：${report.executedAt}`,
    "- 流程：语义检查 → 待办中心 → 确认需治理 → 保留独立记录 → 刷新恢复",
    "- 权限：空间管理员真实 UI；复核候选只能由服务端检查结果创建",
    "- 状态：两种终态刷新后保留，不再显示改判按钮",
    "- 正式知识：检查前后资产 API 完全一致，未自动合并或修改 Wiki",
    "- 留痕：确认与保留决定均进入工作空间哈希审计链",
    "- 桌面与移动端截图：5 张",
    "- 结果：PASS",
    "",
  ].join("\n"), "utf8");
  process.stdout.write("Semantic review loop E2E passed: two persisted decisions, unchanged formal assets, audited and restored after reload.\n");
} finally {
  await context.close();
  await browser.close();
  await gateway.stop();
  store.close();
  await rm(runtime, { recursive: true, force: true });
}
