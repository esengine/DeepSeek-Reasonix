import { useEffect, useLayoutEffect, useRef, useState } from "react";
import type { SessionInfo } from "../App";
import { t, useLang } from "../i18n";
import { I } from "../icons";
import { Shortcut } from "./shortcut";

type PendingDelete = {
  name: string;
  pretty: string;
  x: number;
  y: number;
};

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
  activeName,
  loadingName,
  onNewChat,
  onLoadSession,
  onDeleteSession,
  onOpenSettings,
  onOpenRules,
  onOpenCommands,
  onOpenAbout,
}: {
  sessions: SessionInfo[];
  activeName?: string;
  /** Name of the session currently being loaded — drives the per-row spinner. */
  loadingName?: string | null;
  onNewChat: () => void;
  onLoadSession: (name: string) => void;
  onDeleteSession: (name: string) => void;
  onOpenSettings: () => void;
  onOpenRules: () => void;
  onOpenCommands: () => void;
  onOpenAbout: () => void;
}) {
  useLang();
  const [query, setQuery] = useState("");
  const [pendingDelete, setPendingDelete] = useState<PendingDelete | null>(null);
  const filtered = query
    ? sessions.filter((s) => {
        const q = query.toLowerCase();
        return (
          prettyName(s).toLowerCase().includes(q) || s.name.toLowerCase().includes(q)
        );
      })
    : sessions;

  useEffect(() => {
    if (!pendingDelete) return;
    const onMouseDown = (e: MouseEvent) => {
      const target = e.target as HTMLElement | null;
      if (!target?.closest(".session-delete-popover")) setPendingDelete(null);
    };
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") setPendingDelete(null);
    };
    window.addEventListener("mousedown", onMouseDown);
    window.addEventListener("keydown", onKey);
    return () => {
      window.removeEventListener("mousedown", onMouseDown);
      window.removeEventListener("keydown", onKey);
    };
  }, [pendingDelete]);

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
            const loading = s.name === loadingName;
            const mtime = Date.parse(s.mtime);
            const updated = Number.isFinite(mtime) ? relative(Date.now() - mtime) : s.mtime;
            return (
              <div
                key={s.name}
                className="session-item"
                data-active={active}
                data-loading={loading || undefined}
                style={loadingName && !loading ? { opacity: 0.5, pointerEvents: "none" } : undefined}
                onClick={() => onLoadSession(s.name)}
                role="button"
                tabIndex={0}
                title={s.name}
                onKeyDown={(e) => {
                  if (e.key === "Enter") onLoadSession(s.name);
                }}
              >
                <span
                  className="state"
                  style={{
                    background: loading
                      ? "var(--accent)"
                      : active
                        ? "var(--accent)"
                        : "var(--border-strong)",
                    animation: loading ? "session-pulse 0.9s ease-in-out infinite" : undefined,
                  }}
                />
                <div className="body">
                  <span className="title">{prettyName(s)}</span>
                  <span className="meta">
                    {loading ? (
                      <span>{t("sidebarPanel.loading")}</span>
                    ) : (
                      <>
                        <span>{t("sidebarPanel.messageCount", { count: s.messageCount })}</span>
                        <span className="sep">·</span>
                        <span>{updated}</span>
                      </>
                    )}
                  </span>
                </div>
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
