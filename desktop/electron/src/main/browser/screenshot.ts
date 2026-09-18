import { existsSync, statSync, writeFileSync } from "node:fs";
import { isAbsolute, join } from "node:path";
import { inflateSync } from "node:zlib";
import type { DocumentBinding } from "./documents.js";
import type { GuestPage } from "./guestView.js";
import { resolveRef } from "./refResolver.js";

export interface ScreenshotRequest { ref: string; fullPage: boolean; directory: string; preferCDP?: boolean; }
export interface ScreenshotResult { path: string; mime: "image/png"; width: number; height: number; reason?: string; }
export interface ScreenshotDeps { directoryExists?(path: string): boolean; writeFile?(path: string, data: Buffer): void; now?(): number; }
let sequence = 0;
const defaultDirectoryExists = (path: string) => { try { return existsSync(path) && statSync(path).isDirectory(); } catch { return false; } };

function crc32(data: Buffer): number {
  let crc = 0xffffffff;
  for (const byte of data) { crc ^= byte; for (let bit = 0; bit < 8; bit++) crc = (crc >>> 1) ^ (crc & 1 ? 0xedb88320 : 0); }
  return (crc ^ 0xffffffff) >>> 0;
}

export function validatePNG(data: Buffer): { width: number; height: number } {
  const signature = Buffer.from([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a]);
  if (data.length < 33 || !data.subarray(0, 8).equals(signature)) throw new Error("screenshot is not a PNG");
  let offset = 8, width = 0, height = 0, bitDepth = 0, colorType = -1, interlace = -1;
  const idat: Buffer[] = []; let sawIHDR = false, sawIEND = false;
  while (offset + 12 <= data.length) {
    const length = data.readUInt32BE(offset), end = offset + 12 + length;
    if (end > data.length) throw new Error("screenshot PNG is truncated");
    const type = data.toString("latin1", offset + 4, offset + 8);
    if (crc32(data.subarray(offset + 4, offset + 8 + length)) !== data.readUInt32BE(offset + 8 + length)) throw new Error(`screenshot PNG has an invalid ${type} CRC`);
    const payload = data.subarray(offset + 8, offset + 8 + length);
    if (type === "IHDR") {
      if (sawIHDR || length !== 13 || offset !== 8) throw new Error("screenshot PNG has an invalid IHDR");
      sawIHDR = true; width = payload.readUInt32BE(0); height = payload.readUInt32BE(4); bitDepth = payload[8]; colorType = payload[9]; interlace = payload[12];
      if (width <= 0 || height <= 0) throw new Error("screenshot PNG has empty dimensions");
    } else if (type === "IDAT") idat.push(payload);
    else if (type === "IEND") { if (length !== 0) throw new Error("screenshot PNG has an invalid IEND"); sawIEND = true; offset = end; break; }
    offset = end;
  }
  if (!sawIHDR || !sawIEND || idat.length === 0 || offset !== data.length) throw new Error("screenshot PNG is incomplete");
  if (interlace !== 0) throw new Error("interlaced screenshot PNG is unsupported");
  const channels = new Map([[0, 1], [2, 3], [3, 1], [4, 2], [6, 4]]).get(colorType);
  if (!channels || ![1, 2, 4, 8, 16].includes(bitDepth)) throw new Error("screenshot PNG has an unsupported color format");
  let decoded: Buffer; try { decoded = inflateSync(Buffer.concat(idat)); } catch (error) { throw new Error(`screenshot PNG cannot be decoded: ${String(error)}`); }
  const rowBytes = Math.ceil(width * channels * bitDepth / 8);
  if (decoded.length !== height * (rowBytes + 1)) throw new Error("screenshot PNG decoded length is invalid");
  for (let row = 0; row < height; row++) if (decoded[row * (rowBytes + 1)] > 4) throw new Error("screenshot PNG has an invalid row filter");
  return { width, height };
}

export function screenshotPath(directory: string, now: number): string { sequence += 1; return join(directory, `shot-${now}-${sequence}.png`); }

export async function captureScreenshot(page: GuestPage, binding: DocumentBinding | null, zoom: number, request: ScreenshotRequest, deps: ScreenshotDeps = {}): Promise<ScreenshotResult> {
  const directoryExists = deps.directoryExists ?? defaultDirectoryExists;
  if (!isAbsolute(request.directory) || !directoryExists(request.directory)) throw new Error("screenshot directory must be an existing absolute path");
  const target = screenshotPath(request.directory, (deps.now ?? Date.now)());
  const write = deps.writeFile ?? ((path, data) => writeFileSync(path, data));
  let reason: string | undefined; let rect: Electron.Rectangle | undefined;
  if (!request.fullPage && request.ref !== "") {
    if (!binding) throw new Error("element screenshots need a snapshot first");
    const resolved = await resolveRef(page, binding, request.ref, true);
    if (resolved.ok) { const { element } = resolved.value; rect = { x: Math.max(0, Math.floor(element.x * zoom)), y: Math.max(0, Math.floor(element.y * zoom)), width: Math.max(1, Math.ceil(element.width * zoom)), height: Math.max(1, Math.ceil(element.height * zoom)) }; }
    else reason = `captured the viewport instead: ${resolved.reason}`;
  }
  let png: Buffer;
  if (request.fullPage || request.preferCDP) png = await captureCDP(page, request.fullPage ? undefined : rect, request.fullPage);
  else { const image = await page.capturePage(rect); png = image.toPNG(); try { validatePNG(png); } catch { png = await captureCDP(page, rect, false); } }
  const size = validatePNG(png); write(target, png);
  return reason ? { path: target, mime: "image/png", ...size, reason } : { path: target, mime: "image/png", ...size };
}

function strictBase64(value: string): Buffer {
  const clean = value.trim();
  if (clean === "" || clean.length % 4 !== 0 || !/^[A-Za-z0-9+/]*={0,2}$/.test(clean)) throw new Error("Page.captureScreenshot returned invalid Base64");
  const data = Buffer.from(clean, "base64"); if (data.toString("base64") !== clean) throw new Error("Page.captureScreenshot returned non-canonical Base64"); return data;
}

async function captureCDP(page: GuestPage, rect: Electron.Rectangle | undefined, fullPage: boolean): Promise<Buffer> {
  const dbg = page.debugger, attached = dbg.isAttached(); if (!attached) dbg.attach("1.3");
  try { const params: Record<string, unknown> = { format: "png", captureBeyondViewport: fullPage, fromSurface: true }; if (rect) params.clip = { ...rect, scale: 1 };
    const result = (await dbg.sendCommand("Page.captureScreenshot", params)) as { data?: unknown }; if (typeof result?.data !== "string") throw new Error("Page.captureScreenshot returned no image"); return strictBase64(result.data);
  } finally { if (!attached && dbg.isAttached()) dbg.detach(); }
}
