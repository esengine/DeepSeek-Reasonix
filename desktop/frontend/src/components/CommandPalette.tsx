import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import type { ReactNode } from "react";
import { Command, Search } from "lucide-react";
import { useT } from "../lib/i18n";
import { useMountTransition } from "../lib/useMountTransition";

// CommandPalette is a ⌘K / Ctrl+K modal that surfaces the desktop app's
// long-tail navigation surface. Tabs through sessions, slash-commands, and
// recent files via a single fuzzy search. The list of items is provided by
// the caller (App) so the palette stays decoupled from the controller — the
// same component will work for skills, MCP servers, and future surfaces
// once a buildItems() helper is added for them.
//
// Interaction model:
//   - Input is auto-focused on open; the first match is highlighted.
//   - ↑/↓ move the highlight (wraps at the edges).
//   - Enter runs the highlighted item's action.
//   - Esc closes.
//   - Mouse hover sets the highlight (so a click can be "pre-thought"); the
//     click itself runs the action.
//
// Fuzzy match is a small case-insensitive substring scorer — every query
// token must appear in the candidate's title or hint, in order, but they
// may overlap (a real fuzzy matcher would be overkill for 50-200 items).
export interface PaletteItem {
  // id is stable and unique within a single open of the palette.
  id: string;
  // title is the primary label.
  title: string;
  // hint is the secondary line (a path, a command's source, etc.).
  hint?: string;
  // meta is right-aligned secondary text (e.g. a timestamp).
  meta?: string;
  // badge is a right-aligned counter or label (e.g. turn count).
  badge?: string;
  // icon overrides the default Command icon shown on the left.
  icon?: ReactNode;
  // compact renders the item as a grid chip (icon + title, no hint/meta).
  compact?: boolean;
  // group is the section header this item belongs to.
  group: string;
  // keywords add to the searchable text (e.g. slash-command aliases).
  keywords?: string[];
  // run closes the palette and dispatches the action.
  run: () => void | Promise<void>;
}

