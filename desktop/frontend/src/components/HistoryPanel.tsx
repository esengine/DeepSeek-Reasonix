import { useManagementT } from "../lib/managementLocale";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import type { MouseEvent as ReactMouseEvent } from "react";
import { Archive, GitBranch, Pencil, Search, Trash2, RotateCcw } from "lucide-react";
import { app } from "../lib/bridge";
import { t, useT } from "../lib/i18n";
import { historySearchHitDisplayTitle, historySessionDisplayTitle, sessionActivityTime } from "../lib/session";
import type { HistoryMessage, HistorySearchContextLine, HistorySearchHit, RecoveryLineageView, SessionMeta } from "../lib/types";
import { historyMessagesToItems, type Item } from "../lib/useController";
import { useHistoryCatalog } from "../lib/useHistoryCatalog";
import { Transcript } from "./Transcript";
import { ContextMenu, contextMenuPointFromEvent, type ContextMenuItem, type ContextMenuPoint } from "./ContextMenu";
import { useDeferredClose } from "../lib/useMountTransition";
import { ModalCloseButton } from "./ModalCloseButton";
import { HistoryFilterSelect } from "./HistoryFilterSelect";
import { normalizeRecoveryLineageView, userVisibleRecoveryVersions } from "../lib/sessionRecoveryVersions";

type HistoryScopeFilter = "all" | "project" | "global";
type HistoryStatusFilter = "all" | "current" | "open";
type HistoryDateFilter = "all" | "today" | "yesterday" | "older";

