import { memo, useDeferredValue, useEffect, useMemo, useRef, useState } from "react";
import { Circle, LoaderCircle, MessageCircle, Search } from "lucide-react";
import { useI18n } from "../lib/i18n";
import { sessionActivityTime } from "../lib/session";
import type { SessionMeta } from "../lib/types";
import { VirtualList } from "./VirtualList";

type SearchDialogSession = SessionMeta & {
  displayTitle: string;
  projectName: string;
  ageLabel: string;
};

function copy(locale: string) {
  const zh = locale === "zh";
  return {
    title: zh ? "搜索对话" : "Search chats",
    recent: zh ? "近期对话" : "Recent chats",
    empty: zh ? "没有匹配的对话" : "No matching chats",
    loading: zh ? "正在读取对话..." : "Loading chats...",
    global: zh ? "全局" : "Global",
    untitled: zh ? "未命名对话" : "Untitled chat",
  };
}

export function SearchDialog({
  open,
  sessions,
  loading,
  running,
  onResume,
  onClose,
}: {
  open: boolean;
  sessions: SessionMeta[];
  loading: boolean;
  running: boolean;
  onResume: (session: SessionMeta) => void;
  onClose: () => void;
}) {
  const { locale } = useI18n();
  const c = copy(locale);
  const [query, setQuery] = useState("");
  const deferredQuery = useDeferredValue(query);
  const [active, setActive] = useState(0);
  const inputRef = useRef<HTMLInputElement>(null);
  const listRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (!open) return;
    setQuery("");
    setActive(0);
    requestAnimationFrame(() => inputRef.current?.focus());
  }, [open]);

  const items = useMemo(() => {
    return sessions.map((session) => ({
      ...session,
      displayTitle: sessionTitle(session, c.untitled),
      projectName: sessionProjectName(session, c.global),
      ageLabel: sessionAgeLabel(session, locale),
    }));
  }, [c.global, c.untitled, locale, sessions]);

  const filtered = useMemo(() => {
    const q = deferredQuery.trim().toLowerCase();
    if (!q) return items;
    return items
      .filter((session) => {
        const haystack = [
          session.displayTitle,
          session.projectName,
          session.preview,
          session.topicTitle,
          session.workspaceRoot,
          session.path,
        ].join("\n").toLowerCase();
        return q.split(/\s+/).every((token) => haystack.includes(token));
      });
  }, [deferredQuery, items]);

  useEffect(() => {
    if (active >= filtered.length) setActive(Math.max(0, filtered.length - 1));
  }, [active, filtered.length]);

  useEffect(() => {
    setActive(0);
  }, [query]);

  useEffect(() => {
    if (!open) return;
    const onKey = (event: globalThis.KeyboardEvent) => {
      if (event.key === "Escape") {
        event.preventDefault();
        onClose();
        return;
      }
      if (event.key === "ArrowDown") {
        event.preventDefault();
        setActive((index) => (filtered.length === 0 ? 0 : (index + 1) % filtered.length));
        return;
      }
      if (event.key === "ArrowUp") {
        event.preventDefault();
        setActive((index) => (filtered.length === 0 ? 0 : (index - 1 + filtered.length) % filtered.length));
        return;
      }
      if ((event.ctrlKey || event.metaKey) && /^[1-9]$/.test(event.key)) {
        const index = Number(event.key) - 1;
        const session = filtered[index];
        if (session && !running) {
          event.preventDefault();
          onResume(session);
          onClose();
        }
        return;
      }
      if (event.key === "Enter") {
        const session = filtered[active];
        if (session && !running) {
          event.preventDefault();
          onResume(session);
          onClose();
        }
      }
    };
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, [active, filtered, onClose, onResume, open, running]);

  if (!open) return null;

  return (
    <div className="search-dialog-backdrop" role="presentation" onMouseDown={(event) => { if (event.target === event.currentTarget) onClose(); }}>
      <section className="search-dialog" role="dialog" aria-modal="true" aria-label={c.title}>
        <label className="search-dialog__field">
          <Search size={18} />
          <input
            ref={inputRef}
            value={query}
            onChange={(event) => setQuery(event.target.value)}
            placeholder={c.title}
            spellCheck={false}
            autoComplete="off"
          />
        </label>

        <div className="search-dialog__section">{c.recent}</div>

        <div className="search-dialog__list" role="listbox" ref={listRef}>
          {loading ? (
            <div className="search-dialog__empty">
              <LoaderCircle size={17} className="search-dialog__spin" />
              <span>{c.loading}</span>
            </div>
          ) : filtered.length === 0 ? (
            <div className="search-dialog__empty">{c.empty}</div>
          ) : (
            <VirtualList
              items={filtered}
              scrollRef={listRef}
              estimateSize={48}
              scrollToIndex={active}
              getKey={(session) => session.path}
              render={(session, index) => (
                <SearchDialogRow
                  session={session}
                  index={index}
                  active={index === active}
                  running={running}
                  onHover={() => setActive(index)}
                  onOpen={() => {
                    if (running) return;
                    onResume(session);
                    onClose();
                  }}
                />
              )}
            />
          )}
        </div>
      </section>
    </div>
  );
}

const SearchDialogRow = memo(function SearchDialogRow({
  session,
  index,
  active,
  running,
  onHover,
  onOpen,
}: {
  session: SearchDialogSession;
  index: number;
  active: boolean;
  running: boolean;
  onHover: () => void;
  onOpen: () => void;
}) {
  return (
    <button
      type="button"
      role="option"
      aria-selected={active}
      className={`search-dialog__row${active ? " search-dialog__row--active" : ""}`}
      onMouseEnter={onHover}
      onClick={onOpen}
      disabled={running}
    >
      <span className="search-dialog__indicator" aria-hidden="true">
        {active ? <Circle size={14} /> : <MessageCircle size={14} />}
      </span>
      <span className="search-dialog__title">{session.displayTitle}</span>
      <span className="search-dialog__meta">
        <span>{session.projectName}</span>
        {session.ageLabel ? <span>{session.ageLabel}</span> : null}
      </span>
      {index < 9 ? <kbd>Ctrl+{index + 1}</kbd> : null}
    </button>
  );
});

function sessionTitle(session: SessionMeta, fallback: string): string {
  return (session.title || session.topicTitle || session.preview || "").trim() || fallback;
}

function sessionProjectName(session: SessionMeta, fallback: string): string {
  if (session.workspaceRoot) {
    const parts = session.workspaceRoot.split(/[/\\]/).filter(Boolean);
    return parts[parts.length - 1] || session.workspaceRoot;
  }
  if (session.scope === "project") return "Project";
  return fallback;
}

function sessionAgeLabel(session: SessionMeta, locale: string): string {
  const time = sessionActivityTime(session);
  if (!time) return "";
  const delta = Math.max(0, Date.now() - time);
  const minute = 60_000;
  const hour = 60 * minute;
  const day = 24 * hour;
  const week = 7 * day;
  const zh = locale === "zh";
  if (delta < minute) return zh ? "刚刚" : "now";
  if (delta < hour) {
    const n = Math.max(1, Math.round(delta / minute));
    return zh ? `${n} 分钟` : `${n}m`;
  }
  if (delta < day) {
    const n = Math.max(1, Math.round(delta / hour));
    return zh ? `${n} 小时` : `${n}h`;
  }
  if (delta < week) {
    const n = Math.max(1, Math.round(delta / day));
    return zh ? `${n} 天` : `${n}d`;
  }
  const n = Math.max(1, Math.round(delta / week));
  return zh ? `${n} 周` : `${n}w`;
}
