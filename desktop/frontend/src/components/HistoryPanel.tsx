import { useCallback, useState } from "react";
import { Check, CheckSquare, Pencil, Square, Trash2, X } from "lucide-react";
import { t, useT } from "../lib/i18n";
import type { SessionMeta } from "../lib/types";

// HistoryPanel is the desktop session switcher: a right-side drawer listing saved
// sessions newest-first, grouped by day. Each row resumes on click, and carries
// rename (a custom display name) and delete actions on hover — the desktop
// analogue of managing conversations in Claude Code. The active session can't be
// deleted (auto-save would just recreate it).
//
// Selection mode: clicking "Select" in the header toggles a checkbox overlay on
// each row. The user can then batch-delete the selected sessions. The active
// session is never selectable.
export function HistoryPanel({
  sessions,
  onResume,
  onDelete,
  onDeleteBatch,
  onRename,
  onClose,
}: {
  sessions: SessionMeta[];
  onResume: (path: string) => void;
  onDelete: (path: string) => void;
  onDeleteBatch: (paths: string[]) => void;
  onRename: (path: string, title: string) => void;
  onClose: () => void;
}) {
  const tr = useT();
  const [editing, setEditing] = useState<string | null>(null);
  const [draft, setDraft] = useState("");
  const [confirming, setConfirming] = useState<string | null>(null);

  // Selection mode state.
  const [selecting, setSelecting] = useState(false);
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [batchConfirming, setBatchConfirming] = useState(false);

  const startRename = (s: SessionMeta) => {
    setConfirming(null);
    setEditing(s.path);
    setDraft(s.title || s.preview || "");
  };
  const commitRename = (path: string) => {
    onRename(path, draft.trim());
    setEditing(null);
  };

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
  const selectablePaths = sessions.filter((s) => !s.current).map((s) => s.path);
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
  for (const s of sessions) {
    const label = dayLabel(s.modTime);
    const last = groups[groups.length - 1];
    if (last && last.label === label) last.items.push(s);
    else groups.push({ label, items: [s] });
  }

  return (
    <div className="drawer-backdrop" onClick={onClose}>
      <aside className="drawer" onClick={(e) => e.stopPropagation()}>
        <header className="drawer__head">
          <div className="drawer__title">{tr("history.title")}</div>
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
            <button className="chip" onClick={onClose} title={tr("common.close")}>
              ✕
            </button>
          </div>
        </header>

        <div className="drawer__body">
          {sessions.length === 0 ? (
            <div className="mem-empty">{tr("history.empty")}</div>
          ) : (
            groups.map((g) => (
              <section className="mem-section" key={g.label}>
                <div className="mem-section__title">{g.label}</div>
                {g.items.map((s) => (
                  <div className={`hist-item${s.current ? " hist-item--current" : ""}${selecting && selected.has(s.path) ? " hist-item--selected" : ""}`} key={s.path}>
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
                      <button className="hist-item__main" onClick={() => (selecting && !s.current ? toggleSelect(s.path) : onResume(s.path))} title={s.path}>
                        <div className="hist-item__preview">{s.title || s.preview || tr("history.emptySession")}</div>
                        <div className="hist-item__meta">
                          {s.current && <span className="hist-item__badge">{tr("history.current")}</span>}
                          <span>{tr(s.turns === 1 ? "history.turnOne" : "history.turnOther", { n: s.turns })}</span>
                          <span>·</span>
                          <span>{timeLabel(s.modTime)}</span>
                        </div>
                      </button>
                    )}

                    {!selecting && editing !== s.path && (
                      <div className="hist-item__actions">
                        {confirming === s.path ? (
                          <>
                            <button
                              className="hist-act hist-act--danger"
                              title={tr("history.confirmDelete")}
                              onClick={() => {
                                onDelete(s.path);
                                setConfirming(null);
                              }}
                            >
                              <Check size={14} />
                            </button>
                            <button className="hist-act" title={tr("common.cancel")} onClick={() => setConfirming(null)}>
                              <X size={14} />
                            </button>
                          </>
                        ) : (
                          <>
                            <button className="hist-act" title={tr("history.rename")} onClick={() => startRename(s)}>
                              <Pencil size={13} />
                            </button>
                            {!s.current && (
                              <button className="hist-act" title={tr("common.delete")} onClick={() => setConfirming(s.path)}>
                                <Trash2 size={13} />
                              </button>
                            )}
                          </>
                        )}
                      </div>
                    )}
                  </div>
                ))}
              </section>
            ))
          )}
        </div>

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
      </aside>
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
