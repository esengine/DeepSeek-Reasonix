import type { WireEvent } from "../port/wire";
import type { Item } from "./session_types";
import { nextId } from "./ids";

// Keyed by item id rather than carried on the item: a start time is not part of
// what the card renders, and putting it there would make every append rewrite it.
const thoughtSince = new Map<string, number>();

/** When this window saw the card start thinking; undefined for a card it did
 *  not watch begin, which then has no clock to run. */
export function thoughtStartedAt(id: string): number | undefined {
  return thoughtSince.get(id);
}

export function appendText(items: Item[], text: string, field: "text" | "reasoning", source?: string, model?: string): Item[] {
  const last = items[items.length - 1];
  if (last && last.t === "say" && !last.done) {
    const next = items.slice();
    // Thinking runs until the first answer token, so the clock is the gap
    // between the two streams, not the length of either.
    const stop = field === "text" && last.thoughtMs === undefined && last.reasoning;
    next[next.length - 1] = {
      ...last,
      [field]: (last[field] ?? "") + text,
      ...(stop ? { thoughtMs: Date.now() - (thoughtSince.get(last.id) ?? Date.now()) } : null),
    };
    return next;
  }
  const id = nextId();
  if (field === "reasoning") thoughtSince.set(id, Date.now());
  return [...items, { t: "say", id, text: "", done: false, source, model, [field]: text }];
}

// Settles the message still being written. turn_done settles every open one:
// a tool call between two answers leaves the earlier card unsealed forever, and
// an unsealed card keeps a caret blinking and its reveal clock running on text
// nothing will add to. Returning the same array keeps every card's memo intact.
export function sealSay(items: Item[], all = false): Item[] {
  let next: Item[] | null = null;
  for (let i = items.length - 1; i >= 0; i--) {
    const it = items[i];
    if (it.t !== "say" || it.done) continue;
    next ??= items.slice();
    const ran = it.thoughtMs ?? (it.reasoning ? Date.now() - (thoughtSince.get(it.id) ?? Date.now()) : undefined);
    thoughtSince.delete(it.id);
    next[i] = { ...it, done: true, thoughtMs: ran };
    if (!all) break;
  }
  return next ?? items;
}

// A message frame is the assistant turn in its settled form. The deltas before
// it were the same text arriving in pieces, and a transcript rebuilt from the
// record has only this frame — so one fold serves both: it settles the open
// card when there is one, and builds the card when there is none. That is what
// makes a rebuilt transcript the same transcript rather than a shorter one.
//
// The frame also wins on content. It carries the display-correct text with the
// host's own markers already out of it, and the reasoning an extension may have
// replaced — the kernel emits it so that what is shown and what is persisted
// agree. A stream that lost a chunk is therefore repaired here, never
// preserved; an empty field is not content and overwrites nothing.
export function foldMessage(items: Item[], ev: WireEvent): Item[] {
  let at = -1;
  for (let i = items.length - 1; i >= 0; i--) {
    if (items[i].t === "say" && !(items[i] as Extract<Item, { t: "say" }>).done) {
      at = i;
      break;
    }
  }
  // The kernel's measure wins over this window's: it is the one the transcript
  // keeps, so a reopened turn says the same thing.
  const measured = ev.thoughtMs || undefined;
  if (at < 0) {
    if (!ev.text && !ev.reasoning) return items;
    return [...items, { t: "say", id: nextId(), text: ev.text ?? "", reasoning: ev.reasoning, done: true, source: ev.source, thoughtMs: measured }];
  }
  const open = items[at] as Extract<Item, { t: "say" }>;
  const next = items.slice();
  const ran = measured ?? open.thoughtMs ?? (open.reasoning ? Date.now() - (thoughtSince.get(open.id) ?? Date.now()) : undefined);
  thoughtSince.delete(open.id);
  next[at] = {
    ...open,
    done: true,
    thoughtMs: ran,
    text: ev.text || open.text,
    reasoning: ev.reasoning || open.reasoning,
    source: open.source ?? ev.source,
  };
  return next;
}
