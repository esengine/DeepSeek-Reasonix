// Geometric primitives used by the renderer, screen, and selection code.
// Kept tiny and dependency-free on purpose: these types appear in hot paths
// and on every layout result, so allocation cost matters.

export type Point = {
  x: number;
  y: number;
};

export type Size = {
  width: number;
  height: number;
};

export type Rectangle = Point & Size;

// Edge insets covering padding, margin and border. Modelled after the
// CSS box model so values map cleanly to Yoga's per-edge setters.
export type Edges = {
  top: number;
  right: number;
  bottom: number;
  left: number;
};

// Three overloads mirror the CSS shorthand syntax: `edges(all)`, the
// `edges(vertical, horizontal)` pair, and full per-side specification.
// Co-locating the overloads with the implementation keeps callers honest —
// passing three or "wrong" counts is a compile error.
export function edges(all: number): Edges;
export function edges(vertical: number, horizontal: number): Edges;
export function edges(
  top: number,
  right: number,
  bottom: number,
  left: number,
): Edges;
export function edges(a: number, b?: number, c?: number, d?: number): Edges {
  if (b === undefined) {
    return { top: a, right: a, bottom: a, left: a };
  }
  if (c === undefined) {
    return { top: a, right: b, bottom: a, left: b };
  }
  return { top: a, right: b, bottom: c, left: d! };
}

// Sum of two edge values — used when stacking insets such as
// padding + border to derive the inner content rect.
export function addEdges(a: Edges, b: Edges): Edges {
  return {
    top: a.top + b.top,
    right: a.right + b.right,
    bottom: a.bottom + b.bottom,
    left: a.left + b.left,
  };
}

// Shared empty-edges constant. Frozen at the type level via the export
// signature; do not mutate.
export const ZERO_EDGES: Edges = { top: 0, right: 0, bottom: 0, left: 0 };

// Normalises a `Partial<Edges>` into a fully-populated value. Missing
// sides default to zero so downstream arithmetic never sees `undefined`.
export function resolveEdges(partial?: Partial<Edges>): Edges {
  return {
    top: partial?.top ?? 0,
    right: partial?.right ?? 0,
    bottom: partial?.bottom ?? 0,
    left: partial?.left ?? 0,
  };
}

// Axis-aligned bounding box of two rectangles. The renderer uses this when
// merging dirty regions — combining two redraw rects into a single tighter
// invalidation.
export function unionRect(a: Rectangle, b: Rectangle): Rectangle {
  const minX = Math.min(a.x, b.x);
  const minY = Math.min(a.y, b.y);
  const maxX = Math.max(a.x + a.width, b.x + b.width);
  const maxY = Math.max(a.y + a.height, b.y + b.height);
  return { x: minX, y: minY, width: maxX - minX, height: maxY - minY };
}

// Clips a rectangle to the visible screen area. Uses inclusive coordinates
// internally (hence the `-1`/`+1` dance) because terminal cells are indexed
// from 0 and a width of N occupies columns 0..N-1.
export function clampRect(rect: Rectangle, size: Size): Rectangle {
  const minX = Math.max(0, rect.x);
  const minY = Math.max(0, rect.y);
  const maxX = Math.min(size.width - 1, rect.x + rect.width - 1);
  const maxY = Math.min(size.height - 1, rect.y + rect.height - 1);
  return {
    x: minX,
    y: minY,
    width: Math.max(0, maxX - minX + 1),
    height: Math.max(0, maxY - minY + 1),
  };
}

export function withinBounds(size: Size, point: Point): boolean {
  return (
    point.x >= 0 &&
    point.y >= 0 &&
    point.x < size.width &&
    point.y < size.height
  );
}

// Both bounds are optional so callers can use this for "clamp to floor",
// "clamp to ceiling", or full clamp without separate helpers.
export function clamp(value: number, min?: number, max?: number): number {
  if (min !== undefined && value < min) return min;
  if (max !== undefined && value > max) return max;
  return value;
}
