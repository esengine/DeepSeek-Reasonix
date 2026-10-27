import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { HttpError, type AgentPort } from "../port/port";
import type { HistoryPage } from "../port/session";
import { fromHistory, type SessionEvent } from "../state/session";

export interface HistoryPaging {
  snapshot: string;
  revision: number;
  hasMore: boolean;
  busy: boolean;
  failed: boolean;
  /** How many raw messages sit before the loaded window. The rail draws only
   *  what is mounted, so without this a paged conversation looks shorter than
   *  it is until someone scrolls up. */
  earlier: number;
  more: () => Promise<void>;
  reload: () => Promise<void>;
}

export function useHistorySearch(paging: HistoryPaging, open: boolean, query: string) {
  useEffect(() => {
    if (open && query.trim() && paging.hasMore && !paging.busy && !paging.failed) void paging.more().catch(() => {});
  }, [open, query, paging.revision, paging.hasMore, paging.busy, paging.failed, paging.more]);
}

const projection = (page: HistoryPage) => fromHistory(Array.isArray(page.messages) ? page.messages : [], {
  snapshot: page.snapshot, toolContext: Array.isArray(page.toolContext) ? page.toolContext : [], turnStartedAt: page.turnStartedAt,
});

function checkedPage(page: HistoryPage): HistoryPage {
  if (!page || !Array.isArray(page.messages) || typeof page.snapshot !== "string" ||
      !Number.isSafeInteger(page.start) || !Number.isSafeInteger(page.end) || !Number.isSafeInteger(page.total) ||
      page.start < 0 || page.end < page.start || page.total < page.end ||
      page.hasMoreBefore !== (page.start > 0) || page.hasMoreBefore && !page.nextCursor) throw new Error("invalid history page");
  return page;
}

function unsupported(error: unknown): boolean {
  if (!(error instanceof HttpError)) return false;
  const code = error.reason?.code;
  return code === "history.pagination_unsupported" || !code && [404, 405, 501].includes(error.status);
}

export function useHistoryPages(port: Pick<AgentPort, "history" | "historyPage">, dispatch: (event: SessionEvent) => void) {
  const owner = useMemo(() => ({ epoch: 0, cursor: "", snapshot: "", before: 0, total: 0, paging: !!port.historyPage, more: null as Promise<void> | null }), [port]);
  const current = useRef(owner);
  current.current = owner;
  const [state, setState] = useState({ snapshot: "", revision: 0, hasMore: false, busy: false, failed: false, earlier: 0 });
  useEffect(() => () => { owner.epoch++; }, [owner]);

  const load = useCallback(async (applies: () => boolean = () => true) => {
    const epoch = ++owner.epoch;
    const valid = () => current.current === owner && epoch === owner.epoch && applies();
    owner.cursor = "";
    owner.more = null;
    setState((old) => ({ ...old, hasMore: false, busy: true, failed: false }));
    try {
      let page: HistoryPage | undefined;
      if (owner.paging && port.historyPage) {
        try { page = checkedPage(await port.historyPage()); }
        catch (error) {
          if (!unsupported(error)) throw error;
          owner.paging = false;
        }
      }
      const messages = page ? null : await port.history();
      if (!valid()) return;
      const restored = page ? projection(page) : fromHistory(messages ?? []);
      owner.cursor = page?.nextCursor ?? "";
      owner.snapshot = page?.snapshot ?? "";
      owner.before = page?.start ?? 0;
      owner.total = page?.total ?? 0;
      dispatch({ kind: "__restore", ...restored });
      setState((old) => ({ snapshot: owner.snapshot, revision: old.revision + 1, hasMore: !!owner.cursor, busy: false, failed: false, earlier: owner.before }));
    } catch (error) {
      if (valid()) setState((old) => ({ ...old, busy: false, failed: true }));
      throw error;
    }
  }, [owner, port, dispatch]);

  const more = useCallback((): Promise<void> => {
    if (owner.more) return owner.more;
    if (!owner.cursor || !port.historyPage) return Promise.resolve();
    const epoch = owner.epoch;
    const cursor = owner.cursor;
    const valid = () => current.current === owner && epoch === owner.epoch;
    setState((old) => ({ ...old, busy: true, failed: false }));
    const pending = port.historyPage(cursor).then((page) => {
      if (!valid()) return;
      checkedPage(page);
      if (page.snapshot !== owner.snapshot) return load();
      if (page.end !== owner.before || page.total !== owner.total || page.start >= page.end) throw new Error("history page did not advance");
      owner.cursor = page.nextCursor ?? "";
      owner.before = page.start;
      owner.more = null;
      dispatch({ kind: "__history_prepend", ...projection(page) });
      setState((old) => ({ ...old, revision: old.revision + 1, hasMore: !!owner.cursor, busy: false, failed: false, earlier: owner.before }));
    }).catch(async (error) => {
      if (!valid()) return;
      if (error instanceof HttpError && error.reason?.code === "history.snapshot_changed") { await load(); return; }
      setState((old) => ({ ...old, busy: false, failed: true }));
      throw error;
    }).finally(() => { if (owner.more === pending) owner.more = null; });
    owner.more = pending;
    return pending;
  }, [owner, port, dispatch, load]);

  const reload = useCallback(() => load(), [load]);
  return { load, paging: { ...state, more, reload } satisfies HistoryPaging };
}
