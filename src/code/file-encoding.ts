/** Read/write preserves the file's original encoding so edit_file on GB18030 (CN Windows) or UTF-8-BOM files doesn't silently convert or fail SEARCH on mangled decode (issue #1445). */

import { promises as fsp, readFileSync, writeFileSync } from "node:fs";
import iconv from "iconv-lite";

export type FileEncoding = "utf8" | "utf8-bom" | "gb18030";

export interface DecodedFile {
  text: string;
  encoding: FileEncoding;
}

const UTF8_BOM = Buffer.from([0xef, 0xbb, 0xbf]);

export function decodeFileBuffer(buf: Buffer): DecodedFile {
  if (buf.length >= 3 && buf[0] === 0xef && buf[1] === 0xbb && buf[2] === 0xbf) {
    return { text: buf.subarray(3).toString("utf8"), encoding: "utf8-bom" };
  }
  try {
    return { text: new TextDecoder("utf-8", { fatal: true }).decode(buf), encoding: "utf8" };
  } catch {
    // Fall through.
  }
  // GB18030 isn't platform-conditional — files originated on CN Windows can travel anywhere.
  try {
    return {
      text: new TextDecoder("gb18030", { fatal: true }).decode(buf),
      encoding: "gb18030",
    };
  } catch {
    // Fall through.
  }
  return { text: buf.toString("utf8"), encoding: "utf8" };
}

export function encodeFile(text: string, encoding: FileEncoding): Buffer {
  if (encoding === "utf8") return Buffer.from(text, "utf8");
  if (encoding === "utf8-bom") {
    return Buffer.concat([UTF8_BOM, Buffer.from(text, "utf8")]);
  }
  return iconv.encode(text, "gb18030");
}

export function readTextFileSmartSync(path: string): DecodedFile {
  return decodeFileBuffer(readFileSync(path));
}

export async function readTextFileSmart(path: string): Promise<DecodedFile> {
  return decodeFileBuffer(await fsp.readFile(path));
}

export function writeTextFileSmartSync(path: string, text: string, encoding: FileEncoding): void {
  writeFileSync(path, encodeFile(text, encoding));
}

export async function writeTextFileSmart(
  path: string,
  text: string,
  encoding: FileEncoding,
): Promise<void> {
  await fsp.writeFile(path, encodeFile(text, encoding));
}
