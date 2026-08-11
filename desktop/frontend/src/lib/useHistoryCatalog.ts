import { useCallback, useEffect, useRef, useState } from "react";
import { app } from "./bridge";
import type { HistorySearchHit, SessionMeta } from "./types";

function hitKey(hit: HistorySearchHit): string {
  return `${hit.sessionPath}:${hit.messageIndex}:${hit.kind}:${hit.toolName ?? ""}`;
}

export function useHistoryCatalog({
  isTrash,
  suppliedSessions,
  scope,
  status,
  timeFilter,
  query,
}: {
  isTrash: boolean;
  suppliedSessions: SessionMeta[];
  scope: string;
  status: string;
  timeFilter: string;
  query: string;
}) {
  const [catalogSessions, setCatalogSessions] = useState<SessionMeta[]>(suppliedSessions);
  const [nextSessionCursor, setNextSessionCursor] = useState("");
  const [nextSearchCursor, setNextSearchCursor] = useState("");
  const [partial, setPartial] = useState(false);
  const [progress, setProgress] = useState({ indexed: 0, total: 0 });
  const [searchHits, setSearchHits] = useState<HistorySearchHit[]>([]);
  const [refreshNonce, setRefreshNonce] = useState(0);
  const requestSeq = useRef(0);
  const revision = useRef(0);
  const refreshTimer = useRef<number | null>(null);
  const workspaceRoot = suppliedSessions.find((session) => session.current)?.workspaceRoot ?? "";

  useEffect(() => {
    if (isTrash) setCatalogSessions(suppliedSessions);
  }, [isTrash, suppliedSessions]);

  const fetchPage = useCallback(async (
    sessionCursor: string,
    searchCursor: string,
    append: boolean,
    seq: number,
  ) => {
    const base = { scope, workspaceRoot, status, timeFilter, query: query.trim(), limit: 50 };
    const sessionPromise = (!append || sessionCursor)
      ? app.ListHistorySessions({ ...base, cursor: sessionCursor })
      : Promise.resolve(null);
    const searchPromise = query.trim() && (!append || searchCursor)
      ? app.SearchHistoryContent({ ...base, cursor: searchCursor, kinds: [], toolName: "" })
      : Promise.resolve(null);
    const [page, bodyPage] = await Promise.all([sessionPromise, searchPromise]);
    if (seq !== requestSeq.current) return;
    if (page?.staleCursor && sessionCursor) {
      void fetchPage("", "", false, seq);
      return;
    }
    if (bodyPage?.staleCursor && searchCursor) {
      void fetchPage("", "", false, seq);
      return;
    }
    if (page) {
      setCatalogSessions((current) => append
        ? [...current, ...page.items.filter((item) => !current.some((existing) => existing.path === item.path))]
        : page.items);
      setNextSessionCursor(page.nextCursor || "");
    } else if (!append) {
      setCatalogSessions([]);
      setNextSessionCursor("");
    }
    setPartial(Boolean(page?.partial) || Boolean(bodyPage?.partial));
    if (bodyPage) {
      setSearchHits((current) => {
        if (!append) return bodyPage.items ?? [];
        const seen = new Set(current.map(hitKey));
        const merged = [...current];
        for (const hit of bodyPage.items ?? []) {
          const key = hitKey(hit);
          if (seen.has(key)) continue;
          seen.add(key);
          merged.push(hit);
        }
        return merged;
      });
      setNextSearchCursor(bodyPage.nextCursor || "");
      setProgress({ indexed: bodyPage.status.indexed, total: bodyPage.status.total });
    } else if (!append) {
      setSearchHits([]);
      setNextSearchCursor("");
    }
  }, [query, scope, status, timeFilter, workspaceRoot]);

  useEffect(() => {
    if (isTrash || typeof window === "undefined" || !window.runtime) return;
    const seq = ++requestSeq.current;
    const timer = window.setTimeout(() => {
      void fetchPage("", "", false, seq).catch(() => {
        if (seq !== requestSeq.current) return;
        setCatalogSessions(suppliedSessions);
        setNextSessionCursor("");
        setNextSearchCursor("");
        setSearchHits([]);
      });
    }, query.trim() ? 200 : 0);
    return () => window.clearTimeout(timer);
  }, [fetchPage, isTrash, query, refreshNonce, suppliedSessions]);

  useEffect(() => {
    if (isTrash || typeof window === "undefined" || !window.runtime) return;
    const unsubscribe = window.runtime.EventsOn("history-index:changed-v1", (payload?: unknown) => {
      if (!payload || typeof payload !== "object") return;
      const event = payload as { revision?: number; indexed?: number; total?: number; pending?: number };
      const nextRevision = typeof event.revision === "number" ? event.revision : 0;
      if (nextRevision <= revision.current) return;
      revision.current = nextRevision;
      setProgress({ indexed: Number(event.indexed) || 0, total: Number(event.total) || 0 });
      setPartial((Number(event.pending) || 0) > 0 || (Number(event.indexed) || 0) < (Number(event.total) || 0));
      if (refreshTimer.current === null) {
        refreshTimer.current = window.setTimeout(() => {
          refreshTimer.current = null;
          setRefreshNonce((value) => value + 1);
        }, 200);
      }
    });
    return () => {
      unsubscribe();
      if (refreshTimer.current !== null) window.clearTimeout(refreshTimer.current);
      refreshTimer.current = null;
    };
  }, [isTrash]);

  const loadMore = useCallback(() => {
    if (!nextSessionCursor && !nextSearchCursor) return;
    void fetchPage(nextSessionCursor, nextSearchCursor, true, requestSeq.current);
  }, [fetchPage, nextSearchCursor, nextSessionCursor]);

  return {
    sessions: isTrash ? suppliedSessions : catalogSessions,
    nextCursor: nextSessionCursor || nextSearchCursor,
    partial,
    progress,
    searchHits,
    loadMore,
  };
}
