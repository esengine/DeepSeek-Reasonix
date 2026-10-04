import type { Item } from "./session_types";
import type { Tool } from "../port/wire";
import { nextId } from "./ids";

// How a tool call is drawn. Folding, nesting and replacing cards is all this
// module does, and none of it is a record of what ran — the executions beside
// it hold that, so a new collapse rule here cannot reach a runtime fact.

// The wire omits `partial: false`, so spreading a full dispatch over the
// streaming placeholder would leave its flag standing and every dispatched call
// would read as one that never started. Merging through here is what lets
// sealTurn tell the two apart.
const merge = (prev: Tool, next: Tool): Tool => ({ ...prev, ...next, partial: next.partial ?? false });

// The kernel's reserved ToolProgress channels for a child's live preview. They
// describe the child, never the call that spawned it, so they are kept apart
// from the call's card: folding one in renames it and reopens it.
export const SUBAGENT_PROGRESS_PREFIX = "reasonix.subagent.";
export const isSubagentProgress = (name: string | undefined): boolean => !!name?.startsWith(SUBAGENT_PROGRESS_PREFIX);
const TERMINAL = new Set(["completed", "failed", "cancelled"]);
export function notePhase(phases: Record<string, string>, tool: Tool): Record<string, string> {
  if (tool.name !== SUBAGENT_PROGRESS_PREFIX + "status" || !tool.id) return phases;
  return { ...phases, [tool.id]: tool.output ?? "" };
}
/** A child is working until its tracker says otherwise. */
export const subagentLive = (phase: string | undefined): boolean => !!phase && !TERMINAL.has(phase);

export function foldTool(items: Item[], tool: Tool, running: boolean): Item[] {
  // A subagent's calls carry parentId; they belong inside the task that spawned
  // them, not as siblings in the main flow. A fleet worker's parent is itself a
  // child card, so the owning card is the one that holds the parent as a child.
  if (tool.parentId) {
    const at = items.findIndex(
      (i) => i.t === "tool" && (i.tool.id === tool.parentId || i.children.some((c) => c.id === tool.parentId)),
    );
    if (at >= 0) {
      const parent = items[at] as Extract<Item, { t: "tool" }>;
      const kids = parent.children.slice();
      const k = kids.findIndex((c) => c.id === tool.id);
      if (k >= 0) kids[k] = merge(kids[k], tool);
      else kids.push({ ...tool, partial: tool.partial ?? false });
      const next = items.slice();
      next[at] = { ...parent, children: kids };
      return next;
    }
  }
  const key = tool.id;
  if (key) {
    const at = items.findIndex((i) => i.t === "tool" && i.tool.id === key);
    if (at >= 0) {
      const prev = items[at] as Extract<Item, { t: "tool" }>;
      const next = items.slice();
      next[at] = { ...prev, tool: merge(prev.tool, tool), running };
      return next;
    }
  }
  return [...items, { t: "tool", id: nextId(), tool: { ...tool, partial: tool.partial ?? false }, running, children: [] }];
}

// dropTool removes the dispatch row a specialised card replaces, so the same
// call is not shown twice once its result arrives.
export function dropTool(items: Item[], id?: string): Item[] {
  if (!id) return items;
  return items.filter((i) => !(i.t === "tool" && i.tool.id === id));
}

// read_file is the most-called tool by a wide margin — 96 calls across a sample
// of recent sessions, against 47 for bash. One card each is the noise the spec
// collapses into a single manifest. Merging happens here rather than at render
// time so each card keeps a stable identity and stays memoised.
// The spec's manifest is one step for a whole run of lookups, not for reads
// alone: its own fixture folds grep and glob rows in beside the files. A group
// still has to be anchored by a read — a lone grep is better served by its own
// excerpt list than by a row that says only how many times it matched.
const LOOKUP = new Set(["read_file", "grep", "glob", "ls"]);

const lookup = (i: Item | undefined) =>
  i?.t === "tool" && !i.running && LOOKUP.has(i.tool.name) && i.children.length === 0;

const toolOf = (i: Item) => (i as Extract<Item, { t: "tool" }>).tool;

// foldLastRead folds the item just appended into the one before it, in place,
// and says whether it did. Rebuilding a whole transcript calls it once per item,
// so it must not copy the list it is folding.
export function foldLastRead(items: Item[]): boolean {
  const n = items.length;
  const last = items[n - 1];
  if (!lookup(last)) return false;
  const tool = toolOf(last);
  const prev = items[n - 2];
  if (prev?.t === "reads") {
    items[n - 2] = { ...prev, tools: [...prev.tools, tool] };
    items.length = n - 1;
    return true;
  }
  if (lookup(prev) && (tool.name === "read_file" || toolOf(prev).name === "read_file")) {
    items[n - 2] = { t: "reads", id: prev.id, tools: [toolOf(prev), tool] };
    items.length = n - 1;
    return true;
  }
  return false;
}

export function mergeReads(items: Item[]): Item[] {
  const next = items.slice();
  return foldLastRead(next) ? next : items;
}
