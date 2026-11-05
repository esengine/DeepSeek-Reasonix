import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useModalFocus } from "./useModalFocus";
import type { TreeWorkspace } from "../port/hub";
import { t } from "../i18n";
import { StudioIcon, type StudioIconName } from "./StudioIcon";
import { ageWord } from "./SessionAge";

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

export interface PaletteWorkspace extends TreeWorkspace { host?: string; }

export interface PaletteSetting {
  id: string;
  label: string;
  path: string;
  keywords: string;
}

type Scope = "" | "actions" | "sessions" | "settings";
const SCOPES: Scope[] = ["", "actions", "sessions", "settings"];
const SCOPE_LABEL: Record<Exclude<Scope, "">, string> = { actions: "app.action", sessions: "common.session", settings: "common.settings" };
const PREFIX: Record<string, Exclude<Scope, "">> = { ">": "actions", "#": "sessions", ",": "settings" };
const PREFIX_OF: Record<Exclude<Scope, "">, string> = { actions: ">", sessions: "#", settings: "," };

interface Props {
  open: boolean;
  onClose: () => void;
  commands: Command[];
  tree: PaletteWorkspace[];
  onOpenSession: (path: string, workspace: PaletteWorkspace) => void;
  settings?: PaletteSetting[];
  onOpenSetting?: (target: string) => void;
  /** What a session is doing, if it is open: "running" or "halt" (waiting on the person). */
  runOf?: (runtimeId: string | undefined) => string | undefined;
}

interface Hit {
  key: string;
  label: string;
  icon: StudioIconName;
  note?: string;
  alert?: boolean;
  dot?: "running" | "halt" | "none";
  inline?: boolean;
  run: () => void;
}

const LIMIT = 5;
const RECENT_KEY = "palette.recent";
const RECENT_KEPT = 3;

function readRecent(): string[] {
  try {
    const raw = JSON.parse(localStorage.getItem(RECENT_KEY) ?? "[]");
    return Array.isArray(raw) ? raw.filter((x): x is string => typeof x === "string") : [];
  } catch {
    return [];
  }
}

function remember(key: string) {
  try {
    localStorage.setItem(RECENT_KEY, JSON.stringify([key, ...readRecent().filter((x) => x !== key)].slice(0, RECENT_KEPT)));
  } catch {}
}

