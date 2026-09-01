export interface WorktreeMergeInspection {
  available: boolean;
  reason?: string;
  canMerge: boolean;
  alreadyMerged: boolean;
  worktreeRoot?: string;
  sourceRoot?: string;
  worktreeBranch?: string;
  targetBranch?: string;
  createdHead?: string;
  worktreeHead?: string;
  worktreeStateToken?: string;
  targetHead?: string;
  aheadCount: number;
  behindCount: number;
  filesChanged: number;
  insertions: number;
  deletions: number;
  changedFiles: string[];
  hasConflicts: boolean;
  conflictFiles: string[];
  worktreeDirty: boolean;
  sourceDirty: boolean;
  blockers: WorktreeMergeBlocker[];
  cleanupBlockers: WorktreeMergeBlocker[];
}

export interface WorktreeMergeBlocker {
  code: string;
  message: string;
  paths: string[];
}

export interface WorktreeMergeRequest {
  tabId: string;
  expectedTargetBranch: string;
  expectedTargetHead: string;
  expectedWorktreeHead: string;
  expectedWorktreeStateToken: string;
  autoCommitDirty: boolean;
}

export interface CloseMergedWorktreeTabRequest {
  tabId: string;
  worktreeRoot: string;
  sourceTabId: string;
  sourceRoot: string;
  navigationIntentToken: string;
}

export interface CloseMergedWorktreeTabResult {
  closed: boolean;
  idempotent: boolean;
}

export interface WorktreeMergeResult {
  merged: boolean;
  alreadyMerged: boolean;
  recoveryRequired: boolean;
  sourceRoot?: string;
  targetBranch?: string;
  targetHead?: string;
  mergedCommit?: string;
  worktreeRoot?: string;
  worktreeBranch?: string;
  worktreeHead?: string;
  error?: string;
}

export interface WorktreeCleanupRequest {
  worktreeRoot: string;
  sourceRoot: string;
  targetBranch: string;
  mergedCommit: string;
  worktreeBranch: string;
  worktreeHead: string;
}

export interface WorktreeCleanupResult {
  completed: boolean;
  worktreeRemoved: boolean;
  branchDeleted: boolean;
  recoveryRetained?: boolean;
  recoveryRoot?: string;
  recoveryWorktreeRegistered?: boolean;
  branchRetained?: boolean;
  blockers: WorktreeMergeBlocker[];
  error?: string;
}
