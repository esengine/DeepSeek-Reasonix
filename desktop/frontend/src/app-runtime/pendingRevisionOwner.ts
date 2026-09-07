import type { SessionOperationAuthority, SessionResource, useSessionOperations } from "./useSessionOperations";

export type PendingRevisionInput = {
  visible: SessionResource; resources: readonly SessionResource[]; running: boolean; ready: boolean;
  operations: ReturnType<typeof useSessionOperations>;
  send(target: SessionResource, text: string, authority: SessionOperationAuthority): Promise<void>; report(error: unknown): void;
};
type Committed = { epoch: number; input: PendingRevisionInput };
type Entry = { target: SessionResource; text: string; failedAt?: number; failed?: boolean };
const key = (target: SessionResource) => JSON.stringify([target.tabId, target.sessionKey]);

async function deliver(entry: Entry, committed: Committed) {
  return committed.input.operations(entry.target, "plan-revision", { entry, send: committed.input.send }, async ({ entry, send }, authority) => {
    authority.checkpoint();
    try { await send(entry.target, entry.text, authority); } catch (error) {
      authority.checkpoint();
      // Resource failure retention is independent of permission to show error UI.
      entry.failed = true;
      throw error;
    }
    authority.checkpoint();
  });
}

/** Latest revision per source; only an identical active request can release its slot. */
export function createPendingRevisionOwner(read: () => Committed | undefined) {
  const queued = new Map<string, Entry>();
  const active = new Map<string, Entry>();
  let eligibility = "", eligibilityRevision = 0;
  const pump = () => {
    const committed = read();
    if (!committed) return;
    const nextEligibility = JSON.stringify([key(committed.input.visible), committed.input.running, committed.input.ready]);
    if (eligibility !== nextEligibility) { eligibility = nextEligibility; eligibilityRevision++; }
    const valid = new Set(committed.input.resources.map(key));
    for (const id of queued.keys()) if (!valid.has(id)) queued.delete(id);
    for (const id of active.keys()) if (!valid.has(id)) active.delete(id);
    const id = key(committed.input.visible), entry = queued.get(id);
    if (!committed.input.ready || committed.input.running || !entry || entry.failedAt === eligibilityRevision || active.has(id)) return;
    entry.failed = false;
    active.set(id, entry);
    void deliver(entry, committed).then(outcome => {
      if (read()?.epoch !== committed.epoch || active.get(id) !== entry) return;
      if (entry.failed) {
        // Keep the user's revision, but only a later source activation/idle
        // transition (or a new revision) may retry it, never unrelated renders.
        entry.failedAt = eligibilityRevision;
        if (outcome.status === "failed") read()?.input.report(outcome.error);
      } else if (queued.get(id) === entry) queued.delete(id);
    }).finally(() => {
      if (read()?.epoch !== committed.epoch || active.get(id) !== entry) return;
      active.delete(id);
      // A replacement revision is a new request, not a retry of the old one.
      pump();
    });
  };
  return {
    remember(tabId: string, text: string) {
      const committed = read();
      const target = committed?.input.resources.find(resource => resource.tabId === tabId);
      if (!target || !text) return;
      queued.set(key(target), { target, text });
      pump();
    },
    pump,
    dispose() { queued.clear(); active.clear(); },
  };
}
