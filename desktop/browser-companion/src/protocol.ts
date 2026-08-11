// Wire protocol between the Reasonix desktop host and this companion.
// Frames are length-prefixed JSON over stdin/stdout, byte-identical to the Go
// host implementation (desktop/internal/browseripc): 4-byte big-endian length,
// then the JSON payload. Every constant mirrors the canonical schema document
// (desktop/internal/browseripc/schema.json); the generated types carry the
// schema hash and the companion test suite pins both sides together.

import {
  BROWSER_LIMITS,
  BROWSER_PROTOCOL_VERSION,
  type BrowserErrorCode,
} from "./generated/browserProtocol.generated";

export const PROTOCOL_VERSION = BROWSER_PROTOCOL_VERSION;
export const FRAME_MAX_BYTES = BROWSER_LIMITS.frameBytes;

export class FrameTooLargeError extends Error {
  constructor(length: number) {
    super(`frame exceeds size limit: ${length} > ${FRAME_MAX_BYTES}`);
    this.name = "FrameTooLargeError";
  }
}

export class ZeroFrameError extends Error {
  constructor() {
    super("zero-length frame");
    this.name = "ZeroFrameError";
  }
}

export class ProtocolError extends Error {
  readonly code: BrowserErrorCode;
  constructor(code: BrowserErrorCode, message: string) {
    super(message);
    this.name = "ProtocolError";
    this.code = code;
  }
}

// Envelope shapes (canonical):
//   request  {protocolVersion, requestId, ownerId?, method, params}
//   response {protocolVersion, requestId, result? | error?}
//   event    {protocolVersion, event: {name, ownerId?, data}}
export interface BrowserRequest {
  protocolVersion: number;
  requestId: string;
  ownerId?: string;
  method: string;
  params: unknown;
}

export interface BrowserResponse {
  protocolVersion: number;
  requestId: string;
  result?: unknown;
  error?: { code: BrowserErrorCode; message: string };
}

export interface BrowserEvent {
  protocolVersion: number;
  event: { name: string; ownerId?: string; data: unknown };
}

/** Serializes a frame: 4-byte big-endian length prefix + JSON payload. */
export function writeFrame(payload: string | Buffer): Buffer {
  const body = Buffer.isBuffer(payload) ? payload : Buffer.from(payload, "utf8");
  if (body.length > FRAME_MAX_BYTES) {
    throw new FrameTooLargeError(body.length);
  }
  const header = Buffer.alloc(4);
  header.writeUInt32BE(body.length, 0);
  return Buffer.concat([header, body]);
}

/**
 * A byte-stream frame reader: feeds raw stdin chunks in, yields one complete
 * frame payload at a time. Mirrors the Go ReadFrame semantics: an announced
 * length above the limit is a protocol violation (throws FrameTooLargeError
 * and poisons the reader — the caller must treat the peer as hostile and
 * exit), and a zero length is invalid.
 */
export class FrameReader {
  private buffer: Buffer = Buffer.alloc(0);
  private poisoned = false;

  /** Feed raw bytes; returns complete frame payloads in order. */
  feed(chunk: Buffer): Buffer[] {
    if (this.poisoned) {
      throw new ProtocolError("internal", "frame reader is poisoned");
    }
    this.buffer = this.buffer.length === 0 ? chunk : Buffer.concat([this.buffer, chunk]);
    const frames: Buffer[] = [];
    for (;;) {
      if (this.buffer.length < 4) return frames;
      const length = this.buffer.readUInt32BE(0);
      if (length === 0) {
        this.poisoned = true;
        throw new ZeroFrameError();
      }
      if (length > FRAME_MAX_BYTES) {
        this.poisoned = true;
        throw new FrameTooLargeError(length);
      }
      if (this.buffer.length < 4 + length) return frames;
      frames.push(this.buffer.subarray(4, 4 + length));
      this.buffer = this.buffer.subarray(4 + length);
    }
  }
}

const REQUEST_ID_MAX = BROWSER_LIMITS.maxRequestIdBytes;
const OWNER_ID_MAX = BROWSER_LIMITS.maxOwnerIdBytes;
const METHOD_MAX = BROWSER_LIMITS.maxMethodBytes;

const knownMethods = new Set([
  "hello",
  "request.cancel",
  "window.open",
  "window.focus",
  "window.close",
  "owner.activate",
  "owner.remove",
  "tab.open",
  "tab.list",
  "tab.activate",
  "tab.close",
  "tab.navigate",
  "tab.snapshot",
  "tab.screenshot",
  "tab.wait",
  "tab.act",
  "data.clear",
  "permissions.list",
  "permissions.revoke",
]);

/**
 * Validates a decoded request envelope. This is the companion's fail-closed
 * gate: unknown methods, wrong protocol versions, oversized tokens, and
 * non-object params are rejected before dispatch.
 */
export function validateRequest(raw: unknown): BrowserRequest {
  if (typeof raw !== "object" || raw === null) {
    throw new ProtocolError("invalid_params", "request is not an object");
  }
  const req = raw as Record<string, unknown>;
  if (req.protocolVersion !== PROTOCOL_VERSION) {
    throw new ProtocolError("protocol_mismatch", `protocol version ${String(req.protocolVersion)} != ${PROTOCOL_VERSION}`);
  }
  const requestId = typeof req.requestId === "string" ? req.requestId : "";
  if (requestId.length === 0 || requestId.length > REQUEST_ID_MAX) {
    throw new ProtocolError("invalid_params", "invalid requestId");
  }
  const ownerId = typeof req.ownerId === "string" ? req.ownerId : "";
  if (ownerId.length > OWNER_ID_MAX) {
    throw new ProtocolError("invalid_params", "ownerId too long");
  }
  const method = typeof req.method === "string" ? req.method : "";
  if (method.length === 0 || method.length > METHOD_MAX) {
    throw new ProtocolError("invalid_params", "invalid method");
  }
  if (!knownMethods.has(method)) {
    throw new ProtocolError("unknown_method", `unknown method ${JSON.stringify(method)}`);
  }
  if (typeof req.params !== "object" || req.params === null || Array.isArray(req.params)) {
    throw new ProtocolError("invalid_params", "params must be an object");
  }
  return {
    protocolVersion: PROTOCOL_VERSION,
    requestId,
    ownerId,
    method,
    params: req.params as Record<string, unknown>,
  };
}

/** Validates the URL contract before any navigation: http(s) only. */
export function assertHttpUrl(raw: unknown): string {
  if (typeof raw !== "string" || raw.length === 0 || raw.length > BROWSER_LIMITS.maxUrlBytes) {
    throw new ProtocolError("invalid_params", "invalid url");
  }
  if (!/^https?:\/\//i.test(raw)) {
    throw new ProtocolError("invalid_params", "url must be http(s)");
  }
  return raw;
}

export function responseOk(requestId: string, result: unknown): BrowserResponse {
  return { protocolVersion: PROTOCOL_VERSION, requestId, result };
}

export function responseError(requestId: string, code: BrowserErrorCode, message: string): BrowserResponse {
  return { protocolVersion: PROTOCOL_VERSION, requestId, error: { code, message } };
}
