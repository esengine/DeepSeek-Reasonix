import assert from "node:assert/strict";
import { mkdtemp, rm } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import test from "node:test";
import { createAgentService } from "./agent-service.mjs";
import { createPlatformStore } from "./platform-store.mjs";

async function fixture(overrides = {}) {
  const directory = await mkdtemp(path.join(os.tmpdir(), "intelifar-agent-service-"));
  const store = createPlatformStore({ dbPath: path.join(directory, "platform.sqlite") });
  store.ensureWorkspace({ id: "WS-A", name: "甲公司" });
  store.ensureWorkspace({ id: "WS-B", name: "乙公司" });
  let plannerCalls = 0;
  let synthesisCalls = 0;
  const modelClient = overrides.modelClient ?? {
    async planTask() { plannerCalls += 1; return { model: "deepseek-chat", usage: { totalTokens: 10 }, value: { title: "影响分析", intent: "impact_analysis", outputType: "impact_report", steps: [{ id: "S1", title: "读取资产", tool: "read_asset", arguments: { assetId: "IP-REAL-A" } }] } }; },
    async synthesizeTask() { synthesisCalls += 1; return { model: "deepseek-chat", usage: { totalTokens: 12 }, value: { status: "complete", title: "影响分析", summary: "完成", findings: [{ title: "存在依赖", detail: "资产需要版面解析能力", sourceIds: ["IP-REAL-A"], confidence: .9 }], uncertainties: [], deliverables: [{ type: "impact_report", title: "影响清单", content: "人工复核后使用" }], nextActions: ["由资产负责人复核"] } }; },
  };
  const tools = overrides.tools ?? { async execute(_tool, _args, context) { return { asset: { id: "IP-REAL-A", title: "知识抽取引擎", roleSeen: context.role } }; } };
  let currentRole = "viewer";
  const audits = [];
  const service = createAgentService({
    store,
    modelClient,
    tools,
    resolveContext: async ({ workspaceId, userId }) => ({ workspaceId, userId, role: currentRole, active: true }),
    onAudit: (event) => audits.push(event),
    idFactory: (() => { let index = 0; return (prefix) => `${prefix}-${++index}`; })(),
    ...overrides.serviceOptions,
  });
  return { directory, store, service, audits, get plannerCalls() { return plannerCalls; }, get synthesisCalls() { return synthesisCalls; }, setRole(role) { currentRole = role; }, async close() { store.close(); await rm(directory, { recursive: true, force: true }); } };
}

test("runs a bounded plan, records receipts and delivers only grounded findings", async () => {
  const fx = await fixture();
  try {
    const submitted = await fx.service.submit({ prompt: "分析知识抽取引擎的依赖影响" }, { workspaceId: "WS-A", userId: "USR-A", role: "viewer" });
    assert.equal(submitted.state, "planning");
    const task = await fx.service.whenSettled(submitted.id, { workspaceId: "WS-A", userId: "USR-A" });
    assert.equal(task.state, "complete");
    assert.equal(task.result.findings.length, 1);
    assert.equal(task.result.quality.evidenceCoverage, 1);
    assert.deepEqual(task.events.map((event) => event.type), ["task.created", "plan.ready", "step.started", "step.complete", "delivery.complete"]);
    assert.equal(fx.plannerCalls, 1);
    assert.equal(fx.synthesisCalls, 1);
    assert.equal(fx.service.get(task.id, { workspaceId: "WS-A", userId: "USR-OTHER" }), null);
    assert.equal(fx.service.get(task.id, { workspaceId: "WS-B", userId: "USR-A" }), null);
    assert.ok(fx.audits.some((event) => event.action === "agent.task_complete"));
  } finally { await fx.close(); }
});

