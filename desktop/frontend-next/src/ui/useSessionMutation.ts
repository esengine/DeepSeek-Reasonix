import { useCallback, useLayoutEffect, useRef, useState } from "react";

export type SessionMutation = "pin" | "archive" | "delete";

export function captureSessionFocus(row: HTMLElement | null): () => void {
  const from = document.activeElement as HTMLElement | null;
  if (!row || !from || !row.contains(from)) return () => {};
  const rows = [...document.querySelectorAll<HTMLElement>('[role="treeitem"]')];
  const at = rows.indexOf(row);
  const neighbors = [rows[at + 1], rows[at - 1]];
  const target = row.dataset.target;
  return () => {
    if (document.activeElement !== from && document.activeElement !== document.body) return;
    const replacement = target ? [...document.querySelectorAll<HTMLElement>('[role="treeitem"]')].find((node) => node.dataset.target === target) : null;
    const candidates = [from, row, replacement, ...neighbors,
      document.querySelector<HTMLElement>('[data-action="machine.menu"]')];
    candidates.find((node) => node?.isConnected && !node.matches(":disabled"))?.focus({ preventScroll: true });
  };
}

export function useSessionMutation(onError: (error: unknown) => void) {
  const [pending, setPending] = useState<ReadonlyMap<string, SessionMutation>>(new Map());
  const flights = useRef(new Set<string>());
  const finished = useRef<(() => void)[]>([]);
  useLayoutEffect(() => { for (const restore of finished.current.splice(0)) restore(); }, [pending]);
  const run = useCallback(async (key: string, kind: SessionMutation, row: HTMLElement | null, task: () => void | Promise<void>, restore = captureSessionFocus(row)) => {
    if (flights.current.has(key)) return;
    flights.current.add(key);
    setPending((current) => new Map(current).set(key, kind));
    try { await task(); }
    catch (error) { onError(error); }
    finally {
      flights.current.delete(key);
      finished.current.push(restore);
      setPending((current) => { const next = new Map(current); next.delete(key); return next; });
    }
  }, [onError]);
  return { pending, run };
}
