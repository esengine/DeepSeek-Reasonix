import { inflateRawSync } from "node:zlib";

const EOCD_SIGNATURE = 0x06054b50;
const CENTRAL_SIGNATURE = 0x02014b50;
const LOCAL_SIGNATURE = 0x04034b50;

function findEndOfCentralDirectory(buffer) {
  const floor = Math.max(0, buffer.length - 65_557);
  for (let offset = buffer.length - 22; offset >= floor; offset -= 1) {
    if (buffer.readUInt32LE(offset) === EOCD_SIGNATURE) return offset;
  }
  throw new Error("MinerU archive is not a valid ZIP file");
}

export function inspectZipEntries(input, options = {}) {
  const buffer = Buffer.isBuffer(input) ? input : Buffer.from(input);
  const maxEntries = Math.max(1, Number(options.maxEntries ?? 10_000));
  const eocd = findEndOfCentralDirectory(buffer);
  const entryCount = buffer.readUInt16LE(eocd + 10);
  if (entryCount > maxEntries) {
    const error = new Error("ZIP archive contains too many entries");
    error.code = "ZIP_ENTRY_LIMIT";
    throw error;
  }
  let offset = buffer.readUInt32LE(eocd + 16);
  const entries = [];
  for (let index = 0; index < entryCount; index += 1) {
    if (offset + 46 > eocd || buffer.readUInt32LE(offset) !== CENTRAL_SIGNATURE) {
      const error = new Error("ZIP central directory is malformed");
      error.code = "ZIP_MALFORMED";
      throw error;
    }
    const compressedSize = buffer.readUInt32LE(offset + 20);
    const uncompressedSize = buffer.readUInt32LE(offset + 24);
    const fileNameLength = buffer.readUInt16LE(offset + 28);
    const extraLength = buffer.readUInt16LE(offset + 30);
    const commentLength = buffer.readUInt16LE(offset + 32);
    const nextOffset = offset + 46 + fileNameLength + extraLength + commentLength;
    if (nextOffset > eocd) {
      const error = new Error("ZIP central directory entry exceeds archive bounds");
      error.code = "ZIP_MALFORMED";
      throw error;
    }
    const name = buffer.subarray(offset + 46, offset + 46 + fileNameLength).toString("utf8").replaceAll("\\", "/");
    entries.push({ name, compressedSize, uncompressedSize, isDirectory: name.endsWith("/") });
    offset = nextOffset;
  }
  return entries;
}

export function extractFullMarkdown(input, maxBytes = 12 * 1024 * 1024) {
  const buffer = Buffer.isBuffer(input) ? input : Buffer.from(input);
  const eocd = findEndOfCentralDirectory(buffer);
  const entryCount = buffer.readUInt16LE(eocd + 10);
  let offset = buffer.readUInt32LE(eocd + 16);
  for (let index = 0; index < entryCount; index += 1) {
    if (buffer.readUInt32LE(offset) !== CENTRAL_SIGNATURE) throw new Error("MinerU ZIP directory is malformed");
    const method = buffer.readUInt16LE(offset + 10);
    const compressedSize = buffer.readUInt32LE(offset + 20);
    const uncompressedSize = buffer.readUInt32LE(offset + 24);
    const fileNameLength = buffer.readUInt16LE(offset + 28);
    const extraLength = buffer.readUInt16LE(offset + 30);
    const commentLength = buffer.readUInt16LE(offset + 32);
    const localOffset = buffer.readUInt32LE(offset + 42);
    const name = buffer.subarray(offset + 46, offset + 46 + fileNameLength).toString("utf8").replaceAll("\\", "/");
    if (name === "full.md" || name.endsWith("/full.md")) {
      if (uncompressedSize > maxBytes) throw new Error("MinerU Markdown exceeds the configured size limit");
      if (buffer.readUInt32LE(localOffset) !== LOCAL_SIGNATURE) throw new Error("MinerU ZIP entry is malformed");
      const localNameLength = buffer.readUInt16LE(localOffset + 26);
      const localExtraLength = buffer.readUInt16LE(localOffset + 28);
      const dataOffset = localOffset + 30 + localNameLength + localExtraLength;
      const compressed = buffer.subarray(dataOffset, dataOffset + compressedSize);
      const output = method === 0 ? compressed : method === 8 ? inflateRawSync(compressed) : null;
      if (!output) throw new Error(`Unsupported MinerU ZIP compression method: ${method}`);
      if (output.length > maxBytes) throw new Error("MinerU Markdown exceeds the configured size limit");
      return output.toString("utf8");
    }
    offset += 46 + fileNameLength + extraLength + commentLength;
  }
  throw new Error("MinerU archive does not contain full.md");
}
