import { useEffect, useLayoutEffect, useRef, useState } from "react";
import { open as openFileDialog } from "@tauri-apps/plugin-dialog";
import type { SessionInfo } from "../App";
import { t, useLang } from "../i18n";
import { I } from "../icons";
import type { ExternalSessionApp, ExternalSessionSource } from "../protocol";
import { Shortcut } from "./shortcut";

const RENAME_MAX_CHARS = 200;

type PendingDelete = {
  name: string;
  pretty: string;
  x: number;
  y: number;
};

type PendingImport = {
  x: number;
  y: number;
};

type ImportSource = ExternalSessionSource;

function prettyName(s: SessionInfo): string {
  if (s.summary && s.summary.trim()) return s.summary.trim();
  const m = s.name.match(/^desktop-(\d{4})(\d{2})(\d{2})(\d{2})(\d{2})(?:-(\d+))?$/);
  if (m) {
    const [, , month, day, hh, mm, tab] = m;
    return `${t("sidebarPanel.sessionTitle", {
      month,
      day,
      hour: hh,
      minute: mm,
    })}${tab && tab !== "1" ? ` · #${tab}` : ""}`;
  }
  return s.name.replace(/^desktop-/, "").replace(/[-_]+/g, " ");
}

function relative(ms: number): string {
  const min = ms / 60_000;
  if (min < 1) return t("sidebarPanel.justNow");
  if (min < 60) return t("sidebarPanel.minutesAgo", { n: Math.floor(min) });
  const hr = min / 60;
  if (hr < 24) return t("sidebarPanel.hoursAgo", { n: Math.floor(hr) });
  const d = hr / 24;
  if (d < 7) return t("sidebarPanel.daysAgo", { n: Math.floor(d) });
  return t("sidebarPanel.weeksAgo", { n: Math.floor(d / 7) });
}

