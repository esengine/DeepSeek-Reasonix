import assert from "node:assert/strict";
import { rm, readFile, mkdir, writeFile } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { loadRuntimeConfig } from "../server/config.mjs";
import { createRealAnalysisServer } from "../server/real-analysis-server.mjs";
import { prepareCorpusSource } from "./internet-corpus-source.mjs";

const here = path.dirname(fileURLToPath(import.meta.url));
const repoRoot = path.resolve(here, "../..");
const siteRoot = path.join(repoRoot, "site");
const runtimeRoot = path.join(siteRoot, ".runtime", "internet-corpus");
const artifactsRoot = path.join(repoRoot, "artifacts", "internet-corpus");
const databasePath = path.join(runtimeRoot, "platform.sqlite");
const sources = JSON.parse(await readFile(path.join(here, "fixtures", "internet-corpus.sources.json"), "utf8"));
const keyFile = path.resolve(repoRoot, "..", "apikey.txt");
await mkdir(runtimeRoot, { recursive: true });
await mkdir(artifactsRoot, { recursive: true });

if (process.env.INTELIFAR_CORPUS_REUSE_DB !== "true") {
  for (const suffix of ["", "-wal", "-shm"]) await rm(`${databasePath}${suffix}`, { force: true });
}

const preparedSources = [];
for (const source of sources) {
  const prepared = await prepareCorpusSource(source, {
    cacheDir: runtimeRoot,
    refresh: process.env.INTELIFAR_CORPUS_REFRESH === "true",
  });
  preparedSources.push(prepared);
  process.stdout.write(`[internet-corpus] source ${source.id}: ${prepared.size} bytes sha256=${prepared.sha256.slice(0, 12)}…${prepared.cached ? " cached" : " downloaded"}\n`);
}

const sourceManifest = preparedSources.map(({ source, finalUrl, contentType, size, sha256, cached }) => ({
  id: source.id,
  title: source.title,
  sourcePage: source.sourcePage,
  downloadUrl: source.url,
  finalUrl,
  fileName: source.fileName,
  format: source.format,
  category: source.category,
  expectedConcepts: source.expectedConcepts,
  contentType,
  size,
  sha256,
  cached,
  fetchedAt: new Date().toISOString(),
}));
await writeFile(path.join(artifactsRoot, "source-manifest.json"), `${JSON.stringify(sourceManifest, null, 2)}\n`, "utf8");

const runtimeConfig = await loadRuntimeConfig({ keyFile, cwd: siteRoot });
const gateway = await createRealAnalysisServer({
  config: runtimeConfig,
  distRoot: path.join(siteRoot, "dist"),
  databasePath,
  uploadRoot: path.join(runtimeRoot, "uploads"),
  mineruMaxWaitMs: 30 * 60_000,
  analysisRateLimit: Math.max(8, sources.length + 2),
});
const baseURL = await gateway.start();
const startedAt = Date.now();
const states = new Map();
const jobs = [];
const publications = [];
let graph = null;
let crossSourceSearch = null;
let failure = null;

async function jsonResponse(response, context) {
  const payload = await response.json().catch(() => ({}));
  if (!response.ok) throw new Error(`${context} failed (${response.status}): ${String(payload.error || "unknown error").slice(0, 180)}`);
  return payload;
}

