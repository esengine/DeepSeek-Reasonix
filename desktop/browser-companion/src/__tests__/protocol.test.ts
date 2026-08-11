// Wire protocol tests: byte-identical framing with the Go host, envelope
// validation, and the schema hash pin. Run with `pnpm test` (node --test).

import { test } from "node:test";
import * as assert from "node:assert/strict";
import * as fs from "node:fs";
import * as path from "node:path";
import * as crypto from "node:crypto";
import {
  FrameReader,
  FrameTooLargeError,
  ZeroFrameError,
  PROTOCOL_VERSION,
  FRAME_MAX_BYTES,
  responseError,
  responseOk,
  validateRequest,
  writeFrame,
} from "../protocol";
import {
  BROWSER_SCHEMA_HASH,
  BROWSER_METHODS,
  BROWSER_PROTOCOL_VERSION,
} from "../generated/browserProtocol.generated";

test("frame wire bytes match the Go host format (big-endian length prefix)", () => {
  const frame = writeFrame("{}");
  assert.deepEqual([...frame.subarray(0, 4)], [0, 0, 0, 2]);
  assert.equal(frame.toString("utf8", 4), "{}");
});

test("frame round trip through the reader", () => {
  const reader = new FrameReader();
  const frames = reader.feed(writeFrame(JSON.stringify({ a: 1 })));
  assert.equal(frames.length, 1);
  assert.deepEqual(JSON.parse(frames[0]!.toString("utf8")), { a: 1 });
});

test("multiple frames in one chunk are split", () => {
  const reader = new FrameReader();
  const a = writeFrame("{\"n\":1}");
  const b = writeFrame("{\"n\":2}");
  const frames = reader.feed(Buffer.concat([a, b]));
  assert.equal(frames.length, 2);
  assert.deepEqual(JSON.parse(frames[1]!.toString("utf8")), { n: 2 });
});

test("fragmented frames accumulate across chunks", () => {
  const reader = new FrameReader();
  const whole = writeFrame("{\"n\":42}");
  const first = reader.feed(whole.subarray(0, 3));
  assert.equal(first.length, 0);
  const rest = reader.feed(whole.subarray(3));
  assert.equal(rest.length, 1);
  assert.deepEqual(JSON.parse(rest[0]!.toString("utf8")), { n: 42 });
});

test("oversized frame poisons the reader", () => {
  const reader = new FrameReader();
  const header = Buffer.alloc(4);
  header.writeUInt32BE(FRAME_MAX_BYTES + 1, 0);
  assert.throws(() => reader.feed(header), FrameTooLargeError);
  assert.throws(() => reader.feed(Buffer.alloc(0)), /poisoned/);
});

test("zero-length frame is rejected", () => {
  const reader = new FrameReader();
  assert.throws(() => reader.feed(Buffer.from([0, 0, 0, 0])), ZeroFrameError);
});

test("writeFrame rejects oversized payloads", () => {
  assert.throws(() => writeFrame(Buffer.alloc(FRAME_MAX_BYTES + 1)), FrameTooLargeError);
});

test("validateRequest accepts a canonical request", () => {
  const req = validateRequest({
    protocolVersion: PROTOCOL_VERSION,
    requestId: "r-1",
    ownerId: "chat-1",
    method: "tab.open",
    params: { ownerId: "chat-1", url: "https://example.com", disposition: "foreground" },
  });
  assert.equal(req.method, "tab.open");
});

test("validateRequest rejects protocol mismatches and unknown methods", () => {
  assert.throws(
    () => validateRequest({ protocolVersion: 99, requestId: "r", method: "hello", params: {} }),
    /protocol version/,
  );
  assert.throws(
    () => validateRequest({ protocolVersion: PROTOCOL_VERSION, requestId: "r", method: "tab.explode", params: {} }),
    /unknown method/,
  );
  assert.throws(
    () => validateRequest({ protocolVersion: PROTOCOL_VERSION, requestId: "", method: "hello", params: {} }),
    /requestId/,
  );
  assert.throws(
    () => validateRequest({ protocolVersion: PROTOCOL_VERSION, requestId: "r", method: "hello", params: [] }),
    /params/,
  );
});

test("response envelope builders produce valid shapes", () => {
  const ok = responseOk("r-1", { tabs: [] });
  assert.equal(ok.protocolVersion, PROTOCOL_VERSION);
  assert.equal(ok.requestId, "r-1");
  assert.ok(ok.error === undefined);

  const err = responseError("r-2", "tab_busy", "busy");
  assert.equal(err.error?.code, "tab_busy");
  assert.ok(err.result === undefined);
});

test("generated protocol is pinned to the canonical schema document", () => {
  const schemaPath = path.join(__dirname, "..", "..", "..", "internal", "browseripc", "schema.json");
  const bytes = fs.readFileSync(schemaPath);
  const hash = "sha256:" + crypto.createHash("sha256").update(bytes).digest("hex");
  assert.equal(BROWSER_SCHEMA_HASH, hash, "generated TS is stale; run cmd/browser-ipc-gen");
  assert.equal(BROWSER_PROTOCOL_VERSION, 1);
  // The companion must know every canonical method for dispatch.
  assert.ok(BROWSER_METHODS.length >= 19);
});
