import assert from "node:assert/strict";
import { mkdtemp, mkdir, rm, writeFile } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import test from "node:test";
import { createRealAnalysisServer } from "./real-analysis-server.mjs";

test("serves the secured same-origin real analysis API", async () => {
  const directory = await mkdtemp(path.join(os.tmpdir(), "intelifar-gateway-"));
  const dist = path.join(directory, "dist");
  await mkdir(dist);
  await writeFile(path.join(dist, "index.html"), "<!doctype html><title>intelifar</title>", "utf8");
  await writeFile(path.join(dist, "public-runtime.mjs"), "export const ready = true;", "utf8");
  const gateway = await createRealAnalysisServer({
    distRoot: dist,
    config: { mineruApiKey: "mineru-private", deepseekApiKey: "deepseek-private", deepseekModel: "deepseek-v4-flash" },
    mineruClient: { async parseDocument(file) { return { provider: "MinerU", model: "MinerU-HTML", batchId: "batch-test", traceId: "trace-test", fileName: file.name, markdown: "# parsed" }; } },
    registryRoot: path.join(directory, "registry"),
    deepseekClient: { async analyzeMarkdown() { return { provider: "DeepSeek", model: "deepseek-v4-flash", responseId: "chat-test", usage: { totalTokens: 12 }, analysis: { document: { title: "Report", summary: "Enterprise summary", category: "技术报告" }, assets: [{ id: "IP-1", title: "Published Wiki", type: "技术方案", summary: "Traceable", confidence: 0.97, tags: ["Wiki"], source_quotes: [{ quote: "Verbatim enterprise evidence", section: "Overview" }] }], risks: [], wiki: { executive_summary: "Wiki summary", key_mechanism: "Evidence first", metrics: [], relationships: [] } } }; } },
  });
  try {
    const baseUrl = await gateway.start();
    const health = await fetch(`${baseUrl}/api/health`);
    assert.equal(health.status, 200);
    assert.equal(health.headers.get("x-content-type-options"), "nosniff");
    assert.ok(health.headers.get("x-request-id"));
    const publicRuntime = await fetch(`${baseUrl}/public-runtime.mjs`);
    assert.equal(publicRuntime.status, 200);
    assert.match(publicRuntime.headers.get("content-type"), /^text\/javascript/);
    const healthPayload = await health.json();
    assert.equal(healthPayload.dataBoundary.gateway, "local");
    assert.deepEqual(healthPayload.dataBoundary.externalProcessors, ["MinerU", "DeepSeek"]);
    const form = new FormData();
    form.append("file", new Blob(["<!doctype html><html><body>IP</body></html>"], { type: "text/html" }), "report.html");
    form.append("category", "技术报告");
    const submitted = await fetch(`${baseUrl}/api/analysis`, { method: "POST", body: form });
    assert.equal(submitted.status, 202);
    const { job } = await submitted.json();
    const complete = await gateway.analysisService.whenSettled(job.id);
    const result = await (await fetch(`${baseUrl}/api/analysis/${job.id}`)).text();
    assert.equal(complete.state, "complete");
    assert.doesNotMatch(result, /mineru-private|deepseek-private/);
    const publishedResponse = await fetch(`${baseUrl}/api/analysis/${job.id}/publish`, { method: "POST", headers: { "content-type": "application/json" }, body: JSON.stringify({ owner: "知识平台主管", sensitivity: "内部" }) });
    assert.equal(publishedResponse.status, 201);
    const { publication } = await publishedResponse.json();
    const assetId = publication.assets[0].id;
    const evidenceId = publication.assets[0].evidence[0].id;
    assert.equal((await (await fetch(`${baseUrl}/api/assets`)).json()).assets.length, 1);
    assert.equal((await (await fetch(`${baseUrl}/api/assets/${assetId}`)).json()).asset.title, "Published Wiki");
    assert.equal((await (await fetch(`${baseUrl}/api/wiki/${assetId}`)).json()).wiki.keyMechanism, "Evidence first");
    assert.equal((await (await fetch(`${baseUrl}/api/evidence/${evidenceId}`)).json()).evidence.precision, "章节级");
    assert.equal((await (await fetch(`${baseUrl}/api/search?q=Overview`)).json()).results.length, 1);
    const republished = await fetch(`${baseUrl}/api/analysis/${job.id}/publish`, { method: "POST" });
    assert.equal(republished.status, 200);
  } finally {
    await gateway.stop();
    await rm(directory, { recursive: true, force: true });
  }
});

test("rate limits analysis creation and rejects cross-origin state changes", async () => {
  const directory = await mkdtemp(path.join(os.tmpdir(), "intelifar-gateway-"));
  const dist = path.join(directory, "dist");
  await mkdir(dist);
  await writeFile(path.join(dist, "index.html"), "<!doctype html><title>intelifar</title>", "utf8");
  const gateway = await createRealAnalysisServer({
    distRoot: dist,
    registryRoot: path.join(directory, "registry"),
    analysisRateLimit: 1,
    config: { mineruApiKey: "m", deepseekApiKey: "d", deepseekModel: "deepseek-v4-flash" },
    mineruClient: { async parseDocument(file) { return { provider: "MinerU", model: "HTML", batchId: "b", fileName: file.name, markdown: "# ok" }; } },
    deepseekClient: { async analyzeMarkdown() { return { provider: "DeepSeek", model: "deepseek-v4-flash", usage: {}, analysis: { document: {}, assets: [], risks: [], wiki: {} } }; } },
  });
  try {
    const baseUrl = await gateway.start();
    const makeForm = () => { const form = new FormData(); form.append("file", new Blob(["<html>ok</html>"], { type: "text/html" }), "ok.html"); return form; };
    assert.equal((await fetch(`${baseUrl}/api/analysis`, { method: "POST", body: makeForm() })).status, 202);
    const limited = await fetch(`${baseUrl}/api/analysis`, { method: "POST", body: makeForm() });
    assert.equal(limited.status, 429);
    assert.equal(limited.headers.get("retry-after"), "60");
    const crossOrigin = await fetch(`${baseUrl}/api/analysis`, { method: "POST", headers: { origin: "https://attacker.invalid" }, body: makeForm() });
    assert.equal(crossOrigin.status, 403);
  } finally {
    await gateway.stop();
    await rm(directory, { recursive: true, force: true });
  }
});
