import type { CompletionItem, InvocationRequest } from "../port/port";

// A skill the user picked from the menu, held in the box as the text "/name"
// starting at `at`. The chip is the record of that pick: the same characters
// typed by hand are prose, and nothing here ever promotes them into a chip.
export interface SkillChip {
  name: string;
  kind: "skill" | "subagent";
  at: number;
}

export const chipText = (c: SkillChip) => "/" + c.name;
const chipEnd = (c: SkillChip) => c.at + chipText(c).length;

export function chipKind(item: CompletionItem): SkillChip["kind"] | null {
  if (!item.label.startsWith("/")) return null;
  return item.kind === "skill" || item.kind === "subagent" ? item.kind : null;
}

// The one span an edit changed, [from, oldTo) in prev and [from, newTo) in
// next. The caret settles where typing ended, which picks the right span when
// a repeated letter makes the plain common-prefix answer ambiguous.
function changed(prev: string, next: string, caret?: number): [number, number, number] {
  let tail = caret !== undefined && caret <= next.length && prev.endsWith(next.slice(caret)) ? next.length - caret : -1;
  if (tail < 0) {
    tail = 0;
    while (tail < prev.length && tail < next.length && prev[prev.length - 1 - tail] === next[next.length - 1 - tail]) tail++;
  }
  let from = 0;
  const limit = Math.min(prev.length, next.length) - tail;
  while (from < limit && prev[from] === next[from]) from++;
  return [from, prev.length - tail, next.length - tail];
}

// Carries the chips across one edit. A chip the edit touched is gone whole, and
// whatever of its "/name" the edit left behind goes with it: half a chip is
// neither a pick nor anything the user typed.
export function reconcile(prev: string, next: string, chips: SkillChip[], caret?: number): { text: string; chips: SkillChip[]; caret?: number } {
  if (prev === next) return { text: next, chips, caret };
  const [from, oldTo, newTo] = changed(prev, next, caret);
  const delta = newTo - oldTo;
  const kept: SkillChip[] = [];
  const cuts: [number, number][] = [];
  for (const c of chips) {
    if (chipEnd(c) <= from) kept.push(c);
    else if (c.at >= oldTo) kept.push({ ...c, at: c.at + delta });
    else {
      if (c.at < from) cuts.push([c.at, from]);
      if (chipEnd(c) > oldTo) cuts.push([newTo, chipEnd(c) + delta]);
    }
  }
  let text = next;
  let at = caret;
  let out = kept;
  for (const [a, b] of cuts.sort((x, y) => y[0] - x[0])) {
    text = text.slice(0, a) + text.slice(b);
    out = out.map((c) => (c.at >= b ? { ...c, at: c.at - (b - a) } : c));
    if (at !== undefined && at > a) at = Math.max(a, at - (b - a));
  }
  return { text, chips: out, caret: at };
}

// Replaces [from, to) with a chip for item and the space that ends it.
export function place(text: string, from: number, to: number, chips: SkillChip[], item: CompletionItem): { text: string; chips: SkillChip[]; caret: number } | null {
  const kind = chipKind(item);
  if (!kind) return null;
  const chip: SkillChip = { name: item.label.slice(1), kind, at: from };
  const insert = chipText(chip) + (text[to] === " " ? "" : " ");
  const next = text.slice(0, from) + insert + text.slice(to);
  const moved = reconcile(text, next, chips, from + insert.length).chips;
  return { text: next, chips: [...moved, chip].sort((a, b) => a.at - b.at), caret: from + chipText(chip).length + 1 };
}

// Where a caret that landed inside a chip belongs: a chip is one thing, so the
// caret sits before or after it, on the side it was moving toward.
export function snap(chips: SkillChip[], caret: number, before: number): number {
  const c = chips.find((x) => caret > x.at && caret < chipEnd(x));
  if (!c) return caret;
  return caret < before ? c.at : chipEnd(c);
}

export function touches(chips: SkillChip[], caret: number): boolean {
  return chips.some((c) => caret >= c.at && caret <= chipEnd(c));
}

// What the model is handed: the line with every chip taken out and the space
// that set it apart folded, and the chips in the order they were placed.
export function split(text: string, chips: SkillChip[]): { submit: string; invocations: InvocationRequest[] } {
  let submit = "";
  let from = 0;
  const invocations: InvocationRequest[] = [];
  for (const c of [...chips].sort((a, b) => a.at - b.at)) {
    submit += text.slice(from, c.at);
    from = chipEnd(c);
    if (/\s$/.test(submit) || submit === "") while (text[from] === " ") from++;
    invocations.push({ name: c.name, kind: c.kind, offset: submit.length });
  }
  return { submit: (submit + text.slice(from)).trim(), invocations };
}