export function CommandPalette({
  open,
  onClose,
  items,
  placeholder,
  emptyText,
}: {
  open: boolean;
  onClose: () => void;
  items: PaletteItem[];
  placeholder: string;
  emptyText: string;
}) {
  const t = useT();
  const [query, setQuery] = useState("");
  const [active, setActive] = useState(-1);
  const inputRef = useRef<HTMLInputElement>(null);
  const isOpenRef = useRef(false);
  isOpenRef.current = open;
  // Keep the palette mounted through its exit animation after `open` flips
  // false; `status` drives the enter/exit keyframes via data-state.
  const { mounted, status } = useMountTransition(open, 200);

  // Re-init whenever the palette opens: clear the query, reset the
  // highlight, and steal focus. Doing it on the open edge (not on every
  // render) means a previously-typed query doesn't leak across opens.
  // useLayoutEffect fires synchronously after DOM mutations, before the
  // browser paints — ensures focus lands before any paint-time transitions
  // can interfere.
  useLayoutEffect(() => {
    if (open) {
      setQuery("");
      setActive(items.length > 0 ? 0 : -1);
      inputRef.current?.focus();
    }
  }, [open, items.length]);

  // Callback ref: when the input element mounts while the palette is open,
  // focus it immediately. This handles the case where the DOM element
  // becomes available after the useLayoutEffect already ran.
  const inputCallbackRef = useCallback(
    (el: HTMLInputElement | null) => {
      inputRef.current = el;
      if (el && isOpenRef.current) el.focus();
    },
    [],
  );

  // score is the fuzzy match: every space-separated query token must
  // appear (case-insensitively) in the candidate's haystack, in the order
  // given. The score is the sum of the inverse lengths of the matching
  // substrings (smaller span → higher rank) so a tight prefix match wins
  // over a spread match.
  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase();
    if (!q) return items;
    const tokens = q.split(/\s+/);
    const scored: { item: PaletteItem; score: number }[] = [];
    for (const it of items) {
      const hay = [it.title, it.hint ?? "", ...(it.keywords ?? [])].join("\n").toLowerCase();
      let cursor = 0;
      let score = 0;
      let ok = true;
      for (const tok of tokens) {
        const at = hay.indexOf(tok, cursor);
        if (at < 0) {
          ok = false;
          break;
        }
        // Reward tight matches (smaller span) and matches early in the string.
        score += 1000 - (at - cursor) - at;
        cursor = at + tok.length;
      }
      if (ok) scored.push({ item: it, score });
    }
    scored.sort((a, b) => b.score - a.score);
    return scored.map((s) => s.item);
  }, [query, items]);

  // Group the filtered items by their `group` field, preserving the order
  // the groups first appear (so a "Sessions" group with a hit is shown
  // before a "Commands" group with a hit, even if the commands' raw
  // scores would outrank it). This matches the user's mental model:
  // sessions are the most frequent target.
  const grouped = useMemo(() => {
    const out: { group: string; items: PaletteItem[] }[] = [];
    const indexOf = (g: string) => out.findIndex((o) => o.group === g);
    for (const it of filtered) {
      const at = indexOf(it.group);
      if (at < 0) out.push({ group: it.group, items: [it] });
      else out[at].items.push(it);
    }
    return out;
  }, [filtered]);

  // Flat index -> grouped item lookup. The keyboard handler only needs
  // the linear index, so we keep a parallel array to avoid a quadratic
  // walk on every keypress.
  const flat = useMemo(() => grouped.flatMap((g) => g.items), [grouped]);

  // Clamp the active index whenever the result set shrinks (e.g. user
  // typed something that filtered out the previously-highlighted item).
  useEffect(() => {
    if (active >= 0 && active >= flat.length) setActive(Math.max(0, flat.length - 1));
  }, [flat.length, active]);

  // Reset the highlight to the first match on every query change — the user
  // just refined their search, the old highlight is rarely still interesting.
  useEffect(() => {
    setActive(0);
  }, [query]);

  // Esc closes; ↑/↓ move the highlight; Enter runs. We use a document-level
  // listener so the palette is responsive even when focus drifts (e.g. the
  // user clicks a result row, then presses ↑).
  useEffect(() => {
    if (!open) return;
    const onKey = (e: KeyboardEvent) => {
      const closeButtonHasFocus = e.target instanceof HTMLElement && Boolean(e.target.closest("[data-palette-close]"));
      if (closeButtonHasFocus && (e.key === "Enter" || e.key === " ")) return;
      if (e.key === "Escape") {
        e.preventDefault();
        onClose();
        return;
      }
      if (e.key === "ArrowDown") {
        e.preventDefault();
        setActive((i) => (flat.length === 0 ? -1 : i < 0 ? 0 : (i + 1) % flat.length));
        return;
      }
      if (e.key === "ArrowUp") {
        e.preventDefault();
        setActive((i) => (flat.length === 0 ? -1 : i <= 0 ? flat.length - 1 : i - 1));
        return;
      }
      if (e.key === "Enter") {
        e.preventDefault();
        const it = flat[active];
        if (it) void it.run();
        onClose();
        return;
      }
    };
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, [open, flat, active, onClose]);

  if (!mounted) return null;

  // The running counter maps a flat-index back to its group header so we
  // can render the section dividers in order.
  let running = 0;

  return (
    <div
      className="drawer-backdrop"
      data-state={status}
      onClick={onClose}
      role="presentation"
    >
      <div className="palette" data-state={status} onClick={(e) => e.stopPropagation()} role="dialog" aria-modal="true" aria-label={placeholder}>
        <div className="palette__inputrow">
          <Search className="palette__search-icon" size={18} aria-hidden="true" />
          <input
            ref={inputCallbackRef}
            className="palette__input"
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder={placeholder}
            spellCheck={false}
            autoComplete="off"
          />
          <button
            className="palette__esc"
            type="button"
            onClick={onClose}
            aria-label={t("common.close")}
            title={t("common.close")}
            data-palette-close
          >
            esc
          </button>
        </div>
        <div className="palette__list" role="listbox">
          {flat.length === 0 ? (
            <div className="palette__empty">{emptyText}</div>
          ) : (
            grouped.map((g) => {
              const isCompact = g.items[0]?.compact;
              return (
              <div className={`palette__group ${isCompact ? "palette__group--grid" : ""}`} key={g.group}>
                <div className="palette__group-title">{g.group}</div>
                {isCompact ? (
                  <div className="palette__grid">
                  {g.items.map((it) => {
                    const idx = running++;
                    const on = idx === active;
                    return (
                      <button
                        type="button"
                        role="option"
                        aria-selected={on}
                        key={it.id}
                        className={`palette__chip ${on ? "palette__chip--on" : ""}`}
                        onMouseEnter={() => setActive(idx)}
                        onClick={() => {
                          void it.run();
                          onClose();
                        }}
                      >
                        <span className="palette__chip-icon" aria-hidden="true">
                          {it.icon ?? <Command size={15} />}
                        </span>
                        <span className="palette__chip-label">{it.title}</span>
                      </button>
                    );
                  })}
                  </div>
                ) : (
                  g.items.map((it) => {
                    const idx = running++;
                    const on = idx === active;
                    return (
                      <button
                        type="button"
                        role="option"
                        aria-selected={on}
                        key={it.id}
                        className={`palette__item ${on ? "palette__item--on" : ""}`}
                        onMouseEnter={() => setActive(idx)}
                        onClick={() => {
                          void it.run();
                          onClose();
                        }}
                      >
                        <span className="palette__item-icon" aria-hidden="true">
                          {it.icon ?? <Command size={15} />}
                        </span>
                        <span className="palette__body">
                          <span className="palette__title">{it.title}</span>
                          {(it.hint || it.meta || it.badge) && (
                            <span className="palette__hint">
                              {it.hint && <span className="palette__hint-text">{it.hint}</span>}
                              {it.meta && <span className="palette__meta">{it.meta}</span>}
                              {it.badge && <span className="palette__badge">{it.badge}</span>}
                            </span>
                          )}
                        </span>
                      </button>
                    );
                  })
                )}
              </div>
              );
            })
          )}
        </div>
        <div className="palette__foot">
          <span>
            <kbd>↑</kbd>
            <kbd>↓</kbd> navigate
          </span>
          <span>
            <kbd>↵</kbd> run
          </span>
          <span>
            <kbd>esc</kbd> close
          </span>
        </div>
      </div>
    </div>
  );
}
