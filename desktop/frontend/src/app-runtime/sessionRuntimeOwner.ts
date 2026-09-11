import type { SessionOperationAuthority, SessionResource } from "./useResourceOperations";

export async function executeCancelRuntimeJob(
  target: SessionResource,
  jobId: string,
  ports: { cancelForTab: (tabId: string, jobId: string) => Promise<boolean>; refresh: () => Promise<void> },
  authority: SessionOperationAuthority,
): Promise<boolean> {
  authority.checkpoint();
  const cancelled = await ports.cancelForTab(target.tabId, jobId);
  authority.checkpoint();
  if (authority.ownsUI()) await ports.refresh();
  authority.checkpoint();
  return cancelled;
}

export async function executeTerminalOutputInsertion(
  target: SessionResource,
  sessionId: string,
  ports: { read: (tabId: string, sessionId: string) => Promise<string>; apply: (text: string) => void },
  format: (output: string) => string,
  authority: SessionOperationAuthority,
): Promise<boolean> {
  authority.checkpoint();
  const text = format(await ports.read(target.tabId, sessionId));
  authority.checkpoint();
  if (!text) return false;
  if (authority.ownsUI()) ports.apply(text);
  return true;
}

export async function executeTodoDismissal(
  target: SessionResource,
  batchKey: string,
  port: (tabId: string, batchKey: string) => Promise<void>,
  authority: SessionOperationAuthority,
): Promise<void> {
  authority.checkpoint();
  await port(target.tabId, batchKey);
  authority.checkpoint();
}

export type ClearSessionPorts = {
  clearSession: () => Promise<void>;
  clearRemoteSession: (tabId: string) => Promise<void>;
  retryRemoteHydration: () => Promise<void>;
};

export async function executeClearSession(
  target: SessionResource,
  input: { remote: boolean },
  ports: ClearSessionPorts,
  authority: SessionOperationAuthority,
): Promise<void> {
  authority.checkpoint();
  if (input.remote) {
    await ports.clearRemoteSession(target.tabId);
    authority.checkpoint();
    await ports.retryRemoteHydration();
  } else {
    await ports.clearSession();
  }
  authority.checkpoint();
}
