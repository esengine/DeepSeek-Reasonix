import assert from "node:assert/strict";
import { test } from "node:test";
import { deflateSync } from "node:zlib";
import { FakePage } from "./fakeGuestViews.js";
import { captureScreenshot, validatePNG } from "./screenshot.js";

function crc32(data: Buffer): number {
  let crc = 0xffffffff;
  for (const byte of data) {
    crc ^= byte;
    for (let bit = 0; bit < 8; bit++) crc = (crc >>> 1) ^ (crc & 1 ? 0xedb88320 : 0);
  }
  return (crc ^ 0xffffffff) >>> 0;
}

function chunk(type: string, payload: Buffer): Buffer {
  const name = Buffer.from(type, "ascii");
  const out = Buffer.alloc(payload.length + 12);
  out.writeUInt32BE(payload.length, 0);
  name.copy(out, 4);
  payload.copy(out, 8);
  out.writeUInt32BE(crc32(Buffer.concat([name, payload])), payload.length + 8);
  return out;
}

function png(width = 2, height = 1): Buffer {
  const ihdr = Buffer.alloc(13);
  ihdr.writeUInt32BE(width, 0);
  ihdr.writeUInt32BE(height, 4);
  ihdr[8] = 8;
  ihdr[9] = 6;
  const rows = Buffer.alloc(height * (1 + width * 4));
  for (let row = 0; row < height; row++) rows[row * (1 + width * 4)] = 0;
  return Buffer.concat([
    Buffer.from([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a]),
    chunk("IHDR", ihdr),
    chunk("IDAT", deflateSync(rows)),
    chunk("IEND", Buffer.alloc(0)),
  ]);
}

const deps = (writes: Buffer[]) => ({
  directoryExists: () => true,
  writeFile: (_path: string, data: Buffer) => writes.push(Buffer.from(data)),
  now: () => 7,
});

test("valid screenshots are decoded before the owned file is written", async () => {
  const page = new FakePage(1);
  page.image = { toPNG: () => png(2, 1), getSize: () => ({ width: 2, height: 1 }) };
  const writes: Buffer[] = [];
  const result = await captureScreenshot(page, null, 1, { ref: "", fullPage: false, directory: "/scratch" }, deps(writes));
  assert.equal(result.mime, "image/png");
  assert.deepEqual({ width: result.width, height: result.height }, { width: 2, height: 1 });
  assert.equal(writes.length, 1);
  assert.deepEqual(validatePNG(writes[0]), { width: 2, height: 1 });
});

test("an empty native capture falls back to CDP at most once", async () => {
  const page = new FakePage(2);
  page.image = { toPNG: () => Buffer.alloc(0), getSize: () => ({ width: 0, height: 0 }) };
  page.debugger.respond = method => method === "Page.captureScreenshot" ? { data: png(3, 2).toString("base64") } : {};
  const writes: Buffer[] = [];
  const result = await captureScreenshot(page, null, 1, { ref: "", fullPage: false, directory: "/scratch" }, deps(writes));
  assert.deepEqual({ width: result.width, height: result.height }, { width: 3, height: 2 });
  assert.equal(page.capturedRects.length, 1);
  assert.equal(page.debugger.commands.filter(call => call.method === "Page.captureScreenshot").length, 1);
});

test("fake PNG bytes and truncated CDP Base64 never produce a file", async () => {
  for (const data of ["AAAA", "AAA"]) {
    const page = new FakePage(3);
    page.image = { toPNG: () => Buffer.from("not-png"), getSize: () => ({ width: 1, height: 1 }) };
    page.debugger.respond = method => method === "Page.captureScreenshot" ? { data } : {};
    const writes: Buffer[] = [];
    await assert.rejects(
      captureScreenshot(page, null, 1, { ref: "", fullPage: false, directory: "/scratch" }, deps(writes)),
      /PNG|Base64/,
    );
    assert.equal(writes.length, 0);
    assert.equal(page.debugger.commands.filter(call => call.method === "Page.captureScreenshot").length, 1);
  }
});

test("hidden-tab capture uses CDP without switching through native capture", async () => {
  const page = new FakePage(4);
  page.debugger.respond = method => method === "Page.captureScreenshot" ? { data: png().toString("base64") } : {};
  const writes: Buffer[] = [];
  await captureScreenshot(page, null, 1, { ref: "", fullPage: false, directory: "/scratch", preferCDP: true }, deps(writes));
  assert.equal(page.capturedRects.length, 0);
  assert.equal(writes.length, 1);
});
