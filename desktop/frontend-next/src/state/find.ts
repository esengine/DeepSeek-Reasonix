import type { Item } from "./session_types";

/** Searching the conversation reads the rows, never the document: the transcript
 *  mounts a few dozen cards out of however many the session holds, so anything
 *  reading the DOM can only see the part that happens to be on screen. */
export interface Hit {
  id: string;
  row: number;
  at: number;
}

/** Everything a row says, in reading order. Exhaustive over Item on purpose: a
 *  new kind of card has to answer what text it carries. */
export function saidBy(it: Item): string[] {
  switch (it.t) {
    case "user":
      return [it.text];
    case "say":
      return [it.reasoning ?? "", it.text];
    case "tool":
      return [
        it.tool.resolvedName || it.tool.name,
        it.tool.args ?? "",
        it.tool.diff ?? "",
        it.tool.output ?? "",
        it.tool.err ?? "",
        ...it.children.flatMap((c) => [c.name, c.args ?? "", c.output ?? "", c.err ?? ""]),
      ];
    case "reads":
      return it.tools.flatMap((c) => [c.name, c.args ?? "", c.output ?? "", c.err ?? ""]);
    case "guardian":
      return [it.g.tool, it.g.subject, it.g.rationale ?? ""];
    case "approval":
      return [it.a.tool, it.a.subject, it.a.reason ?? ""];
    case "ask":
      return [...it.ask.questions.flatMap((q) => [q.header ?? "", q.prompt, ...q.options.map((o) => o.label)]), it.recorded ?? ""];
    case "compaction":
      return [it.c.summary ?? ""];
    case "remember":
      return [it.m.title, it.m.description, it.m.body];
    case "receipt":
      return [
        ...(it.r.changes ?? []).map((c) => c.path),
        ...(it.r.verifications ?? []).map((v) => v.command),
        ...(it.r.gaps ?? []).map((g) => `${g.kind} ${g.detail ?? ""}`),
        ...(it.r.risks ?? []),
        ...(it.r.unverified ?? []),
      ];
    case "extension":
      return [it.ext.pluginId, it.ext.card?.title ?? "", it.ext.card?.text ?? "", it.ext.card?.markdown ?? "",
        it.ext.notification?.title ?? "", it.ext.notification?.body ?? "", it.ext.status?.label ?? ""];
    case "notice":
      return [it.text, it.detail ?? ""];
  }
}

// A row's text cannot change without the row being replaced, so the fold is
// cached against the object and a growing session pays only for what it grew.
const folded = new WeakMap<Item, string>();

function haystack(it: Item): string {
  const held = folded.get(it);
  if (held !== undefined) return held;
  const made = saidBy(it).filter(Boolean).join("\n").toLocaleLowerCase();
  folded.set(it, made);
  return made;
}

export function search(items: Item[], query: string): Hit[] {
  const needle = query.trim().toLocaleLowerCase();
  if (!needle) return [];
  const out: Hit[] = [];
  for (let row = 0; row < items.length; row++) {
    const hay = haystack(items[row]);
    for (let at = hay.indexOf(needle); at >= 0; at = hay.indexOf(needle, at + needle.length)) {
      out.push({ id: items[row].id, row, at });
      if (out.length >= MAX) return out;
    }
  }
  return out;
}

/** Past this the count stops being a number anyone acts on. Reported as "{n}+"
 *  rather than silently cut. */
export const MAX = 2000;

/** Wraps in both directions: stopping at the last hit makes the reader find the
 *  first one by hand. */
export const stepped = (index: number, total: number, by: 1 | -1) =>
  total === 0 ? 0 : (index + by + total) % total;
