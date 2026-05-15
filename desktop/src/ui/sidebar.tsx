import { useEffect, useLayoutEffect, useRef, useState } from "react";
import { I } from "../icons";
import type { SessionInfo } from "../App";

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
    return `会话 ${month}-${day} ${hh}:${mm}${tab && tab !== "1" ? ` · #${tab}` : ""}`;
  }
  return s.name.replace(/^desktop-/, "").replace(/[-_]+/g, " ");
}

function relative(ms: number): string {
  const min = ms / 60_000;
  if (min < 1) return "刚刚";
  if (min < 60) return `${Math.floor(min)} 分钟前`;
  const hr = min / 60;
  if (hr < 24) return `${Math.floor(hr)} 小时前`;
  const d = hr / 24;
  if (d < 7) return `${Math.floor(d)} 天前`;
  return `${Math.floor(d / 7)} 周前`;
}

export function Sidebar({
  sessions,
  activeName,
  onNewChat,
  onLoadSession,
  onDeleteSession,
  onOpenSettings,
  onOpenRules,
  onOpenCommands,
}: {
  sessions: SessionInfo[];
  activeName?: string;
  onNewChat: () => void;
  onLoadSession: (name: string) => void;
  onDeleteSession: (name: string) => void;
  onOpenSettings: () => void;
  onOpenRules: () => void;
  onOpenCommands: () => void;
}) {
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
          <span>新会话</span>
          <kbd>⌘N</kbd>
        </button>
        <button
          type="button"
          className="icon-btn"
          title="命令面板"
          onClick={onOpenCommands}
        >
          <I.history size={14} />
        </button>
      </div>

      <div className="search-row">
        <div className="input">
          <I.search size={13} />
          <input
            placeholder="搜索会话…"
            value={query}
            onChange={(e) => setQuery(e.target.value)}
          />
          <kbd>⌘K</kbd>
        </div>
      </div>

      <div className="session-list">
        <div className="side-section">
          <div className="label">
            <span>近期</span>
            <span className="count">{filtered.length}</span>
          </div>
          {sessions.length === 0 ? (
            <div
              style={{
                padding: "12px 8px",
                fontSize: 11,
                color: "var(--muted-2)",
                fontFamily: "IBM Plex Mono, monospace",
              }}
            >
              暂无会话
            </div>
          ) : filtered.length === 0 ? (
            <div
              style={{
                padding: "12px 8px",
                fontSize: 11,
                color: "var(--muted-2)",
                fontFamily: "IBM Plex Mono, monospace",
              }}
            >
              无匹配结果
            </div>
          ) : null}
          {filtered.map((s) => {
            const active = s.name === activeName;
            const mtime = Date.parse(s.mtime);
            const updated = Number.isFinite(mtime) ? relative(Date.now() - mtime) : s.mtime;
            return (
              <div
                key={s.name}
                className="session-item"
                data-active={active}
                onClick={() => onLoadSession(s.name)}
                onContextMenu={(e) => {
                  e.preventDefault();
                  setPendingDelete({
                    name: s.name,
                    pretty: prettyName(s),
                    x: e.clientX,
                    y: e.clientY,
                  });
                }}
                role="button"
                tabIndex={0}
                title={s.name}
                onKeyDown={(e) => {
                  if (e.key === "Enter") onLoadSession(s.name);
                }}
              >
                <span
                  className="state"
                  style={{ background: active ? "var(--accent)" : "var(--border-strong)" }}
                />
                <div className="body">
                  <span className="title">{prettyName(s)}</span>
                  <span className="meta">
                    <span>{s.messageCount} 条</span>
                    <span className="sep">·</span>
                    <span>{updated}</span>
                  </span>
                </div>
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
          <span>审批规则</span>
        </div>
        <div className="row" onClick={onOpenSettings}>
          <span className="ico">
            <I.cog size={13} />
          </span>
          <span>设置</span>
          <span className="right">⌘,</span>
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
        删除会话
        <span className="name">{target.pretty}</span>
      </div>
      <div className="actions">
        <button ref={cancelRef} type="button" className="cancel" onClick={onCancel}>
          取消
        </button>
        <button type="button" className="confirm" onClick={onConfirm}>
          <I.x size={11} />
          删除
        </button>
      </div>
    </div>
  );
}
