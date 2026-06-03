import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Check, CheckSquare, Pencil, Search, Square, Trash2, X } from "lucide-react";
import { t, useT } from "../lib/i18n";
import { sessionActivityTime } from "../lib/session";
import type { HistoryMessage, SessionMeta } from "../lib/types";
import type { Item } from "../lib/useController";
import { ResizableDrawer } from "./ResizableDrawer";
import { Tooltip } from "./Tooltip";
import { Transcript } from "./Transcript";

// HistoryPanel lists saved sessions newest-first. Idle clicks resume a session;
// running clicks load a read-only preview so the active stream keeps writing to
// the current controller/session. Selection mode lets the user batch-delete
// multiple sessions at once; the active session is never selectable.
export function HistoryPanel({
  sessions,
  running,
  onResume,
  onPreview,
  onDelete,
  onDeleteBatch,
  onRename,
  onClose,
}: {
  sessions: SessionMeta[];
  running: boolean;
  onResume: (path: string) => void;
  onPreview: (path: string) => Promise<HistoryMessage[]>;
  onDelete: (path: string) => void;
  onDeleteBatch: (paths: string[]) => void;
  onRename: (path: string, title: string) => void;
  onClose: () => void;
}) {
  const tr = useT();
  const [editing, setEditing] = useState<string | null>(null);
  const [draft, setDraft] = useState("");
  const [confirming, setConfirming] = useState<string | null>(null);
  const [query, setQuery] = useState("");
  const [preview, setPreview] = useState<{
    path: string;
    title: string;
    meta: string;
    messages: HistoryMessage[];
    loading: boolean;
  } | null>(null);
  const previewSeq = useRef(0);

  // Selection mode state.
  const [selecting, setSelecting] = useState(false);
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [batchConfirming, setBatchConfirming] = useState(false);

  const startRename = (s: SessionMeta) => {
    if (running) return;
    setConfirming(null);
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
      setConfirming(null);
      setPreview({
        path: s.path,
        title: sessionDisplayTitle(s, tr("history.emptySession")),
        meta: sessionMetaLine(s, tr),
        messages: [],
        loading: true,
      });
      const messages = await onPreview(s.path);
      if (seq === previewSeq.current) {
        setPreview((cur) => (cur?.path === s.path ? { ...cur, messages, loading: false } : cur));
      }
    },
    [onPreview, tr],
  );

  const filteredSessions = useMemo(() => {
    const q = query.trim().toLowerCase();
    if (!q) return sessions;
    return sessions.filter((s) =>
      [s.title, s.preview, s.path].some((part) => (part ?? "").toLowerCase().includes(q)),
    );
  }, [query, sessions]);

  // Toggle a single session's selection. The active session is never selectable.
  const toggleSelect = useCallback((path: string) => {
    setSelected((prev) => {
      const next = new Set(prev);
      if (next.has(path)) next.delete(path);
      else next.add(path);
      return next;
    });
  }, []);

  // Select/deselect all non-current sessions.
  const selectablePaths = filteredSessions.filter((s) => !s.current).map((s) => s.path);
  const allSelected = selectablePaths.length > 0 && selectablePaths.every((p) => selected.has(p));
  const toggleAll = useCallback(() => {
    if (allSelected) setSelected(new Set());
    else setSelected(new Set(selectablePaths));
  }, [allSelected, selectablePaths]);

  // Enter selection mode clears any pending single-delete confirmation.
  const enterSelect = () => {
    setSelecting(true);
    setSelected(new Set());
    setBatchConfirming(false);
    setConfirming(null);
    setEditing(null);
  };
  const exitSelect = () => {
    setSelecting(false);
    setSelected(new Set());
    setBatchConfirming(false);
  };

  const doBatchDelete = () => {
    const paths = Array.from(selected);
    if (paths.length === 0) return;
    onDeleteBatch(paths);
    exitSelect();
  };

  // Sessions arrive newest-first; bucket consecutive ones under a day heading
  // (Today / Yesterday / a date) while preserving that order.
  const groups: { label: string; items: SessionMeta[] }[] = [];
  for (const s of filteredSessions) {
    const label = dayLabel(sessionActivityTime(s));
    const last = groups[groups.length - 1];
    if (last && last.label === label) last.items.push(s);
    else groups.push({ label, items: [s] });
  }

  useEffect(() => {
    if (!preview) return;
    if (!filteredSessions.some((s) => s.path === preview.path)) setPreview(null);
  }, [filteredSessions, preview]);

  useEffect(() => {
    if (!running) return;
    setEditing(null);
    setConfirming(null);
    if (preview || filteredSessions.length === 0) return;
    const first = filteredSessions.find((s) => !s.current) ?? filteredSessions[0];
    void loadPreview(first);
  }, [filteredSessions, loadPreview, preview, running]);

  const previewItems = useMemo(() => previewMessagesToItems(preview?.messages ?? []), [preview?.messages]);
  const showPreview = preview !== null;

  return (
    <ResizableDrawer onClose={onClose} wide={showPreview || running}>
      <header className="drawer__head">
        <div>
          <div className="drawer__title">{tr("history.title")}</div>
          {running && <div className="drawer__summary">{tr("history.readOnlyHint")}</div>}
        </div>
        <div className="drawer__head-actions">
          {selecting ? (
            <button className="chip chip--active" onClick={exitSelect} title={tr("history.selectDone")}>
              {tr("history.selectDone")}
            </button>
          ) : (
            sessions.length > 0 && (
              <button className="chip" onClick={enterSelect} title={tr("history.select")}>
                {tr("history.select")}
              </button>
            )
          )}
          <Tooltip label={tr("common.close")}>
            <button className="chip" onClick={onClose}>
              ✕
            </button>
          </Tooltip>
        </div>
      </header>

      <div className={`drawer__body history-drawer${showPreview ? " history-drawer--preview" : ""}`}>
        <div className="history-list">
          {sessions.length > 0 && (
            <label className="mem-search history-search">
              <Search size={13} />
              <input value={query} onChange={(e) => setQuery(e.target.value)} placeholder={tr("history.searchPlaceholder")} />
            </label>
          )}
          {sessions.length === 0 ? (
            <div className="mem-empty">{tr("history.empty")}</div>
          ) : filteredSessions.length === 0 ? (
            <div className="mem-empty">{tr("history.noResults")}</div>
          ) : (
            groups.map((g) => (
              <section className="mem-section" key={g.label}>
                <div className="mem-section__title">{g.label}</div>
                {g.items.map((s) => {
                  const previewing = preview?.path === s.path;
                  return (
                    <div
                      className={`hist-item${s.current ? " hist-item--current" : ""}${previewing ? " hist-item--selected" : ""}${selecting && selected.has(s.path) ? " hist-item--checked" : ""}`}
                      key={s.path}
                    >
                      {selecting && (
                        <button
                          className="hist-item__check"
                          onClick={() => toggleSelect(s.path)}
                          disabled={s.current}
                          title={s.current ? tr("history.current") : undefined}
                        >
                          {s.current ? (
                            <span className="hist-item__check-lock">—</span>
                          ) : selected.has(s.path) ? (
                            <CheckSquare size={16} />
                          ) : (
                            <Square size={16} />
                          )}
                        </button>
                      )}

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
                            if (selecting && !s.current) toggleSelect(s.path);
                            else if (running) void loadPreview(s);
                            else onResume(s.path);
                          }}
                        >
                          <div className="hist-item__preview">{sessionDisplayTitle(s, tr("history.emptySession"))}</div>
                          <div className="hist-item__meta">
                            {s.current && <span className="hist-item__badge">{tr("history.current")}</span>}
                            <span>{tr(s.turns === 1 ? "history.turnOne" : "history.turnOther", { n: s.turns })}</span>
                            <span>·</span>
                            <span>{timeLabel(sessionActivityTime(s))}</span>
                            {running && !selecting && (
                              <>
                                <span>·</span>
                                <span>{tr("history.preview")}</span>
                              </>
                            )}
                          </div>
                        </button>
                      )}

                      {!selecting && editing !== s.path && (
                        <div className="hist-item__actions">
                          {confirming === s.path ? (
                            <>
                              <Tooltip label={tr("history.confirmDelete")}>
                                <button
                                  className="hist-act hist-act--danger"
                                  disabled={running}
                                  onClick={() => {
                                    if (running) return;
                                    onDelete(s.path);
                                    setConfirming(null);
                                  }}
                                >
                                  <Check size={14} />
                                </button>
                              </Tooltip>
                              <Tooltip label={tr("common.cancel")}>
                                <button className="hist-act" onClick={() => setConfirming(null)}>
                                  <X size={14} />
                                </button>
                              </Tooltip>
                            </>
                          ) : (
                            <>
                              <Tooltip label={tr("history.rename")}>
                                <button
                                  className="hist-act"
                                  disabled={running}
                                  onClick={() => startRename(s)}
                                >
                                  <Pencil size={13} />
                                </button>
                              </Tooltip>
                              {!s.current && (
                                <Tooltip label={tr("common.delete")}>
                                  <button
                                    className="hist-act"
                                    disabled={running}
                                    onClick={() => setConfirming(s.path)}
                                  >
                                    <Trash2 size={13} />
                                  </button>
                                </Tooltip>
                              )}
                            </>
                          )}
                        </div>
                      )}
                    </div>
                  );
                })}
              </section>
            ))
          )}
        </div>

        {showPreview && !selecting && (
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
                <Transcript items={previewItems} onPrompt={() => {}} />
              )}
            </div>
          </section>
        )}

        {selecting && (
          <footer className="drawer__footer">
            <button className="drawer__footer-btn" onClick={toggleAll}>
              {allSelected ? tr("history.deselectAll") : tr("history.selectAll")}
            </button>
            {selected.size > 0 &&
              (batchConfirming ? (
                <div className="drawer__footer-confirm">
                  <span className="drawer__footer-prompt">{tr("history.confirmDeleteSelected", { n: selected.size })}</span>
                  <button className="btn btn--danger btn--sm" onClick={doBatchDelete}>
                    <Check size={14} />
                  </button>
                  <button className="btn btn--sm" onClick={() => setBatchConfirming(false)}>
                    <X size={14} />
                  </button>
                </div>
              ) : (
                <button className="btn btn--danger btn--sm" onClick={() => setBatchConfirming(true)}>
                  <Trash2 size={14} />
                  {tr("history.deleteSelected", { n: selected.size })}
                </button>
              ))}
          </footer>
        )}
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

function sessionMetaLine(s: SessionMeta, tr: ReturnType<typeof useT>): string {
  return `${tr(s.turns === 1 ? "history.turnOne" : "history.turnOther", { n: s.turns })} · ${timeLabel(sessionActivityTime(s))}`;
}

function previewMessagesToItems(messages: HistoryMessage[]): Item[] {
  return messages
    .filter(
      (m) =>
        (m.role === "user" && m.content.trim() !== "") ||
        (m.role === "assistant" && (m.content.trim() !== "" || (m.reasoning ?? "").trim() !== "")),
    )
    .map((m, i) =>
      m.role === "user"
        ? { kind: "user", id: `hp${i}`, text: m.content }
        : { kind: "assistant", id: `hp${i}`, text: m.content, reasoning: m.reasoning ?? "", streaming: false },
    );
}
