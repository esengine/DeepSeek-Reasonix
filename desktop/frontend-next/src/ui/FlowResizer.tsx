import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState, type KeyboardEvent as ReactKeyboardEvent } from "react";
import type { PointerEvent as ReactPointerEvent } from "react";
import { t } from "../i18n";
import { onRoomWidth } from "./viewport";

/** The width the "readable" measure names, in pixels.
 *
 *  The stylesheet spells the same number as `--column`, which `--doc` reads
 *  through `min(100%, …)`: on a window too narrow for it the window wins, which
 *  is why the floor below is computed against the ceiling rather than used on
 *  its own. */
const STANDARD = 760;

/** Where the dragged width is remembered. It belongs to this machine's screen,
 *  like the two sidebars' own widths, so it is stored beside them rather than in
 *  the kernel's config. */
export const FLOW_KEY = "rx-flow-w";

const clamp = (v: number, lo: number, hi: number) => Math.min(hi, Math.max(lo, v));

/** What the column may grow to: the window, less the room the two sidebars need
 *  at their narrowest. Past that there is nothing left to give, and on a window
 *  too small to hold even that, the ceiling falls with it rather than promising
 *  room the layout cannot hand over. */
export const flowCeiling = (room: number, railMin: number, dockMin: number) =>
  Math.max(0, Math.round(room - railMin - dockMin));

/** What the column may shrink to: the readable measure, or the whole window when
 *  the window is the smaller of the two. A drag runs between this and the
 *  ceiling, and neither end depends on where the column currently sits — so a
 *  column drawn at full width can still be pulled back to the readable measure,
 *  and one at the readable measure can still be pulled out. */
export const flowFloor = (room: number, railMin: number, dockMin: number) =>
  Math.min(STANDARD, flowCeiling(room, railMin, dockMin));

/** How wide each sidebar is drawn while the column asks for `ask`.
 *
 *  They give way in proportion to the widths they were set to, down to their own
 *  floors and no further. Narrowing the column again hands the room back in that
 *  same proportion — which is why neither ends up wider than its setting: the
 *  proportions are recomputed from the settings each time, never from the last
 *  width drawn. */
export function barsFit(room: number, ask: number, rail: number, dock: number, railMin: number, dockMin: number) {
  const want = rail + dock;
  const left = Math.max(railMin + dockMin, Math.round(room - ask));
  if (want <= left) return { rail, dock };
  // Both are clamped to what they were set to as well as to their floors: taking
  // room from one must never hand the other more than its own setting.
  const railFit = clamp(Math.round((left * rail) / want), railMin, Math.min(rail, left - dockMin));
  return { rail: railFit, dock: Math.min(dock, Math.max(dockMin, left - railFit)) };
}

/** The column's two ends, and what the sidebars are drawn at once it asks. */
export interface Column {
  width: number;
  floor: number;
  ceiling: number;
  rail: number;
  dock: number;
  /** Absent until a hand has moved the column: an untouched window keeps
   *  whatever the measure setting asks for, and that setting is read by the
   *  stylesheet, not by this number. */
  dragged: number;
  commit: (w: number) => void;
  /** How wide the sidebars are drawn while the column asks for `w`.
   *
   *  The same rule as `rail`/`dock`, but for a width a hand is still moving:
   *  a drag has to ask this on every frame, because the committed numbers only
   *  catch up when the pointer is released — and until they do, the column
   *  would grow into room the sidebars are still occupying. */
  fit: (w: number) => { rail: number; dock: number };
}

/** Everything the reading column's width is made of: the number a hand set, the
 *  two ends it is held between, and how much room that leaves the sidebars.
 *
 *  It lives here rather than in the window so the window stays a window: this is
 *  one feature's whole state, and the rules it obeys are the three pure
 *  functions above it. */
