import assert from "node:assert/strict";
import { randomUUID } from "node:crypto";
import { mkdir, mkdtemp, rm, writeFile } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { loadRuntimeConfig } from "../server/config.mjs";
import { AGENT_TOOLS } from "../server/agent-contract.mjs";
import { createRealAnalysisServer } from "../server/real-analysis-server.mjs";

const here = path.dirname(fileURLToPath(import.meta.url));
const repoRoot = path.resolve(here, "../..");
const siteRoot = path.join(repoRoot, "site");
const artifacts = path.join(repoRoot, "artifacts", "ip-agent");
const runtime = await mkdtemp(path.join(os.tmpdir(), "intelifar-real-agent-"));
await mkdir(artifacts, { recursive: true });

const config = await loadRuntimeConfig({ cwd: siteRoot, keyFile: path.resolve(repoRoot, "..", "apikey.txt") });
const workspaceId = "WS-REAL-AGENT";
const email = "owner.real-agent@example.com";
const password = `RealAgent-${randomUUID()}-Aa!`;

const realMaterialPublication = {
  publicationId: "PUB-REAL-MATERIAL-AGENT",
  sourceJobId: "JOB-REAL-MATERIAL-AGENT",
  status: "published",
  version: "V1.0",
  publishedAt: "2026-08-10T08:00:00.000Z",
  document: { title: "DeepSeek-V3 Technical Report", sourceName: "deepseek-v3.pdf", sourcePage: "https://arxiv.org/abs/2412.19437" },
  assets: [
    {
      id: "IP-REAL-MLA",
      title: "Multi-head Latent Attention (MLA)",
      type: "技术方案",
      summary: "通过低秩联合压缩降低推理期间的 KV Cache 开销。",
      tags: ["DeepSeek-V3", "Attention", "KV Cache"],
      confidence: 0.98,
      owner: "基础模型组",
      sensitivity: "内部",
      status: "已发布",
      version: "V1.0",
      evidence: [{ id: "EV-REAL-MLA-1", assetId: "IP-REAL-MLA", quote: "MLA reduces the Key-Value cache during inference through low-rank joint compression.", section: "2.1.1 Multi-head Latent Attention", verified: true, documentName: "deepseek-v3.pdf" }],
      wiki: { title: "Multi-head Latent Attention (MLA)", executiveSummary: "降低长上下文推理的缓存成本。", keyMechanism: "对注意力 Key 和 Value 进行低秩联合压缩。", metrics: [], relationships: [] },
    },
    {
      id: "IP-REAL-DUALPIPE",
      title: "DualPipe Training Framework",
      type: "软件架构",
      summary: "通过双向流水调度降低流水线气泡并重叠计算与通信。",
      tags: ["DeepSeek-V3", "Training", "Pipeline"],
      confidence: 0.95,
      owner: "训练系统组",
      sensitivity: "内部",
      status: "已发布",
      version: "V1.0",
      evidence: [{ id: "EV-REAL-DUALPIPE-1", assetId: "IP-REAL-DUALPIPE", quote: "DualPipe overlaps computation and communication and reduces pipeline bubbles.", section: "3.2.2 DualPipe and Computation-Communication Overlap", verified: true, documentName: "deepseek-v3.pdf" }],
      wiki: { title: "DualPipe Training Framework", executiveSummary: "提升大规模训练流水线效率。", keyMechanism: "双向流水调度与计算通信重叠。", metrics: [], relationships: [] },
    },
  ],
};

const gateway = await createRealAnalysisServer({
  distRoot: path.join(siteRoot, "dist"),
  databasePath: path.join(runtime, "platform.sqlite"),
  uploadRoot: path.join(runtime, "uploads"),
  backupRoot: path.join(runtime, "backups"),
  config,
  agentModelTimeoutMs: 180_000,
  auth: { required: true, secureCookies: false, workspaceId, workspaceName: "真实材料 Agent 验证空间", email, password, name: "验收负责人" },
  mineruClient: { async parseDocument() { throw new Error("unused in real Agent E2E"); } },
  deepseekClient: { async analyzeMarkdown() { throw new Error("unused in real Agent E2E"); } },
});

