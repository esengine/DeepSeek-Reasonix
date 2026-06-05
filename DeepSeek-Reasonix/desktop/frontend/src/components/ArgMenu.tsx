import { useEffect, useRef } from "react";
import type { SlashArgItem } from "../lib/types";

// ArgMenu is the autocomplete dropdown for a slash command's arguments (the part
// after the command word) — e.g. /skill → list/show/new/paths, /model → refs.
// Like SlashMenu but the entries are bare tokens (no leading "/"); the Composer
// owns filtering, the active index, and key handling. Reuses .slashmenu styling.
export function ArgMenu({
  items,
  activeIndex,
  onPick,
  onHover,
}: {
  items: SlashArgItem[];
  activeIndex: number;
  onPick: (it: SlashArgItem) => void;
  onHover: (i: number) => void;
}) {
  // Keep the keyboard-selected item in view (the list overflows at 280px).
  const activeRef = useRef<HTMLButtonElement>(null);
  useEffect(() => {
    activeRef.current?.scrollIntoView({ block: "nearest" });
  }, [activeIndex]);
  return (
    <div className="slashmenu" role="listbox">
      {items.map((it, i) => (
        <button
          key={it.label}
          ref={i === activeIndex ? activeRef : undefined}
          role="option"
          aria-selected={i === activeIndex}
          className={`slashmenu__item ${i === activeIndex ? "slashmenu__item--active" : ""}`}
          onMouseDown={(e) => {
            e.preventDefault();
            onPick(it);
          }}
          onMouseMove={() => onHover(i)}
        >
          <span className="slashmenu__name">{it.label}</span>
          {it.hint && <span className="slashmenu__hint">{it.hint}</span>}
        </button>
      ))}
    </div>
  );
}
