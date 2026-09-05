// How much room the interface actually has, published for the stylesheet to
// match on. A media query answers for the real viewport, and the zoom setting
// does not move it — at 1.3 everything is a third larger while the layout still
// believes it has the whole screen, so the columns never give way when they have
// run out of room. document.body is already laid out in the scaled space, so its
// content box is the viewport divided by the zoom with no arithmetic here.

// A fold is a column the interface gives up. They are cumulative by
// construction: anything narrow enough to drop the metrics column has already
// dropped the workspace one. The stylesheet names the fold, never the number.
const FOLDS = [
  { upTo: 1200, fold: "rail" }, // the workspace column goes
  { upTo: 840, fold: "side" }, // metrics moves under the flow
  { upTo: 640, fold: "scene" }, // the opening scene tightens
] as const;

// Height folds the same way width does, and for the same reason: every surface
// that stacks above or below the transcript is budgeted as a fraction of the
// window, and on a short one those fractions add up to the transcript. Under
// "side" the metrics column is one of them — it moved *under* the flow.
const TALL = [{ downTo: 720, fold: "short" }] as const;

/** foldsAt names everything given up at this size, widest fold first. Height is
 *  optional so a caller asking only about columns still reads. */
export function foldsAt(width: number, height = Infinity): string {
  const cols = FOLDS.filter((f) => width <= f.upTo).map((f) => f.fold);
  const rows = TALL.filter((f) => height <= f.downTo).map((f) => f.fold);
  return [...cols, ...rows].join(" ");
}

const subs = new Set<(folds: string) => void>();

/** refresh re-reads the room and republishes it. Two things change it — the
 *  window resizing and the zoom setting — and each calls this rather than
 *  keeping its own copy of the thresholds. */
export function refresh() {
  publishShape();
  const now = foldsAt(document.body.clientWidth, document.body.clientHeight);
  // Only on a real change: writing the attribute is a style invalidation, and an
  // unconditional write inside the observer is how a resize loop starts.
  if (document.documentElement.dataset.fold === now) return;
  document.documentElement.dataset.fold = now;
  subs.forEach((fn) => fn(now));
}

// The window's shape, not its size: anything drawn *in* the window rather than
// beside it has to be laid out at this ratio or it shows something else. A
// `cover` background only leaves a focal point room on the axis that overflows,
// so a preview at any other ratio moves along the wrong axis — see .paperview.
// Zoom cancels out of a ratio, which is why this reads the viewport directly.
function publishShape() {
  if (!innerHeight) return;
  const ar = String(Math.round((innerWidth / innerHeight) * 1000) / 1000);
  const style = document.documentElement.style;
  if (style.getPropertyValue("--win-ar") === ar) return;
  style.setProperty("--win-ar", ar);
}

/** onFolds reports a change in what the layout has given up. A column that folds
 *  also has to stop being open, and open is state the stylesheet cannot reach. */
export function onFolds(fn: (folds: string) => void): () => void {
  subs.add(fn);
  return () => subs.delete(fn);
}

/** folded answers for right now — for state that has to be right on first paint,
 *  before any change has been reported. */
export function folded(name: string): boolean {
  return (document.documentElement.dataset.fold ?? "").split(" ").includes(name);
}

/** track publishes the width for the life of the page. */
export function track(): () => void {
  refresh();
  const ro = new ResizeObserver(refresh);
  ro.observe(document.body);
  return () => ro.disconnect();
}
