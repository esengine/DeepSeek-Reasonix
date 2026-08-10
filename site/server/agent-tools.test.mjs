import assert from "node:assert/strict";
import test from "node:test";
import { createAgentTools } from "./agent-tools.mjs";

function registryFixture() {
  const calls = [];
  const publicAsset = { id: "IP-REAL-PUBLIC", title: "知识抽取引擎", type: "技术方案", sensitivity: "内部", summary: "提取可追溯资产", evidence: [{ id: "EV-PUBLIC", quote: "可追溯证据", section: "目标" }], wiki: { title: "知识抽取引擎", executiveSummary: "摘要", keyMechanism: "证据抽取", metrics: [], relationships: [] } };
  const secretAsset = { id: "IP-REAL-SECRET", title: "秘密策略", sensitivity: "机密", evidence: [{ id: "EV-SECRET", quote: "秘密" }] };
  return {
    calls,
    publicAsset,
    registry: {
      async search(_query, _workspaceId, options) { calls.push(["search", options]); return options.role === "viewer" ? [publicAsset] : [publicAsset, secretAsset]; },
      async getAsset(_workspaceId, id, options) { calls.push(["asset", options]); return id === publicAsset.id || options.role !== "viewer" ? (id === secretAsset.id ? secretAsset : publicAsset) : null; },
      async getWiki(_workspaceId, id, options) { calls.push(["wiki", options]); return id === publicAsset.id ? { assetId: id, version: "V1.0", ...publicAsset.wiki, evidence: publicAsset.evidence } : null; },
      async getEvidence(_workspaceId, id, options) { calls.push(["evidence", options]); return id === "EV-PUBLIC" ? { id, assetId: publicAsset.id, quote: "可追溯证据", section: "目标", verified: true } : null; },
      async getAssetGraph(_workspaceId, options) { calls.push(["graph", options]); return { nodes: options.role === "viewer" ? [publicAsset] : [publicAsset, secretAsset], edges: [{ id: "REL-1", sourceAssetId: publicAsset.id, targetAssetId: secretAsset.id, verificationStatus: options.includeProposed ? "proposed" : "confirmed" }], meta: {} }; },
      async updateWiki() { throw new Error("must never mutate Wiki"); },
    },
  };
}

test("re-applies viewer visibility to search, reads and graph traversal", async () => {
  const fx = registryFixture();
  const tools = createAgentTools({ publicationRegistry: fx.registry });
  const context = { workspaceId: "WS-A", userId: "USR-A", role: "viewer" };
  assert.deepEqual((await tools.execute("search_assets", { query: "知识", limit: 20 }, context)).assets.map((item) => item.id), ["IP-REAL-PUBLIC"]);
  await assert.rejects(() => tools.execute("read_asset", { assetId: "IP-REAL-SECRET" }, context), (error) => error.code === "AGENT_SOURCE_NOT_FOUND");
  const graph = await tools.execute("inspect_neighborhood", { assetId: "IP-REAL-PUBLIC", depth: 2, includeProposed: true }, context);
  assert.equal(fx.calls.at(-1)[1].includeProposed, false);
  assert.equal(graph.graph.nodes.length, 1);
});

test("permits editor draft context but never saves or publishes Wiki", async () => {
  const fx = registryFixture();
  const tools = createAgentTools({ publicationRegistry: fx.registry });
  await assert.rejects(() => tools.execute("compose_wiki_draft", { assetId: "IP-REAL-PUBLIC", instructions: "补充风险" }, { workspaceId: "WS-A", userId: "USR-A", role: "viewer" }), (error) => error.code === "AGENT_ROLE_REQUIRED");
  const before = JSON.stringify(fx.publicAsset.wiki);
  const result = await tools.execute("compose_wiki_draft", { assetId: "IP-REAL-PUBLIC", instructions: "补充风险" }, { workspaceId: "WS-A", userId: "USR-E", role: "editor" });
  assert.equal(result.mode, "draft_only");
  assert.equal(result.currentWiki.version, "V1.0");
  assert.equal(JSON.stringify(fx.publicAsset.wiki), before);
});

test("compares only assets visible to the current role and bounds output", async () => {
  const fx = registryFixture();
  const tools = createAgentTools({ publicationRegistry: fx.registry });
  await assert.rejects(() => tools.execute("compare_assets", { assetIds: ["IP-REAL-PUBLIC", "IP-REAL-SECRET"] }, { workspaceId: "WS-A", userId: "USR-A", role: "viewer" }), (error) => error.code === "AGENT_SOURCE_NOT_FOUND");
  const result = await tools.execute("compare_assets", { assetIds: ["IP-REAL-PUBLIC", "IP-REAL-SECRET"] }, { workspaceId: "WS-A", userId: "USR-E", role: "editor" });
  assert.equal(result.assets.length, 2);
  assert.doesNotMatch(JSON.stringify(result), /workspaceId|password|cookie/i);
});
