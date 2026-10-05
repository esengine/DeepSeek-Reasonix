// How a stored fold bound reads as an intent. The field holds one number and
// the number is not the intent: 0 and 160000 fold at the same place today and
// mean different things the moment the default moves, and a negative value is
// not a smaller threshold at all — it retires the economic bound and leaves the
// window share still firing.
//
// Shared because the setting is offered in two places — the settings sheet and
// the context rail — and those are one intent, not two.
export type FoldMode = "custom" | "capacity";

export const foldModeOf = (stored: number): FoldMode =>
  stored > 0 ? "custom" : "capacity";

// What each mode writes. Custom is absent: it is a choice before it is a
// number, and the write happens when a number is committed.
export const foldModeValue: Partial<Record<FoldMode, number>> = { capacity: 0 };

// How finely the rail's handle moves. A fixed step is wrong at both ends: 8k
// leaves a 128k window 15 positions, and 1k leaves a 1M window a thousand of
// them, which is a drag nobody can land on a round number with. Rounded in
// thousands rather than in powers of two, so the grid passes through the
// defaults people actually have — a 4096-aligned grid puts 160k between two
// stops and the handle can never sit on the number the panel is naming.
export const foldStep = (window: number) => Math.max(1000, 1000 * Math.round(window / 128000));

// Which intent a dragged position expresses. The two ends are not "very large"
// and "very small" — they are the other two answers, so the handle can say all
// three and the number under it stays the one the kernel stores.
//
// The detent is two steps wide, not one: one step is about two pixels of track,
// which is a target nobody hits, and landing next to the default instead of on
// it silently converts "use the default" into a number that stops following it.
export function foldIntent(value: number, capacity: number): number {
  if (value >= capacity) return 0;
  return value;
}