// HistoryPanel lists saved sessions newest-first. In the wide management modal,
// a single click selects a read-only preview; explicit actions resume, restore,
// rename, or delete the selected session.
export function HistoryPanel({
  presentation = "dialog", active = true, busy = false,
  kind = "history",
  sessions: suppliedSessions,
  running,
  onResume,
  onPreview,
  onDelete,
  onRename,
  onRestore,
  onPurge,
  onPurgeAll,
  onInspectVersions,
  onClose,
}: {
  presentation?: "dialog" | "page"; active?: boolean; busy?: boolean;
  kind?: "history" | "trash";
  sessions: SessionMeta[];
  running: boolean;
  onResume: (session: SessionMeta) => void;
  onPreview: (path: string) => Promise<HistoryMessage[]>;
  onDelete: (path: string) => void;
  onRename: (session: SessionMeta, title: string) => void;
  onRestore?: (path: string) => Promise<void>;
  onPurge?: (path: string) => Promise<void>;
  onPurgeAll?: (paths: string[]) => Promise<void>;
  onInspectVersions?: (session: SessionMeta, view: RecoveryLineageView) => void;
  onClose: () => void;
}) {
  const tr = useT();
  const m = useManagementT();
  const [detailVisible, setDetailVisible] = useState(false);
  const previousPaths = useRef<string[]>([]);
  const isTrash = kind === "trash";
  // Play the modal exit animation, then let the parent unmount us.
  const { status, requestClose } = useDeferredClose(onClose, 240);
  const [editing, setEditing] = useState<string | null>(null);
  const [draft, setDraft] = useState("");
  const [query, setQuery] = useState("");
  const [scopeFilter, setScopeFilter] = useState<HistoryScopeFilter>("all");
  const [statusFilter, setStatusFilter] = useState<HistoryStatusFilter>("all");
  const [dateFilter, setDateFilter] = useState<HistoryDateFilter>("all");
  const [showSystemRecoveryData, setShowSystemRecoveryData] = useState(false);
  const [selectedVersions, setSelectedVersions] = useState<RecoveryLineageView | null>(null);
  const [searchContext, setSearchContext] = useState<{ hit: HistorySearchHit; lines: HistorySearchContextLine[]; loading: boolean } | null>(null);
  const { sessions, nextCursor, partial: catalogPartial, progress: catalogProgress, searchHits, loadMore } = useHistoryCatalog({
    isTrash, suppliedSessions, scope: scopeFilter, status: statusFilter, timeFilter: dateFilter, query,
  });
  const [menuSession, setMenuSession] = useState<SessionMeta | null>(null);
  const [menuPoint, setMenuPoint] = useState<ContextMenuPoint | null>(null);
  const [blankMenuPoint, setBlankMenuPoint] = useState<ContextMenuPoint | null>(null);
  const [menuConfirmTarget, setMenuConfirmTarget] = useState<
    { kind: "delete"; path: string } | { kind: "purge"; path: string } | { kind: "clear" } | null
  >(null);
  const [preview, setPreview] = useState<{
    path: string;
    title: string;
    meta: string;
    messages: HistoryMessage[];
    loading: boolean;
    error?: boolean;
  } | null>(null);
  const previewSeq = useRef(0);
  const lineageSeq = useRef(0);

  const loadSearchContext = useCallback(async (hit: HistorySearchHit) => {
    const seq = ++previewSeq.current;
    setPreview(null);
    setSearchContext({ hit, lines: [], loading: true });
    const lines = await app.GetHistorySearchContext({ sessionPath: hit.sessionPath, messageIndex: hit.messageIndex, before: 2, after: 2 }).catch(() => []);
    if (seq === previewSeq.current) setSearchContext({ hit, lines, loading: false });
  }, []);

  const startRename = (s: SessionMeta) => {
    if (running) return;
    setEditing(s.path);
    setDraft((s.topicId ? s.topicTitle : s.title) || s.preview || "");
  };
  const commitRename = (session: SessionMeta) => {
    if (running) return;
    onRename(session, draft.trim());
    setEditing(null);
  };
  const loadPreview = useCallback(
    async (s: SessionMeta) => {
      const seq = ++previewSeq.current;
      setSearchContext(null);
      setEditing(null);
      setPreview({
        path: s.path,
        title: historySessionDisplayTitle(s, tr("history.emptySession")),
        meta: sessionMetaLine(s, tr, isTrash),
        messages: [],
        loading: true,
      });
      try {
        const messages = await onPreview(s.path);
        if (seq === previewSeq.current) setPreview((cur) => cur?.path === s.path ? { ...cur, messages, loading: false } : cur);
      } catch {
        if (seq === previewSeq.current) setPreview((cur) => cur?.path === s.path ? { ...cur, loading: false, error: true } : cur);
      }
    },
    [isTrash, onPreview, tr],
  );

  const ordinarySessions = useMemo(() => sessions.filter((session) => !session.recoveryCopy), [sessions]);
  const scopeCounts = useMemo(
    () => ({
      all: ordinarySessions.length,
      project: ordinarySessions.filter((s) => sessionScope(s) === "project").length,
      global: ordinarySessions.filter((s) => sessionScope(s) === "global").length,
    }),
    [ordinarySessions],
  );
  const statusCounts = useMemo(
    () => ({
      all: ordinarySessions.length,
      current: ordinarySessions.filter((s) => s.current).length,
      open: ordinarySessions.filter((s) => s.open && !s.current).length,
    }),
    [ordinarySessions],
  );
  const dateCounts = useMemo(() => {
    const counts: Record<HistoryDateFilter, number> = { all: ordinarySessions.length, today: 0, yesterday: 0, older: 0 };
    for (const s of ordinarySessions) counts[dateBucket(sessionTimeForGrouping(s, isTrash))]++;
    return counts;
  }, [isTrash, ordinarySessions]);

  useEffect(() => {
    if (!isTrash || presentation === "page") return;
    if (scopeFilter === "project" && scopeCounts.project === 0) setScopeFilter("all");
    if (scopeFilter === "global" && scopeCounts.global === 0) setScopeFilter("all");
  }, [isTrash, presentation, scopeCounts.global, scopeCounts.project, scopeFilter]);

  useEffect(() => {
    if (!isTrash) return;
    if (statusFilter === "current" && statusCounts.current === 0) setStatusFilter("all");
    if (statusFilter === "open" && statusCounts.open === 0) setStatusFilter("all");
  }, [isTrash, statusCounts.current, statusCounts.open, statusFilter]);

  useEffect(() => {
    if (!isTrash || presentation === "page") return;
    if (dateFilter !== "all" && dateCounts[dateFilter] === 0) setDateFilter("all");
  }, [dateCounts, dateFilter, isTrash, presentation]);

  const filteredSessions = useMemo(() => {
    const q = isTrash ? query.trim().toLowerCase() : "";
    return sessions.filter((s) => {
      if (scopeFilter !== "all" && sessionScope(s) !== scopeFilter) return false;
      if (!isTrash && statusFilter === "current" && !s.current) return false;
      if (!isTrash && statusFilter === "open" && (!s.open || s.current)) return false;
      if (dateFilter !== "all" && dateBucket(sessionTimeForGrouping(s, isTrash)) !== dateFilter) return false;
      if (!q) return true;
      return [s.title, s.preview, s.path, s.topicTitle, s.workspaceRoot].some((part) => (part ?? "").toLowerCase().includes(q));
    });
  }, [dateFilter, isTrash, query, scopeFilter, sessions, statusFilter]);
  const displayedSessions = useMemo(
    () => filteredSessions.filter((session) => !session.recoveryCopy),
    [filteredSessions],
  );
  const systemRecoverySessions = useMemo(
    () => isTrash ? filteredSessions.filter((session) => session.recoveryCopy) : [],
    [filteredSessions, isTrash],
  );
  const selectableSessions = useMemo(
    () => isTrash && showSystemRecoveryData ? [...displayedSessions, ...systemRecoverySessions] : displayedSessions,
    [displayedSessions, isTrash, showSystemRecoveryData, systemRecoverySessions],
  );

  // Sessions arrive newest-first; bucket consecutive ones under a day heading
  // (Today / Yesterday / a date) while preserving that order.
  const groups: { label: string; items: SessionMeta[] }[] = [];
  for (const s of displayedSessions) {
    const label = dayLabel(sessionTimeForGrouping(s, isTrash));
    const last = groups[groups.length - 1];
    if (last && last.label === label) last.items.push(s);
    else groups.push({ label, items: [s] });
  }

  useEffect(() => {
    setMenuSession(null);
    setMenuPoint(null);
    setBlankMenuPoint(null);
    setMenuConfirmTarget(null);
  }, [isTrash]);

  useEffect(() => {
    if (isTrash) setStatusFilter("all");
  }, [isTrash]);

  useEffect(() => {
    if (!active) return;
    const oldPaths = previousPaths.current;
    previousPaths.current = displayedSessions.map((item) => item.path);
    if (preview && selectableSessions.some((item) => item.path === preview.path)) return;
    if (!displayedSessions.length) { ++previewSeq.current; setPreview(null); return; }
    const oldIndex = preview ? oldPaths.indexOf(preview.path) : 0;
    const next = displayedSessions[Math.max(0, Math.min(oldIndex, displayedSessions.length - 1))];
    void loadPreview(next);
  }, [active, displayedSessions, loadPreview, preview, selectableSessions]);
  useEffect(() => {
    if (!active) { setMenuSession(null); setBlankMenuPoint(null); setMenuConfirmTarget(null); }
  }, [active]);

  const previewItems = useMemo(() => previewMessagesToItems(preview?.messages ?? []), [preview?.messages]);
  const selectedSession = useMemo(
    () => (preview ? selectableSessions.find((s) => s.path === preview.path) ?? null : null),
    [preview, selectableSessions],
  );
  useEffect(() => {
    const seq = ++lineageSeq.current;
    setSelectedVersions(null);
    if (isTrash || !selectedSession?.topicId) return;
    const topic = {
      scope: selectedSession.scope || "global",
      workspaceRoot: selectedSession.workspaceRoot || undefined,
      topicId: selectedSession.topicId,
      path: selectedSession.path,
    };
    void app.GetRecoveryLineage(topic)
      .then((value) => {
        if (seq !== lineageSeq.current) return;
        const view = normalizeRecoveryLineageView(value);
        if (userVisibleRecoveryVersions(view).length > 1) setSelectedVersions(view);
      })
      .catch(() => undefined);
  }, [isTrash, selectedSession?.path, selectedSession?.scope, selectedSession?.topicId, selectedSession?.workspaceRoot]);
  const openSessionMenu = (event: ReactMouseEvent<HTMLElement>, s: SessionMeta) => {
    event.preventDefault();
    event.stopPropagation();
    setMenuConfirmTarget(null);
    setBlankMenuPoint(null);
    setMenuSession(s);
    setMenuPoint(contextMenuPointFromEvent(event));
  };
  const openTrashBlankMenu = (event: ReactMouseEvent<HTMLDivElement>) => {
    if (!isTrash || busy || ordinarySessions.length === 0) return;
    if (presentation === "page") { onPurgeAll?.(ordinarySessions.map((item) => item.path)); return; }
    const target = event.target as HTMLElement | null;
    if (target?.closest(".hist-item,.history-search,.history-preview,button,input,textarea,select")) return;
    event.preventDefault();
    setMenuConfirmTarget(null);
    setMenuSession(null);
    setMenuPoint(null);
    setBlankMenuPoint(contextMenuPointFromEvent(event));
  };
  const armClearTrash = () => {
    if (!isTrash || busy || ordinarySessions.length === 0) return;
    if (presentation === "page") { onPurgeAll?.(ordinarySessions.map((item) => item.path)); return; }
    setMenuSession(null);
    setMenuPoint(null);
    setBlankMenuPoint(null);
    setMenuConfirmTarget({ kind: "clear" });
  };
  const closeHistoryMenus = () => {
    setMenuSession(null);
    setMenuPoint(null);
    setBlankMenuPoint(null);
    setMenuConfirmTarget(null);
  };
  const deleteHistorySession = (s: SessionMeta) => {
    closeHistoryMenus();
    onDelete(s.path);
  };
  const purgeTrashSession = (s: SessionMeta) => {
    closeHistoryMenus();
    if (!busy) onPurge?.(s.path);
  };
  const clearTrash = () => {
    const paths = ordinarySessions.map((s) => s.path);
    closeHistoryMenus();
    if (!busy) onPurgeAll?.(paths);
  };
  const sessionMenuItems: ContextMenuItem[] = menuSession
    ? isTrash
      ? [
        {
          key: "restore",
          icon: <RotateCcw size={13} />,
          label: tr("history.restoreSession"),
          onSelect: () => {
            if (!busy) onRestore?.(menuSession.path);
            closeHistoryMenus();
          },
        },
        { type: "separator", key: "trash-session-separator" },
        {
          key: "purge",
          icon: <Trash2 size={13} />,
          label:
            menuConfirmTarget?.kind === "purge" && menuConfirmTarget.path === menuSession.path
              ? tr("history.confirmPurge")
              : tr("history.purgeSession"),
          danger: true,
          onSelect: () => {
            if (presentation === "page" || (menuConfirmTarget?.kind === "purge" && menuConfirmTarget.path === menuSession.path)) {
              purgeTrashSession(menuSession);
            } else {
              setMenuConfirmTarget({ kind: "purge", path: menuSession.path });
            }
          },
        },
      ]
      : [
          {
            key: "rename",
            icon: <Pencil size={13} />,
            label: tr("history.rename"),
            disabled: running,
            onSelect: () => {
              const target = menuSession;
              closeHistoryMenus();
              startRename(target);
            },
          },
          ...(menuSession.current
            ? []
            : [
                {
                  key: "delete",
                  icon: <Archive size={13} />,
                  label:
                    menuConfirmTarget?.kind === "delete" && menuConfirmTarget.path === menuSession.path
                      ? tr("history.confirmMoveToTrash")
                      : tr("history.moveToTrash"),
                  disabled: running,
                  danger: menuConfirmTarget?.kind === "delete" && menuConfirmTarget.path === menuSession.path,
                  onSelect: () => {
                    if (menuConfirmTarget?.kind === "delete" && menuConfirmTarget.path === menuSession.path) {
                      deleteHistorySession(menuSession);
                    } else {
                      setMenuConfirmTarget({ kind: "delete", path: menuSession.path });
                    }
                  },
                } as ContextMenuItem,
              ]),
        ]
    : [];
  const trashBlankMenuItems: ContextMenuItem[] =
    menuConfirmTarget?.kind === "clear"
        ? [
            {
              key: "clear-trash-confirm",
              icon: <Trash2 size={13} />,
              label: tr("history.confirmClearTrash"),
              danger: true,
              onSelect: clearTrash,
            },
          ]
        : [
            {
              key: "clear-trash",
              icon: <Trash2 size={13} />,
              label: tr("history.clearTrashMenu"),
              danger: true,
              onSelect: armClearTrash,
            },
          ];
  const actionConfirmDelete =
    selectedSession && menuConfirmTarget?.kind === "delete" && menuConfirmTarget.path === selectedSession.path;
  const actionConfirmPurge =
    selectedSession && menuConfirmTarget?.kind === "purge" && menuConfirmTarget.path === selectedSession.path;
  const actionConfirmClearTrash = isTrash && menuConfirmTarget?.kind === "clear";

  const openSelected = () => {
    if (!selectedSession || running || isTrash) return;
    onResume(selectedSession);
  };
  const inspectSelectedVersions = () => {
    if (isTrash || !selectedSession || !selectedVersions) return;
    closeHistoryMenus();
    onInspectVersions?.(selectedSession, selectedVersions);
  };
  const renameSelected = () => {
    if (!selectedSession || running || isTrash) return;
    closeHistoryMenus();
    startRename(selectedSession);
  };
  const moveSelectedToTrash = () => {
    if (!selectedSession || running || isTrash || selectedSession.current) return;
    if (actionConfirmDelete) deleteHistorySession(selectedSession);
    else setMenuConfirmTarget({ kind: "delete", path: selectedSession.path });
  };
  const restoreSelected = () => {
    if (!selectedSession || !isTrash) return;
    closeHistoryMenus();
    if (!busy) onRestore?.(selectedSession.path);
  };
  const purgeSelected = () => {
    if (!selectedSession || !isTrash) return;
    if (presentation === "page" || actionConfirmPurge) purgeTrashSession(selectedSession);
    else setMenuConfirmTarget({ kind: "purge", path: selectedSession.path });
  };

  const renderSessionItem = (session: SessionMeta) => {
    const selected = preview?.path === session.path;
    return (
      <div
        className={`hist-item${session.current ? " hist-item--current" : ""}${selected ? " hist-item--selected" : ""}`}
        key={session.path}
        onContextMenu={(event) => openSessionMenu(event, session)}
      >
        {editing === session.path ? (
          <input
            className="hist-item__rename"
            autoFocus
            value={draft}
            onChange={(event) => setDraft(event.target.value)}
            onKeyDown={(event) => {
              if (event.key === "Enter") commitRename(session);
              if (event.key === "Escape") setEditing(null);
            }}
            onBlur={() => commitRename(session)}
            placeholder={tr("history.namePlaceholder")}
          />
        ) : (
          <button
            className="hist-item__main"
            aria-pressed={selected}
            onClick={() => {
              setMenuConfirmTarget(null);
              setDetailVisible(true);
              void loadPreview(session);
            }}
            onDoubleClick={() => {
              if (!isTrash && !running) onResume(session);
            }}
          >
            <div className="hist-item__preview">{historySessionDisplayTitle(session, tr("history.emptySession"))}</div>
            <div className="hist-item__meta">
              {!isTrash && isChannelSession(session) && <span className="hist-item__badge hist-item__badge--open">{tr("history.channel")}</span>}
              {!isTrash && session.current && <span className="hist-item__badge hist-item__badge--current">{tr("history.current")}</span>}
              {!isTrash && !session.current && session.open && <span className="hist-item__badge hist-item__badge--open">{tr("history.open")}</span>}
              {isTrash && <span className="hist-item__badge hist-item__badge--deleted">{tr("history.deleted")}</span>}
              {sessionLocation(session, tr) && <span className="hist-item__scope">{sessionLocation(session, tr)}</span>}
              <span className="hist-item__metaspacer" />
              <span className="hist-item__stat">
                {session.turnsState === "unknown"
                  ? tr("history.indexing")
                  : tr(session.turns === 1 ? "history.turnOne" : "history.turnOther", { n: session.turns })}
              </span>
              <span className="hist-item__dot">·</span>
              <span className="hist-item__stat">{timeLabel(isTrash ? session.deletedAt || sessionActivityTime(session) : sessionActivityTime(session))}</span>
              {!isTrash && running && (
                <>
                  <span className="hist-item__dot">·</span>
                  <span className="hist-item__stat">{tr("history.preview")}</span>
                </>
              )}
            </div>
          </button>
        )}
      </div>
    );
  };

  const content = (<>
      {presentation === "dialog" && <header className="management-modal__head history-modal__head">
        <div>
          <div className="management-modal__title history-modal__title">{tr(isTrash ? "history.trashTitle" : "history.title")}</div>
          {!isTrash && running && <div className="management-modal__summary history-modal__summary">{tr("history.readOnlyHint")}</div>}
        </div>
        <div className="management-modal__actions history-modal__actions">
          {isTrash && ordinarySessions.length > 0 && (
            <button
              className={`chip history-clear${actionConfirmClearTrash ? " history-clear--confirm" : ""}`}
              type="button" disabled={busy}
              onClick={actionConfirmClearTrash ? clearTrash : armClearTrash}
            >
              {tr(actionConfirmClearTrash ? "history.confirmClearTrash" : "history.clearTrash")}
            </button>
          )}
          {presentation === "dialog" && <ModalCloseButton label={tr("common.close")} onClick={requestClose} />}
        </div>
      </header>}

      <div
        className="history-manager"
        onContextMenu={openTrashBlankMenu}
      >
        <div className="history-toolbar" aria-label={tr("history.filters")}>
          {/* Keep search available for body-only hits (metadata sessions may be empty). */}
          {!isTrash && (
            <label className="mem-search history-search">
              <Search size={13} />
              <input value={query} onChange={(e) => setQuery(e.target.value)} placeholder={tr("history.searchPlaceholder")} />
            </label>
          )}
          {isTrash && sessions.length > 0 && (
            <label className="mem-search history-search">
              <Search size={13} />
              <input value={query} onChange={(e) => setQuery(e.target.value)} placeholder={tr("history.searchPlaceholder")} />
            </label>
          )}
          <HistoryFilterSelect
            label={tr("history.filterScope")}
            options={[
              { id: "all", label: tr("history.filterAll"), count: scopeCounts.all },
              { id: "project", label: tr("history.filterProject"), count: scopeCounts.project },
              { id: "global", label: tr("history.filterGlobal"), count: scopeCounts.global },
            ]}
            value={scopeFilter}
            onChange={(next) => setScopeFilter(next as HistoryScopeFilter)}
          />
          {!isTrash && (
            <HistoryFilterSelect
              label={tr("history.filterStatus")}
              options={[
                { id: "all", label: tr("history.filterAll"), count: statusCounts.all },
                { id: "current", label: tr("history.filterCurrent"), count: statusCounts.current },
                { id: "open", label: tr("history.filterOpen"), count: statusCounts.open },
              ]}
              value={statusFilter}
              onChange={(next) => setStatusFilter(next as HistoryStatusFilter)}
            />
          )}
          <HistoryFilterSelect
            label={tr(isTrash ? "history.filterDeletedAt" : "history.filterActivity")}
            options={[
              { id: "all", label: tr("history.filterAll"), count: dateCounts.all },
              { id: "today", label: tr("history.today"), count: dateCounts.today },
              { id: "yesterday", label: tr("history.yesterday"), count: dateCounts.yesterday },
              { id: "older", label: tr("history.older"), count: dateCounts.older },
            ]}
            value={dateFilter}
            onChange={(next) => setDateFilter(next as HistoryDateFilter)}
          />
        </div>
        {!isTrash && catalogPartial && (
          <div className="management-modal__summary history-modal__summary" role="status">
            History index is still building ({catalogProgress.indexed}/{catalogProgress.total}); results may be incomplete.
          </div>
        )}

        <div className="history-content" data-detail={detailVisible}>
          <div className={`history-list${isTrash ? " history-list--trash" : ""}`}>
            {(() => {
              const hasBodyHits = !isTrash && searchHits.length > 0;
              if (ordinarySessions.length === 0 && (!isTrash || systemRecoverySessions.length === 0) && !hasBodyHits) {
                return (
                  <div className={`mem-empty${isTrash ? " mem-empty--trash" : ""}`}>
                    {isTrash && <Trash2 size={22} />}
                    <span>{tr(isTrash ? "history.trashEmpty" : "history.empty")}</span>
                  </div>
                );
              }
              if (displayedSessions.length === 0 && (!isTrash || systemRecoverySessions.length === 0) && !hasBodyHits) {
                return <div className="mem-empty">{tr("history.noResults")}<button className="btn btn--small" onClick={() => { setQuery(""); setScopeFilter("all"); setDateFilter("all"); }}>{m("clearFilters")}</button></div>;
              }
              return (
              <>
              {hasBodyHits && (
                <section className="mem-section history-search-results">
                  <div className="mem-section__title hist-group__title">
                    <span>Content matches</span>
                    <span className="hist-group__count">{searchHits.length}</span>
                  </div>
                  {searchHits.map((hit) => (
                    <div className="hist-item" key={`${hit.sessionPath}:${hit.messageIndex}:${hit.kind}:${hit.toolName ?? ""}`}>
                      <button className="hist-item__main" type="button" onClick={() => void loadSearchContext(hit)}>
                        <div className="hist-item__preview">{historySearchHitDisplayTitle(hit)}</div>
                        <div className="hist-item__meta">
                          <span className="hist-item__badge">{hit.role} · {hit.kind}</span>
                          {hit.toolName && <span className="hist-item__scope">{hit.toolName}</span>}
                        </div>
                        <div className="hist-item__meta">{hit.snippet}</div>
                      </button>
                    </div>
                  ))}
                </section>
              )}
              {groups.map((g) => (
                <section className="mem-section" key={g.label}>
                  <div className="mem-section__title hist-group__title">
                    <span>{g.label}</span>
                    <span className="hist-group__count">{g.items.length}</span>
                  </div>
                  {g.items.map(renderSessionItem)}
                </section>
              ))}
              {isTrash && systemRecoverySessions.length > 0 && (
                <section className="mem-section history-system-recovery">
                  <button
                    className="mem-section__title hist-group__title history-system-recovery__toggle"
                    type="button"
                    aria-expanded={showSystemRecoveryData}
                    onClick={() => setShowSystemRecoveryData((value) => !value)}
                  >
                    <span>{tr("history.systemRecoveryData")}</span>
                    <span className="hist-group__count">{systemRecoverySessions.length}</span>
                  </button>
                  {showSystemRecoveryData && (
                    <div className="history-system-recovery__items" aria-label={tr("history.systemRecoveryData")}>
                      {systemRecoverySessions.map(renderSessionItem)}
                    </div>
                  )}
                </section>
              )}
              {!isTrash && nextCursor && (
                <button
                  className="btn btn--small"
                  type="button"
                  onClick={loadMore}
                >
                  Load more
                </button>
              )}
              </>
              );
            })()}
          </div>

          <section className={`history-preview${!preview && !searchContext ? " history-preview--empty" : ""}`}>
            {presentation === "page" && <button className="btn btn--small management-list-back" onClick={() => setDetailVisible(false)}>{m("listBack")}</button>}
            {searchContext ? (
              <>
                <div className="history-preview__head">
                  <div className="history-preview__copy">
                    <div className="history-preview__title">{historySearchHitDisplayTitle(searchContext.hit)}</div>
                    <div className="history-preview__meta">{searchContext.hit.role} · {searchContext.hit.kind}</div>
                  </div>
                </div>
                <div className="history-preview__body">
                  {searchContext.loading ? (
                    <div className="mem-empty">{tr("common.loading")}</div>
                  ) : searchContext.lines.length === 0 ? (
                    <div className="mem-empty">{tr("history.previewEmpty")}</div>
                  ) : (
                    searchContext.lines.map((line) => (
                      <div className="hist-item" key={line.index}>
                        <div className="hist-item__meta"><span className="hist-item__badge">{line.role}</span></div>
                        <div className="hist-item__preview">{line.text}</div>
                      </div>
                    ))
                  )}
                </div>
              </>
            ) : preview ? (
              <>
              <div className="history-preview__head">
                <div className="history-preview__copy">
                  <div className="history-preview__title">{preview.title}</div>
                  <div className="history-preview__meta">{preview.meta}</div>
                </div>
                <div className="history-preview__actions">
                  {isTrash ? (
                    <>
                      <button className="btn btn--primary btn--small" type="button" disabled={!selectedSession || busy} onClick={restoreSelected}>
                        {tr("history.restore")}
                      </button>
                      <button className="btn btn--small btn--danger" type="button" disabled={!selectedSession || busy} onClick={purgeSelected}>
                        {actionConfirmPurge ? tr("history.confirmPurge") : tr("history.purge")}
                      </button>
                    </>
                  ) : (
                    <>
                      <button className="btn btn--primary btn--small" type="button" disabled={!selectedSession || running} onClick={openSelected}>
                        {tr("history.openSession")}
                      </button>
                      <button className="btn btn--small" type="button" disabled={!selectedSession || running} onClick={renameSelected}>
                        {tr("history.rename")}
                      </button>
                      {selectedSession && selectedVersions && (
                        <button
                          className="btn btn--small"
                          type="button"
                          onClick={inspectSelectedVersions}
                        >
                          <GitBranch size={13} /> {tr("recovery.inspectLineage")}
                        </button>
                      )}
                      <button
                        className="btn btn--small btn--danger"
                        type="button"
                        disabled={!selectedSession || running || selectedSession.current}
                        onClick={moveSelectedToTrash}
                      >
                        {actionConfirmDelete ? tr("history.confirmMoveToTrash") : tr("history.moveToTrash")}
                      </button>
                    </>
                  )}
                </div>
              </div>
              <div className="history-preview__body">
                {preview.loading ? (
                  <div className="mem-empty">{tr("common.loading")}</div>
                ) : preview.error ? (
                  <div className="mem-empty" role="alert">{m("loadFailed")}<button className="btn btn--small" onClick={() => { if (selectedSession) void loadPreview(selectedSession); }}>{m("retry")}</button></div>
                ) : previewItems.length === 0 ? (
                  <div className="mem-empty">{tr("history.previewEmpty")}</div>
                ) : (
                  <Transcript items={previewItems} onPrompt={() => {}} questionNavigator={false} rewindDisabled />
                )}
              </div>
              </>
            ) : (
              <div className="history-preview__empty">{tr("history.selectSession")}</div>
            )}
          </section>
            </div>
        <ContextMenu
          open={Boolean(menuSession)}
          point={menuPoint}
          items={sessionMenuItems}
          minWidth={220}
          ariaLabel={isTrash ? tr("history.trashSessionActions") : tr("history.historySessionActions")}
          onClose={closeHistoryMenus}
        />
        <ContextMenu
          open={Boolean(blankMenuPoint)}
          point={blankMenuPoint}
          items={trashBlankMenuItems}
          minWidth={220}
          ariaLabel={tr("history.trashActions")}
          onClose={closeHistoryMenus}
        />
      </div>
    </>);
  if (presentation === "page") return <div className="history-page" aria-busy={busy}>{content}</div>;
  return <div className="management-modal-backdrop history-modal-backdrop" data-state={status} onMouseDown={(e) => { if (e.target === e.currentTarget) requestClose(); }}>
    <section className="management-modal history-modal" data-state={status} aria-label={tr(isTrash ? "history.trashTitle" : "history.title")} onClick={(e) => e.stopPropagation()}>{content}</section>
  </div>;
}

