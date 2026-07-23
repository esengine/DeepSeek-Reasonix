/**
 * Workbench Target projection helpers for Local vs Remote adapters.
 * Desktop always starts Local; Remote is opt-in via reconnect / Connect.
 */

import { app } from "./bridge";

export type WorkbenchTargetKind = "local" | "ssh";

export type WorkbenchActiveTarget = {
  state?: string;
  kind: WorkbenchTargetKind;
  hostId?: string;
  workspace?: string;
  identityGen?: number;
  requestSeq?: number;
  error?: string;
};

export type WorkbenchRemoteHint = {
  hostId?: string;
  workspace?: string;
  label?: string;
};

export type ProviderTrustPrompt = {
  hostId: string;
  host: string;
  keyType: string;
  fingerprint: string;
  workspace: string;
  providerRefs: string[];
  warning: string;
};

/** True while the backend is preparing a candidate target. The identity in
 * the event remains the currently committed target until activation. */
export function workbenchTargetTransitioning(target: WorkbenchActiveTarget): boolean {
  return target.state === "connecting";
}

/** Resolve the workspace used by one-click Remote connections. */
export function resolveRemoteWorkspace(lastWorkspace?: string, defaultWorkspace?: string): string {
  return lastWorkspace?.trim() || defaultWorkspace?.trim() || "";
}

export async function preferredRemoteWorkspace(hostId: string, defaultWorkspace?: string): Promise<string> {
  try {
    const lastWorkspace = await app.RemoteLastWorkspace(hostId);
    return resolveRemoteWorkspace(lastWorkspace, defaultWorkspace);
  } catch {
    return resolveRemoteWorkspace(undefined, defaultWorkspace);
  }
}

export async function fetchActiveTarget(): Promise<WorkbenchActiveTarget> {
  return app.WorkbenchActiveTarget();
}

export async function fetchLastRemoteHint(): Promise<WorkbenchRemoteHint | null> {
  const hint = await app.WorkbenchLastRemoteHint();
  if (!hint?.hostId) return null;
  return hint;
}

export async function switchToLocal(): Promise<WorkbenchActiveTarget> {
  return app.WorkbenchSwitchLocal();
}

export async function connectRemote(hostId: string, workspace: string): Promise<void> {
  await app.WorkbenchConnectRemote(hostId, workspace);
}

export async function disconnectRemote(): Promise<void> {
  await app.WorkbenchDisconnectRemote();
}

export async function remoteRequest(method: string, params: unknown = {}): Promise<unknown> {
  const raw = await app.WorkbenchRemoteRequest(method, JSON.stringify(params ?? {}));
  try {
    return JSON.parse(raw);
  } catch {
    return raw;
  }
}

export async function resolveProviderTrust(accept: boolean): Promise<void> {
  await app.WorkbenchResolveProviderTrust(accept);
}

export async function pendingProviderTrust(): Promise<ProviderTrustPrompt | null> {
  const p = await app.WorkbenchPendingProviderTrust();
  return p?.hostId ? p : null;
}
