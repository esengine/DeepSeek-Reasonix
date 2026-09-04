// What the host asked a person, and how it ended.
//
// Two surfaces, not one list with a flag: a live Decision is a question with
// someone waiting on the answer, and an adjudication entry is provenance for a
// wait that happened. The second is never answerable — the run that was waiting
// died with its process — so nothing here is a handle, and `barrierId` exists
// for debugging and audit rather than for posting back.
//
// `state` comes from the kernel because "interrupted" appears in no record: it
// is what an open barrier means once nothing is waiting on it, and only the
// host knows whether anything still is.
export type AdjudicationState = "interrupted" | "resolved" | "cancelled" | "superseded";

export interface AdjudicationEntry {
  barrier_id: string;
  kind: string;
  state: AdjudicationState;
  question?: string;
  opened_at?: string;
  settled_at?: string;
  // The turn that took an interruption over. It is why the question stopped
  // being shown — work moved on, rather than someone dismissing a notice.
  superseded_by?: string;
}

export interface Adjudications {
  schema_version: number;
  active: AdjudicationEntry[];
  history: AdjudicationEntry[];
}
