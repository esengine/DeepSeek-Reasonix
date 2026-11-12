import { useLayoutEffect, useRef } from "react";
import type { KeyboardEvent as ReactKeyboardEvent, PointerEvent as ReactPointerEvent } from "react";
import { t } from "../i18n";

const KEY = "rx-wb-tree-w";
const MIN = 150;
const MAX = 560;
const VAR = "--wb-tree-w";

const clamp = (v: number) => Math.min(MAX, Math.max(MIN, Math.round(v)));

function saved(): number | null {
  try {
    const n = Number(localStorage.getItem(KEY));
    return Number.isFinite(n) && n > 0 ? clamp(n) : null;
  } catch {
    return null;
  }
}

function keep(w: number | null) {
  try {
    if (w === null) localStorage.removeItem(KEY);
    else localStorage.setItem(KEY, String(w));
  } catch {
    /* the width only fails to be remembered */
  }
}

/** The explorer's width is the reader's call about their own screen, so it is
 *  kept in this browser and absent until they drag; without it the stylesheet's
 *  clamp decides. */
export function ExplorerResize() {
  const bar = useRef<HTMLDivElement>(null);
  const host = () => bar.current?.closest<HTMLElement>(".workbench-body") ?? null;
  const explorer = () => bar.current?.closest<HTMLElement>(".workbench-explorer") ?? null;

  const apply = (w: number | null) => {
    const body = host();
    if (!body) return;
    if (w === null) body.style.removeProperty(VAR);
    else body.style.setProperty(VAR, `${w}px`);
    bar.current?.setAttribute("aria-valuenow", String(Math.round(explorer()?.getBoundingClientRect().width ?? 0)));
  };

  useLayoutEffect(() => {
    const w = saved();
    if (w !== null) apply(w);
  }, []);

  const drag = (e: ReactPointerEvent<HTMLDivElement>) => {
    if (e.button !== 0) return;
    e.preventDefault();
    const el = e.currentTarget;
    const from = explorer()?.getBoundingClientRect().width ?? 0;
    const x0 = e.clientX;
    let now = from;
    el.setPointerCapture(e.pointerId);
    el.dataset.on = "";
    const move = (ev: PointerEvent) => {
      now = clamp(from + ev.clientX - x0);
      apply(now);
    };
    const up = () => {
      el.removeEventListener("pointermove", move);
      el.removeEventListener("pointerup", up);
      el.removeEventListener("pointercancel", up);
      delete el.dataset.on;
      keep(clamp(now));
    };
    el.addEventListener("pointermove", move);
    el.addEventListener("pointerup", up);
    el.addEventListener("pointercancel", up);
  };

  const reset = () => {
    apply(null);
    keep(null);
  };

  const key = (e: ReactKeyboardEvent<HTMLDivElement>) => {
    if (e.key === "Home") {
      e.preventDefault();
      reset();
      return;
    }
    const dir = e.key === "ArrowLeft" ? -1 : e.key === "ArrowRight" ? 1 : 0;
    if (!dir) return;
    e.preventDefault();
    const next = clamp((explorer()?.getBoundingClientRect().width ?? 0) + dir * (e.shiftKey ? 48 : 12));
    apply(next);
    keep(next);
  };

  return (
    <div
      ref={bar}
      className="workbench-tree-resize"
      role="separator"
      aria-orientation="vertical"
      aria-label={t("workbench.resize_the_file_tree")}
      aria-valuemin={MIN}
      aria-valuemax={MAX}
      tabIndex={0}
      onPointerDown={drag}
      onDoubleClick={reset}
      onKeyDown={key}
      data-action-pointerdown="workbench.tree-resize"
      data-action-dblclick="workbench.tree-resize"
      data-action-keydown="workbench.tree-resize"
    />
  );
}
