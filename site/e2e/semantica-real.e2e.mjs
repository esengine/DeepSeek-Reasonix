import assert from "node:assert/strict";
import { mkdir, writeFile } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";

const here = path.dirname(fileURLToPath(import.meta.url));
const repoRoot = path.resolve(here, "../..");
const artifactRoot = path.join(repoRoot, "artifacts", "semantica-phase-1");
const baseURL = String(process.env.INTELIFAR_E2E_BASE_URL || "http://127.0.0.1:4388").replace(/\/$/, "");
await mkdir(artifactRoot, { recursive: true });

async function json(pathname, options) {
  const response = await fetch(`${baseURL}${pathname}`, options);
  const payload = await response.json().catch(() => ({}));
  return { response, payload };
}

const health = await json("/api/health");
assert.equal(health.response.status, 200);
assert.equal(health.payload.providers?.semantica, "ready");
assert.equal(health.payload.semanticEnhancement?.version, "0.6.0");
assert.deepEqual(health.payload.dataBoundary?.localProcessors, ["Semantica"]);

const operations = await json("/api/admin/operations");
assert.equal(operations.response.status, 200);
assert.equal(operations.payload.semanticEnhancement?.state, "ready");

const before = await json("/api/assets");
assert.equal(before.response.status, 200);
assert.ok(Array.isArray(before.payload.assets));
assert.ok(before.payload.assets.length > 0, "real workspace must contain at least one published asset");

const check = await json("/api/admin/semantic/enrich", { method: "POST", headers: { accept: "application/json" } });
assert.equal(check.response.status, 200);
assert.equal(check.payload.result?.status, "complete");
assert.equal(check.payload.result?.version, "0.6.0");
assert.equal(check.payload.result?.checkedAssets, Math.min(before.payload.assets.length, 100));

const authorizedIds = new Set(before.payload.assets.slice(0, 100).map((asset) => asset.id));
for (const candidate of check.payload.result.duplicates || []) for (const id of candidate.assetIds) assert.ok(authorizedIds.has(id), `duplicate result leaked unauthorized asset ${id}`);
for (const conflict of check.payload.result.conflicts || []) for (const source of conflict.sources || []) assert.ok(authorizedIds.has(source.assetId), `conflict result leaked unauthorized asset ${source.assetId}`);

const after = await json("/api/assets");
assert.equal(after.response.status, 200);
assert.deepEqual(after.payload.assets, before.payload.assets, "read-only semantic check changed formal assets");

const audit = await json("/api/audit?limit=20");
assert.equal(audit.response.status, 200);
assert.ok((audit.payload.events || []).some((event) => event.action === "semantic.check" && event.detail?.formalKnowledgeMutation === false));

const resultArtifact = {
  executedAt: new Date().toISOString(),
  baseURL,
  status: "PASS",
  engine: check.payload.result.engine,
  version: check.payload.result.version,
  publishedAssetsBefore: before.payload.assets.length,
  checkedAssets: check.payload.result.checkedAssets,
  duplicateCandidates: check.payload.result.duplicates,
  conflicts: check.payload.result.conflicts,
  provenance: check.payload.result.provenance,
  formalAssetsUnchanged: true,
  auditRecorded: true,
};
await writeFile(path.join(artifactRoot, "result.json"), `${JSON.stringify(resultArtifact, null, 2)}\n`, "utf8");
await writeFile(path.join(artifactRoot, "report.md"), [
  "# Semantica 只读语义资产体检 E2E",
  "",
  `- 执行时间：${resultArtifact.executedAt}`,
  `- 真实 UI 网关：${baseURL}`,
  `- 引擎：Semantica ${resultArtifact.version}`,
  `- 当前发布资产：${resultArtifact.publishedAssetsBefore} 项；实际检查：${resultArtifact.checkedAssets} 项`,
  `- 疑似重复：${resultArtifact.duplicateCandidates.length} 组；信息冲突：${resultArtifact.conflicts.length} 项；来源依据：${resultArtifact.provenance.evidence} 处`,
  "- 权限：结果中的全部资产 ID 均属于网关授权输入",
  "- 无写入验证：检查前后正式资产响应完全一致",
  "- 留痕：写入 semantic.check 审计事件，明确 formalKnowledgeMutation=false",
  "- 结果：PASS",
  "",
].join("\n"), "utf8");

process.stdout.write(`Semantica real E2E passed: ${resultArtifact.checkedAssets} assets, ${resultArtifact.duplicateCandidates.length} duplicate candidates, ${resultArtifact.conflicts.length} conflicts.\n`);