export function useColumn(railW: number, dockW: number, railMin: number, dockMin: number): Column {
  const [dragged, setDragged] = useState(() => {
    const saved = Number(localStorage.getItem(FLOW_KEY));
    return Number.isFinite(saved) && saved > 0 ? saved : 0;
  });
  const [room, setRoom] = useState(() => document.body.clientWidth);
  useEffect(() => onRoomWidth(setRoom), []);

  const floor = flowFloor(room, railMin, dockMin);
  const ceiling = flowCeiling(room, railMin, dockMin);
  // A window that shrank since the drag may leave the remembered number outside
  // the range it is now allowed; the drawn column, and the sidebars beside it,
  // both follow what is allowed rather than what was once asked for.
  const width = dragged > 0 ? clamp(dragged, floor, ceiling) : floor;
  const bars = dragged > 0 ? barsFit(room, width, railW, dockW, railMin, dockMin) : { rail: railW, dock: dockW };

  const commit = useCallback((w: number) => {
    setDragged(w);
    localStorage.setItem(FLOW_KEY, String(Math.round(w)));
  }, []);

  // The same rule the committed numbers above obey, asked for a width the
  // pointer is still holding. A drag reads this on every frame: committing on
  // release only is what let the column run under sidebars that had not moved
  // yet, so the room was taken from nothing.
  const fit = useCallback(
    (w: number) => barsFit(room, w, railW, dockW, railMin, dockMin),
    [room, railW, dockW, railMin, dockMin],
  );

  // The same object for as long as the numbers are the same. Every pane is
  // memoised on its props, so a fresh object each render would look like news to
  // all of them and re-draw every transcript in the window on an unrelated
  // state change — the one thing the pane memo exists to prevent.
  return useMemo(
    () => ({ width, floor, ceiling, rail: bars.rail, dock: bars.dock, dragged, commit, fit }),
    [width, floor, ceiling, bars.rail, bars.dock, dragged, commit, fit],
  );
}

/** The two edges, as things a pointer can take hold of.
 *
 *  They are placed against the column rather than by repeating its width rule
 *  here: that rule depends on what the scroller reserves for the scrollbar, and a
 *  copy of it would drift from the original the first time either changed.
 *
 *  The box to measure against is this element's own parent, not a ref handed
 *  down: a child's layout effects run before its parent's ref is attached, so
 *  asking the parent through a ref would read null and never try again. */
export function FlowGrips({ column }: { column: Column }) {
  const grips = useRef<HTMLDivElement>(null);

  useLayoutEffect(() => {
    const box = grips.current;
    const outer = box?.parentElement;
    if (!box || !outer) return;
    const place = () => {
      const flow = outer.querySelector(".flow");
      if (!flow) return;
      const a = outer.getBoundingClientRect();
      const b = flow.getBoundingClientRect();
      box.style.setProperty("--grip-left", `${Math.round(b.left - a.left)}px`);
      box.style.setProperty("--grip-right", `${Math.round(a.right - b.right)}px`);
    };
    place();
    const ro = new ResizeObserver(place);
    ro.observe(outer);
    const flow = outer.querySelector(".flow");
    if (flow) ro.observe(flow);
    return () => ro.disconnect();
  }, []);

  return (
    <div className="flowgrips" ref={grips}>
      {(["l", "r"] as const).map((edge) => (
        <FlowGrip key={edge} edge={edge} column={column} />
      ))}
    </div>
  );
}

/** Put a width on the window while a hand still holds the edge: the column
 *  itself, and the two sidebars giving way in proportion to what they were set
 *  to.
 *
 *  Everything is written to the `.app` element, never to `:root`. Once a width
 *  has been committed, `App` keeps `--flow-w` on `.app` inline, and an inherited
 *  `:root` value can never outrank its own descendant — which is why every drag
 *  after the first one moved nothing at all. */
function paint(app: HTMLElement | null, w: number, column: Column) {
  const bars = column.fit(w);
  const target = app ?? document.documentElement;
  target.style.setProperty("--flow-w", `${w}px`);
  target.style.setProperty("--rail-open", `${bars.rail}px`);
  target.style.setProperty("--dock-open", `${bars.dock}px`);
}

