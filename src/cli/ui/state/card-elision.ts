import type { Card, DiffCard, ReasoningCard, StreamingCard, ToolCard } from "./cards.js";

/** Heavy card fields older than this many cards get stubbed so long sessions don't keep one-off file reads, reasoning streams, diff hunks, or large tool inputs pinned in the heap. */
const RECENT_CARDS_WINDOW = 200;
/** Don't bother eliding tiny payloads — the stub is itself ~150 chars and the savings aren't worth the lost context. */
const MIN_ELIDE_OUTPUT_LENGTH = 4096;
/** Marker for already-elided fields so we don't re-stub on every subsequent append. */
const ELIDED_PREFIX = "[elided — older than the last ";

function elidedStub(originalChars: number): string {
  return `${ELIDED_PREFIX}${RECENT_CARDS_WINDOW} cards; ${originalChars.toLocaleString()} chars dropped to save memory. Full content is on disk in the session log.]`;
}

function serializedArgChars(args: unknown): number {
  if (typeof args === "string") return args.length;
  try {
    return JSON.stringify(args ?? null).length;
  } catch {
    return 0;
  }
}

function isElidedString(value: unknown): boolean {
  return typeof value === "string" && value.startsWith(ELIDED_PREFIX);
}

function stubHeavyContent(c: Card): Card {
  switch (c.kind) {
    case "tool": {
      const t = c as ToolCard;
      let next: ToolCard | null = null;
      const out = t.output;
      if (typeof out === "string" && out.length > MIN_ELIDE_OUTPUT_LENGTH && !isElidedString(out)) {
        next = { ...t, output: elidedStub(out.length) };
      }
      const args = (next ?? t).args;
      const argsChars = serializedArgChars(args);
      if (argsChars > MIN_ELIDE_OUTPUT_LENGTH && !isElidedString(args)) {
        next = { ...(next ?? t), args: elidedStub(argsChars) };
      }
      return next ?? c;
    }
    case "reasoning": {
      const r = c as ReasoningCard;
      if (r.streaming) return c;
      if (r.text.length <= MIN_ELIDE_OUTPUT_LENGTH) return c;
      if (isElidedString(r.text)) return c;
      return { ...r, text: elidedStub(r.text.length) };
    }
    case "streaming": {
      const s = c as StreamingCard;
      if (!s.done) return c;
      if (s.text.length <= MIN_ELIDE_OUTPUT_LENGTH) return c;
      if (isElidedString(s.text)) return c;
      return { ...s, text: elidedStub(s.text.length) };
    }
    case "diff": {
      const d = c as DiffCard;
      if (d.hunks.length === 0) return c;
      let totalChars = 0;
      for (const h of d.hunks) for (const l of h.lines) totalChars += l.text.length;
      if (totalChars <= MIN_ELIDE_OUTPUT_LENGTH) return c;
      return { ...d, hunks: [] };
    }
    default:
      return c;
  }
}

/** True when card content is fixed at append time — never mutated, never grows. */
function isImmutableCardKind(kind: Card["kind"]): boolean {
  return (
    kind === "user" ||
    kind === "plan" ||
    kind === "usage" ||
    kind === "ctx" ||
    kind === "doctor" ||
    kind === "tip" ||
    kind === "live" ||
    kind === "memory" ||
    kind === "search" ||
    kind === "error" ||
    kind === "warn" ||
    kind === "compaction"
  );
}

function canStillGrow(c: Card, assumeSettled: boolean): boolean {
  if (
    assumeSettled &&
    (c.kind === "tool" || c.kind === "reasoning" || c.kind === "streaming" || c.kind === "diff")
  ) {
    return false;
  }
  switch (c.kind) {
    case "tool":
      return !(c as ToolCard).done;
    case "reasoning":
      return (c as ReasoningCard).streaming;
    case "streaming":
      return !(c as StreamingCard).done;
    case "diff":
      return false;
    default:
      return !isImmutableCardKind(c.kind);
  }
}

export function elideFromCursor(
  cards: ReadonlyArray<Card>,
  cursor: number,
  opts: { assumeSettled?: boolean } = {},
): { cards: ReadonlyArray<Card>; cursor: number } {
  if (cards.length < RECENT_CARDS_WINDOW) return { cards, cursor };
  const cutoff = cards.length + 1 - RECENT_CARDS_WINDOW;
  let next: Card[] | null = null;
  let nextCursor = cursor;
  for (let i = cursor; i < cutoff; i++) {
    const c = cards[i]!;
    const stubbed = stubHeavyContent(c);
    if (stubbed !== c) {
      if (next === null) next = cards.slice();
      next[i] = stubbed;
      nextCursor = i + 1;
      continue;
    }
    if (!canStillGrow(c, opts.assumeSettled === true)) {
      nextCursor = i + 1;
      continue;
    }
    break;
  }
  return { cards: next ?? cards, cursor: nextCursor };
}

export function elideHydratedCards(cards: ReadonlyArray<Card>): ReadonlyArray<Card> {
  return elideFromCursor(cards, 0, { assumeSettled: true }).cards;
}
