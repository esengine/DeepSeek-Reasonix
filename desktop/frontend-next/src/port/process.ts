/** Where a unit of work stands, in terms every kind of work can be mapped onto. */
export type ProcessState = "pending" | "queued" | "running" | "waiting" | "completed" | "failed" | "stopped" | "interrupted" | "skipped";

/** What kind of work a process is: the owner that produced it. */
export type ProcessKind = "subagent" | "job";

/** One unit of work this session has running now: GET /processes. */
export interface ProcessEntry {
  /** "<kind>:<owner id>". */
  id: string;
  kind: ProcessKind;
  /** The process that started this one; absent for a root. */
  parent?: string;
  name: string;
  state: ProcessState;
  /** Why it is in this state, when the owner records one. */
  cause?: string;
  startedAt: number;
}
