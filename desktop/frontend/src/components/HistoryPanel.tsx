import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import type { MouseEvent as ReactMouseEvent } from "react";
import { Archive, Pencil, Search, Trash2, RotateCcw } from "lucide-react";
import { t, useT } from "../lib/i18n";
import { historySessionDisplayTitle, sessionActivityTime } from "../lib/session";
import type { HistoryMessage, SessionMeta } from "../lib/types";
import { historyMessagesToItems, type Item } from "../lib/useController";
import { Transcript } from "./Transcript";
import { ContextMenu, contextMenuPointFromEvent, type ContextMenuItem, type ContextMenuPoint } from "./ContextMenu";
import { useDeferredClose } from "../lib/useMountTransition";
import { ModalCloseButton } from "./ModalCloseButton";

type HistoryScopeFilter = "all" | "project" | "global";
type HistoryStatusFilter = "all" | "current" | "open";
type HistoryDateFilter = "all" | "today" | "yesterday" | "older";

// HistoryPanel lists saved sessions newest-first. In the wide management modal,
// a single click selects a read-only preview; explicit actions resume, restore,
// rename, or delete the selected session.
export function HistoryPanel({
  kind = "history",
  sessions,
  running,
  onResume,
  onPreview,
  onDelete,
  onRename,
  onRestore,
  onPurge,
  onPurgeAll,
  onClose,
}: {
  kind?: "history" | "trash";
  sessions: SessionMeta[];
  running: boolean;
  onResume: (session: SessionMeta) => void;
  onPreview: (path: string) => Promise<HistoryMessage[]>;
  onDelete: (path: string) => void;
  onRename: (path: string, title: string) => void;
  onRestore?: (path: string) => void;
  onPurge?: (path: string) => void;
  onPurgeAll?: (paths: string[]) => void;
  onClose: () => void;
}) {
  const tr = useT();
  const isTrash = kind === "trash";
  // Play the modal exit animation, then let the parent unmount us.
  const { status, requestClose } = useDeferredClose(onClose, 240);
  const [editing, setEditing] = useState<string | null>(null);
  const [draft, setDraft] = useState("");
  const [query, setQuery] = useState("");
  const [scopeFilter, setScopeFilter] = useState<HistoryScopeFilter>("all");
  const [statusFilter, setStatusFilter] = useState<HistoryStatusFilter>("all");
  const [dateFilter, setDateFilter] = useState<HistoryDateFilter>("all");
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
  } | null>(null);
  const previewSeq = useRef(0);

  const startRename = (s: SessionMeta) => {
    if (running) return;
    setEditing(s.path);
    setDraft(s.title || s.preview || "");
  };
  const commitRename = (path: string) => {
    if (running) return;
    onRename(path, draft.trim());
    setEditing(null);
  };
  const loadPreview = useCallback(
    async (s: SessionMeta) => {
      const seq = ++previewSeq.current;
      setEditing(null);
      setPreview({
        path: s.path,
        title: historySessionDisplayTitle(s, tr("history.emptySession")),
        meta: sessionMetaLine(s, tr, isTrash),
        messages: [],
        loading: true,
      });
      const messages = await onPreview(s.path);
      if (seq === previewSeq.current) {
        setPreview((cur) => (cur?.path === s.path ? { ...cur, messages, loading: false } : cur));
      }
    },
    [isTrash, onPreview, tr],
  );

  const scopeCounts = useMemo(
    () => ({
      all: sessions.length,
      project: sessions.filter((s) => sessionScope(s) === "project").length,
      global: sessions.filter((s) => sessionScope(s) === "global").length,
    }),
    [sessions],
  );
  const statusCounts = useMemo(
    () => ({
      all: sessions.length,
      current: sessions.filter((s) => s.current).length,
      open: sessions.filter((s) => s.open && !s.current).length,
    }),
    [sessions],
  );
  const dateCounts = useMemo(() => {
    const counts: Record<HistoryDateFilter, number> = { all: sessions.length, today: 0, yesterday: 0, older: 0 };
    for (const s of sessions) counts[dateBucket(sessionTimeForGrouping(s, isTrash))]++;
    return counts;
  }, [isTrash, sessions]);

  useEffect(() => {
    if (scopeFilter === "project" && scopeCounts.project === 0) setScopeFilter("all");
    if (scopeFilter === "global" && scopeCounts.global === 0) setScopeFilter("all");
  }, [scopeCounts.global, scopeCounts.project, scopeFilter]);

  useEffect(() => {
    if (isTrash) return;
    if (statusFilter === "current" && statusCounts.current === 0) setStatusFilter("all");
    if (statusFilter === "open" && statusCounts.open === 0) setStatusFilter("all");
  }, [isTrash, statusCounts.current, statusCounts.open, statusFilter]);

  useEffect(() => {
    if (dateFilter !== "all" && dateCounts[dateFilter] === 0) setDateFilter("all");
  }, [dateCounts, dateFilter]);

  const filteredSessions = useMemo(() => {
    const q = query.trim().toLowerCase();
    return sessions.filter((s) => {
      if (scopeFilter !== "all" && sessionScope(s) !== scopeFilter) return false;
      if (!isTrash && statusFilter === "current" && !s.current) return false;
      if (!isTrash && statusFilter === "open" && (!s.open || s.current)) return false;
      if (dateFilter !== "all" && dateBucket(sessionTimeForGrouping(s, isTrash)) !== dateFilter) return false;
      if (!q) return true;
      return [s.title, s.preview, s.path, s.topicTitle, s.workspaceRoot].some((part) => (part ?? "").toLowerCase().includes(q));
    });
  }, [dateFilter, isTrash, query, scopeFilter, sessions, statusFilter]);

  // Sessions arrive newest-first; bucket consecutive ones under a day heading
  // (Today / Yesterday / a date) while preserving that order.
  const groups: { label: string; items: SessionMeta[] }[] = [];
  for (const s of filteredSessions) {
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
    setEditing(null);
    if (filteredSessions.length === 0) {
      if (preview) setPreview(null);
      return;
    }
    if (preview && filteredSessions.some((s) => s.path === preview.path)) return;
    const first = filteredSessions.find((s) => !s.current) ?? filteredSessions[0];
    void loadPreview(first);
  }, [filteredSessions, loadPreview, preview]);

  const previewItems = useMemo(() => previewMessagesToItems(preview?.messages ?? []), [preview?.messages]);
  const selectedSession = useMemo(
    () => (preview ? filteredSessions.find((s) => s.path === preview.path) ?? null : null),
    [filteredSessions, preview],
  );
  const openSessionMenu = (event: ReactMouseEvent<HTMLElement>, s: SessionMeta) => {
    event.preventDefault();
    event.stopPropagation();
    setMenuConfirmTarget(null);
    setBlankMenuPoint(null);
    setMenuSession(s);
    setMenuPoint(contextMenuPointFromEvent(event));
  };
  const openTrashBlankMenu = (event: ReactMouseEvent<HTMLDivElement>) => {
    if (!isTrash || sessions.length === 0) return;
    const target = event.target as HTMLElement | null;
    if (target?.closest(".hist-item,.history-search,.history-preview,button,input,textarea,select")) return;
    event.preventDefault();
    setMenuConfirmTarget(null);
    setMenuSession(null);
    setMenuPoint(null);
    setBlankMenuPoint(contextMenuPointFromEvent(event));
  };
  const armClearTrash = () => {
    if (!isTrash || sessions.length === 0) return;
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
    onPurge?.(s.path);
  };
  const clearTrash = () => {
    const paths = sessions.map((s) => s.path);
    closeHistoryMenus();
    onPurgeAll?.(paths);
  };
  const sessionMenuItems: ContextMenuItem[] = menuSession
    ? isTrash
      ? [
        {
          key: "restore",
          icon: <RotateCcw size={13} />,
          label: tr("history.restoreSession"),
          onSelect: () => {
            onRestore?.(menuSession.path);
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
            if (menuConfirmTarget?.kind === "purge" && menuConfirmTarget.path === menuSession.path) {
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
            onSelect: () => setMenuConfirmTarget({ kind: "clear" }),
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
    onRestore?.(selectedSession.path);
  };
  const purgeSelected = () => {
    if (!selectedSession || !isTrash) return;
    if (actionConfirmPurge) purgeTrashSession(selectedSession);
    else setMenuConfirmTarget({ kind: "purge", path: selectedSession.path });
  };

  return (
    <div className="management-modal-backdrop history-modal-backdrop" data-state={status} onClick={(e) => { if (e.target === e.currentTarget) requestClose(); }}>
      <section
        className="management-modal history-modal"
        data-state={status}
        aria-label={tr(isTrash ? "history.trashTitle" : "history.title")}
        onClick={(e) => e.stopPropagation()}
      >
      <header className="management-modal__head history-modal__head">
        <div>
          <div className="management-modal__title history-modal__title">{tr(isTrash ? "history.trashTitle" : "history.title")}</div>
          {!isTrash && running && <div className="management-modal__summary history-modal__summary">{tr("history.readOnlyHint")}</div>}
        </div>
        <div className="management-modal__actions history-modal__actions">
          {isTrash && sessions.length > 0 && (
            <button
              className={`chip history-clear${actionConfirmClearTrash ? " history-clear--confirm" : ""}`}
              type="button"
              onClick={actionConfirmClearTrash ? clearTrash : armClearTrash}
            >
              {tr(actionConfirmClearTrash ? "history.confirmClearTrash" : "history.clearTrash")}
            </button>
          )}
          <ModalCloseButton label={tr("common.close")} onClick={requestClose} />
        </div>
      </header>

      <div
        className="history-manager"
        onContextMenu={openTrashBlankMenu}
      >
        <div className="history-toolbar" aria-label={tr("history.filters")}>
          {sessions.length > 0 && (
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

        <div className="history-content">
          <div className={`history-list${isTrash ? " history-list--trash" : ""}`}>
            {sessions.length === 0 ? (
              <div className={`mem-empty${isTrash ? " mem-empty--trash" : ""}`}>
                {isTrash && <Trash2 size={22} />}
                <span>{tr(isTrash ? "history.trashEmpty" : "history.empty")}</span>
              </div>
            ) : filteredSessions.length === 0 ? (
              <div className="mem-empty">{tr("history.noResults")}</div>
            ) : (
              groups.map((g) => (
                <section className="mem-section" key={g.label}>
                  <div className="mem-section__title hist-group__title">
                    <span>{g.label}</span>
                    <span className="hist-group__count">{g.items.length}</span>
                  </div>
                  {g.items.map((s) => {
                    const selected = preview?.path === s.path;
                    return (
                      <div
                        className={`hist-item${s.current ? " hist-item--current" : ""}${selected ? " hist-item--selected" : ""}`}
                        key={s.path}
                        onContextMenu={(event) => openSessionMenu(event, s)}
                      >
                        {editing === s.path ? (
                          <input
                            className="hist-item__rename"
                            autoFocus
                            value={draft}
                            onChange={(e) => setDraft(e.target.value)}
                            onKeyDown={(e) => {
                              if (e.key === "Enter") commitRename(s.path);
                              if (e.key === "Escape") setEditing(null);
                            }}
                            onBlur={() => commitRename(s.path)}
                            placeholder={tr("history.namePlaceholder")}
                          />
                        ) : (
                          <button
                            className="hist-item__main"
                            aria-pressed={selected}
                            onClick={() => {
                              setMenuConfirmTarget(null);
                              void loadPreview(s);
                            }}
                            onDoubleClick={() => {
                              if (!isTrash && !running) onResume(s);
                            }}
                          >
                            <div className="hist-item__preview">{historySessionDisplayTitle(s, tr("history.emptySession"))}</div>
                            <div className="hist-item__meta">
                              {!isTrash && isChannelSession(s) && <span className="hist-item__badge hist-item__badge--open">{tr("history.channel")}</span>}
                              {!isTrash && s.current && <span className="hist-item__badge hist-item__badge--current">{tr("history.current")}</span>}
                              {!isTrash && !s.current && s.open && <span className="hist-item__badge hist-item__badge--open">{tr("history.open")}</span>}
                              {isTrash && <span className="hist-item__badge hist-item__badge--deleted">{tr("history.deleted")}</span>}
                              {s.recovered && <span className="hist-item__badge">{tr("recovery.badge")}</span>}
                              {sessionLocation(s, tr) && <span className="hist-item__scope">{sessionLocation(s, tr)}</span>}
                              <span className="hist-item__metaspacer" />
                              <span className="hist-item__stat">{tr(s.turns === 1 ? "history.turnOne" : "history.turnOther", { n: s.turns })}</span>
                              <span className="hist-item__dot">·</span>
                              <span className="hist-item__stat">{timeLabel(isTrash ? s.deletedAt || sessionActivityTime(s) : sessionActivityTime(s))}</span>
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
                  })}
                </section>
              ))
            )}
          </div>

          <section className={`history-preview${!preview ? " history-preview--empty" : ""}`}>
            {preview ? (
              <>
              <div className="history-preview__head">
                <div className="history-preview__copy">
                  <div className="history-preview__title">{preview.title}</div>
                  <div className="history-preview__meta">{preview.meta}</div>
                </div>
                <div className="history-preview__actions">
                  {isTrash ? (
                    <>
                      <button className="btn btn--primary btn--small" type="button" disabled={!selectedSession} onClick={restoreSelected}>
                        {tr("history.restore")}
                      </button>
                      <button className="btn btn--small btn--danger" type="button" disabled={!selectedSession} onClick={purgeSelected}>
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
                ) : previewItems.length === 0 ? (
                  <div className="mem-empty">{tr("history.previewEmpty")}</div>
                ) : (
                  <Transcript items={previewItems} onPrompt={() => {}} questionNavigator={false} />
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
      </section>
    </div>
  );
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
  return `${prefix}${tr(s.turns === 1 ? "history.turnOne" : "history.turnOther", { n: s.turns })} · ${time}${suffix}`;
}

function previewMessagesToItems(messages: HistoryMessage[]): Item[] {
  return historyMessagesToItems(messages, "hp").items;
}

function HistoryFilterSelect({
  label,
  options,
  value,
  onChange,
}: {
  label: string;
  options: { id: string; label: string; count: number }[];
  value: string;
  onChange: (next: string) => void;
}) {
  const visibleOptions = options.filter((option) => option.id === "all" || option.id === value || option.count > 0);
  return (
    <div className="history-filter" role="group" aria-label={label}>
      {visibleOptions.map((option) => (
        <button
          key={option.id}
          type="button"
          className={`history-filter__pill${value === option.id ? " history-filter__pill--on" : ""}`}
          aria-pressed={value === option.id}
          disabled={option.id !== "all" && option.count === 0}
          onClick={() => onChange(option.id)}
        >
          {option.label}
          <span className="history-filter__count">{option.count}</span>
        </button>
      ))}
    </div>
  );
}
