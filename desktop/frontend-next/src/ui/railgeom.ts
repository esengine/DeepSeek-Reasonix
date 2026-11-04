/** One drawn thing on the message rail: a single mark, or a run of marks too
 *  close to tell apart, drawn as one segment from its first to its last. */
export interface RailView {
  top: number;
  bottom: number;
  from: number;
  to: number;
  single: boolean;
}

export const PAD = 18;
// Room the earlier-messages marker takes above the first mark.
export const HEAD = 34;
// Closer than this, two lines are neither told apart by eye nor by pointer.
const CLUSTER_AT = 5;
// A run longer than this stops reading as one place in the conversation.
const MAX_RUN = 56;
const GROW = [12, 6, 2];

/** Where each mark sits, from its place in the whole conversation (0..1) to a
 *  y on a rail `rail` tall, with `head` reserved above the first. */
export function place(fractions: number[], rail: number, head = 0): number[] {
  const room = Math.max(0, rail - PAD * 2 - head);
  return fractions.map((f) => PAD + head + Math.min(1, Math.max(0, f)) * room);
}

export function fold(tops: number[]): RailView[] {
  const out: RailView[] = [];
  let i = 0;
  while (i < tops.length) {
    let j = i;
    while (j + 1 < tops.length && tops[j + 1] - tops[j] < CLUSTER_AT && tops[j + 1] - tops[i] <= MAX_RUN) j++;
    out.push({ top: tops[i], bottom: tops[j], from: i, to: j, single: i === j });
    i = j + 1;
  }
  return out;
}

/** How much longer a line draws for being `d` drawn things from the pointer. */
export function grow(d: number): number {
  return d >= 0 && d < GROW.length ? GROW[d] : 0;
}

/** The stretch of the rail the viewport is showing, or null when the whole
 *  conversation fits and there is nothing to place it within. */
export function viewBand(scrollTop: number, clientHeight: number, scrollHeight: number, rail: number, head = 0) {
  if (scrollHeight <= clientHeight + 1) return null;
  const [top, bottom] = place([scrollTop / scrollHeight, (scrollTop + clientHeight) / scrollHeight], rail, head);
  return { top, height: Math.max(16, bottom - top) };
}

/** The drawn thing nearest y, or -1 when y is on bare track far from any. */
export function nearest(view: RailView[], y: number, reach = 40): number {
  let best = -1;
  let dist = Infinity;
  view.forEach((v, i) => {
    const d = y < v.top ? v.top - y : y > v.bottom ? y - v.bottom : 0;
    if (d < dist) {
      dist = d;
      best = i;
    }
  });
  return dist <= reach ? best : -1;
}

/** Inside a run, the mark whose own place is nearest y. */
export function within(tops: number[], v: RailView, y: number): number {
  let best = v.from;
  for (let k = v.from; k <= v.to; k++) if (Math.abs(tops[k] - y) < Math.abs(tops[best] - y)) best = k;
  return best;
}
