import assert from "node:assert/strict";
import test from "node:test";
import {
  AGENT_INTENTS,
  AGENT_TOOLS,
  collectSourceIds,
  normalizeAgentResult,
  validateAgentPlan,
} from "./agent-contract.mjs";

function validPlan() {
  return {
    title: "知识抽取资产影响分析",
    intent: "impact_analysis",
    outputType: "impact_report",
    steps: [
      { id: "S1", title: "查找目标资产", tool: "search_assets", arguments: { query: "知识抽取", limit: 8 } },
      { id: "S2", title: "检查一跳依赖", tool: "inspect_neighborhood", arguments: { assetId: "IP-REAL-ABC", depth: 1 } },
    ],
  };
}

test("exports a closed list of domain intents and tools", () => {
  assert.deepEqual([...AGENT_INTENTS], ["asset_inventory", "evidence_review", "impact_analysis", "document_comparison", "wiki_draft", "risk_gap_review", "due_diligence_pack"]);
  assert.deepEqual([...AGENT_TOOLS], ["search_assets", "read_asset", "read_wiki", "read_evidence", "inspect_neighborhood", "compare_assets", "compose_wiki_draft"]);
});

test("accepts a bounded deterministic plan and normalizes its fields", () => {
  const plan = validateAgentPlan(validPlan());
  assert.equal(plan.steps.length, 2);
  assert.equal(plan.maxToolCalls, 12);
  assert.equal(plan.steps[0].arguments.limit, 8);
});

test("rejects unknown intents, tools, excessive steps and unsafe arguments", () => {
  assert.throws(() => validateAgentPlan({ ...validPlan(), intent: "general_assistant" }), (error) => error.code === "INVALID_AGENT_PLAN");
  assert.throws(() => validateAgentPlan({ ...validPlan(), steps: [{ id: "S1", title: "运行", tool: "shell", arguments: { command: "dir" } }] }), (error) => error.code === "INVALID_AGENT_PLAN");
  assert.throws(() => validateAgentPlan({ ...validPlan(), steps: Array.from({ length: 7 }, (_, index) => ({ id: `S${index + 1}`, title: "检索", tool: "search_assets", arguments: { query: "资产" } })) }), (error) => error.code === "INVALID_AGENT_PLAN");
  assert.throws(() => validateAgentPlan({ ...validPlan(), steps: [{ id: "S1", title: "越权", tool: "search_assets", arguments: { query: "资产", workspaceId: "WS-B" } }] }), (error) => error.code === "INVALID_AGENT_PLAN");
  assert.throws(() => validateAgentPlan({ ...validPlan(), steps: [{ id: "S1", title: "网络", tool: "search_assets", arguments: { query: "资产", url: "https://example.com" } }] }), (error) => error.code === "INVALID_AGENT_PLAN");
  assert.throws(() => validateAgentPlan({ ...validPlan(), steps: [{ id: "S1", title: "太深", tool: "inspect_neighborhood", arguments: { assetId: "IP-REAL-ABC", depth: 3 } }] }), (error) => error.code === "INVALID_AGENT_PLAN");
});

test("validates tool-specific argument shapes", () => {
  const compare = validateAgentPlan({ ...validPlan(), intent: "document_comparison", steps: [{ id: "S1", title: "比较", tool: "compare_assets", arguments: { assetIds: ["IP-REAL-A", "IP-REAL-B"] } }] });
  assert.deepEqual(compare.steps[0].arguments.assetIds, ["IP-REAL-A", "IP-REAL-B"]);
  assert.throws(() => validateAgentPlan({ ...validPlan(), steps: [{ id: "S1", title: "比较", tool: "compare_assets", arguments: { assetIds: ["IP-REAL-A"] } }] }));
  assert.throws(() => validateAgentPlan({ ...validPlan(), steps: [{ id: "S1", title: "草案", tool: "compose_wiki_draft", arguments: { assetId: "IP-REAL-A", instructions: "x".repeat(2_001) } }] }));
});

