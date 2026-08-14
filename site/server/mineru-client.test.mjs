import assert from "node:assert/strict";
import { deflateRawSync } from "node:zlib";
import test from "node:test";
import { createMineruClient, validateDocumentUpload } from "./mineru-client.mjs";

const json = (value, status = 200) => new Response(JSON.stringify(value), { status, headers: { "content-type": "application/json" } });

function zipMarkdown(markdown) {
  const name = Buffer.from("task/full.md");
  const source = Buffer.from(markdown);
  const compressed = deflateRawSync(source);
  const local = Buffer.alloc(30);
  local.writeUInt32LE(0x04034b50, 0); local.writeUInt16LE(20, 4); local.writeUInt16LE(8, 8);
  local.writeUInt32LE(compressed.length, 18); local.writeUInt32LE(source.length, 22); local.writeUInt16LE(name.length, 26);
  const central = Buffer.alloc(46);
  central.writeUInt32LE(0x02014b50, 0); central.writeUInt16LE(20, 4); central.writeUInt16LE(20, 6); central.writeUInt16LE(8, 10);
  central.writeUInt32LE(compressed.length, 20); central.writeUInt32LE(source.length, 24); central.writeUInt16LE(name.length, 28);
  const directoryOffset = local.length + name.length + compressed.length;
  const eocd = Buffer.alloc(22);
  eocd.writeUInt32LE(0x06054b50, 0); eocd.writeUInt16LE(1, 8); eocd.writeUInt16LE(1, 10);
  eocd.writeUInt32LE(central.length + name.length, 12); eocd.writeUInt32LE(directoryOffset, 16);
  return Buffer.concat([local, name, compressed, central, name, eocd]);
}

test("runs the documented MinerU upload, poll and Markdown download flow", async () => {
  const calls = [];
  const responses = [
    json({ code: 0, trace_id: "trace-real", data: { batch_id: "batch-real", file_urls: ["https://mineru.oss-cn-shanghai.aliyuncs.com/upload"] } }),
    new Response(null, { status: 200 }),
    json({ code: 0, data: { extract_result: [{ state: "running", extract_progress: { extracted_pages: 1, total_pages: 2 } }] } }),
    json({ code: 0, data: { extract_result: [{ state: "done", file_name: "report.html", full_zip_url: "https://cdn-mineru.openxlab.org.cn/result.zip" }] } }),
    new Response(zipMarkdown("# MinerU 真实结果"), { status: 200 }),
  ];
  const client = createMineruClient({
    apiKey: "secret-token",
    fetchImpl: async (url, options) => { calls.push({ url, options }); return responses.shift(); },
    sleep: async () => {},
  });
  const result = await client.parseDocument({ name: "report.html", bytes: Buffer.from("<!doctype html><html><body>IP</body></html>") });
  assert.equal(result.batchId, "batch-real");
  assert.equal(result.markdown, "# MinerU 真实结果");
  assert.equal(JSON.parse(calls[0].options.body).model_version, "MinerU-HTML");
  assert.equal(calls[1].options.method, "PUT");
  assert.equal(calls[0].options.headers.authorization, "Bearer secret-token");
});

test("rejects an HTML extension whose content signature is not HTML", () => {
  assert.throws(() => validateDocumentUpload({ name: "fake.html", bytes: Buffer.from("not html") }), /signature/);
});

test("does not expose signed URLs or credentials when upload fails", async () => {
  const client = createMineruClient({
    apiKey: "secret-token",
    fetchImpl: async (url) => url.includes("file-urls")
      ? json({ code: 0, data: { batch_id: "batch", file_urls: ["https://mineru.oss-cn-shanghai.aliyuncs.com/private-secret"] } })
      : new Response(null, { status: 403 }),
  });
  await assert.rejects(
    client.parseDocument({ name: "report.html", bytes: Buffer.from("<html></html>") }),
    (error) => !error.message.includes("secret-token") && !error.message.includes("aliyuncs.com"),
  );
});

test("rejects provider-controlled URLs outside MinerU and its storage domains", async () => {
  const client = createMineruClient({
    apiKey: "secret-token",
    fetchImpl: async () => json({ code: 0, data: { batch_id: "batch", file_urls: ["https://127.0.0.1/internal"] } }),
  });
  await assert.rejects(client.parseDocument({ name: "report.html", bytes: Buffer.from("<html></html>") }), /trusted provider domains/);
});
