import assert from "node:assert/strict";
import { mkdtemp, mkdir, rm, writeFile } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import test from "node:test";
import { createRealAnalysisServer } from "./real-analysis-server.mjs";

function publication() {
  return {
    publicationId: "PUB-AGENT-A",
    sourceJobId: "JOB-AGENT-A",
    status: "published",
    version: "V1.0",
    publishedAt: "2026-08-10T08:00:00.000Z",
    document: { title: "知识抽取报告", sourceName: "report.pdf" },
    assets: [{
      id: "IP-REAL-A",
      title: "知识抽取引擎",
      type: "技术方案",
      summary: "从长文档提取可追溯资产",
      tags: ["知识抽取"],
      confidence: 0.95,
      owner: "知识团队",
      sensitivity: "内部",
      status: "已发布",
      version: "V1.0",
      evidence: [{ id: "EV-A", assetId: "IP-REAL-A", quote: "提取结果绑定原文证据", section: "目标", verified: true }],
      wiki: { title: "知识抽取引擎", executiveSummary: "摘要", keyMechanism: "证据抽取", metrics: [], relationships: [] },
    }],
  };
}

test("exposes authenticated, rate-limited and policy-bounded Agent task APIs", async () => {
  const directory = await mkdtemp(path.join(os.tmpdir(), "intelifar-agent-api-"));
  const dist = path.join(directory, "dist");
  await mkdir(dist);
  await writeFile(path.join(dist, "index.html"), "<!doctype html><title>intelifar</title>", "utf8");
  let plannerCalls = 0;
  const gateway = await createRealAnalysisServer({
    distRoot: dist,
    databasePath: path.join(directory, "platform.sqlite"),
    backupRoot: path.join(directory, "backups"),
    agentRateLimit: 2,
    config: { mineruApiKey: "mineru", deepseekApiKey: "deepseek", deepseekModel: "deepseek-chat" },
    auth: { required: true, secureCookies: false, workspaceId: "WS-A", workspaceName: "甲公司", email: "owner@example.com", password: "StrongPassword!2026", name: "所有者" },
    mineruClient: { async parseDocument() { throw new Error("unused"); } },
    deepseekClient: { async analyzeMarkdown() { throw new Error("unused"); } },
    agentModelClient: {
      async planTask() { plannerCalls += 1; return { model: "deepseek-chat", usage: { totalTokens: 10 }, value: { title: "资产盘点", intent: "asset_inventory", outputType: "inventory_report", steps: [{ id: "S1", title: "搜索资产", tool: "search_assets", arguments: { query: "知识抽取", limit: 10 } }] } }; },
      async synthesizeTask() { return { model: "deepseek-chat", usage: { totalTokens: 10 }, value: { status: "complete", title: "资产盘点", summary: "完成", findings: [{ title: "已发布资产", detail: "知识抽取引擎已入库", sourceIds: ["IP-REAL-A"], confidence: .9 }], uncertainties: [], deliverables: [], nextActions: ["人工复核"] } }; },
    },
  });
  try {
    gateway.platformStore.savePublication("WS-A", publication());
    const baseUrl = await gateway.start();
    const health = await (await fetch(`${baseUrl}/api/health`)).json();
    assert.deepEqual(health.agent, { available: true, boundary: "document-ip-wiki-readonly", maxSteps: 6, maxToolCalls: 12, formalKnowledgeMutation: false });
    assert.equal((await fetch(`${baseUrl}/api/agent/tasks`)).status, 401);
    const login = await fetch(`${baseUrl}/api/auth/login`, { method: "POST", headers: { "content-type": "application/json" }, body: JSON.stringify({ email: "owner@example.com", password: "StrongPassword!2026" }) });
    const cookie = login.headers.get("set-cookie").split(";")[0];

    const submittedResponse = await fetch(`${baseUrl}/api/agent/tasks`, { method: "POST", headers: { cookie, "content-type": "application/json" }, body: JSON.stringify({ prompt: "盘点知识抽取相关资产" }) });
    assert.equal(submittedResponse.status, 202);
    const submitted = (await submittedResponse.json()).task;
    const settled = await gateway.agentService.whenSettled(submitted.id, { workspaceId: "WS-A", userId: gateway.platformStore.getUserByEmail("owner@example.com").id });
    assert.equal(settled.state, "complete", JSON.stringify(settled));
    const fetched = await (await fetch(`${baseUrl}/api/agent/tasks/${submitted.id}`, { headers: { cookie } })).json();
    assert.equal(fetched.task.result.findings[0].sourceIds[0], "IP-REAL-A");
    assert.equal((await (await fetch(`${baseUrl}/api/agent/tasks`, { headers: { cookie } })).json()).tasks.length, 1);

    const blocked = await fetch(`${baseUrl}/api/agent/tasks`, { method: "POST", headers: { cookie, "content-type": "application/json" }, body: JSON.stringify({ prompt: "帮我写 Python 代码并部署" }) });
    assert.equal(blocked.status, 200);
    assert.equal((await blocked.json()).task.state, "blocked");
    assert.equal(plannerCalls, 1);

    const limited = await fetch(`${baseUrl}/api/agent/tasks`, { method: "POST", headers: { cookie, "content-type": "application/json" }, body: JSON.stringify({ prompt: "盘点资产" }) });
    assert.equal(limited.status, 429);
    const crossOrigin = await fetch(`${baseUrl}/api/agent/tasks`, { method: "POST", headers: { cookie, origin: "https://attacker.invalid", "content-type": "application/json" }, body: JSON.stringify({ prompt: "盘点资产" }) });
    assert.equal(crossOrigin.status, 403);
    assert.equal(gateway.platformStore.verifyAuditChain("WS-A").valid, true);
  } finally {
    await gateway.stop();
    await rm(directory, { recursive: true, force: true });
  }
});