export function Sidebar({
  sessions,
  importSources,
  activeName,
  onNewChat,
  onLoadSession,
  onDeleteSession,
  onRenameSession,
  onRefreshImportSources,
  onImportDetectedSessions,
  onImportSession,
  onOpenSettings,
  onOpenRules,
  onOpenCommands,
  onOpenAbout,
}: {
  sessions: SessionInfo[];
  importSources: ExternalSessionApp[];
  activeName?: string;
  onNewChat: () => void;
  onLoadSession: (name: string) => void;
  onDeleteSession: (name: string) => void;
  onRenameSession: (name: string, title: string) => void;
  onRefreshImportSources: () => void;
  onImportDetectedSessions: (sources: ImportSource[]) => void;
  onImportSession: (payload: { source: ImportSource; path: string; name?: string }) => void;
  onOpenSettings: () => void;
  onOpenRules: () => void;
  onOpenCommands: () => void;
  onOpenAbout: () => void;
}) {
  useLang();
  const [query, setQuery] = useState("");
  const [pendingDelete, setPendingDelete] = useState<PendingDelete | null>(null);
  const [pendingImport, setPendingImport] = useState<PendingImport | null>(null);
  const [editingName, setEditingName] = useState<string | null>(null);
  const [editValue, setEditValue] = useState("");
  const filtered = query
    ? sessions.filter((s) => {
        const q = query.toLowerCase();
        return (
          prettyName(s).toLowerCase().includes(q) || s.name.toLowerCase().includes(q)
        );
      })
    : sessions;

  useEffect(() => {
    if (!pendingDelete && !pendingImport) return;
    const onMouseDown = (e: MouseEvent) => {
      const target = e.target as HTMLElement | null;
      if (!target?.closest(".session-delete-popover")) setPendingDelete(null);
      if (!target?.closest(".session-import-popover")) setPendingImport(null);
    };
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") setPendingDelete(null);
      if (e.key === "Escape") setPendingImport(null);
    };
    window.addEventListener("mousedown", onMouseDown);
    window.addEventListener("keydown", onKey);
    return () => {
      window.removeEventListener("mousedown", onMouseDown);
      window.removeEventListener("keydown", onKey);
    };
  }, [pendingDelete, pendingImport]);

  return (
    <aside className="sidebar">
      <div className="side-head">
        <button type="button" className="new-btn" onClick={onNewChat}>
          <I.plus size={14} />
          <span>{t("sidebarPanel.newChat")}</span>
          <Shortcut keys={["mod", "N"]} />
        </button>
        <button
          type="button"
          className="icon-btn"
          title={t("sidebarPanel.importSessions")}
          onClick={(e) => {
            const rect = e.currentTarget.getBoundingClientRect();
            onRefreshImportSources();
            setPendingImport({ x: rect.right, y: rect.bottom });
          }}
        >
          <I.upload size={14} />
        </button>
        <button
          type="button"
          className="icon-btn"
          title={t("sidebarPanel.commandPalette")}
          onClick={onOpenCommands}
        >
          <I.history size={14} />
        </button>
      </div>

      <div className="search-row">
        <div className="input">
          <I.search size={13} />
          <input
            placeholder={t("sidebarPanel.searchSessions")}
            value={query}
            onChange={(e) => setQuery(e.target.value)}
          />
          <Shortcut keys={["mod", "K"]} />
        </div>
      </div>

      <div className="session-list">
        <div className="side-section">
          <div className="label">
            <span>{t("sidebarPanel.recent")}</span>
            <span className="count">{filtered.length}</span>
          </div>
          {sessions.length === 0 ? (
            <div
              style={{
                padding: "12px 8px",
                fontSize: 11,
                color: "var(--muted-2)",
                fontFamily: "Geist Mono, monospace",
              }}
            >
              {t("sidebarPanel.noSessions")}
            </div>
          ) : filtered.length === 0 ? (
            <div
              style={{
                padding: "12px 8px",
                fontSize: 11,
                color: "var(--muted-2)",
                fontFamily: "Geist Mono, monospace",
              }}
            >
              {t("sidebarPanel.noMatches")}
            </div>
          ) : null}
          {filtered.map((s) => {
            const active = s.name === activeName;
            const mtime = Date.parse(s.mtime);
            const updated = Number.isFinite(mtime) ? relative(Date.now() - mtime) : s.mtime;
            const editing = editingName === s.name;
            const currentSummary = s.summary?.trim() ?? "";
            const commitRename = () => {
              const next = editValue.trim().slice(0, RENAME_MAX_CHARS);
              if (next !== currentSummary) onRenameSession(s.name, next);
              setEditingName(null);
              setEditValue("");
            };
            return (
              <div
                key={s.name}
                className="session-item"
                data-active={active}
                data-editing={editing || undefined}
                onClick={
                  editing
                    ? undefined
                    : () => {
                        // Skip the round-trip when clicking the already-loaded
                        // session — a reload would clear live in-turn state (#1653).
                        if (s.name === activeName) return;
                        onLoadSession(s.name);
                      }
                }
                role={editing ? undefined : "button"}
                tabIndex={editing ? -1 : 0}
                title={s.name}
                onKeyDown={(e) => {
                  if (editing) return;
                  if (e.key === "Enter" && s.name !== activeName) onLoadSession(s.name);
                }}
              >
                <span
                  className="state"
                  style={{ background: active ? "var(--accent)" : "var(--border-strong)" }}
                />
                <div className="body">
                  {editing ? (
                    <input
                      className="title-edit"
                      autoFocus
                      value={editValue}
                      maxLength={RENAME_MAX_CHARS}
                      placeholder={t("sidebarPanel.renamePlaceholder")}
                      aria-label={t("sidebarPanel.renameSession")}
                      onClick={(e) => e.stopPropagation()}
                      onChange={(e) => setEditValue(e.target.value)}
                      onBlur={commitRename}
                      onKeyDown={(e) => {
                        e.stopPropagation();
                        if (e.key === "Enter") {
                          e.preventDefault();
                          commitRename();
                        } else if (e.key === "Escape") {
                          e.preventDefault();
                          setEditingName(null);
                          setEditValue("");
                        }
                      }}
                    />
                  ) : (
                    <span className="title">{prettyName(s)}</span>
                  )}
                  <span className="meta">
                    <span>{t("sidebarPanel.messageCount", { count: s.messageCount })}</span>
                    <span className="sep">·</span>
                    <span>{updated}</span>
                  </span>
                </div>
                {editing ? null : (
                  <>
                    <button
                      type="button"
                      className="rename-btn"
                      title={t("sidebarPanel.renameSession")}
                      aria-label={t("sidebarPanel.renameSession")}
                      onClick={(e) => {
                        e.stopPropagation();
                        setEditingName(s.name);
                        setEditValue(currentSummary);
                      }}
                      onKeyDown={(e) => {
                        if (e.key === "Enter" || e.key === " ") e.stopPropagation();
                      }}
                    >
                      <I.pencil size={12} />
                    </button>
                    <button
                      type="button"
                      className="delete-btn"
                      title={t("sidebarPanel.deleteSession")}
                      aria-label={t("sidebarPanel.deleteSession")}
                      onClick={(e) => {
                        e.stopPropagation();
                        const rect = e.currentTarget.getBoundingClientRect();
                        setPendingDelete({
                          name: s.name,
                          pretty: prettyName(s),
                          x: rect.right,
                          y: rect.bottom,
                        });
                      }}
                      onKeyDown={(e) => {
                        if (e.key === "Enter" || e.key === " ") e.stopPropagation();
                      }}
                    >
                      <I.x size={12} />
                    </button>
                  </>
                )}
              </div>
            );
          })}
        </div>
      </div>

      <div className="side-foot">
        <div className="row" onClick={onOpenRules}>
          <span className="ico">
            <I.shield size={13} />
          </span>
          <span>{t("sidebarPanel.approvalRules")}</span>
        </div>
        <div className="row" onClick={onOpenAbout}>
          <span className="ico">
            <I.help size={13} />
          </span>
          <span>{t("about.sidebarLabel")}</span>
        </div>
        <div className="row" onClick={onOpenSettings}>
          <span className="ico">
            <I.cog size={13} />
          </span>
          <span>{t("sidebarPanel.settings")}</span>
          <span className="right">
            <Shortcut keys={["mod", ","]} />
          </span>
        </div>
      </div>

      {pendingDelete ? (
        <SessionDeletePopover
          target={pendingDelete}
          onCancel={() => setPendingDelete(null)}
          onConfirm={() => {
            onDeleteSession(pendingDelete.name);
            setPendingDelete(null);
          }}
        />
      ) : null}
      {pendingImport ? (
        <SessionImportPopover
          target={pendingImport}
          importSources={importSources}
          onRefresh={onRefreshImportSources}
          onCancel={() => setPendingImport(null)}
          onImportDetected={(sources) => {
            onImportDetectedSessions(sources);
            setPendingImport(null);
          }}
          onImport={(payload) => {
            onImportSession(payload);
            setPendingImport(null);
          }}
        />
      ) : null}
    </aside>
  );
}

