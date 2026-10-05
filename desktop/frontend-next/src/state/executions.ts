import type { Tool } from "../port/wire";

/** What a tool call did, keyed by the identity the wire gave it.
 *
 *  The transcript folds lookups into one card, nests a subagent's calls under
 *  the task that spawned them, and replaces a saved memory's row outright. None
 *  of that reaches here: a card is a drawing of an execution, never where the
 *  execution is recorded. */
export interface Execution {
  /** What actually answered — a proxy's own name says nothing about that. */
  name: string;
  /** Absent until the call settles. A call still running has no outcome, and
   *  "has not failed yet" is not the answer "succeeded". */
  failed?: boolean;
}

export type Executions = Record<string, Execution>;

/** Records what one tool event says about its execution. Keyed by the wire's
 *  id, so the same event reduced twice — a replay, a rebuild — says the same
 *  thing rather than counting twice. A call the wire gave no id has no identity
 *  to key on and is not recorded; the transcript cannot merge one either, so it
 *  is already drawn as two cards. */
export function noteTool(execs: Executions, tool: Tool, settled: boolean): Executions {
  const id = tool.id;
  if (!id) return execs;
  const was = execs[id];
  const name = tool.resolvedName || tool.name;
  const failed = settled ? !!tool.err : was?.failed;
  if (was && was.name === name && was.failed === failed) return execs;
  return { ...execs, [id]: { name, ...(failed === undefined ? {} : { failed }) } };
}
