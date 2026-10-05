import { useEffect, useRef, type KeyboardEvent as ReactKeyboardEvent } from "react";

/** The keyboard half of role="tree".
 *
 *  Every row in the sidebar declares itself a treeitem, which tells a screen
 *  reader it can be walked with the arrow keys — and none of them could be
 *  reached by any key at all. What Tab did reach were the small buttons inside
 *  the rows, so a keyboard could delete a session it had no way to open.
 *
 *  Roving tabindex rather than a tabbable row each: a tree is one stop on the
 *  Tab path and the arrows move inside it, which is the whole reason the
 *  pattern exists — forty rows in the tab order is the same as none.
 *
 *  The order is read from the DOM rather than from a list the render has to
 *  keep in step: document order already is visual order, a collapsed branch is
 *  already absent, and nothing has to be told when the tree changes shape.
 *
 *  Returned as props rather than bound to the node here, because a handler
 *  registered through addEventListener has nowhere to write which action it
 *  belongs to — and an input outside the action census is one nothing checks. */
export function useTreeKeys() {
  const ref = useRef<HTMLDivElement>(null);

  // Exactly one way in. Re-settled as the tree changes shape, because the row
  // holding the entry can be the one that just collapsed.
  useEffect(() => {
    const root = ref.current;
    if (!root) return;
    const settle = () => {
      const list = [...root.querySelectorAll<HTMLElement>('[role="treeitem"]')];
      if (!list.length) return;
      const held = list.find((el) => el.tabIndex === 0) ?? list.find((el) => el.dataset.on !== undefined) ?? list[0];
      for (const el of list) el.tabIndex = el === held ? 0 : -1;
    };
    settle();
    const obs = new MutationObserver(settle);
    obs.observe(root, { childList: true, subtree: true });
    return () => obs.disconnect();
  }, []);

  const onKeyDown = (ev: ReactKeyboardEvent<HTMLDivElement>) => {
    const root = ref.current;
    // A rename field inside a row takes its own keys: the arrows move a caret
    // there, and a tree that grabbed them would make the field unusable.
    if (!root || (ev.target as HTMLElement).matches("input, textarea, select")) return;
    const list = [...root.querySelectorAll<HTMLElement>('[role="treeitem"]')];
    const cur = document.activeElement as HTMLElement | null;
    const i = cur ? list.indexOf(cur) : -1;
    if (i < 0 || !cur) return;
    const go = (to: number) => {
      const next = list[to];
      if (!next) return;
      for (const el of list) el.tabIndex = el === next ? 0 : -1;
      next.focus();
      ev.preventDefault();
    };
    const expanded = cur.getAttribute("aria-expanded");
    const level = (el: HTMLElement) => Number(el.getAttribute("aria-level") ?? 1);
    switch (ev.key) {
      case "ArrowDown":
        return go(i + 1);
      case "ArrowUp":
        return go(i - 1);
      case "Home":
        return go(0);
      case "End":
        return go(list.length - 1);
      case "ArrowRight":
        // Open what is closed, step into what is already open. A leaf does
        // neither, which is the arrow saying there is nothing inside.
        if (expanded === "false") {
          cur.click();
          ev.preventDefault();
        } else if (expanded === "true") {
          go(i + 1);
        }
        return;
      case "ArrowLeft": {
        if (expanded === "true") {
          cur.click();
          ev.preventDefault();
          return;
        }
        // Out to the branch this row hangs under, found by level rather than by
        // counting containers: the markup nests rows two different ways.
        const mine = level(cur);
        for (let j = i - 1; j >= 0; j--) {
          if (level(list[j]) < mine) return go(j);
        }
        return;
      }
      case "Enter":
      case " ":
        cur.click();
        ev.preventDefault();
        return;
    }
  };

  return { ref, onKeyDown };
}
