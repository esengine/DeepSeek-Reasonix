import type { ZoomRange } from "../port/port";

export const ZOOM_PRESETS: [number, string][] = [
  [0.9, "紧凑"],
  [1, "标准"],
  [1.15, "宽松"],
  [1.3, "更大"],
];

const EPS = 1e-9;

/** The next interface scale for a keyboard press: -1 steps down, 1 up, 0 resets
 *  to standard. The rungs are the named presets with the kernel's range ends
 *  outside them, so a press always lands on something the control can show; a
 *  value in between, from the slider, moves to the next rung in that direction.
 *  Nothing is answered until the kernel has announced a range. */
export function stepZoom(current: number | undefined, range: ZoomRange | undefined, dir: -1 | 0 | 1): number | undefined {
  if (!range) return undefined;
  const hold = (v: number) => Math.min(Math.max(v, range.min), range.max);
  if (dir === 0) return hold(1);
  const rungs = [range.min, ...ZOOM_PRESETS.map(([v]) => v).filter((v) => v > range.min && v < range.max), range.max];
  const at = current || 1;
  const next = dir > 0 ? rungs.find((v) => v > at + EPS) : [...rungs].reverse().find((v) => v < at - EPS);
  return hold(next ?? at);
}
