import type { WireEvent } from "../port/wire";
import type { Item, ThoughtCut } from "./session_types";
import { nextId } from "./ids";

// Keyed by item id rather than carried on the item: a start time is not part of
// what the card renders, and putting it there would make every append rewrite it.
const thoughtSince = new Map<string, number>();

/** When this window saw the card start thinking; undefined for a card it did
 *  not watch begin, which then has no clock to run. */
export function thoughtStartedAt(id: string): number | undefined {
  return thoughtSince.get(id);
}

function openSayIndex(items: Item[]): number {
  for (let i = items.length - 1; i >= 0; i--) {
    const item = items[i];
    if (item.t === "say") return item.done ? -1 : i;
    if (item.t === "user" && !item.pending) return -1;
  }
  return -1;
}

// A partial tool dispatch ends thinking but its response still owes a message frame.
export function stopThought(items: Item[]): Item[] {
  const at = openSayIndex(items);
  if (at < 0) return items;
  const item = items[at] as Extract<Item, { t: "say" }>;
  if (!item.reasoning || item.thoughtMs !== undefined) return items;
  const thoughtMs = Math.max(0, Date.now() - (thoughtSince.get(item.id) ?? Date.now()));
  thoughtSince.delete(item.id);
  const next = items.slice();
  next[at] = { ...item, thoughtMs };
  return next;
}

export function appendText(items: Item[], text: string, field: "text" | "reasoning", source?: string, model?: string): Item[] {
  const at = openSayIndex(items);
  if (field === "text") items = stopThought(items);
  const last = items[at];
  if (last && last.t === "say" && !last.done) {
    const next = items.slice();
    next[at] = {
      ...last,
      [field]: (last[field] ?? "") + text,
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
// cut is why the turn ended early; it marks only a thought still arriving.
export function sealSay(items: Item[], all = false, cut?: ThoughtCut): Item[] {
  let next: Item[] | null = null;
  for (let i = items.length - 1; i >= 0; i--) {
    const it = items[i];
    if (it.t !== "say" || it.done) continue;
    next ??= items.slice();
    const ran = it.thoughtMs ?? (it.reasoning ? Date.now() - (thoughtSince.get(it.id) ?? Date.now()) : undefined);
    thoughtSince.delete(it.id);
    const midThought = cut && it.reasoning && it.thoughtMs === undefined && !it.text;
    next[i] = { ...it, done: true, thoughtMs: ran, ...(midThought ? { cut } : null) };
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
  const at = openSayIndex(items);
  // The kernel's measure wins over this window's: it is the one the transcript
  // keeps, so a reopened turn says the same thing.
  const measured = ev.thoughtMs;
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
