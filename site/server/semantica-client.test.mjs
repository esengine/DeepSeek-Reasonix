import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import path from "node:path";
import test from "node:test";
import { createSemanticaClient } from "./semantica-client.mjs";

const sampleAssets = [{
  id: "IP-REAL-A1",
  title: "智能知识中台",
  type: "技术方案",
  summary: "用于企业知识治理的技术方案".repeat(100),
  owner: "产品平台部",
  sensitivity: "内部",
  tags: ["知识治理", "平台"],
  document: { title: "企业知识中台白皮书", sourceName: "whitepaper.pdf", sha256: "a".repeat(64), secret: "must-not-pass" },
  evidence: Array.from({ length: 25 }, (_, index) => ({ id: `EV-A${index}`, section: `章节 ${index}`, locator: `P-${index}`, quote: "不应发送的完整证据", sha256: "b".repeat(64) })),
  wiki: { keyMechanism: "不应发送的完整 Wiki" },
}];
const runtimePaths = { pythonPath: path.resolve("test-python"), sourcePath: path.resolve("test-semantica"), bridgePath: path.resolve("test-bridge.py") };

test("Semantica supply-chain lock identifies the tested official source exactly", async () => {
  const lock = JSON.parse(await readFile(new URL("../../integrations/semantica/semantica.lock.json", import.meta.url), "utf8"));
  assert.deepEqual(
    { repository: lock.repository, tag: lock.tag, tagObject: lock.tagObject, commit: lock.commit, version: lock.version, license: lock.license },
    { repository: "https://github.com/semantica-agi/semantica", tag: "v0.6.0", tagObject: "f2361d148798b590b3c0ac094a3047913b522222", commit: "9c9ab6a23fe67f1818bd94029f975c9db51247eb", version: "0.6.0", license: "MIT" },
  );
});

test("disabled Semantica client degrades without spawning a process", async () => {
  let calls = 0;
  const client = createSemanticaClient({ enabled: false, runner: async () => { calls += 1; } });
  assert.deepEqual(await client.status(), { state: "disabled", enabled: false, engine: "Semantica", version: null, message: "未启用可选语义增强" });
  await assert.rejects(client.enrich(sampleAssets), (error) => error.code === "SEMANTICA_DISABLED");
  assert.equal(calls, 0);
});

test("client sends bounded permission-filtered asset projections and accepts a locked engine", async () => {
  const calls = [];
  const runner = async (request) => {
    calls.push(request);
    if (request.payload.action === "status") return { contractVersion: 1, status: "ready", engine: "Semantica", version: "0.6.0", capabilities: ["duplicates", "conflicts", "provenance"] };
    return {
      contractVersion: 1,
      status: "complete",
      engine: "Semantica",
      version: "0.6.0",
      checkedAssets: 1,
      duplicates: [],
      conflicts: [],
      provenance: { assets: 1, evidence: 20, entries: [{ assetId: "IP-REAL-A1", source: "whitepaper.pdf", checksum: "c".repeat(64), evidenceCount: 20 }] },
    };
  };
  const client = createSemanticaClient({ enabled: true, ...runtimePaths, runner });
  assert.equal((await client.status()).state, "ready");
  const result = await client.enrich(sampleAssets);
  assert.equal(result.checkedAssets, 1);
  assert.equal(calls.length, 2);
  const projected = calls[1].payload.assets[0];
  assert.equal(projected.summary.length, 1200);
  assert.equal(projected.evidence.length, 20);
  assert.equal(projected.document.secret, undefined);
  assert.equal(projected.evidence[0].quote, undefined);
  assert.equal(projected.wiki, undefined);
});

test("client rejects version drift and never pretends the engine ran", async () => {
  const client = createSemanticaClient({
    enabled: true,
    ...runtimePaths,
    runner: async () => ({ contractVersion: 1, status: "ready", engine: "Semantica", version: "0.7.0" }),
  });
  const status = await client.status();
  assert.equal(status.state, "unavailable");
  assert.match(status.message, /版本不符合/);
  await assert.rejects(client.enrich(sampleAssets), (error) => error.code === "SEMANTICA_UNAVAILABLE");
});

test("client rejects output that references an asset outside the authorized input", async () => {
  let step = 0;
  const client = createSemanticaClient({
    enabled: true,
    ...runtimePaths,
    runner: async () => {
      step += 1;
      if (step === 1) return { contractVersion: 1, status: "ready", engine: "Semantica", version: "0.6.0" };
      return { contractVersion: 1, status: "complete", engine: "Semantica", version: "0.6.0", checkedAssets: 1, duplicates: [{ assetIds: ["IP-REAL-A1", "IP-SECRET-X"] }], conflicts: [], provenance: { assets: 1, evidence: 0, entries: [] } };
    },
  });
  await assert.rejects(client.enrich(sampleAssets), (error) => error.code === "SEMANTICA_INVALID_OUTPUT");
});

test("client rejects relative or path-list runtime configuration before execution", async () => {
  let calls = 0;
  const client = createSemanticaClient({ enabled: true, pythonPath: "python", sourcePath: `first${path.delimiter}second`, bridgePath: "bridge.py", runner: async () => { calls += 1; } });
  const status = await client.status();
  assert.equal(status.state, "unavailable");
  assert.match(status.message, /绝对路径/);
  assert.equal(calls, 0);
});
