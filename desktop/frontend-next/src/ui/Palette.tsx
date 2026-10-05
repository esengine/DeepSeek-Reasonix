import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import type { TreeWorkspace } from "../port/hub";
import { t } from "../i18n";
import { StudioIcon, type StudioIconName } from "./StudioIcon";

/** One thing the palette can do. Sessions are found by title; a command is
 *  found by its own words and by the terms someone would reach for instead,
 *  which is why keywords are carried rather than matched against the label. */
export interface Command {
  id: string;
  label: string;
  icon: StudioIconName;
  keywords: string;
  // The shortcut that does the same thing, printed where "操作" would be.
  keys?: string;
  // What the command has to say right now, printed instead of a shortcut.
  status?: string;
  run: () => void;
}

interface Props {
  open: boolean;
  onClose: () => void;
  commands: Command[];
  tree: TreeWorkspace[];
  onOpenSession: (path: string) => void;
}

interface Hit {
  key: string;
  label: string;
  icon: StudioIconName;
  note?: string;
  alert?: boolean;
  run: () => void;
}

const SESSION_LIMIT = 8;

export function Palette({ open, onClose, commands, tree, onOpenSession }: Props) {
  const [query, setQuery] = useState("");
  const [at, setAt] = useState(0);
  const box = useRef<HTMLInputElement>(null);
  const list = useRef<HTMLDivElement>(null);

  // A palette opened on what the last one was looking for is a palette that
  // answers the wrong question; it starts empty every time.
  useEffect(() => {
    if (!open) return;
    setQuery("");
    setAt(0);
    box.current?.focus();
  }, [open]);

  const groups = useMemo(() => {
    const q = query.trim().toLowerCase();
    const matched = commands.filter((c) => !q || `${c.label} ${c.keywords}`.toLowerCase().includes(q));
    const sessions: Hit[] = [];
    for (const workspace of tree) {
      for (const session of workspace.sessions) {
        if (sessions.length >= SESSION_LIMIT) break;
        const title = session.title || session.name;
        if (q && !title.toLowerCase().includes(q)) continue;
        sessions.push({
          key: `s:${session.path}`,
          label: title,
          icon: "clock",
          note: workspace.name,
          run: () => onOpenSession(session.path),
        });
      }
    }
    return [
      { label: t("快捷操作"), hits: matched.map((c) => ({ key: `c:${c.id}`, label: c.label, icon: c.icon, note: c.status ?? c.keys ?? t("操作"), alert: c.status !== undefined, run: c.run })) },
      { label: t("会话"), hits: sessions },
    ].filter((g) => g.hits.length > 0);
  }, [commands, tree, query, onOpenSession]);

  const flat = useMemo(() => groups.flatMap((g) => g.hits), [groups]);
  // A cursor past the end of a shorter list selects nothing and Enter does
  // nothing, which reads as the palette having stopped working.
  const cursor = flat.length === 0 ? -1 : Math.min(at, flat.length - 1);

  const pick = useCallback(
    (hit: Hit | undefined) => {
      if (!hit) return;
      onClose();
      hit.run();
    },
    [onClose],
  );

  useEffect(() => {
    if (cursor < 0) return;
    list.current?.querySelectorAll<HTMLElement>(".palette-hit")[cursor]?.scrollIntoView({ block: "nearest" });
  }, [cursor]);

  if (!open) return null;

  return (
    <div className="palette-veil" role="presentation" onMouseDown={(e) => e.target === e.currentTarget && onClose()}>
      <div className="palette" role="dialog" aria-modal="true" aria-label={t("搜索与快捷操作")}>
        <label className="palette-input">
          <StudioIcon name="search" />
          <input
            ref={box}
            data-action-keydown="palette.search"
            aria-label={t("搜索会话或操作")}
            placeholder={t("搜索会话、工具、设置…")}
            autoComplete="off"
            value={query}
            onChange={(e) => {
              setQuery(e.target.value);
              setAt(0);
            }}
            onKeyDown={(e) => {
              if (e.key === "Escape") return onClose();
              if (e.key === "Enter") {
                e.preventDefault();
                return pick(flat[cursor]);
              }
              if (e.key !== "ArrowDown" && e.key !== "ArrowUp") return;
              e.preventDefault();
              if (flat.length === 0) return;
              const step = e.key === "ArrowDown" ? 1 : -1;
              setAt((n) => (Math.min(n, flat.length - 1) + step + flat.length) % flat.length);
            }}
          />
        </label>
        <div className="palette-results" ref={list}>
          {groups.map((group) => (
            <div key={group.label}>
              <div className="palette-label">{group.label}</div>
              {group.hits.map((hit) => (
                <button
                  key={hit.key}
                  className="palette-hit"
                  data-action="palette.pick"
                  data-target={hit.key}
                  aria-selected={flat[cursor]?.key === hit.key}
                  onMouseMove={() => setAt(flat.findIndex((h) => h.key === hit.key))}
                  onClick={() => pick(hit)}
                >
                  <StudioIcon name={hit.icon} />
                  <span>{hit.label}</span>
                  {hit.note && <small data-alert={hit.alert ? "" : undefined}>{hit.note}</small>}
                </button>
              ))}
            </div>
          ))}
          {flat.length === 0 && <div className="palette-empty">{t("没有匹配的会话或操作")}</div>}
        </div>
        <div className="palette-help">{t("↑ ↓ 选择 · Enter 打开 · Esc 关闭")}</div>
      </div>
    </div>
  );
}
