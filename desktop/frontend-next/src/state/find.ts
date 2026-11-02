import type { Item } from "./session_types";

/** Searching the conversation reads the rows, never the document: the transcript
 *  mounts a few dozen cards out of however many the session holds, so anything
 *  reading the DOM can only see the part that happens to be on screen. */
export interface Hit {
  id: string;
  row: number;
  at: number;
  /** How much the hit covers: the needle's length, or the regex match's. */
  length: number;
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
    case "worked":
      return [];
    case "origin":
      return [it.text];
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

// The same text before it was flattened to lower case: a case-sensitive search
// and a regular expression both have to see the row as it was written.
const exact = new WeakMap<Item, string>();

function exactHaystack(it: Item): string {
  const held = exact.get(it);
  if (held !== undefined) return held;
  const made = saidBy(it).filter(Boolean).join("\n");
  exact.set(it, made);
  return made;
}

function haystack(it: Item): string {
  const held = folded.get(it);
  if (held !== undefined) return held;
  const made = saidBy(it).filter(Boolean).join("\n").toLocaleLowerCase();
  folded.set(it, made);
  return made;
}

/** How the query is read. Both default to off, which is the plain
 *  case-insensitive substring search the box has always done. */
export interface FindMode {
  regex?: boolean;
  caseSensitive?: boolean;
}

/** One match: where it starts and how much it covers. */
export interface Match {
  at: number;
  length: number;
}

export type Matcher = (text: string, from: number) => Match | null;

/** How one query is read. The count and the painter share this, so a hit the
 *  counter sees is one the painter can draw. */
export interface Finder {
  match: Matcher;
  /** Whether the text must be read as written (a regex, or a case-sensitive
   *  search). A substring search reads the folded copy instead. */
  verbatim: boolean;
}

/** null when the query is empty or the pattern does not compile yet: a
 *  half-typed `(` is not an error worth a message, the box just has nothing to
 *  point at. */
export function finderFor(query: string, mode: FindMode = {}): Finder | null {
  const text = query.trim();
  if (!text) return null;
  if (mode.regex) {
    let re: RegExp;
    try {
      re = new RegExp(text, mode.caseSensitive ? "g" : "gi");
    } catch {
      return null;
    }
    return {
      verbatim: true,
      match: (hay, from) => {
        re.lastIndex = from;
        const hit = re.exec(hay);
        return hit ? { at: hit.index, length: hit[0].length } : null;
      },
    };
  }
  if (mode.caseSensitive) {
    return {
      verbatim: true,
      match: (hay, from) => {
        const at = hay.indexOf(text, from);
        return at < 0 ? null : { at, length: text.length };
      },
    };
  }
  const needle = text.toLocaleLowerCase();
  return {
    verbatim: false,
    match: (hay, from) => {
      const at = hay.indexOf(needle, from);
      return at < 0 ? null : { at, length: needle.length };
    },
  };
}

/** The match after `m`. A plain substring advances by its own length, so
 *  overlapping text is counted the way it always was ("aa" in "aaaa" is two,
 *  not three). A regex may match nothing at all (`^`, a lookahead), so an empty
 *  match steps one position instead of standing still. */
export function afterMatch(finder: Finder, hay: string, m: Match): Match | null {
  const from = m.at + (m.length || 1);
  return from > hay.length ? null : finder.match(hay, from);
}

/** One search may not spend longer than this on a pathological pattern: a typed
 *  `(a+)+$` would otherwise run against the whole transcript on every keystroke
 *  and freeze the window. Cutting it short keeps the window usable; the count
 *  then speaks for what was scanned. */
const BUDGET_MS = 50;

export function search(items: Item[], query: string, receipts = false, mode: FindMode = {}): Hit[] {
  const finder = finderFor(query, mode);
  if (!finder) return [];
  const out: Hit[] = [];
  const deadline = Date.now() + BUDGET_MS;
  for (let row = 0; row < items.length; row++) {
    if (items[row].t === "receipt" && !receipts) continue;
    if (Date.now() > deadline) break;
    const hay = finder.verbatim ? exactHaystack(items[row]) : haystack(items[row]);
    for (let m = finder.match(hay, 0); m; m = afterMatch(finder, hay, m)) {
      // An empty match (`^`, `a*`) covers nothing to paint; the painter skips
      // it, so the count has to as well or the box shows hits with no range.
      if (m.length === 0) continue;
      out.push({ id: items[row].id, row, at: m.at, length: m.length });
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