try {
  const health = await jsonResponse(await fetch(`${baseURL}/api/health`), "Health check");
  assert.equal(health.mode, "real");
  assert.equal(health.storage.adapter, "sqlite");

  for (const prepared of preparedSources) {
    const form = new FormData();
    form.append("file", new Blob([prepared.bytes], { type: prepared.contentType }), prepared.source.fileName);
    form.append("category", prepared.source.category);
    const payload = await jsonResponse(await fetch(`${baseURL}/api/analysis`, { method: "POST", body: form }), `Submit ${prepared.source.id}`);
    jobs.push({ source: prepared.source, id: payload.job.id, submittedAt: Date.now(), job: payload.job });
    process.stdout.write(`[internet-corpus] submitted ${prepared.source.id}: ${payload.job.id}\n`);
  }

  const deadline = Date.now() + 40 * 60_000;
  while (Date.now() < deadline && jobs.some((entry) => !["complete", "failed", "blocked"].includes(entry.job.state))) {
    for (const entry of jobs) {
      if (["complete", "failed", "blocked"].includes(entry.job.state)) continue;
      entry.job = (await jsonResponse(await fetch(`${baseURL}/api/analysis/${entry.id}`), `Poll ${entry.source.id}`)).job;
      if (states.get(entry.id) !== entry.job.state) {
        states.set(entry.id, entry.job.state);
        process.stdout.write(`[internet-corpus] ${entry.source.id}: ${entry.job.state} ${entry.job.progress}%\n`);
      }
    }
    if (jobs.some((entry) => !["complete", "failed", "blocked"].includes(entry.job.state))) await new Promise((resolve) => setTimeout(resolve, 5_000));
  }

  for (const entry of jobs) {
    assert.equal(entry.job.state, "complete", `${entry.source.id}: ${entry.job.error || "did not finish before the corpus deadline"}`);
    const parser = entry.job.result.parser;
    const analysis = entry.job.result.analysis;
    assert.ok(parser.markdownCharacters > 0, `${entry.source.id}: MinerU returned no Markdown`);
    assert.ok(parser.analysisInputCharacters > 0 && parser.analysisInputCharacters <= 60_000, `${entry.source.id}: analysis input range is invalid`);
    assert.ok(parser.quoteValidation.verified > 0, `${entry.source.id}: no source quotation passed verification`);
    assert.ok(analysis.assets.length > 0, `${entry.source.id}: no evidence-backed IP assets remained`);
    assert.ok(analysis.assets.every((asset) => asset.source_quotes.length > 0 && asset.source_quotes.every((quote) => quote.verified)), `${entry.source.id}: an asset contains unverified evidence`);
    const publicationPayload = await jsonResponse(await fetch(`${baseURL}/api/analysis/${entry.id}/publish`, {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({ owner: "公开材料验收", sensitivity: "公开" }),
    }), `Publish ${entry.source.id}`);
    const publication = publicationPayload.publication;
    assert.ok(publication.assets.length > 0 && publication.assets.every((asset) => asset.evidence.length > 0 && asset.evidence.every((evidence) => evidence.verified)), `${entry.source.id}: publication contains unsupported evidence`);
    publications.push({ sourceId: entry.source.id, publication });
    entry.completedAt = Date.now();
  }

  graph = (await jsonResponse(await fetch(`${baseURL}/api/assets/graph?includeProposed=true&limit=200&edgeLimit=500`), "Graph query")).graph;
  const publishedAssets = publications.reduce((sum, item) => sum + item.publication.assets.length, 0);
  assert.equal(graph.nodes.length, publishedAssets, "The asset graph did not contain every published corpus asset");
  assert.ok(graph.edges.length > 0, "No evidence-derived asset relationship was projected into the graph");
  assert.ok(graph.edges.every((edge) => edge.evidenceIds?.length > 0), "A proposed relationship has no verified endpoint evidence for human review");
  const searchCandidates = ["模型", "model", "attention", "知识图谱"];
  for (const query of searchCandidates) {
    const result = await jsonResponse(await fetch(`${baseURL}/api/assets/search?q=${encodeURIComponent(query)}&depth=2&limit=100`), `Graph search ${query}`);
    if (!crossSourceSearch || result.results.length > crossSourceSearch.results.length) crossSourceSearch = { query, ...result };
  }
  assert.ok(crossSourceSearch.results.length >= 2, "Cross-document graph search returned too little reusable context");
} catch (error) {
  failure = error;
} finally {
  await gateway.stop();
}