// dayLabel buckets a timestamp into "Today", "Yesterday", or a locale date. It's
// module-level (not a component), so it uses the non-reactive translator; the
// panel re-renders on a locale switch via its parent, picking up the new strings.
function dayLabel(ms: number): string {
  const startOfDay = (d: Date) => new Date(d.getFullYear(), d.getMonth(), d.getDate()).getTime();
  const days = Math.round((startOfDay(new Date()) - startOfDay(new Date(ms))) / 86_400_000);
  if (days <= 0) return t("history.today");
  if (days === 1) return t("history.yesterday");
  return new Date(ms).toLocaleDateString();
}

function timeLabel(ms: number): string {
  return new Date(ms).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" });
}

function dateBucket(ms: number): Exclude<HistoryDateFilter, "all"> {
  const startOfDay = (d: Date) => new Date(d.getFullYear(), d.getMonth(), d.getDate()).getTime();
  const days = Math.round((startOfDay(new Date()) - startOfDay(new Date(ms))) / 86_400_000);
  if (days <= 0) return "today";
  if (days === 1) return "yesterday";
  return "older";
}

function sessionTimeForGrouping(s: SessionMeta, isTrash: boolean): number {
  return isTrash ? s.deletedAt || sessionActivityTime(s) : sessionActivityTime(s);
}