test("blocks an out-of-bound request before any model or tool call", async () => {
  const fx = await fixture();
  try {
    const task = await fx.service.submit({ prompt: "帮我写 Python 代码并部署" }, { workspaceId: "WS-A", userId: "USR-A", role: "editor" });
    assert.equal(task.state, "blocked");
    assert.equal(task.boundary.code, "coding");
    assert.equal(fx.plannerCalls, 0);
    assert.equal(fx.synthesisCalls, 0);
    assert.equal(fx.service.get(task.id, { workspaceId: "WS-A", userId: "USR-A" }).events[0].type, "policy.blocked");
  } finally { await fx.close(); }
});

test("blocks a viewer Wiki draft before the model while keeping read-only tasks available", async () => {
  const fx = await fixture();
  try {
    const task = await fx.service.submit({ prompt: "为目标资产生成 Wiki 更新草案", templateId: "wiki_draft" }, { workspaceId: "WS-A", userId: "USR-A", role: "viewer" });
    assert.equal(task.state, "blocked");
    assert.equal(task.boundary.code, "role_required");
    assert.equal(fx.plannerCalls, 0);
    assert.match(task.boundary.message, /知识编辑者/);
  } finally { await fx.close(); }
});

test("fails closed when the model proposes an unknown tool", async () => {
  const fx = await fixture({ modelClient: {
    async planTask() { return { value: { title: "越界", intent: "impact_analysis", steps: [{ id: "S1", title: "运行", tool: "shell", arguments: { command: "dir" } }] } }; },
    async synthesizeTask() { throw new Error("must not synthesize"); },
  } });
  try {
    const submitted = await fx.service.submit({ prompt: "分析资产影响" }, { workspaceId: "WS-A", userId: "USR-A", role: "viewer" });
    const task = await fx.service.whenSettled(submitted.id, { workspaceId: "WS-A", userId: "USR-A" });
    assert.equal(task.state, "failed");
    assert.match(task.error, /安全计划校验/);
  } finally { await fx.close(); }
});

test("re-evaluates current role before every tool and stops disabled users", async () => {
  let active = true;
  const seenRoles = [];
  const fx = await fixture({
    tools: { async execute(_tool, _args, context) { seenRoles.push(context.role); active = false; return { asset: { id: "IP-REAL-A" } }; } },
    serviceOptions: { resolveContext: async ({ workspaceId, userId }) => ({ workspaceId, userId, role: "viewer", active }) },
    modelClient: {
      async planTask() { return { value: { title: "双步骤", intent: "impact_analysis", steps: [{ id: "S1", title: "读取一", tool: "read_asset", arguments: { assetId: "IP-REAL-A" } }, { id: "S2", title: "读取二", tool: "read_asset", arguments: { assetId: "IP-REAL-A" } }] } }; },
      async synthesizeTask() { throw new Error("must not synthesize"); },
    },
  });
  try {
    const submitted = await fx.service.submit({ prompt: "分析资产影响" }, { workspaceId: "WS-A", userId: "USR-A", role: "viewer" });
    const task = await fx.service.whenSettled(submitted.id, { workspaceId: "WS-A", userId: "USR-A" });
    assert.equal(task.state, "failed");
    assert.deepEqual(seenRoles, ["viewer"]);
  } finally { await fx.close(); }
});

test("cancels a queued task without claiming completion", async () => {
  let release;
  const fx = await fixture({ modelClient: {
    async planTask() { return new Promise((resolve) => { release = () => resolve({ value: { title: "计划", intent: "asset_inventory", steps: [{ id: "S1", title: "搜索", tool: "search_assets", arguments: { query: "资产" } }] } }); }); },
    async synthesizeTask() { throw new Error("must not synthesize"); },
  } });
  try {
    const submitted = await fx.service.submit({ prompt: "盘点资产" }, { workspaceId: "WS-A", userId: "USR-A", role: "viewer" });
    const cancelled = fx.service.cancel(submitted.id, { workspaceId: "WS-A", userId: "USR-A" });
    assert.equal(cancelled.state, "cancelled");
    release();
    const settled = await fx.service.whenSettled(submitted.id, { workspaceId: "WS-A", userId: "USR-A" });
    assert.equal(settled.state, "cancelled");
  } finally { await fx.close(); }
});
