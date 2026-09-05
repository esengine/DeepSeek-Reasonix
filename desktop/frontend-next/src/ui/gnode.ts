import { t } from "../i18n";
import type { GraphNode } from "../port/wire";
import type { Item } from "../state/session";
import { argOf, shortArgs } from "./args";

// The calls this transcript holds, by the id a node names. Timing lives on the
// tool stream rather than being published a second time — the node is the call,
// so one id answers both — and membership is what says a node can be shown in
// the activity stream at all: an adopted answer came from a run that is not in
// this transcript.
export function callsIn(items: Item[]): Map<string, number | undefined> {
  const out = new Map<string, number | undefined>();
  for (const i of items) {
    if (i.t !== "tool") continue;
    for (const call of [i.tool, ...i.children]) if (call.id) out.set(call.id, call.durationMs ?? undefined);
  }
  return out;
}

/** How long a node ran. The transcript's own measurement first, since that is
 *  the number its tool card shows; the span the kernel published is the fallback,
 *  and the only answer for a node this run never held. */
export function spanOf(node: GraphNode, calls: Map<string, number | undefined>): number | undefined {
  const measured = calls.get(node.id);
  if (measured != null) return measured;
  return node.startedAt && node.endedAt ? node.endedAt - node.startedAt : undefined;
}

// Spelled as literals rather than looked up in a table: the catalogue gate
// reads what sits inside a t() call, and a word laundered through a lookup is
// one it cannot see is missing.
export function stateWord(state: string): string {
  switch (state) {
    case "queued":
      return t("排队中");
    case "running":
      return t("运行中");
    case "completed":
      return t("完成");
    case "adopted":
      return t("复用");
    case "failed":
      return t("失败");
    case "cancelled":
      return t("已取消");
    case "skipped":
      return t("跳过");
    default:
      return t("待运行");
  }
}

/** What held this node out of a slot when it queued. The kernel keeps it after
 *  the node starts, so it is read as the refusal that was recorded then — not
 *  as what the node is waiting for now, which for a running one is nothing. */
export function waitWord(cause?: string): string | undefined {
  switch (cause) {
    case "slots":
      return t("没有空位");
    case "writers":
      return t("同时写的名额满了");
    case "claim":
      return t("要写的路径被占着");
    default:
      return undefined;
  }
}

/** How long this node sat waiting for a concurrency slot. A wait of nothing is
 *  not one: every node passes through the queue, and only a node the ceiling
 *  actually held back has anything to say about where the run's time went. */
export function slotWaitOf(node: GraphNode): number | undefined {
  if (!node.queuedAt || !node.startedAt) return undefined;
  const wait = node.startedAt - node.queuedAt;
  return wait > 0 ? wait : undefined;
}

export interface Touched {
  path: string;
  added: number;
  removed: number;
}

/** The files each node's own work changed, keyed by node id. A write is credited
 *  to the delegate the call ran under, which is what lets a step say what it
 *  produced. The rail can only say what the whole run did, and a delegate's own
 *  cards are folded inside its call — so the step is where the answer belongs. */
export function filesByNode(items: Item[]): Map<string, Touched[]> {
  const out = new Map<string, Map<string, Touched>>();
  for (const i of items) {
    if (i.t !== "tool") continue;
    for (const call of [i.tool, ...i.children]) {
      if (call.added == null && call.removed == null) continue;
      // A call the executor made itself belongs to no node. Crediting it to its
      // own id would invent a step the run graph never published.
      const owner = call.parentId;
      if (!owner) continue;
      // Falling back to the tool's own name would print "edit_file" where a path
      // belongs; the raw argument is the path whenever it is not JSON.
      const path = argOf(call.args, "path", "file_path") || shortArgs(call.args ?? "") || call.name;
      let held = out.get(owner);
      if (!held) out.set(owner, (held = new Map()));
      const cur = held.get(path) ?? { path, added: 0, removed: 0 };
      held.set(path, { path, added: cur.added + (call.added ?? 0), removed: cur.removed + (call.removed ?? 0) });
    }
  }
  return new Map([...out].map(([id, files]) => [id, [...files.values()]]));
}

/** One row per file across several nodes — what a fan-out changed, rather than
 *  the same file listed once per worker that touched it. */
export function mergeTouched(lists: (Touched[] | undefined)[]): Touched[] {
  const by = new Map<string, Touched>();
  for (const list of lists ?? []) {
    for (const f of list ?? []) {
      const cur = by.get(f.path) ?? { path: f.path, added: 0, removed: 0 };
      by.set(f.path, { path: f.path, added: cur.added + f.added, removed: cur.removed + f.removed });
    }
  }
  return [...by.values()];
}
