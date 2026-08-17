import test from "node:test";
import assert from "node:assert/strict";
import { mkdtemp, rm } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import { createPublicationRegistry } from "./publication-registry.mjs";
import { createPlatformStore } from "./platform-store.mjs";

function completedJob() {
  return {
    id: "JOB-REAL-12345678-1234-1234-1234-123456789abc",
    state: "complete",
    document: { name: "enterprise-report.html", expectedCategory: "技术报告" },
    result: {
      parser: { provider: "MinerU", model: "MinerU-HTML", batchId: "batch-42", traceId: "trace-42", markdownSha256: "a".repeat(64) },
      llm: { provider: "DeepSeek", model: "deepseek-v4-flash", responseId: "resp-42", usage: { totalTokens: 99 } },
      analysis: {
        document: { title: "企业知识平台报告", category: "技术报告", summary: "可发布摘要", language: "zh-CN" },
        assets: [{ id: "IP-001", title: "证据驱动 Wiki", type: "技术方案", summary: "把分析结果沉淀为资产", tags: ["Wiki", "证据"], confidence: 0.96, source_quotes: [{ quote: "所有结论必须绑定可验证原文。", section: "产品目标" }] }],
        risks: [],
        wiki: { executive_summary: "可信知识资产", key_mechanism: "解析、提取、复核、发布", metrics: [{ label: "覆盖率", value: "96%" }], relationships: [{ source: "Wiki", relation: "引用", target: "原文" }] },
      },
    },
  };
}

test("publishes stable assets and evidence atomically and idempotently", async () => {
  const rootDir = await mkdtemp(path.join(os.tmpdir(), "intelifar-registry-"));
  try {
    const registry = createPublicationRegistry({ rootDir });
    const first = await registry.publish(completedJob(), { owner: "知识平台主管", sensitivity: "内部" });
    const second = await registry.publish(completedJob());
    assert.equal(first.publicationId, second.publicationId);
    assert.equal(first.assets.length, 1);
    assert.match(first.assets[0].id, /^IP-REAL-/);
    assert.match(first.assets[0].evidence[0].id, /^EV-/);
    assert.equal(first.assets[0].evidence[0].quoteHash.length, 64);
    assert.equal(first.assets[0].evidence[0].precision, "章节级");
    assert.equal(first.assets[0].evidence[0].section, "产品目标");
    assert.equal(first.assets[0].evidence[0].verified, true);
    assert.doesNotMatch(JSON.stringify(first), /Bearer|signed-url|private-key/);
  } finally {
    await rm(rootDir, { recursive: true, force: true });
  }
});

test("reloads published records and supports asset, Wiki, evidence and search lookup", async () => {
  const rootDir = await mkdtemp(path.join(os.tmpdir(), "intelifar-registry-"));
  try {
    const first = createPublicationRegistry({ rootDir });
    const publication = await first.publish(completedJob());
    const assetId = publication.assets[0].id;
    const evidenceId = publication.assets[0].evidence[0].id;
    const reloaded = createPublicationRegistry({ rootDir });
    assert.equal((await reloaded.listAssets()).length, 1);
    assert.equal((await reloaded.getAsset(assetId)).title, "证据驱动 Wiki");
    assert.equal((await reloaded.getWiki(assetId)).keyMechanism, "解析、提取、复核、发布");
    assert.equal((await reloaded.getEvidence(evidenceId)).documentHash, "a".repeat(64));
    assert.equal((await reloaded.search("产品目标")).length, 1);
    assert.equal((await reloaded.search("不存在")).length, 0);
  } finally {
    await rm(rootDir, { recursive: true, force: true });
  }
});

test("rejects publication before analysis completion", async () => {
  const rootDir = await mkdtemp(path.join(os.tmpdir(), "intelifar-registry-"));
  try {
    const registry = createPublicationRegistry({ rootDir });
    await assert.rejects(() => registry.publish({ ...completedJob(), state: "deepseek" }), /completed/i);
  } finally {
    await rm(rootDir, { recursive: true, force: true });
  }
});

test("migrates legacy atomic publications to SQLite exactly once", async () => {
  const directory = await mkdtemp(path.join(os.tmpdir(), "intelifar-registry-migration-"));
  const rootDir = path.join(directory, "publications");
  const store = createPlatformStore({ dbPath: path.join(directory, "platform.sqlite") });
  try {
    const legacy = createPublicationRegistry({ rootDir });
    const publication = await legacy.publish(completedJob(), { owner: "知识平台主管", sensitivity: "内部" });
    store.ensureWorkspace({ id: "WS-DEMO", name: "澜图科技" });
    const persistent = createPublicationRegistry({ rootDir, store, defaultWorkspaceId: "WS-DEMO" });

    assert.deepEqual(await persistent.migrateLegacyPublications("WS-DEMO"), { discovered: 1, imported: 1, skipped: 0 });
    assert.deepEqual(await persistent.migrateLegacyPublications("WS-DEMO"), { discovered: 1, imported: 0, skipped: 1 });
    assert.equal((await persistent.listAssets("WS-DEMO")).length, publication.assets.length);
    assert.equal(store.getAssetGraph("WS-DEMO", { role: "owner", includeProposed: true }).nodes.length, publication.assets.length);
  } finally {
    store.close();
    await rm(directory, { recursive: true, force: true });
  }
});
