/**
 * Mobile envelope + session contracts.
 * Keep field names aligned with internal/mobileprotocol (Go).
 */

export const MOBILE_PROTOCOL_VERSION = 1 as const;

export type EnvelopeType =
  | "hello"
  | "command"
  | "event"
  | "ack"
  | "snapshot"
  | "error"
  | "ping"
  | "pong";

export type SessionRuntime = "local" | "remote";

export interface MobileEnvelope<T = unknown> {
  version: number;
  type: EnvelopeType | string;
  requestId?: string;
  sessionId?: string;
  seq?: number;
  ack?: number;
  payload?: T;
}

export interface SessionDescriptor {
  id: string;
  runtime: SessionRuntime;
  nodeId?: string;
  providerRef?: string;
  capabilities: string[];
  revision: number;
  lastEventSeq: number;
  title?: string;
  status?: "idle" | "running" | "pending_approval" | "failed" | string;
  updatedAt?: string;
}

export const LOCAL_CAPABILITIES = [
  "web_read",
  "attachment_read",
  "image_input",
  "http_mcp",
] as const;

export const REMOTE_CAPABILITIES = [
  "shell",
  "git",
  "filesystem",
  "web_read",
  "attachment_read",
  "image_input",
  "http_mcp",
  "stdio_mcp",
  "background_jobs",
  "approval",
] as const;

export type CommandName =
  | "create_session"
  | "restore_session"
  | "submit"
  | "cancel"
  | "answer"
  | "approve"
  | "snapshot"
  | "list_models"
  | "probe_provider"
  | "subscribe"
  | "hello";

export interface CommandPayload<T = unknown> {
  name: CommandName | string;
  args?: T;
}

export interface CreateSessionArgs {
  runtime: SessionRuntime;
  providerRef?: string;
  title?: string;
  modelRef?: string;
}

export interface SubmitArgs {
  text: string;
  display?: string;
  images?: string[];
}

export interface ApproveArgs {
  id: string;
  allow: boolean;
  session?: boolean;
  persist?: boolean;
}

export interface SnapshotPayload {
  descriptor: SessionDescriptor;
  history?: unknown;
  partialTurn?: unknown;
  todos?: unknown;
  running?: boolean;
  pendingApproval?: unknown;
  lastEventSeq: number;
  revision: number;
}

export interface ErrorPayload {
  code?: string;
  message: string;
  retry?: boolean;
}

export function newEnvelope<T>(
  type: EnvelopeType | string,
  partial: Partial<MobileEnvelope<T>> = {},
): MobileEnvelope<T> {
  return {
    version: MOBILE_PROTOCOL_VERSION,
    type,
    ...partial,
  };
}

export function isWriteCommand(name: string): boolean {
  switch (name) {
    case "create_session":
    case "restore_session":
    case "submit":
    case "cancel":
    case "approve":
    case "answer":
      return true;
    default:
      return false;
  }
}