function FlowGrip({ edge, column }: { edge: "l" | "r"; column: Column }) {
  // The drag's own state, kept out of React so a pointer that moves sixty times
  // a second does not re-render the transcript behind it.
  const drag = useRef<{ x0: number; start: number; pending: number; app: HTMLElement | null } | null>(null);
  const raf = useRef(0);

  const down = useCallback(
    (e: ReactPointerEvent<HTMLDivElement>) => {
      if (e.button !== 0) return;
      e.preventDefault();
      e.currentTarget.setPointerCapture(e.pointerId);
      drag.current = {
        x0: e.clientX,
        start: column.width,
        pending: column.width,
        app: e.currentTarget.closest(".app") as HTMLElement | null,
      };
      document.body.dataset.flowdrag = edge;
    },
    [edge, column.width],
  );

  const move = useCallback(
    (e: ReactPointerEvent<HTMLDivElement>) => {
      const d = drag.current;
      if (!d) return;
      const dx = e.clientX - d.x0;
      // Both edges move the same number, so the column grows at both ends and
      // the pointer travels half of what it gains.
      d.pending = clamp(d.start + (edge === "l" ? -dx : dx) * 2, column.floor, column.ceiling);
      if (!raf.current) {
        raf.current = requestAnimationFrame(() => {
          raf.current = 0;
          const cur = drag.current;
          if (cur) paint(cur.app, cur.pending, column);
        });
      }
    },
    [edge, column],
  );

  // Everything a drag leaves behind in the document, undone in one place: the
  // pending frame and the flag the stylesheet reads to turn every cursor into a
  // resize one. A drag that ends any way at all has to pass through this, or the
  // window keeps a col-resize cursor over everything with no way to clear it.
  const stop = useCallback(() => {
    cancelAnimationFrame(raf.current);
    raf.current = 0;
    drag.current = null;
    delete document.body.dataset.flowdrag;
  }, []);

  const up = useCallback(() => {
    const d = drag.current;
    if (!d) return;
    const wanted = d.pending;
    // Leave the numbers where the hand left them: the commit below re-renders
    // with the same values, and the frame in between must not fall back to the
    // width the column started at.
    paint(d.app, wanted, column);
    stop();
    column.commit(wanted);
  }, [column, stop]);

  // The drag never committed, so the width the reader let go of is dropped: the
  // column and both sidebars go back to the numbers React is holding.
  const cancel = useCallback(() => {
    const d = drag.current;
    if (!d) return;
    if (d.app) {
      // A width that was never committed has no owner on `.app`: `App` writes
      // `--flow-w` itself only once something has been dragged. Putting a pixel
      // value back here would leave it owned by nobody, so React would never
      // remove it and the column would stay pinned to that width while the
      // window was free to change around it.
      if (column.dragged > 0) d.app.style.setProperty("--flow-w", `${column.width}px`);
      else d.app.style.removeProperty("--flow-w");
      d.app.style.setProperty("--rail-open", `${column.rail}px`);
      d.app.style.setProperty("--dock-open", `${column.dock}px`);
    }
    stop();
  }, [column, stop]);

  // Switching conversation, closing the tab or collapsing the pane mid-drag
  // unmounts this element with the pointer still down. Nothing would arrive to
  // release it, so the cleanup does what the release would have: the width it
  // had reached is dropped rather than committed — the reader never let go.
  useEffect(() => cancel, [cancel]);

  // The keys walk the same path the hand does: an arrow towards this edge's own
  // side of the window widens the column, so the left handle's ArrowLeft and the
  // right handle's ArrowRight both mean "wider". Reversing it per handle would
  // make one of the two teach the opposite of what dragging it does.
  const key = useCallback(
    (e: ReactKeyboardEvent<HTMLDivElement>) => {
      const dir = e.key === "ArrowLeft" ? -1 : e.key === "ArrowRight" ? 1 : 0;
      if (dir) {
        e.preventDefault();
        const grow = edge === "l" ? -dir : dir;
        const next = clamp(column.width + grow * (e.shiftKey ? 48 : 12), column.floor, column.ceiling);
        if (next !== column.width) column.commit(next);
        return;
      }
      if (e.key === "Home") {
        e.preventDefault();
        column.commit(column.floor);
      }
    },
    [edge, column],
  );

  return (
    <div
      className={`flowgrip flowgrip-${edge}`}
      role="separator"
      aria-orientation="vertical"
      aria-label={t("调整正文字宽")}
      aria-valuenow={Math.round(column.width)}
      aria-valuemin={column.floor}
      aria-valuemax={column.ceiling}
      tabIndex={0}
      data-action-pointerdown="appearance.reading-width"
      data-action-pointermove="appearance.reading-width"
      data-action-pointerup="appearance.reading-width"
      data-action-pointercancel="appearance.reading-width"
      data-action-keydown="appearance.reading-width"
      onKeyDown={key}
      onPointerDown={down}
      onPointerMove={move}
      onPointerUp={up}
      onPointerCancel={cancel}
    />
  );
}
