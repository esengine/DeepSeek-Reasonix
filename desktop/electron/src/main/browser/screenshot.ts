import { existsSync, statSync, writeFileSync } from "node:fs";
import { isAbsolute, join } from "node:path";
import type { DocumentBinding } from "./documents.js";
import type { GuestPage } from "./guestView.js";
import { resolveRef } from "./refResolver.js";

export interface ScreenshotRequest {
  ref: string;
  fullPage: boolean;
  directory: string;
}

export interface ScreenshotResult {
  path: string;
  mime: "image/png";
  width: number;
  height: number;
  reason?: string;
}

export interface ScreenshotDeps {
  directoryExists?(path: string): boolean;
  writeFile?(path: string, data: Buffer): void;
  now?(): number;
}

let sequence = 0;

const defaultDirectoryExists = (path: string) => {
  try {
    return existsSync(path) && statSync(path).isDirectory();
  } catch {
    return false;
  }
};

export function pngSize(data: Buffer): { width: number; height: number } {
  if (data.length < 24 || data.toString("latin1", 12, 16) !== "IHDR") return { width: 0, height: 0 };
  return { width: data.readUInt32BE(16), height: data.readUInt32BE(20) };
}

export function screenshotPath(directory: string, now: number): string {
  sequence += 1;
  return join(directory, `shot-${now}-${sequence}.png`);
}

// Full-page captures go through the DevTools protocol because a
// WebContentsView cannot be resized past the window; everything else uses
// capturePage on the visible viewport or the element's rect.
export async function captureScreenshot(page: GuestPage, binding: DocumentBinding | null, zoom: number, request: ScreenshotRequest, deps: ScreenshotDeps = {}): Promise<ScreenshotResult> {
  const directoryExists = deps.directoryExists ?? defaultDirectoryExists;
  if (!isAbsolute(request.directory) || !directoryExists(request.directory)) throw new Error("screenshot directory must be an existing absolute path");
  const target = screenshotPath(request.directory, (deps.now ?? Date.now)());
  const write = deps.writeFile ?? ((path, data) => writeFileSync(path, data));
  let reason: string | undefined;
  let png: Buffer;
  let size: { width: number; height: number };
  if (request.fullPage) {
    png = await captureFullPage(page);
    size = pngSize(png);
  } else {
    let rect: Electron.Rectangle | undefined;
    if (request.ref !== "") {
      if (!binding) throw new Error("element screenshots need a snapshot first");
      const resolved = await resolveRef(page, binding, request.ref, true);
      if (resolved.ok) {
        const { element } = resolved.value;
        rect = {
          x: Math.max(0, Math.floor(element.x * zoom)),
          y: Math.max(0, Math.floor(element.y * zoom)),
          width: Math.max(1, Math.ceil(element.width * zoom)),
          height: Math.max(1, Math.ceil(element.height * zoom)),
        };
      } else reason = `captured the viewport instead: ${resolved.reason}`;
    }
    const image = await page.capturePage(rect);
    png = image.toPNG();
    size = image.getSize();
  }
  write(target, png);
  return reason ? { path: target, mime: "image/png", width: size.width, height: size.height, reason } : { path: target, mime: "image/png", width: size.width, height: size.height };
}

async function captureFullPage(page: GuestPage): Promise<Buffer> {
  const dbg = page.debugger;
  const attached = dbg.isAttached();
  if (!attached) dbg.attach("1.3");
  try {
    const result = (await dbg.sendCommand("Page.captureScreenshot", { format: "png", captureBeyondViewport: true, fromSurface: true })) as { data?: unknown };
    if (typeof result?.data !== "string") throw new Error("Page.captureScreenshot returned no image");
    return Buffer.from(result.data, "base64");
  } finally {
    if (!attached && dbg.isAttached()) dbg.detach();
  }
}
