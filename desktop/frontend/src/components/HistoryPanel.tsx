import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import type { MouseEvent as ReactMouseEvent } from "react";
import { Archive, Pencil, Search, Trash2, RotateCcw } from "lucide-react";
import { t, useT } from "../lib/i18n";
import { sessionActivityTime } from "../lib/session";
import type { HistoryMessage, SessionMeta } from "../lib/types";
import { historyMessagesToItems, type Item } from "../lib/useController";
import { ResizableDrawer } from "./ResizableDrawer";
import { Tooltip } from "./Tooltip";
import { Transcript } from "./Transcript";
import { ContextMenu, contextMenuPointFromEvent, type ContextMenuItem, type ContextMenuPoint } from "./ContextMenu";

// HistoryPanel lists saved sessions newest-first. Idle clicks resume a session;
// running clicks load a read-only preview so the active stream keeps writing to
// the current controller/session.
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
  const [editing, setEditing] = useState<string | null>(null);
  const [draft, setDraft] = useState("");
  const [query, setQuery] = useState("");
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
        title: sessionDisplayTitle(s, tr("history.emptySession")),
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

  const filteredSessions = useMemo(() => {
    const q = query.trim().toLowerCase();
    if (!q) return sessions;
    return sessions.filter((s) =>
      [s.title, s.preview, s.path].some((part) => (part ?? "").toLowerCase().includes(q)),
    );
  }, [query, sessions]);

  // Sessions arrive newest-first; bucket consecutive ones under a day heading
  // (Today / Yesterday / a date) while preserving that order.
  const groups: { label: string; items: SessionMeta[] }[] = [];
  for (const s of filteredSessions) {
    const label = dayLabel(isTrash ? s.deletedAt || sessionActivityTime(s) : sessionActivityTime(s));
    const last = groups[groups.length - 1];
    if (last && last.label === label) last.items.push(s);
    else groups.push({ label, items: [s] });
  }

  useEffect(() => {
    if (!preview) return;
    if (!filteredSessions.some((s) => s.path === preview.path)) setPreview(null);
  }, [filteredSessions, preview]);

  useEffect(() => {
    setMenuSession(null);
    setMenuPoint(null);
    setBlankMenuPoint(null);
    setMenuConfirmTarget(null);
  }, [isTrash]);

  useEffect(() => {
    if (!running) return;
    setEditing(null);
    if (preview || filteredSessions.length === 0) return;
    const first = filteredSessions.find((s) => !s.current) ?? filteredSessions[0];
    void loadPreview(first);
  }, [filteredSessions, loadPreview, preview, running]);

  const previewItems = useMemo(() => previewMessagesToItems(preview?.messages ?? []), [preview?.messages]);
  const showPreview = preview !== null;
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
  const openTrashClearMenu = (event: ReactMouseEvent<HTMLElement>) => {
    if (!isTrash || sessions.length === 0) return;
    event.preventDefault();
    event.stopPropagation();
    const rect = event.currentTarget.getBoundingClientRect();
    setMenuSession(null);
    setMenuPoint(null);
    setMenuConfirmTarget({ kind: "clear" });
    setBlankMenuPoint({ left: rect.left, top: rect.bottom + 6 });
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

  return (
    <ResizableDrawer onClose={onClose} wide={showPreview || running}>
      <header className="drawer__head">
        <div>
          <div className="drawer__title">{tr(isTrash ? "history.trashTitle" : "history.title")}</div>
          {isTrash ? (
            <div className="drawer__summary">{tr("history.trashHint")}</div>
          ) : (
            running && <div className="drawer__summary">{tr("history.readOnlyHint")}</div>
          )}
        </div>
        <div className="drawer__actions">
          {isTrash && sessions.length > 0 && (
            <button
              className="chip history-clear"
              type="button"
              onClick={openTrashClearMenu}
            >
              {tr("history.clearTrash")}
            </button>
          )}
          <Tooltip label={tr("common.close")}>
            <button className="chip" onClick={onClose}>
              ✕
            </button>
          </Tooltip>
        </div>
      </header>

      <div
        className={`drawer__body history-drawer${showPreview ? " history-drawer--preview" : ""}`}
        onContextMenu={openTrashBlankMenu}
      >
        <div className={`history-list${isTrash ? " history-list--trash" : ""}`}>
          {sessions.length > 0 && (
            <label className="mem-search history-search">
              <Search size={13} />
              <input value={query} onChange={(e) => setQuery(e.target.value)} placeholder={tr("history.searchPlaceholder")} />
            </label>
          )}
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
                <div className="mem-section__title">{g.label}</div>
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
                          onClick={() => {
                            if (isTrash || running) void loadPreview(s);
                            else onResume(s);
                          }}
                        >
                          <div className="hist-item__preview">{sessionDisplayTitle(s, tr("history.emptySession"))}</div>
                          <div className="hist-item__meta">
                            {!isTrash && s.current && <span className="hist-item__badge">{tr("history.current")}</span>}
                            {!isTrash && !s.current && s.open && <span className="hist-item__badge">{tr("history.open")}</span>}
                            <span>{tr(s.turns === 1 ? "history.turnOne" : "history.turnOther", { n: s.turns })}</span>
                            <span>·</span>
                            <span>{timeLabel(isTrash ? s.deletedAt || sessionActivityTime(s) : sessionActivityTime(s))}</span>
                            {isTrash && s.deletedAt && (
                              <>
                                <span>·</span>
                                <span>{tr("history.deleted")}</span>
                              </>
                            )}
                            {!isTrash && running && (
                              <>
                                <span>·</span>
                                <span>{tr("history.preview")}</span>
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

        {showPreview && (
          <section className="history-preview">
            <div className="history-preview__head">
              <div className="history-preview__title">{preview.title}</div>
              <div className="history-preview__meta">{preview.meta}</div>
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
          </section>
        )}
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
    </ResizableDrawer>
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

function sessionDisplayTitle(s: SessionMeta, fallback: string): string {
  return s.title || s.preview || fallback;
}

function sessionMetaLine(s: SessionMeta, tr: ReturnType<typeof useT>, isTrash = false): string {
  const time = timeLabel(isTrash ? s.deletedAt || sessionActivityTime(s) : sessionActivityTime(s));
  const suffix = isTrash && s.deletedAt ? ` · ${tr("history.deleted")}` : "";
  return `${tr(s.turns === 1 ? "history.turnOne" : "history.turnOther", { n: s.turns })} · ${time}${suffix}`;
}

function previewMessagesToItems(messages: HistoryMessage[]): Item[] {
  return historyMessagesToItems(messages, "hp").items;
}
