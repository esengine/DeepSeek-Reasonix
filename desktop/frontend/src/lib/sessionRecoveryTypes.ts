export interface RecoveryLineageMember {
  path: string;
  role: "normal" | "covered_copy" | "adopted" | "preferred" | "diverged" | string;
  canonical: boolean;
  turns: number;
  open: boolean;
  running: boolean;
  versionNote?: string;
  preview?: string;
  createdAt?: number;
  lastActivityAt?: number;
}

export interface RecoveryLineageView {
  groupId: string;
  state: string;
  branchCount: number;
  unresolved: number;
  cleanupEligible: number;
  members: RecoveryLineageMember[];
}
