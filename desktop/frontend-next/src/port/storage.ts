// What the kernel keeps on disk, and moving it somewhere else. Sizes are the
// ones it measured — a person deletes things on the strength of these numbers,
// so nothing here is an estimate.

// One declared root. relocatable and pinnedBy come from the kernel's own root
// table: the panel never decides for itself which rows may be moved.
export interface StorageRoot {
  id: string;
  dir: string;
  bytes: number;
  files: number;
  relocatable: boolean;
  // The environment variable holding this root in place, when one does. A
  // pinned root shows where it is and why it cannot be moved from here.
  pinnedBy?: string;
  // Never written yet. Not a failure — a fresh install has no worktrees.
  missing?: boolean;
  // The walk reached its time limit: bytes and files are a lower bound.
  truncated?: boolean;
  // Described but not measured yet; bytes and files mean nothing until it is.
  pending?: boolean;
  err?: string;
  volume?: string;
  volumeFree?: number;
  volumeTotal?: number;
}

// A move in flight, or the one that just ended. Polled rather than pushed: it
// is a single operation someone is watching, on a panel that already refreshes.
export interface StorageMove {
  root: string;
  to: string;
  phase: string;
  bytes: number;
  total: number;
  detail?: string;
  err?: string;
  done: boolean;
}

// An unfinished move the kernel found recorded at launch.
export interface StoragePending {
  root: string;
  to: string;
  phase: string;
}

// layout answers at once with every root unmeasured; root measures just that
// one, so a slow root never holds up the rest.
export interface StorageQuery {
  layout?: boolean;
  root?: string;
}

export interface StorageState {
  roots: StorageRoot[];
  // False when the host has not opened up configuration writing; the panel then
  // reports sizes and offers nothing it cannot carry out.
  editable: boolean;
  move?: StorageMove;
  pending?: StoragePending;
  // Entries an earlier move left in the previous root, when this run cannot
  // bring them across itself. Absent is the normal case.
  leftBehind?: { dir: string; names: string[] };
}

// One objection to a move. code is what a client branches on, detail the
// sentence a person reads; both arrive together so neither has to be inferred.
export interface StorageRefusal {
  code: string;
  detail: string;
}

// What a move would do, answered before any of it is done. need is the bytes
// required including the headroom the kernel insists on leaving.
export interface StoragePlan {
  root: string;
  from: string;
  to: string;
  bytes: number;
  files: number;
  need: number;
  free: number;
  // The target already holds this root's own data, so accepting the plan is the
  // pointer alone: nothing is copied, and nothing at either end is deleted.
  adopt?: boolean;
  // What the current location keeps when an adopt is accepted. Anything written
  // there since is not carried across, which is the one thing to say out loud.
  stays?: number;
  ok: boolean;
  refusals?: StorageRefusal[];
}