test("canonicalizes only known model argument aliases and rejects collisions", () => {
  const plan = validateAgentPlan({ ...validPlan(), intent: "document_comparison", steps: [
    { id: "S1", title: "读取", tool: "read_asset", arguments: { asset_id: "IP-REAL-A" } },
    { id: "S2", title: "比较", tool: "compare_assets", arguments: { asset_ids: ["IP-REAL-A", "IP-REAL-B"] } },
  ] });
  assert.equal(plan.steps[0].arguments.assetId, "IP-REAL-A");
  assert.deepEqual(plan.steps[1].arguments.assetIds, ["IP-REAL-A", "IP-REAL-B"]);
  assert.throws(() => validateAgentPlan({ ...validPlan(), steps: [{ id: "S1", title: "冲突", tool: "read_asset", arguments: { assetId: "IP-REAL-A", asset_id: "IP-REAL-B" } }] }));
});

test("collects only recognized source identifiers from tool receipts", () => {
  const ids = collectSourceIds([
    { tool: "search_assets", output: { assets: [{ id: "IP-REAL-A", evidence: [{ id: "EV-A" }] }] } },
    { tool: "inspect_neighborhood", output: { graph: { nodes: [{ id: "IP-REAL-B" }], edges: [{ id: "REL-A" }] } } },
    { tool: "read_wiki", output: { wiki: { assetId: "IP-REAL-C" } } },
    { tool: "unknown", output: { id: "WS-SECRET" } },
  ]);
  assert.deepEqual([...ids].sort(), ["EV-A", "IP-REAL-A", "IP-REAL-B", "IP-REAL-C", "REL-A", "WIKI:IP-REAL-C"]);
});

test("delivery gate keeps grounded findings and downgrades unsupported claims", () => {
  const result = normalizeAgentResult({
    status: "complete",
    title: "影响分析",
    summary: "已定位一个确认依赖，并发现一项模型未能提供依据的结论。",
    findings: [
      { title: "解析器是上游依赖", detail: "知识抽取引擎依赖解析器。", sourceIds: ["IP-REAL-A", "REL-A"], confidence: 0.93 },
      { title: "收入会增长", detail: "预计收入增长 80%。", sourceIds: ["EV-HALLUCINATED"], confidence: 0.99 },
    ],
    uncertainties: [],
    deliverables: [{ type: "impact_report", title: "影响清单", content: "供人工复核" }],
    nextActions: ["由资产负责人复核"],
  }, { allowedSourceIds: new Set(["IP-REAL-A", "REL-A"]), excludedActions: ["未发布或修改任何正式知识"] });
  assert.equal(result.status, "needs_review");
  assert.equal(result.findings.length, 1);
  assert.deepEqual(result.findings[0].sourceIds, ["IP-REAL-A", "REL-A"]);
  assert.equal(result.uncertainties.length, 1);
  assert.match(result.uncertainties[0], /收入会增长/);
  assert.deepEqual(result.quality, { totalClaims: 2, groundedClaims: 1, downgradedClaims: 1, evidenceCoverage: 0.5 });
  assert.deepEqual(result.excludedActions, ["未发布或修改任何正式知识"]);
});

test("delivery gate marks an empty evidence result for review and caps output", () => {
  const result = normalizeAgentResult({
    status: "complete",
    title: "空结果",
    summary: "没有找到资料",
    findings: [],
    uncertainties: Array.from({ length: 30 }, (_, index) => `缺口 ${index}`),
    deliverables: [],
    nextActions: Array.from({ length: 30 }, (_, index) => `动作 ${index}`),
  }, { allowedSourceIds: new Set() });
  assert.equal(result.status, "needs_review");
  assert.equal(result.uncertainties.length, 20);
  assert.equal(result.nextActions.length, 12);
  assert.equal(result.quality.evidenceCoverage, 0);
});

test("delivery gate corrects asset counts to the sources returned in this task", () => {
  const result = normalizeAgentResult({
    status: "complete",
    title: "证据核查",
    summary: "已核查当前账号可见资产，共 22 项，均有原文依据。",
    findings: [{ title: "已找到依据", detail: "本次返回的资产均有依据。", sourceIds: ["IP-REAL-A", "IP-REAL-B"], confidence: 0.9 }],
    uncertainties: ["当前搜索仅返回 20 条资产。"],
    deliverables: [],
    nextActions: [],
  }, { allowedSourceIds: new Set(["IP-REAL-A", "IP-REAL-B"]), visibleAssetCount: 2 });
  assert.equal(result.summary, "已核查本次返回的 2 项资产，均有原文依据。");
});