const resultRows = jobs.map((entry) => {
  const parser = entry.job?.result?.parser || {};
  const llm = entry.job?.result?.llm || {};
  const analysis = entry.job?.result?.analysis || {};
  const publication = publications.find((item) => item.sourceId === entry.source.id)?.publication;
  return {
    sourceId: entry.source.id,
    title: entry.source.title,
    state: entry.job?.state || "unknown",
    durationMs: entry.completedAt ? entry.completedAt - entry.submittedAt : Date.now() - entry.submittedAt,
    mineru: { model: parser.model || null, taskId: parser.batchId || entry.job?.providerTaskId || null, markdownCharacters: parser.markdownCharacters || 0, markdownSha256: parser.markdownSha256 || null },
    analysisRange: { strategy: parser.analysisSamplingStrategy || null, inputCharacters: parser.analysisInputCharacters || 0, selectedSourceCharacters: parser.analysisSelectedSourceCharacters || 0, selectedSections: parser.analysisSelectedSections || 0, totalSections: parser.analysisTotalSections || 0, coveragePositions: parser.analysisCoveragePositions || [] },
    quoteValidation: parser.quoteValidation || { total: 0, verified: 0, rejected: 0 },
    deepseek: { model: llm.model || null, responseId: llm.responseId || null, totalTokens: llm.usage?.totalTokens || 0 },
    relationshipCount: analysis.wiki?.relationships?.length || 0,
    assets: (analysis.assets || []).map((asset) => ({ title: asset.title, type: asset.type, confidence: asset.confidence, evidenceCount: asset.source_quotes?.length || 0 })),
    publication: publication ? { id: publication.publicationId, assetCount: publication.assets.length, evidenceCount: publication.assets.flatMap((asset) => asset.evidence).length } : null,
    error: entry.job?.error || null,
  };
});

const results = {
  runAt: new Date().toISOString(),
  result: failure ? "FAIL" : "PASS",
  durationMs: Date.now() - startedAt,
  sources: resultRows,
  graph: graph ? { nodes: graph.nodes.length, edges: graph.edges.length, proposedEdges: graph.edges.filter((edge) => edge.verificationStatus === "proposed").length, evidenceBackedEdges: graph.edges.filter((edge) => edge.evidenceIds?.length > 0).length } : null,
  crossSourceSearch: crossSourceSearch ? { query: crossSourceSearch.query, count: crossSourceSearch.results.length, resultAssetIds: crossSourceSearch.results.map((item) => item.asset?.id || item.node?.id || item.id).filter(Boolean) } : null,
  failure: failure ? String(failure.message).replace(/https?:\/\/\S+/gu, "[redacted-url]").slice(0, 300) : null,
};

const serialized = JSON.stringify(results, null, 2);
for (const secret of [runtimeConfig.mineruApiKey, runtimeConfig.deepseekApiKey]) assert.ok(!serialized.includes(secret), "A runtime credential leaked into corpus results");
await writeFile(path.join(artifactsRoot, "results.json"), `${serialized}\n`, "utf8");
const report = [
  "# intelifar 真实互联网材料批量 E2E",
  "",
  `- 时间：${results.runAt}`,
  `- 结果：${results.result}`,
  `- 总耗时：${results.durationMs} ms`,
  `- 材料：${resultRows.length} 份（公开技术 PDF 2 份、中文专利 HTML 1 份）`,
  `- 资产图：${results.graph ? `${results.graph.nodes} 节点 / ${results.graph.edges} 关系` : "未生成"}`,
  `- 跨材料搜索：${results.crossSourceSearch ? `“${results.crossSourceSearch.query}”返回 ${results.crossSourceSearch.count} 项` : "未完成"}`,
  "",
  "| 材料 | 状态 | MinerU 字符 | DeepSeek 范围 | 引用校验 | 资产 / 证据 / 关系 | Token |",
  "| --- | --- | ---: | --- | --- | --- | ---: |",
  ...resultRows.map((row) => `| ${row.title} | ${row.state} | ${row.mineru.markdownCharacters} | ${row.analysisRange.strategy || "n/a"} · ${row.analysisRange.inputCharacters} 字符 · ${row.analysisRange.selectedSections}/${row.analysisRange.totalSections} 节 | ${row.quoteValidation.verified} 通过 / ${row.quoteValidation.rejected} 拒绝 | ${row.publication?.assetCount || 0} / ${row.publication?.evidenceCount || 0} / ${row.relationshipCount} | ${row.deepseek.totalTokens} |`),
  "",
  "原始文件只保存在忽略提交的 `site/.runtime/internet-corpus/`。本报告不包含第三方全文、逐字引用或运行时密钥。",
  failure ? `\n失败：${results.failure}` : "",
  "",
].filter(Boolean).join("\n");
await writeFile(path.join(artifactsRoot, "report.md"), report, "utf8");

if (failure) throw failure;
process.stdout.write(`[internet-corpus] PASS: ${resultRows.length} sources, ${results.graph.nodes} assets, search=${results.crossSourceSearch.count}\n`);
