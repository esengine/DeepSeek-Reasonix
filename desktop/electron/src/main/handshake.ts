import { RpcError } from "./rpc.js";

export const PROTOCOL_VERSION = 1;

export const HANDSHAKE_CODES = {
  protocol_mismatch: -32001,
  not_ready: -32002,
  contract_mismatch: -32003,
  build_mismatch: -32004,
  instance_mismatch: -32005,
} as const;

export interface HelloIdentity {
  contractDigest: string;
  version: string;
  channel: string;
  commit: string;
  hostVersion: string;
  chromeVersion: string;
  platform: string;
  arch: string;
  home: string;
  dev: boolean;
}

export interface HelloParams {
  protocolVersion: number;
  contractDigest: string;
  build: { version: string; channel: string; commit: string };
  host: { name: "electron"; version: string; chrome: string; platform: string; arch: string };
  instance: { home: string; dev: boolean };
}

export interface HelloWindow {
  width: number;
  height: number;
  minWidth: number;
  minHeight: number;
  frameless: boolean;
  zoomFactor: number;
}

export interface HelloResult {
  protocolVersion: number;
  contractDigest: string;
  service: { version: string; channel: string; commit: string; pid: number };
  runtimeGeneration: string;
  resources: { origin: string; token: string };
  window: HelloWindow;
}

export interface HandshakeFailure {
  code: number | null;
  name: string;
  title: string;
  detail: string;
}

export class HandshakeError extends Error {
  constructor(message: string) {
    super(message);
    this.name = "HandshakeError";
  }
}

export function buildHelloParams(identity: HelloIdentity): HelloParams {
  return {
    protocolVersion: PROTOCOL_VERSION,
    contractDigest: identity.contractDigest,
    build: { version: identity.version, channel: identity.channel, commit: identity.commit },
    host: {
      name: "electron",
      version: identity.hostVersion,
      chrome: identity.chromeVersion,
      platform: identity.platform,
      arch: identity.arch,
    },
    instance: { home: identity.home, dev: identity.dev },
  };
}

function record(value: unknown, path: string): Record<string, unknown> {
  if (typeof value !== "object" || value === null || Array.isArray(value)) {
    throw new HandshakeError(`hello result: ${path} must be an object`);
  }
  return value as Record<string, unknown>;
}

function str(source: Record<string, unknown>, key: string, path: string, allowEmpty = false): string {
  const value = source[key];
  if (typeof value !== "string" || (!allowEmpty && value === "")) {
    throw new HandshakeError(`hello result: ${path}.${key} must be a non-empty string`);
  }
  return value;
}

function num(source: Record<string, unknown>, key: string, path: string): number {
  const value = source[key];
  if (typeof value !== "number" || !Number.isFinite(value)) {
    throw new HandshakeError(`hello result: ${path}.${key} must be a finite number`);
  }
  return value;
}

export function validateHelloResult(value: unknown): HelloResult {
  const root = record(value, "result");
  const protocolVersion = num(root, "protocolVersion", "result");
  if (protocolVersion !== PROTOCOL_VERSION) {
    throw new HandshakeError(`hello result: protocolVersion ${protocolVersion} differs from ${PROTOCOL_VERSION}`);
  }
  const service = record(root.service, "result.service");
  const resources = record(root.resources, "result.resources");
  const window = record(root.window, "result.window");
  const geometry: HelloWindow = {
    width: num(window, "width", "result.window"),
    height: num(window, "height", "result.window"),
    minWidth: num(window, "minWidth", "result.window"),
    minHeight: num(window, "minHeight", "result.window"),
    frameless: window.frameless === true,
    zoomFactor: typeof window.zoomFactor === "number" && window.zoomFactor > 0 ? window.zoomFactor : 1,
  };
  if (geometry.width < 1 || geometry.height < 1 || geometry.minWidth < 1 || geometry.minHeight < 1) {
    throw new HandshakeError("hello result: window geometry must be positive");
  }
  return {
    protocolVersion,
    contractDigest: str(root, "contractDigest", "result"),
    service: {
      version: str(service, "version", "result.service", true),
      channel: str(service, "channel", "result.service", true),
      commit: str(service, "commit", "result.service", true),
      pid: num(service, "pid", "result.service"),
    },
    runtimeGeneration: str(root, "runtimeGeneration", "result"),
    resources: { origin: str(resources, "origin", "result.resources"), token: str(resources, "token", "result.resources") },
    window: geometry,
  };
}

const FAILURE_TITLES: Record<number, { name: string; title: string }> = {
  [HANDSHAKE_CODES.protocol_mismatch]: { name: "protocol_mismatch", title: "The desktop service speaks a different protocol version" },
  [HANDSHAKE_CODES.not_ready]: { name: "not_ready", title: "The desktop service was not ready for the handshake" },
  [HANDSHAKE_CODES.contract_mismatch]: { name: "contract_mismatch", title: "Mixed installation: the shell and the service disagree on the command contract" },
  [HANDSHAKE_CODES.build_mismatch]: { name: "build_mismatch", title: "The shell and the desktop service are different builds" },
  [HANDSHAKE_CODES.instance_mismatch]: { name: "instance_mismatch", title: "The shell and the desktop service use different data homes" },
};

export function describeHandshakeFailure(error: unknown): HandshakeFailure {
  if (error instanceof RpcError) {
    const known = FAILURE_TITLES[error.code];
    if (known) return { code: error.code, name: known.name, title: known.title, detail: error.message };
    return { code: error.code, name: "rpc_error", title: "The desktop service rejected the handshake", detail: error.message };
  }
  if (error instanceof HandshakeError) {
    return { code: null, name: "invalid_result", title: "The desktop service returned an invalid handshake", detail: error.message };
  }
  const detail = error instanceof Error ? error.message : String(error);
  return { code: null, name: "service_failure", title: "The desktop service could not be started", detail };
}
