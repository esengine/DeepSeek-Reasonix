// Pure-Node ZIP/JAR entry reader — STORED + DEFLATED only.

import * as fs from "node:fs";
import * as zlib from "node:zlib";

const EOCD_SIGNATURE = 0x06054b50;
const CENTRAL_DIR_SIGNATURE = 0x02014b50;
const LOCAL_FILE_SIGNATURE = 0x04034b50;

const EOCD_MIN_SIZE = 22;
const EOCD_MAX_COMMENT = 0xffff;

const COMPRESSION_STORED = 0;
const COMPRESSION_DEFLATED = 8;

interface CentralDirEntry {
  fileName: string;
  compressionMethod: number;
  compressedSize: number;
  uncompressedSize: number;
  localHeaderOffset: number;
}

function readU32LE(buf: Buffer, offset: number): number {
  return buf.readUInt32LE(offset);
}

function readU16LE(buf: Buffer, offset: number): number {
  return buf.readUInt16LE(offset);
}

function findEOCD(fd: number, fileSize: number): { offset: number; buf: Buffer } {
  // EOCD sits at end-of-file but a variable ZIP comment can push it back up to 64 KiB.
  const searchStart = Math.max(0, fileSize - EOCD_MAX_COMMENT - EOCD_MIN_SIZE);
  let chunkOffset = fileSize;
  while (chunkOffset > searchStart) {
    const readSize = Math.min(1024, chunkOffset - searchStart);
    chunkOffset -= readSize;
    const buf = Buffer.alloc(readSize);
    fs.readSync(fd, buf, 0, readSize, chunkOffset);

    for (let i = readSize - 4; i >= 0; i--) {
      if (buf.readUInt32LE(i) === EOCD_SIGNATURE) {
        const eocdOffset = chunkOffset + i;
        const eocdSize = Math.min(EOCD_MIN_SIZE + EOCD_MAX_COMMENT, fileSize - eocdOffset);
        const eocdBuf = Buffer.alloc(eocdSize);
        fs.readSync(fd, eocdBuf, 0, eocdSize, eocdOffset);
        return { offset: eocdOffset, buf: eocdBuf };
      }
    }
  }
  throw new Error("Not a valid ZIP file: EOCD signature not found");
}

function parseCentralDirectory(fd: number, eocdBuf: Buffer): CentralDirEntry[] {
  const centralDirOffset = readU32LE(eocdBuf, 16);
  const totalEntries = readU16LE(eocdBuf, 10);

  const entries: CentralDirEntry[] = [];
  let offset = centralDirOffset;

  for (let i = 0; i < totalEntries; i++) {
    const headerBuf = Buffer.alloc(46);
    fs.readSync(fd, headerBuf, 0, 46, offset);

    if (readU32LE(headerBuf, 0) !== CENTRAL_DIR_SIGNATURE) {
      throw new Error(`Corrupt central directory at offset ${offset}`);
    }

    const compressionMethod = readU16LE(headerBuf, 10);
    const compressedSize = readU32LE(headerBuf, 20);
    const uncompressedSize = readU32LE(headerBuf, 24);
    const fileNameLen = readU16LE(headerBuf, 28);
    const extraLen = readU16LE(headerBuf, 30);
    const commentLen = readU16LE(headerBuf, 32);
    const localHeaderOffset = readU32LE(headerBuf, 42);

    const nameBuf = Buffer.alloc(fileNameLen);
    fs.readSync(fd, nameBuf, 0, fileNameLen, offset + 46);
    const fileName = nameBuf.toString("utf8");

    entries.push({
      fileName,
      compressionMethod,
      compressedSize,
      uncompressedSize,
      localHeaderOffset,
    });

    offset += 46 + fileNameLen + extraLen + commentLen;
  }

  return entries;
}

export interface JarEntry {
  fileName: string;
  data: Buffer;
}

export function readJarEntry(jarPath: string, entryName: string): JarEntry | null {
  const fd = fs.openSync(jarPath, "r");
  try {
    const stat = fs.fstatSync(fd);
    const fileSize = stat.size;

    const eocd = findEOCD(fd, fileSize);
    const entries = parseCentralDirectory(fd, eocd.buf);

    const target = entries.find((e) => e.fileName === entryName);
    if (!target) return null;

    const localHeaderBuf = Buffer.alloc(30);
    fs.readSync(fd, localHeaderBuf, 0, 30, target.localHeaderOffset);

    if (readU32LE(localHeaderBuf, 0) !== LOCAL_FILE_SIGNATURE) {
      throw new Error(`Corrupt local file header at offset ${target.localHeaderOffset}`);
    }

    const localFileNameLen = readU16LE(localHeaderBuf, 26);
    const localExtraLen = readU16LE(localHeaderBuf, 28);

    const dataOffset = target.localHeaderOffset + 30 + localFileNameLen + localExtraLen;
    const compressedBuf = Buffer.alloc(target.compressedSize);
    fs.readSync(fd, compressedBuf, 0, target.compressedSize, dataOffset);

    let data: Buffer;
    if (target.compressionMethod === COMPRESSION_STORED) {
      data = compressedBuf;
    } else if (target.compressionMethod === COMPRESSION_DEFLATED) {
      data = zlib.inflateRawSync(compressedBuf);
    } else {
      throw new Error(
        `Unsupported compression method ${target.compressionMethod} for entry "${entryName}"`,
      );
    }

    return { fileName: target.fileName, data };
  } finally {
    fs.closeSync(fd);
  }
}

export function listJarEntries(jarPath: string): string[] {
  const fd = fs.openSync(jarPath, "r");
  try {
    const stat = fs.fstatSync(fd);
    const eocd = findEOCD(fd, stat.size);
    const entries = parseCentralDirectory(fd, eocd.buf);
    return entries.map((e) => e.fileName);
  } finally {
    fs.closeSync(fd);
  }
}
