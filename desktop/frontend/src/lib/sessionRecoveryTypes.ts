export interface RecoveryLineageMember {
  path: string;
  versionKind?: "normal" | "recovery" | "subagent" | string;
  versionState?: "active" | "pending" | "resolved" | "trashed" | string;
  parentVersionId?: string;
  role: "normal" | "covered_copy" | "adopted" | "preferred" | "diverged" | string;
  canonical: boolean;
  turns: number;
  open: boolean;
  running: boolean;
  versionNote?: string;
  preview?: string;
  createdAt?: number;
  lastActivityAt?: number;
  /** Set when the version is a head inside one schema-2 log rather than a file. */
  headId?: string;
  headKind?: "main" | "fork" | "rewind" | "concurrent" | string;
  headName?: string;
  selected?: boolean;
}

export interface RecoveryLineageView {
  groupId: string;
  state: string;
  branchCount: number;
  unresolved: number;
  cleanupEligible: number;
  members: RecoveryLineageMember[];
}