export function Palette({ open, onClose, commands, tree, onOpenSession, settings = [], onOpenSetting, runOf }: Props) {
  const [query, setQuery] = useState("");
  const [scope, setScope] = useState<Scope>("");
  const [at, setAt] = useState(0);
  const box = useRef<HTMLInputElement>(null);
  const list = useRef<HTMLDivElement>(null);
  const sheet = useRef<HTMLDivElement>(null);
  useModalFocus(sheet, open);

  // A palette opened on what the last one was looking for is a palette that
  // answers the wrong question; it starts empty every time.
  useEffect(() => {
    if (!open) return;
    setQuery("");
    setScope("");
    setAt(0);
    box.current?.focus();
  }, [open]);

  const groups = useMemo(() => {
    const q = query.trim().toLowerCase();
    const wants = (kind: Exclude<Scope, "">) => scope === "" || scope === kind;
    const actions: Hit[] = wants("actions")
      ? commands.filter((c) => !q || `${c.label} ${c.keywords}`.toLowerCase().includes(q)).slice(0, LIMIT)
        .map((c) => ({ key: `c:${c.id}`, label: c.label, icon: c.icon, note: c.status ?? c.keys ?? t("app.action"), alert: c.status !== undefined, inline: c.status !== undefined, run: c.run }))
      : [];
    const sessions: Hit[] = [];
    if (wants("sessions")) {
      for (const workspace of tree) {
        for (const session of workspace.sessions) {
          if (sessions.length >= LIMIT) break;
          const title = session.title || session.name;
          if (q && !`${title} ${workspace.name} ${workspace.host ?? ""}`.toLowerCase().includes(q)) continue;
          const run = runOf?.(session.runtimeId);
          const dot = run === "running" || run === "halt" ? run : undefined;
          const age = !dot && session.lastActiveAt ? ageWord(session.lastActiveAt, Date.now()) : "";
          sessions.push({
            key: `s:${workspace.host ?? ""}:${session.path}`,
            label: title,
            icon: "clock",
            dot: dot ?? "none",
            inline: true,
            note: [workspace.name, workspace.host, dot ? t(dot === "running" ? "common.running" : "session.needs_you") : age].filter(Boolean).join(" · "),
            run: () => onOpenSession(session.path, { ...workspace, root: session.workspaceRoot || workspace.root }),
          });
        }
      }
    }
    const found: Hit[] = wants("settings") && (q || scope === "settings")
      ? settings.filter((x) => !q || `${x.label} ${x.keywords} ${x.path}`.toLowerCase().includes(q)).slice(0, LIMIT)
        .map((x) => ({ key: `g:${x.id}`, label: x.label, icon: "settings" as StudioIconName, note: x.path, run: () => onOpenSetting?.(x.id) }))
      : [];
    if (!q && scope === "") {
      const used = readRecent();
      const pool = [...actions, ...sessions];
      const recent = used.map((key) => pool.find((h) => h.key === key)).filter((h): h is Hit => !!h);
      if (recent.length > 0) {
        const taken = new Set(recent.map((h) => h.key));
        return [
          { label: t("app.recently_used"), hits: recent },
          { label: t("app.quick_actions"), hits: actions.filter((h) => !taken.has(h.key)) },
          { label: t("common.session"), hits: sessions.filter((h) => !taken.has(h.key)) },
        ].filter((g) => g.hits.length > 0);
      }
    }
    const ordered = q || scope !== ""
      ? [{ label: t("common.session"), hits: sessions }, { label: t("common.settings"), hits: found }, { label: t("app.quick_actions"), hits: actions }]
      : [{ label: t("app.quick_actions"), hits: actions }, { label: t("common.session"), hits: sessions }];
    if (q && scope === "") {
      const lead = ordered.find((g) => g.hits.length > 0);
      if (lead) {
        const best = lead.hits[0];
        return [{ label: t("app.best_match"), hits: [best] }, ...ordered.map((g) => (g === lead ? { ...g, hits: g.hits.slice(1) } : g))].filter((g) => g.hits.length > 0);
      }
    }
    return ordered.filter((g) => g.hits.length > 0);
  }, [commands, tree, settings, query, scope, onOpenSession, onOpenSetting, runOf]);

  const flat = useMemo(() => groups.flatMap((g) => g.hits), [groups]);
  // A cursor past the end of a shorter list selects nothing and Enter does
  // nothing, which reads as the palette having stopped working.
  const cursor = flat.length === 0 ? -1 : Math.min(at, flat.length - 1);

  const pick = useCallback(
    (hit: Hit | undefined) => {
      if (!hit) return;
      remember(hit.key);
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
      <div ref={sheet} className="palette" role="dialog" aria-modal="true" aria-label={t("app.search_and_commands")}>
        <label className="palette-input">
          <StudioIcon name="search" />
          {scope && <span className="palette-scope">{t(SCOPE_LABEL[scope])}</span>}
          <input
            ref={box}
            data-action-keydown="palette.search"
            aria-label={t("app.search_conversations_or_actions")}
            placeholder={t("app.search_conversations_tools_settings")}
            autoComplete="off"
            value={query}
            onChange={(e) => {
              const value = e.target.value;
              if (scope === "" && value.length === 1 && PREFIX[value]) {
                setScope(PREFIX[value]);
                setQuery("");
              } else setQuery(value);
              setAt(0);
            }}
            onKeyDown={(e) => {
              if (e.key === "Escape") return onClose();
              if (e.key === "Tab") {
                e.preventDefault();
                setScope((current) => SCOPES[(SCOPES.indexOf(current) + 1) % SCOPES.length]);
                setAt(0);
                return;
              }
              if (e.key === "Backspace" && query === "" && scope !== "") {
                e.preventDefault();
                setScope("");
                return;
              }
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
          <kbd className="palette-esc" aria-hidden="true">Esc</kbd>
        </label>
        <div className="palette-chips" role="group" aria-label={t("app.search_scope")}>
          {(["actions", "sessions", "settings"] as const).map((kind) => (
            <button key={kind} type="button" className="palette-chip" data-action="palette.scope" data-value={kind} aria-pressed={scope === kind}
              onClick={() => { setScope(scope === kind ? "" : kind); setAt(0); box.current?.focus(); }}>
              <b>{PREFIX_OF[kind]}</b>{t(SCOPE_LABEL[kind])}
            </button>
          ))}
        </div>
        <div className="palette-results" ref={list}>
          {groups.map((group) => (
            <div key={group.label}>
              <div className="palette-label">{group.label}{query.trim() && group.label !== t("app.best_match") && <i>{group.hits.length}</i>}</div>
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
                  {hit.dot ? <i className="palette-dot" data-run={hit.dot} aria-hidden="true" /> : <StudioIcon name={hit.icon} />}
                  <span>{hit.label}</span>
                  {hit.note && <small data-alert={hit.alert ? "" : undefined} data-inline={hit.inline ? "" : undefined}>{hit.note}</small>}
                </button>
              ))}
            </div>
          ))}
          {flat.length === 0 && <div className="palette-empty">{t("app.nothing_matches")}</div>}
        </div>
        <div className="palette-help"><span>{t("app.palette_keys")}</span><span>{t("app.palette_scopes")}</span></div>
      </div>
    </div>
  );
}