function SessionDeletePopover({
  target,
  onCancel,
  onConfirm,
}: {
  target: PendingDelete;
  onCancel: () => void;
  onConfirm: () => void;
}) {
  const ref = useRef<HTMLDivElement>(null);
  const cancelRef = useRef<HTMLButtonElement>(null);
  const [pos, setPos] = useState<{ left: number; top: number }>({
    left: target.x,
    top: target.y,
  });

  useLayoutEffect(() => {
    const el = ref.current;
    if (!el) return;
    const rect = el.getBoundingClientRect();
    const pad = 8;
    const vw = window.innerWidth;
    const vh = window.innerHeight;
    let left = target.x;
    let top = target.y;
    if (left + rect.width + pad > vw) left = Math.max(pad, vw - rect.width - pad);
    if (top + rect.height + pad > vh) top = Math.max(pad, vh - rect.height - pad);
    if (left !== pos.left || top !== pos.top) setPos({ left, top });
    cancelRef.current?.focus();
  }, [target.x, target.y, pos.left, pos.top]);

  return (
    <div
      ref={ref}
      className="session-delete-popover"
      role="dialog"
      aria-modal="true"
      style={{ left: pos.left, top: pos.top }}
    >
      <div className="msg">
        {t("sidebarPanel.deleteSession")}
        <span className="name">{target.pretty}</span>
      </div>
      <div className="actions">
        <button ref={cancelRef} type="button" className="cancel" onClick={onCancel}>
          {t("sidebarPanel.cancel")}
        </button>
        <button type="button" className="confirm" onClick={onConfirm}>
          <I.x size={11} />
          {t("sidebarPanel.delete")}
        </button>
      </div>
    </div>
  );
}

