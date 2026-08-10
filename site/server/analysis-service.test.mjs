import assert from "node:assert/strict";
import { mkdtemp, mkdir, rm, writeFile } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import test from "node:test";
import { createAnalysisService } from "./analysis-service.mjs";
import { createPlatformStore } from "./platform-store.mjs";

test("orchestrates MinerU before DeepSeek and exposes sanitized provider evidence", async () => {
  const order = [];
  const service = createAnalysisService({
    mineruClient: { async parseDocument(file, hooks) { order.push("mineru"); hooks.onProgress({ state: "running", progress: 44, batchId: "batch-live" }); return { provider: "MinerU", model: "MinerU-HTML", batchId: "batch-live", traceId: "trace-live", fileName: file.name, markdown: "# 真实解析\n可溯源 IP Wiki" }; } },
    deepseekClient: { async analyzeMarkdown() { order.push("deepseek"); return { provider: "DeepSeek", model: "deepseek-v4-flash", responseId: "chat-live", usage: { totalTokens: 88 }, analysis: { document: { title: "真实报告" }, assets: [{ title: "IP Wiki" }], risks: [], wiki: { executive_summary: "摘要" } } }; } },
  });
  const submitted = await service.submit({ name: "report.html", bytes: Buffer.from("<html></html>") }, { expectedCategory: "技术报告" });
  const complete = await service.whenSettled(submitted.id);
  assert.deepEqual(order, ["mineru", "deepseek"]);
  assert.equal(complete.state, "complete");
  assert.equal(complete.result.parser.batchId, "batch-live");
  assert.equal(complete.result.llm.model, "deepseek-v4-flash");
  assert.equal(complete.result.parser.markdownSha256.length, 64);
});

test("sanitizes unexpected provider errors", async () => {
  const service = createAnalysisService({
    mineruClient: { async parseDocument() { throw new Error("Bearer private-token https://signed.invalid/path"); } },
    deepseekClient: { async analyzeMarkdown() { throw new Error("unused"); } },
  });
  const submitted = await service.submit({ name: "report.html", bytes: Buffer.from("<html></html>") });
  const failed = await service.whenSettled(submitted.id);
  assert.equal(failed.state, "failed");
  assert.doesNotMatch(failed.error, /private-token|signed\.invalid/);
});

test("blocks unsafe files before either external analysis provider is called", async () => {
  const calls = [];
  const service = createAnalysisService({
    fileSecurityService: { async scan() { return { decision: "deny", level: "preflight", engine: "test", findings: ["TEST_MALWARE_SIGNATURE"], sha256: "a".repeat(64), scannedAt: new Date().toISOString() }; } },
    mineruClient: { async parseDocument() { calls.push("mineru"); } },
    deepseekClient: { async analyzeMarkdown() { calls.push("deepseek"); } },
  });
  const submitted = await service.submit({ name: "unsafe.html", bytes: Buffer.from("unsafe") });
  const blocked = await service.whenSettled(submitted.id);
  assert.equal(blocked.state, "blocked");
  assert.equal(blocked.retryable, false);
  assert.equal(blocked.security.decision, "deny");
  assert.deepEqual(calls, []);
});

test("keeps a quarantined file retryable when required scanning is unavailable", async () => {
  const directory = await mkdtemp(path.join(os.tmpdir(), "intelifar-scanner-unavailable-"));
  const store = createPlatformStore({ dbPath: path.join(directory, "platform.sqlite") });
  store.ensureWorkspace({ id: "WS-A", name: "甲公司" });
  const service = createAnalysisService({
    jobStore: store,
    uploadRoot: path.join(directory, "uploads"),
    defaultWorkspaceId: "WS-A",
    fileSecurityService: { async scan() { const error = new Error("scanner unavailable"); error.code = "SCANNER_UNAVAILABLE"; throw error; } },
    mineruClient: { async parseDocument() { throw new Error("must not run"); } },
    deepseekClient: { async analyzeMarkdown() { throw new Error("must not run"); } },
  });
  try {
    const submitted = await service.submit({ name: "clean.html", bytes: Buffer.from("<html>clean</html>") }, { workspaceId: "WS-A" });
    const failed = await service.whenSettled(submitted.id, "WS-A");
    assert.equal(failed.state, "failed");
    assert.equal(failed.retryable, true);
    assert.match(failed.stageLabel, /扫描器/);
  } finally {
    store.close();
    await rm(directory, { recursive: true, force: true });
  }
});

test("recovers a persisted interrupted job and retries from the retained upload", async () => {
  const directory = await mkdtemp(path.join(os.tmpdir(), "intelifar-durable-job-"));
  const uploadRoot = path.join(directory, "uploads");
  await mkdir(uploadRoot);
  const uploadPath = path.join(uploadRoot, "JOB-REAL-recover.upload");
  await writeFile(uploadPath, "<html>recover</html>", { mode: 0o600 });
  const store = createPlatformStore({ dbPath: path.join(directory, "platform.sqlite") });
  store.ensureWorkspace({ id: "WS-A", name: "甲公司" });
  store.saveJob("WS-A", {
    id: "JOB-REAL-recover",
    state: "deepseek",
    progress: 68,
    stageLabel: "处理中",
    createdAt: "2026-08-10T08:00:00.000Z",
    updatedAt: "2026-08-10T08:01:00.000Z",
    document: { name: "recover.html", size: 20, expectedCategory: "技术报告" },
  }, { uploadPath });

  const service = createAnalysisService({
    jobStore: store,
    uploadRoot,
    mineruClient: { async parseDocument(file) { return { provider: "MinerU", model: "MinerU-HTML", batchId: "batch-retry", traceId: "trace-retry", fileName: file.name, markdown: "# recovered" }; } },
    deepseekClient: { async analyzeMarkdown() { return { provider: "DeepSeek", model: "deepseek-chat", responseId: "chat-retry", usage: {}, analysis: { document: { title: "Recovered" }, assets: [], risks: [], wiki: {} } }; } },
  });
  try {
    assert.equal(service.get("JOB-REAL-recover", "WS-A").state, "interrupted");
    const retried = await service.retry("JOB-REAL-recover", "WS-A");
    const complete = await service.whenSettled(retried.id, "WS-A");
    assert.equal(complete.state, "complete");
    assert.equal(complete.result.parser.batchId, "batch-retry");
  } finally {
    store.close();
    await rm(directory, { recursive: true, force: true });
  }
});
