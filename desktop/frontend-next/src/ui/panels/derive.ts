import type { Item } from "../../state/session";
import { subagentLive } from "../../state/fold";
import { categoryOf } from "../icons";
import { agentsOf, isDelegation } from "../delegation";
import { argOf, shortArgs } from "../args";
import type { Task } from "./Agents";
import type { Executions } from "../../state/executions";

// One row per file, not per call. Editing the same file four times is one
// pending change with four edits' worth of lines in it; a row per call turned
// the panel into a log of what happened rather than what is waiting for review.
export interface Change {
  path: string;
  added: number;
  removed: number;
  edits: number;
}

export interface Stats {
  tools: number;
  external: number;
  failed: number;
  waiting: number;
}

export interface Rail {
  tasks: Task[];
  changes: Change[];
  stats: Stats;
}

/** The three facts a tool call leaves behind, counted off the executions the
 *  session recorded rather than off the cards drawing them. Identity is the
 *  state's; which of them count as reaching outside is decided here, where the
 *  categories the rest of the interface uses already live. */
export function toolFacts(execs: Executions): Omit<Stats, "waiting"> {
  let tools = 0, external = 0, failed = 0;
  for (const id in execs) {
    const e = execs[id];
    tools++;
    if (e.failed) failed++;
    const cat = categoryOf(e.name);
    if (cat === "net" || cat === "mcp") external++;
  }
  return { tools, external, failed };
}

/** One walk for everything else the rail reads. It was three — one per panel
 *  that wanted a number — and a walk apiece is a walk apiece over a transcript
 *  that only grows, repeated on every revision while an answer streams. */
export function railOf(items: Item[], execs: Executions, phases: Record<string, string> = {}): Rail {
  const stats: Stats = { ...toolFacts(execs), waiting: 0 };
  const tasks: Task[] = [];
  const by = new Map<string, Change>();
  const take = (path: string) => {
    let c = by.get(path);
    if (!c) by.set(path, (c = { path, added: 0, removed: 0, edits: 0 }));
    return c;
  };

  for (const i of items) {
    if (i.t === "approval") {
      if (!i.verdict) stats.waiting++;
      continue;
    }
    if (i.t === "ask") {
      if (!i.answered) stats.waiting++;
      continue;
    }
    if (i.t !== "tool") continue;
    const call = i.tool;
    // A profile is the kernel's mark that the work left this context; matching
    // the tool name instead misses every delegation reached through a proxy.
    if (isDelegation(call)) {
      const working = subagentLive(phases[call.id ?? ""]) || i.children.some((c) => subagentLive(phases[c.id ?? ""]));
      tasks.push(working && !i.running ? { ...i, running: true } : i);
    }

    if (call.added == null && call.removed == null) continue;
    // Falling back to the tool's own name printed "edit_file" where a path
    // belongs; the raw argument is the path whenever it is not JSON.
    const path = argOf(call.args, "path", "file_path") || shortArgs(call.args ?? "") || call.name;
    // A rename carries the change with it: the old path is no longer pending.
    if (call.name === "move_file") {
      const from = argOf(call.args, "from", "old_path", "source");
      if (from) by.delete(from);
    }
    const c = take(path);
    c.added += call.added ?? 0;
    c.removed += call.removed ?? 0;
    c.edits++;
  }
  return { tasks, changes: [...by.values()], stats };
}

/** One fleet call is many sub-agents; counting cards would under-report it. */
export const agentsIn = (tasks: Task[]) => tasks.reduce((n, x) => n + agentsOf(x.tool), 0);

