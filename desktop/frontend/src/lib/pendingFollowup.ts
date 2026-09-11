import type { AppBindings } from "./bridge";
import type { InvocationRequest, StructuredInvocationSubmit } from "./invocationDisplay";

export type InboxTarget = { tabId: string; sessionPath: string; generation: number; selection: number; remote: boolean; hostId?: string; workspace?: string };
export type FollowupReceipt = { itemId: string; disposition: string; position: number; paused: boolean; idempotent?: boolean; error?: string };
export interface FollowupBindings {
  EnqueueInboxFollowup(tabID: string, display: string, submit: string, idempotency: string): Promise<FollowupReceipt>;
  EnqueueInboxFollowupWithInvocations(tabID: string, display: string, submit: string, invocations: InvocationRequest[], idempotency: string): Promise<FollowupReceipt>;
  CaptureInboxTarget?(tabID: string, expectedPath: string): Promise<InboxTarget>;
  EnqueueInboxFollowupForTarget?(target: InboxTarget, display: string, submit: string, invocations: InvocationRequest[], key: string): Promise<FollowupReceipt>;
  LookupInboxFollowupForTarget?(target: InboxTarget, key: string): Promise<FollowupReceipt>;
}
export type PendingFollowup = {
  key: string; target?: InboxTarget; tabId: string; display: string; submit: string;
  structured?: StructuredInvocationSubmit; draft: string;
};

// TabMeta supplies the backend's session path. UI topic/draft keys and tab
// generations are deliberately excluded from this durable request identity.
export function followupSessionKey(sessionPath?: string, hostId?: string, workspace?: string): string {
  const path = sessionPath?.trim();
  if (!path || (hostId && !workspace)) return "";
  return JSON.stringify(hostId ? ["remote", hostId, workspace, path] : ["local", path]);
}

// Process-local draft ownership survives Composer remounts. Nothing here is
// inferred from the currently executing turn, and unresolved entries never expire.
const requests = new Map<string, PendingFollowup>();
const listeners = new Set<() => void>();
export const pendingFollowups = {
  get: (draft: string) => requests.get(draft),
  subscribe: (listener: () => void) => { listeners.add(listener); return () => { listeners.delete(listener); }; },
  set(draft: string, request: PendingFollowup) { requests.set(draft, request); listeners.forEach(listener => listener()); },
  clear(draft: string, request: PendingFollowup) {
    if (requests.get(draft) !== request) return;
    requests.delete(draft); listeners.forEach(listener => listener());
  },
};

export async function confirmFollowup(binding: AppBindings, request: PendingFollowup): Promise<FollowupReceipt> {
  if (!request.target || !binding.LookupInboxFollowupForTarget) throw new Error("Follow-up receipt lookup unavailable");
  const receipt = await binding.LookupInboxFollowupForTarget(request.target, request.key);
  if (!receipt?.itemId || receipt.error) throw new Error(receipt?.error || "Follow-up receipt unconfirmed");
  return receipt;
}

export function followupNotSubmitted(error: unknown): boolean {
  return /reasonix_error:(inbox_not_submitted|inbox_capacity_items|inbox_capacity_bytes|inbox_item_too_large|inbox_empty|inbox_schema_readonly|channel_read_only)(?:$|\b)/.test(String(error));
}