function sessionScope(s: SessionMeta): "project" | "global" {
  return s.scope === "project" ? "project" : "global";
}

function isChannelSession(s: SessionMeta): boolean {
  return s.kind === "channel" || s.sessionSource === "auto";
}

function sessionLocation(s: SessionMeta, tr: ReturnType<typeof useT>): string {
  if (isChannelSession(s)) {
    return [s.channelLabel || s.channel || tr("history.channel"), s.remoteId].filter(Boolean).join(" · ");
  }
  if (s.workspaceRoot) {
    const parts = s.workspaceRoot.split(/[\\/]/).filter(Boolean);
    return parts[parts.length - 1] || s.workspaceRoot;
  }
  return sessionScope(s) === "project" ? tr("history.filterProject") : tr("history.filterGlobal");
}

function sessionMetaLine(s: SessionMeta, tr: ReturnType<typeof useT>, isTrash = false): string {
  const time = timeLabel(isTrash ? s.deletedAt || sessionActivityTime(s) : sessionActivityTime(s));
  const suffix = isTrash && s.deletedAt ? ` · ${tr("history.deleted")}` : "";
  const prefix = isChannelSession(s) ? `${tr("history.channelReadOnly")} · ` : "";
  const turns = s.turnsState === "unknown"
    ? tr("history.indexing")
    : tr(s.turns === 1 ? "history.turnOne" : "history.turnOther", { n: s.turns });
  return `${prefix}${turns} · ${time}${suffix}`;
}

function previewMessagesToItems(messages: HistoryMessage[]): Item[] {
  return historyMessagesToItems(messages, "hp").items;
}