function SessionImportPopover({
  target,
  importSources,
  onRefresh,
  onCancel,
  onImportDetected,
  onImport,
}: {
  target: PendingImport;
  importSources: ExternalSessionApp[];
  onRefresh: () => void;
  onCancel: () => void;
  onImportDetected: (sources: ImportSource[]) => void;
  onImport: (payload: { source: ImportSource; path: string; name?: string }) => void;
}) {
  const ref = useRef<HTMLDivElement>(null);
  const firstInputRef = useRef<HTMLInputElement>(null);
  const [pos, setPos] = useState<{ left: number; top: number }>({
    left: target.x,
    top: target.y,
  });
  const [mode, setMode] = useState<"detected" | "custom">("detected");
  const [selected, setSelected] = useState<ImportSource[]>([]);
  const [source, setSource] = useState<ImportSource>("claude");
  const [path, setPath] = useState("");
  const [name, setName] = useState("");

  useEffect(() => {
    const available = importSources.filter((app) => app.available).map((app) => app.source);
    setSelected((prev) => {
      const kept = prev.filter((source) => available.includes(source));
      return kept.length > 0 ? kept : available;
    });
  }, [importSources]);

  useLayoutEffect(() => {
    const el = ref.current;
    if (!el) return;
    const rect = el.getBoundingClientRect();
    const pad = 8;
    const vw = window.innerWidth;
    const vh = window.innerHeight;
    let left = target.x;
    let top = target.y;
    if (left + rect.width + pad > vw) left = Math.max(pad, vw - rect.width - pad);
    if (top + rect.height + pad > vh) top = Math.max(pad, vh - rect.height - pad);
    if (left !== pos.left || top !== pos.top) setPos({ left, top });
    if (mode === "custom") firstInputRef.current?.focus();
  }, [target.x, target.y, pos.left, pos.top, mode]);

  const browse = async () => {
    try {
      const picked = await openFileDialog({
        multiple: false,
        directory: false,
        filters: [{ name: "JSONL", extensions: ["jsonl", "json"] }],
      });
      if (typeof picked === "string" && picked) setPath(picked);
    } catch (err) {
      console.error("session import browse failed", err);
    }
  };

  const submit = () => {
    const trimmedPath = path.trim();
    const trimmedName = name.trim();
    if (!trimmedPath) return;
    onImport({
      source,
      path: trimmedPath,
      name: trimmedName || undefined,
    });
  };

  const submitDetected = () => {
    if (selected.length === 0) return;
    onImportDetected(selected);
  };

  const toggleSource = (source: ImportSource) => {
    setSelected((prev) =>
      prev.includes(source) ? prev.filter((item) => item !== source) : [...prev, source],
    );
  };

  return (
    <div
      ref={ref}
      className="session-import-popover"
      role="dialog"
      aria-modal="true"
      style={{ left: pos.left, top: pos.top }}
    >
      <div className="head">
        <span>{t("sidebarPanel.importSessions")}</span>
        <button type="button" className="close" onClick={onCancel} aria-label={t("sidebarPanel.cancel")}>
          <I.x size={12} />
        </button>
      </div>
      <div className="import-tabs">
        <button type="button" data-on={mode === "detected"} onClick={() => setMode("detected")}>
          {t("sidebarPanel.importDetected")}
        </button>
        <button type="button" data-on={mode === "custom"} onClick={() => setMode("custom")}>
          {t("sidebarPanel.importCustom")}
        </button>
      </div>
      {mode === "detected" ? (
        <>
          <div className="import-detected-head">
            <span>{t("sidebarPanel.importFoundApps")}</span>
            <button type="button" onClick={onRefresh} title={t("sidebarPanel.refresh")}>
              <I.refresh size={12} />
            </button>
          </div>
          <div className="import-app-list">
            {importSources.length === 0 ? (
              <div className="import-empty">{t("sidebarPanel.importScanning")}</div>
            ) : null}
            {importSources.map((app) => {
              const checked = selected.includes(app.source);
              return (
                <button
                  key={app.source}
                  type="button"
                  className="import-app-row"
                  data-disabled={!app.available || undefined}
                  onClick={() => {
                    if (app.available) toggleSource(app.source);
                  }}
                >
                  <span className="app-icon">
                    {app.source === "claude" ? <I.terminal size={15} /> : <I.bot size={15} />}
                  </span>
                  <span className="app-body">
                    <span className="app-name">{app.label}</span>
                    <span className="app-meta">
                      {app.available
                        ? t("sidebarPanel.importSessionCount", {
                            count: app.sessionCount,
                          })
                        : t("sidebarPanel.importNotFound")}
                    </span>
                  </span>
                  <span className="switch" data-on={checked && app.available ? true : undefined} />
                </button>
              );
            })}
          </div>
          <div className="hint">{t("sidebarPanel.importPrivacyHint")}</div>
          <div className="actions">
            <button type="button" className="cancel" onClick={onCancel}>
              {t("sidebarPanel.cancel")}
            </button>
            <button
              type="button"
              className="confirm"
              disabled={selected.length === 0}
              onClick={submitDetected}
            >
              {t("sidebarPanel.continue")}
            </button>
          </div>
        </>
      ) : (
        <>
      <div className="field">
        <span className="label">{t("sidebarPanel.importSource")}</span>
        <div className="seg">
          <button type="button" data-on={source === "claude"} onClick={() => setSource("claude")}>
            {t("sidebarPanel.importFromClaude")}
          </button>
          <button type="button" data-on={source === "codex"} onClick={() => setSource("codex")}>
            {t("sidebarPanel.importFromCodex")}
          </button>
        </div>
      </div>
      <div className="field">
        <span className="label">{t("sidebarPanel.importPath")}</span>
        <div className="path-row">
          <input
            ref={firstInputRef}
            value={path}
            placeholder={t("sidebarPanel.importPath")}
            onChange={(e) => setPath(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter" && path.trim()) submit();
            }}
          />
          <button type="button" onClick={() => void browse()}>
            {t("sidebarPanel.browse")}
          </button>
        </div>
      </div>
      <div className="field">
        <span className="label">{t("sidebarPanel.importName")}</span>
        <input
          value={name}
          placeholder={t("sidebarPanel.importNamePlaceholder")}
          onChange={(e) => setName(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Enter" && path.trim()) submit();
          }}
        />
      </div>
      <div className="actions">
        <button type="button" className="cancel" onClick={onCancel}>
          {t("sidebarPanel.cancel")}
        </button>
        <button type="button" className="confirm" disabled={!path.trim()} onClick={submit}>
          <I.upload size={11} />
          {t("sidebarPanel.importConfirm")}
        </button>
      </div>
        </>
      )}
    </div>
  );
}
