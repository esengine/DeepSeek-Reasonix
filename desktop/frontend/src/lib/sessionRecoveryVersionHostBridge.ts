import type { RecoveryLineageView, SessionMeta } from "./types";

type SessionVersionInspector = (session: SessionMeta, view: RecoveryLineageView) => void;

let inspector: SessionVersionInspector | undefined;
let queued: Parameters<SessionVersionInspector> | undefined;

export function requestSessionVersions(session: SessionMeta, view: RecoveryLineageView) {
  if (inspector) inspector(session, view);
  else queued = [session, view];
}

export function bindSessionVersionInspector(next: SessionVersionInspector): () => void {
  inspector = next;
  if (queued) {
    const request = queued;
    queued = undefined;
    next(...request);
  }
  return () => {
    if (inspector === next) inspector = undefined;
  };
}
