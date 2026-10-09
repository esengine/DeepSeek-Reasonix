// Projects on this machine and what has changed inside one.
export interface WorkspaceChange {
  path: string;
  oldPath?: string;
  // git porcelain XY, trimmed: "M", "A", "D", "R", "??".
  status: string;
  // How much the file differs by. Absent is "git did not say" — a binary file,
  // or one nothing counted — which is not the same as changed by nothing.
  insertions?: number;
  deletions?: number;
}

export interface WorkspaceChanges {
  // False when the workspace is not a git repository — the caller falls back
  // rather than showing an empty list as if nothing had changed.
  repo: boolean;
  changes: WorkspaceChange[];
}

// One path's working-tree diff, as unified text for DiffView to render.
// truncated says the kernel stopped at its cap rather than that the file is
// unchanged — the two look the same at the end of a string otherwise.
export interface ChangeDiff {
  path: string;
  diff: string;
  truncated: boolean;
}

export interface WorkspaceFiles {
  files: string[];
  directories: string[];
}

export interface WorkspaceFile {
  path: string;
  content: string;
  revision: string;
}

export interface WorkspaceEntry {
  path: string;
  name: string;
}

// GET /workspaces. canSwitch is the server's answer, not the client's guess:
// a server reachable over the network refuses to be repointed at all.
export interface WorkspaceInfo {
  current: string;
  canSwitch: boolean;
  canIsolate: boolean;
  recents: WorkspaceEntry[];
  isolated?: boolean;
}

export interface CommitFile {
  path: string;
  // git raw status letter: "A", "M", "D", "T".
  status: string;
  insertions?: number;
  deletions?: number;
  // The name marks a file that holds secrets; its diff is never sent to a model.
  sensitive?: boolean;
}

// The staged set as read, and the message the model proposed for it.
// fingerprint names that set: committing hands it back, and the kernel refuses
// an index that has since changed.
export interface CommitProposal {
  message: string;
  fingerprint: string;
  files: CommitFile[];
  truncated: boolean;
  contentSecrets: boolean;
}

export interface CommitRequest {
  message: string;
  fingerprint: string;
  acknowledgeSecrets: boolean;
}

export interface CommitResult {
  hash: string;
  subject: string;
}
