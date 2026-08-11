import type { RemoteConnectionStatus } from "./types";

export type RemoteConnectionErrorKind =
  | "connection_failed"
  | "auth_failed"
  | "host_key_rejected"
  | "host_key_mismatch"
  | "degraded";

export type RemoteConnectionErrorSummaryKey = `remote.error.summary.${RemoteConnectionErrorKind}`;

export function remoteConnectionErrorKind(status?: RemoteConnectionStatus): RemoteConnectionErrorKind {
  if (status?.state === "degraded") return "degraded";
  return status?.errorDetails?.code ?? "connection_failed";
}

export function remoteConnectionErrorSummaryKey(status?: RemoteConnectionStatus): RemoteConnectionErrorSummaryKey {
  return `remote.error.summary.${remoteConnectionErrorKind(status)}`;
}

export function isRemoteHostKeyMismatch(status?: RemoteConnectionStatus): boolean {
  return remoteConnectionErrorKind(status) === "host_key_mismatch";
}

export function isRemoteTerminalFailure(status?: RemoteConnectionStatus): boolean {
  return status?.state === "stopped" && Boolean(status.error);
}

export function isRemoteDegradedWarning(status?: RemoteConnectionStatus): boolean {
  return status?.state === "degraded" && Boolean(status.error);
}
