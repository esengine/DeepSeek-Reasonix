import assert from "node:assert/strict";
import { deflateRawSync } from "node:zlib";
import test from "node:test";
import { createFileSecurityService } from "./file-security-service.mjs";

function makeZip(name, content, declaredUncompressedSize = null) {
  const fileName = Buffer.from(name);
  const source = Buffer.from(content);
  const compressed = deflateRawSync(source);
  const local = Buffer.alloc(30);
  local.writeUInt32LE(0x04034b50, 0);
  local.writeUInt16LE(20, 4);
  local.writeUInt16LE(8, 8);
  local.writeUInt32LE(compressed.length, 18);
  local.writeUInt32LE(source.length, 22);
  local.writeUInt16LE(fileName.length, 26);
  const central = Buffer.alloc(46);
  central.writeUInt32LE(0x02014b50, 0);
  central.writeUInt16LE(20, 4);
  central.writeUInt16LE(20, 6);
  central.writeUInt16LE(8, 10);
  central.writeUInt32LE(compressed.length, 20);
  central.writeUInt32LE(declaredUncompressedSize ?? source.length, 24);
  central.writeUInt16LE(fileName.length, 28);
  const directoryOffset = local.length + fileName.length + compressed.length;
  const eocd = Buffer.alloc(22);
  eocd.writeUInt32LE(0x06054b50, 0);
  eocd.writeUInt16LE(1, 8);
  eocd.writeUInt16LE(1, 10);
  eocd.writeUInt32LE(central.length + fileName.length, 12);
  eocd.writeUInt32LE(directoryOffset, 16);
  return Buffer.concat([local, fileName, compressed, central, fileName, eocd]);
}

test("allows a clean document through deterministic preflight", async () => {
  const service = createFileSecurityService();
  const result = await service.scan({ name: "clean.html", bytes: Buffer.from("<html>clean report</html>") });
  assert.equal(result.decision, "allow");
  assert.equal(result.level, "preflight");
  assert.equal(result.sha256.length, 64);
  assert.deepEqual(result.findings, []);
});

test("blocks EICAR and disguised PE content", async (t) => {
  const service = createFileSecurityService();
  const eicar = ["X5O!P%@AP", "[4\\PZX54(P^)7CC)7}$", "EICAR-STANDARD-ANTIVIRUS-TEST-FILE!$H+H*"].join("");
  await t.test("EICAR", async () => {
    const result = await service.scan({ name: "report.html", bytes: Buffer.from(`<html>${eicar}</html>`) });
    assert.equal(result.decision, "deny");
    assert.deepEqual(result.findings, ["TEST_MALWARE_SIGNATURE"]);
  });
  await t.test("PE executable", async () => {
    const result = await service.scan({ name: "report.pdf", bytes: Buffer.from("MZ disguised executable") });
    assert.equal(result.decision, "deny");
    assert.deepEqual(result.findings, ["DISGUISED_EXECUTABLE"]);
  });
});

test("blocks Office macros, active content, and ZIP bomb indicators", async (t) => {
  const service = createFileSecurityService({ maxCompressionRatio: 100 });
  await t.test("macro", async () => {
    const result = await service.scan({ name: "report.docx", bytes: makeZip("word/vbaProject.bin", "macro") });
    assert.equal(result.decision, "deny");
    assert.ok(result.findings.includes("OFFICE_MACRO"));
  });
  await t.test("active content", async () => {
    const result = await service.scan({ name: "report.docx", bytes: makeZip("word/embeddings/payload.exe", "MZ") });
    assert.equal(result.decision, "deny");
    assert.ok(result.findings.includes("ARCHIVE_ACTIVE_CONTENT"));
  });
  await t.test("compression ratio", async () => {
    const result = await service.scan({ name: "report.docx", bytes: makeZip("word/document.xml", "a", 10_000_000) });
    assert.equal(result.decision, "deny");
    assert.ok(result.findings.includes("ARCHIVE_BOMB_RISK"));
  });
});

test("uses an external scanner when configured and reports its coverage", async () => {
  let calls = 0;
  const service = createFileSecurityService({
    externalScanner: {
      name: "test-av",
      async scan() { calls += 1; return { clean: false, threat: "Test.Signature" }; },
    },
  });
  const result = await service.scan({ name: "clean.html", bytes: Buffer.from("<html>clean</html>") });
  assert.equal(calls, 1);
  assert.equal(result.decision, "deny");
  assert.equal(result.level, "external-av");
  assert.deepEqual(result.findings, ["EXTERNAL_MALWARE_DETECTED"]);
  assert.equal(service.status().lastDecision, "deny");
});

test("fails closed when external malware scanning is required but unavailable", async () => {
  const service = createFileSecurityService({ requireExternal: true });
  await assert.rejects(
    () => service.scan({ name: "clean.html", bytes: Buffer.from("<html>clean</html>") }),
    (error) => error.code === "SCANNER_UNAVAILABLE",
  );
});
