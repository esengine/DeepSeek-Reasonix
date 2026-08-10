import assert from "node:assert/strict";
import { mkdir, mkdtemp, rm, writeFile } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import { performance } from "node:perf_hooks";
import { fileURLToPath } from "node:url";
import { createPlatformStore } from "../server/platform-store.mjs";

const here = path.dirname(fileURLToPath(import.meta.url));
const repoRoot = path.resolve(here, "../..");
const artifactRoot = path.join(repoRoot, "artifacts", "ip-asset-graph");
await mkdir(artifactRoot, { recursive: true });
const temporary = await mkdtemp(path.join(os.tmpdir(), "intelifar-graph-performance-"));
const store = createPlatformStore({ dbPath: path.join(temporary, "graph.sqlite") });

function percentile(values, fraction) {
  const sorted = [...values].sort((left, right) => left - right);
  return sorted[Math.max(0, Math.ceil(sorted.length * fraction) - 1)];
}

try {
  store.ensureWorkspace({ id: "WS-PERF", name: "intelifar 性能验收空间" });
  const database = store.unsafeDatabaseForTests;
  const insertNode = database.prepare(`
    INSERT INTO asset_nodes(
      workspace_id, asset_id, publication_id, title, normalized_title, asset_type, owner,
      sensitivity, summary, tags_json, confidence, status, version, evidence_ids_json, updated_at
    ) VALUES(?, ?, 'PUB-PERF', ?, ?, ?, ?, ?, ?, ?, ?, '有效', 'V1.0', '[]', '2026-08-10T00:00:00.000Z')
  `);
  const insertEdge = database.prepare(`
    INSERT INTO asset_relationships(
      workspace_id, relationship_id, source_asset_id, target_asset_id, relation_type, confidence,
      verification_status, origin, created_by, created_at, updated_at, superseded_by
    ) VALUES('WS-PERF', ?, ?, ?, 'depends_on', .92, 'confirmed', 'import', NULL, '2026-08-10T00:00:00.000Z', '2026-08-10T00:00:00.000Z', NULL)
  `);
  const seed = database.transaction(() => {
    for (let index = 0; index < 10_000; index += 1) {
      const suffix = String(index).padStart(5, "0");
      insertNode.run(
        "WS-PERF",
        `IP-PERF-${suffix}`,
        `企业专利资产 ${suffix}`,
        `企业专利资产 ${suffix}`,
        index % 3 === 0 ? "核心技术" : index % 3 === 1 ? "软件著作权" : "业务规则",
        `业务团队 ${index % 40}`,
        index % 100 === 0 ? "机密" : "内部",
        `面向企业知识治理的可检索资产摘要 ${suffix}`,
        JSON.stringify([`标签-${index % 80}`, "性能样本"]),
        .8 + (index % 20) / 100,
      );
    }
    let relationshipIndex = 0;
    for (let sourceIndex = 0; sourceIndex < 10_000; sourceIndex += 1) {
      for (let offset = 1; offset <= 10; offset += 1) {
        insertEdge.run(
          `REL-PERF-${String(relationshipIndex).padStart(6, "0")}`,
          `IP-PERF-${String(sourceIndex).padStart(5, "0")}`,
          `IP-PERF-${String((sourceIndex + offset) % 10_000).padStart(5, "0")}`,
        );
        relationshipIndex += 1;
      }
    }
  });
  const seedStarted = performance.now();
  seed();
  const seedDurationMs = performance.now() - seedStarted;

  store.searchAssetGraph("WS-PERF", "企业专利资产 09999", { role: "viewer", depth: 1 });
  store.getAssetGraph("WS-PERF", { role: "viewer", rootAssetId: "IP-PERF-00001", depth: 2, limit: 100, edgeLimit: 200 });
  const searchDurations = [];
  const traversalDurations = [];
  for (let run = 0; run < 8; run += 1) {
    let started = performance.now();
    const search = store.searchAssetGraph("WS-PERF", "企业专利资产 09999", { role: "viewer", depth: 1, limit: 50 });
    searchDurations.push(performance.now() - started);
    assert.equal(search.results[0].asset.id, "IP-PERF-09999");
    started = performance.now();
    const graph = store.getAssetGraph("WS-PERF", { role: "viewer", rootAssetId: "IP-PERF-00001", depth: 2, limit: 100, edgeLimit: 200 });
    traversalDurations.push(performance.now() - started);
    assert.ok(graph.nodes.length > 1 && graph.nodes.length <= 100);
    assert.ok(graph.edges.length <= 200);
    assert.ok(graph.nodes.every((node) => node.sensitivity !== "机密"));
  }
  const result = {
    generatedAt: new Date().toISOString(),
    fixture: { nodes: 10_000, relationships: 100_000, confidentialNodes: 100 },
    seedDurationMs: Number(seedDurationMs.toFixed(2)),
    search: {
      samplesMs: searchDurations.map((value) => Number(value.toFixed(2))),
      p50Ms: Number(percentile(searchDurations, .5).toFixed(2)),
      p95Ms: Number(percentile(searchDurations, .95).toFixed(2)),
      targetP95Ms: 400,
    },
    boundedTraversal: {
      samplesMs: traversalDurations.map((value) => Number(value.toFixed(2))),
      p50Ms: Number(percentile(traversalDurations, .5).toFixed(2)),
      p95Ms: Number(percentile(traversalDurations, .95).toFixed(2)),
      targetP95Ms: 500,
    },
  };
  await writeFile(path.join(artifactRoot, "performance-results.json"), `${JSON.stringify(result, null, 2)}\n`, "utf8");
  await writeFile(path.join(artifactRoot, "performance-report.md"), `# IP 资产图性能验收\n\n- 规模：10,000 节点 / 100,000 关系\n- 搜索 P95：${result.search.p95Ms} ms（目标 < ${result.search.targetP95Ms} ms）\n- 两跳遍历 P95：${result.boundedTraversal.p95Ms} ms（目标 < ${result.boundedTraversal.targetP95Ms} ms）\n- 权限：查看者查询结果已排除 100 个机密节点\n`, "utf8");
  assert.ok(result.search.p95Ms < result.search.targetP95Ms, `Search P95 ${result.search.p95Ms} ms exceeds target`);
  assert.ok(result.boundedTraversal.p95Ms < result.boundedTraversal.targetP95Ms, `Traversal P95 ${result.boundedTraversal.p95Ms} ms exceeds target`);
  process.stdout.write(`${JSON.stringify(result, null, 2)}\n`);
} finally {
  store.close();
  await rm(temporary, { recursive: true, force: true });
}