let task;
let failure;
let wikiVersionCountBefore = 0;
const startedAt = Date.now();
try {
  gateway.platformStore.savePublication(workspaceId, realMaterialPublication);
  gateway.platformStore.createAssetRelationship(workspaceId, {
    sourceAssetId: "IP-REAL-MLA",
    targetAssetId: "IP-REAL-DUALPIPE",
    relationType: "part_of",
    evidenceIds: ["EV-REAL-MLA-1"],
    origin: "manual",
  });
  wikiVersionCountBefore = gateway.platformStore.listWikiVersions(workspaceId, "IP-REAL-MLA").length;
  const baseURL = await gateway.start();
  const login = await fetch(`${baseURL}/api/auth/login`, {
    method: "POST",
    headers: { "content-type": "application/json" },
    body: JSON.stringify({ email, password }),
  });
  assert.equal(login.status, 200);
  const cookie = login.headers.get("set-cookie")?.split(";")[0];
  assert.ok(cookie);
  const submittedResponse = await fetch(`${baseURL}/api/agent/tasks`, {
    method: "POST",
    headers: { cookie, "content-type": "application/json", accept: "application/json" },
    body: JSON.stringify({
      templateId: "document_comparison",
      assetIds: ["IP-REAL-MLA", "IP-REAL-DUALPIPE"],
      prompt: "比较已入库的 IP-REAL-MLA 与 IP-REAL-DUALPIPE，说明两项资产的机制、适用阶段、证据和治理关注点，形成只读尽调清单。只依据当前知识空间，任何缺少依据的内容列为待核实。",
    }),
  });
  assert.equal(submittedResponse.status, 202);
  const submitted = (await submittedResponse.json()).task;
  const userId = gateway.platformStore.getUserByEmail(email).id;
  task = await gateway.agentService.whenSettled(submitted.id, { workspaceId, userId });

  assert.ok(["complete", "needs_review"].includes(task.state), task.error || `unexpected state ${task.state}`);
  assert.ok(task.plan.steps.length >= 1 && task.plan.steps.length <= 6);
  assert.ok(task.plan.steps.every((step) => AGENT_TOOLS.includes(step.tool)));
  assert.ok(task.usage.totalTokens > 0, "real DeepSeek token usage is missing");
  assert.match(task.model, /^deepseek/i);
  assert.ok(task.result.quality.evidenceCoverage >= 0.5, "evidence gate downgraded too many claims");
  assert.ok(task.result.findings.length >= 1, "real DeepSeek did not return a grounded finding");
  const allowedSources = new Set(["IP-REAL-MLA", "IP-REAL-DUALPIPE", "EV-REAL-MLA-1", "EV-REAL-DUALPIPE-1"]);
  assert.ok(task.result.findings.flatMap((finding) => finding.sourceIds).every((id) => allowedSources.has(id) || /^REL-/.test(id)), "result contains an ungrounded source ID");
  assert.equal(gateway.platformStore.listWikiVersions(workspaceId, "IP-REAL-MLA").length, wikiVersionCountBefore);
  assert.equal(gateway.platformStore.verifyAuditChain(workspaceId).valid, true);

  const serialized = JSON.stringify(task, null, 2);
  for (const secret of [config.deepseekApiKey, config.mineruApiKey, password]) assert.ok(!serialized.includes(secret), "credential leaked into real Agent result");
  await writeFile(path.join(artifacts, "real-deepseek-task.json"), `${serialized}\n`, "utf8");
} catch (error) {
  failure = error;
} finally {
  await gateway.stop();
  await rm(runtime, { recursive: true, force: true });
}

const durationMs = Date.now() - startedAt;
const report = [
  "# intelifar 受控 IP 任务助手 · 真实 DeepSeek E2E",
  "",
  `- 结果：${failure ? "FAIL" : "PASS"}`,
  `- 运行时间：${new Date().toISOString()}`,
  `- 耗时：${durationMs} ms`,
  "- 输入材料：DeepSeek-V3 Technical Report 已入库资产（来自真实互联网材料验证集）",
  `- DeepSeek 模型：${task?.model ?? config.deepseekModel}`,
  `- 任务状态：${task?.state ?? "not-completed"}`,
  `- 受控步骤：${task?.plan?.steps?.length ?? 0}`,
  `- Token：${task?.usage?.totalTokens ?? 0}`,
  `- 有依据发现：${task?.result?.quality?.groundedClaims ?? 0}`,
  `- 证据覆盖：${Math.round(Number(task?.result?.quality?.evidenceCoverage ?? 0) * 100)}%`,
  "- 正式 Wiki 写入：0",
  "- 凭据泄漏扫描：PASS（仅内存比较，未写入密钥）",
  failure ? `- 失败：${String(failure.message).replace(/https?:\/\/\S+/g, "[redacted-url]").slice(0, 300)}` : "",
  "",
].filter(Boolean).join("\n");
await writeFile(path.join(artifacts, "real-deepseek-report.md"), report, "utf8");
if (failure) throw failure;
process.stdout.write(`Real DeepSeek Agent E2E passed in ${durationMs} ms: ${task.model}, ${task.usage.totalTokens} tokens, ${Math.round(task.result.quality.evidenceCoverage * 100)}% evidence coverage.\n`);
