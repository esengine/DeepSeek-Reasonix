import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Pencil, Search, Trash2, Check, X } from "lucide-react";
import { t, useT } from "../lib/i18n";
import { sessionActivityTime } from "../lib/session";
import type { HistoryMessage, SessionMeta } from "../lib/types";
import type { Item } from "../lib/useController";
import { ResizableDrawer } from "./ResizableDrawer";
import { Tooltip } from "./Tooltip";
import { Transcript } from "./Transcript";

// HistoryPanel lists saved sessions newest-first. Idle clicks resume a session;
// running clicks load a read-only preview so the active stream keeps writing to
// the current controller/session.
export function HistoryPanel({
  sessions,
  running,
  onResume,
  onPreview,
  onDelete,
  onRename,
  onClose,
}: {
  sessions: SessionMeta[];
  running: boolean;
  onResume: (path: string) => void;
  onPreview: (path: string) => Promise<HistoryMessage[]>;
  onDelete: (path: string) => void;
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
        <Tooltip label={tr("common.close")}>
          <button className="chip" onClick={onClose}>
            ✕
          </button>
        </Tooltip>
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
                  const selected = preview?.path === s.path;
                  return (
                    <div
                      className={`hist-item${s.current ? " hist-item--current" : ""}${selected ? " hist-item--selected" : ""}`}
                      key={s.path}
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
                            if (running) void loadPreview(s);
                            else onResume(s.path);
                          }}
                        >
                          <div className="hist-item__preview">{sessionDisplayTitle(s, tr("history.emptySession"))}</div>
                          <div className="hist-item__meta">
                            {s.current && <span className="hist-item__badge">{tr("history.current")}</span>}
                            <span>{tr(s.turns === 1 ? "history.turnOne" : "history.turnOther", { n: s.turns })}</span>
                            <span>·</span>
                            <span>{timeLabel(sessionActivityTime(s))}</span>
                            {running && (
                              <>
                                <span>·</span>
                                <span>{tr("history.preview")}</span>
                              </>
                            )}
                          </div>
                        </button>
                      )}

                      {editing !== s.path && (
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
                <Transcript items={previewItems} onPrompt={() => {}} />
              )}
            </div>
          </section>
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
  // Build a lookup: toolCallId → tool result content for filling in tool cards.
  const resultMap = new Map<string, string>();
  for (const m of messages) {
    if (m.role === "tool" && m.toolCallId && !resultMap.has(m.toolCallId)) {
      resultMap.set(m.toolCallId, m.content);
    }
  }

  const items: Item[] = [];
  let seq = 0;
  for (const m of messages) {
    if (m.role === "system" || m.role === "tool") continue;
    if (m.role === "user") {
      if (m.content.trim() === "") continue;
      items.push({ kind: "user", id: `hp${seq}`, text: m.content });
      seq++;
      continue;
    }
    if (m.role === "assistant") {
      const hasText = m.content.trim() !== "" || (m.reasoning ?? "").trim() !== "";
      const toolCalls = m.toolCalls ?? [];
      if (hasText) {
        items.push({
          kind: "assistant",
          id: `hp${seq}`,
          text: m.content,
          reasoning: m.reasoning ?? "",
          streaming: false,
        });
        seq++;
      }
      for (const tc of toolCalls) {
        const output = resultMap.get(tc.id) ?? "";
        const hasError = output.startsWith("[error") || output.startsWith("Error:");
        items.push({
          kind: "tool",
          id: tc.id || `hpt${seq}`,
          name: tc.name,
          args: tc.arguments ?? "",
          readOnly: false,
          status: hasError ? "error" : "done",
          output,
          error: hasError ? output : undefined,
        });
        seq++;
      }
    }
  }
  return items;
}
