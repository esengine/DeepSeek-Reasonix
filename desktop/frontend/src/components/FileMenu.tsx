import { useEffect, useRef } from "react";
import { Folder, FileText } from "lucide-react";
import type { DirEntry } from "../lib/types";

// FileMenu is the "@" file-reference list — it renders only the items and
// expects the caller (Composer) to wrap them in a .slashmenu container, since
// nesting two .slashmenu absolute containers mis-positions the inner one. The
// Composer owns navigation and the one-level-at-a-time descend logic; this
// stays presentational.
export function FileMenu({
  items,
  activeIndex,
  onPick,
  onHover,
}: {
  items: DirEntry[];
  activeIndex: number;
  onPick: (e: DirEntry) => void;
  onHover: (i: number) => void;
}) {
  // Keep the keyboard-selected item in view (the list overflows at 280px).
  const activeRef = useRef<HTMLButtonElement>(null);
  useEffect(() => {
    activeRef.current?.scrollIntoView({ block: "nearest" });
  }, [activeIndex]);
  return (
    <>
      {items.map((e, i) => (
        <button
          key={(e.isDir ? "d:" : "f:") + e.name}
          ref={i === activeIndex ? activeRef : undefined}
          role="option"
          aria-selected={i === activeIndex}
          className={`slashmenu__item ${i === activeIndex ? "slashmenu__item--active" : ""}`}
          onMouseDown={(ev) => {
            ev.preventDefault();
            onPick(e);
          }}
          onMouseMove={() => onHover(i)}
        >
          {e.isDir ? (
            <Folder size={13} className="filemenu__icon filemenu__icon--dir" />
          ) : (
            <FileText size={13} className="filemenu__icon" />
          )}
          <span className="slashmenu__name slashmenu__name--file">
            {e.name}
            {e.isDir ? "/" : ""}
          </span>
        </button>
      ))}
    </>
  );
}
