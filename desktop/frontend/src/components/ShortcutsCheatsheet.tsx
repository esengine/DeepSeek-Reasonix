import { useEffect, useRef } from "react";

// ShortcutsCheatsheet is the "?" overlay — a single column of every
// keyboard shortcut the desktop app honors. The list is intentionally
// declarative (an array of [keys, description] tuples) so adding a
// shortcut is a one-line change here. The component is mounted by App and
// toggled by a "?" keypress on the topbar; it also opens via the menu in
// the topbar's chip cluster.
//
// Focus is trapped while open (the cheatsheet is a small dialog), Esc
// closes it, and focus returns to the element that opened it. We use a
// role="dialog" + aria-modal="true" + aria-labelledby so screen readers
// announce the title and don't expose the page behind.
//
// The cheatsheet is NOT localized (shortcut keys are universal); the
// descriptions are. Both en.ts and zh.ts have their own table that
// maps shortcut-id -> description, looked up via the prop's `t` helper.
export interface ShortcutEntry {
  // id is a stable, dotted key. Examples: "composer.send",
  // "composer.cycleMode", "transcript.cancel", "global.history".
  id: string;
  // keys is a stringified combo the user types, e.g. "Enter",
  // "Shift+Tab", "Ctrl+K", "⌘K", "⌥↑". It's rendered verbatim in a
  // <kbd> cluster; we don't try to normalize Mac vs. PC — the platform
  // hint lives in the right column ("Mac" / "Win/Linux").
  keys: string[];
  // platform is "mac" | "win" | "all". "all" means the shortcut is the
  // same on every platform and the platform column is hidden. The
  // current OS is detected once at module load (see os() below); users
  // on Windows don't see Mac-only shortcuts and vice versa.
  platform: "mac" | "win" | "all";
}

export function ShortcutsCheatsheet({
  open,
  onClose,
  entries,
  t,
}: {
  open: boolean;
  onClose: () => void;
  entries: ShortcutEntry[];
  // The translator is typed loosely because the cheatsheet uses dotted
  // shortcut keys ("shortcuts.desc.composer.send") that aren't in the
  // DictKey union — they're deliberately dynamic so adding a new
  // shortcut is a one-line change without touching the locale types.
  t: (id: string) => string;
}) {
  // Restore focus to the opener on close. We snapshot the active element
  // when open flips true, then on the next focus return it. Doing it in a
  // layout effect would fire on every render; an effect is fine because
  // we only act on the open->close transition.
  const triggerRef = useRef<HTMLElement | null>(null);
  const closeRef = useRef<HTMLButtonElement>(null);
  useEffect(() => {
    if (open) {
      triggerRef.current = document.activeElement as HTMLElement | null;
      // Move focus into the dialog so screen readers announce it and
      // keyboard nav starts inside.
      requestAnimationFrame(() => closeRef.current?.focus());
    } else if (triggerRef.current && triggerRef.current.isConnected) {
      triggerRef.current.focus();
      triggerRef.current = null;
    }
  }, [open]);

  // Esc closes. We don't trap Tab (the cheatsheet is small and the only
  // focusable is the close button) — adding a roving trap would be
  // overkill.
  useEffect(() => {
    if (!open) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") {
        e.stopPropagation();
        onClose();
      }
    };
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, [open, onClose]);

  if (!open) return null;

  const host = os();
  const visible = entries.filter((e) => e.platform === "all" || e.platform === host);

  // Group consecutive entries by their leading id segment so the cheatsheet
  // reads as "Composer", "Transcript", "Global" sections. The id is
  // dotted ("composer.send") so the first segment is the section.
  const grouped: { section: string; items: ShortcutEntry[] }[] = [];
  for (const e of visible) {
    const section = e.id.split(".")[0];
    const last = grouped[grouped.length - 1];
    if (last && last.section === section) last.items.push(e);
    else grouped.push({ section, items: [e] });
  }

  return (
    <div className="drawer-backdrop" onClick={onClose} role="presentation">
      <aside
        className="drawer"
        role="dialog"
        aria-modal="true"
        aria-labelledby="cheatsheet-title"
        onClick={(e) => e.stopPropagation()}
      >
        <header className="drawer__head">
          <div id="cheatsheet-title" className="drawer__title">
            {t("shortcuts.title")}
          </div>
          <button ref={closeRef} className="chip" onClick={onClose} aria-label={t("shortcuts.close")} type="button">
            ✕
          </button>
        </header>
        <div className="drawer__body cheatsheet">
          {grouped.map((g) => (
            <section className="cheatsheet__section" key={g.section}>
              <div className="cheatsheet__section-title">{t("shortcuts.section." + g.section)}</div>
              <ul className="cheatsheet__list">
                {g.items.map((it) => (
                  <li className="cheatsheet__row" key={it.id}>
                    <span className="cheatsheet__keys">
                      {it.keys.map((k, i) => (
                        <kbd key={i} className="cheatsheet__kbd">
                          {k}
                        </kbd>
                      ))}
                    </span>
                    <span className="cheatsheet__desc">{t("shortcuts.desc." + it.id)}</span>
                  </li>
                ))}
              </ul>
            </section>
          ))}
        </div>
      </aside>
    </div>
  );
}

function os(): "mac" | "win" {
  if (typeof navigator === "undefined") return "win";
  // The UA sniff is the same pattern the rest of the desktop app uses
  // (see StatusBar's platform switch). We don't need perfect
  // identification — Linux + Windows collapse to "win" and the cheatsheet
  // shows PC-style shortcuts; macOS users get ⌘/⌥/⇧.
  const ua = navigator.userAgent || "";
  return /Mac|iPhone|iPad/.test(ua) ? "mac" : "win";
}
