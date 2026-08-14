import assert from "node:assert/strict";
import { deflateRawSync } from "node:zlib";
import test from "node:test";
import { extractFullMarkdown, inspectZipEntries } from "./zip-reader.mjs";

function makeZip(name, content, method = 8) {
  const fileName = Buffer.from(name);
  const source = Buffer.from(content);
  const compressed = method === 8 ? deflateRawSync(source) : source;
  const local = Buffer.alloc(30);
  local.writeUInt32LE(0x04034b50, 0);
  local.writeUInt16LE(20, 4);
  local.writeUInt16LE(method, 8);
  local.writeUInt32LE(compressed.length, 18);
  local.writeUInt32LE(source.length, 22);
  local.writeUInt16LE(fileName.length, 26);
  const central = Buffer.alloc(46);
  central.writeUInt32LE(0x02014b50, 0);
  central.writeUInt16LE(20, 4);
  central.writeUInt16LE(20, 6);
  central.writeUInt16LE(method, 10);
  central.writeUInt32LE(compressed.length, 20);
  central.writeUInt32LE(source.length, 24);
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

test("extracts the nested MinerU full.md entry", () => {
  const markdown = "# 真实解析\n\nMinerU 证据块";
  assert.equal(extractFullMarkdown(makeZip("result/full.md", markdown)), markdown);
});

test("rejects archives without a Markdown result", () => {
  assert.throws(() => extractFullMarkdown(makeZip("result/layout.json", "{}")), /full\.md/);
});

test("inspects ZIP metadata without extracting entry content", () => {
  assert.deepEqual(inspectZipEntries(makeZip("word/vbaProject.bin", "macro")), [{
    name: "word/vbaProject.bin",
    compressedSize: 7,
    uncompressedSize: 5,
    isDirectory: false,
  }]);
});
