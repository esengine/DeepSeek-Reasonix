// Projects on this machine and what has changed inside one.
export interface WorkspaceChange {
  path: string;
  oldPath?: string;
  // git porcelain XY, trimmed: "M", "A", "D", "R", "??".
  status: string;
  // The index differs from HEAD for this path / the work tree differs from the
  // index (or the path is untracked). One path can be both. Absent is false.
  staged?: boolean;
  unstaged?: boolean;
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

// GET /workspace/git — the work tree's one-line identity, from git itself.
// This is the workspace's real Git state: the branch reading a chip that wants
// to stay current asks, not the capability scope's file-derived project name.
export interface WorkspaceGit {
  // False when the workspace is not a git repository — distinguishable from
  // "not answered yet" by whoever renders it.
  repo: boolean;
  name: string;
  // The checked-out branch; the short SHA when HEAD is detached.
  branch: string;
  detached: boolean;
  added: number;
  removed: number;
  untracked: number;
  // Why a folder that carries a .git has no repository: git refused it, or did not answer.
  absent?: "refused" | "unavailable";
  detail?: string;
}

// GET /workspace/git/log: the newest commits of every branch, newest first. A
// commit names its parents, which is all a reader needs to draw the lanes.
export interface GitCommit {
  id: string;
  parents: string[];
  author: string;
  at: number; // unix seconds
  refs: string[]; // branches and tags pointing here, as git decorates them
  subject: string;
}

export interface GitHistory {
  repo: boolean; // false for a workspace that is not under git: no commits, nothing wrong
  commits: GitCommit[];
  truncated: boolean; // the history goes on past these
}

// One local branch, as the composer's branch menu lists it.
export interface WorkspaceBranch {
  name: string;
  // The branch HEAD is on. A detached HEAD marks none — there is no branch.
  current?: boolean;
  // Set when another linked worktree of the same repository holds this branch
  // checked out; git refuses to check it out here too, so the menu closes the
  // row and says where it lives instead of letting the switch fail first.
  worktree?: string;
}

export interface WorkspaceBranches {
  // False when the workspace is not a git repository, as for /changes.
  repo: boolean;
  branches: WorkspaceBranch[];
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
  observedVersion?: string;
  // The granted folder the file was read from; absent for the workspace itself.
  root?: string;
  // True for a file read from a granted folder: it can be read here, never saved.
  readOnly?: boolean;
}

export interface FileRef {
  path: string;
  root?: string;
}

export interface PreviewInfo {
  ref: FileRef;
  kind: "markdown" | "text" | "image" | "pdf" | "table" | "html" | "unsupported";
  size: number;
  version: string;
  limit: number;
  readOnly: boolean;
}

export interface DeliveredFile extends FileRef {
  description?: string;
}

export interface WorkspacePreviewPort {
  workspacePreviewInfo(ref: FileRef, signal?: AbortSignal): Promise<PreviewInfo>;
  workspacePreviewContent(ref: FileRef, signal?: AbortSignal): Promise<{ data: ArrayBuffer; version: string }>;
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

export interface WorktreeSource {
  ref: string;
  name: string;
  remote: boolean;
  current: boolean;
}

export interface WorktreeSources {
  available: boolean;
  sourceRoot: string;
  sources: WorktreeSource[];
}

export interface WorktreeSelection {
  ref: string;
  sourceRoot: string;
  attachments?: string[];
}

export interface WorktreePreparation {
  id: string;
  phase: string;
  sourceRoot: string;
  ref: string;
  startedAt: number;
  branch?: string;
  workspaceRoot?: string;
  code?: string;
  detail?: string;
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
  // Why message is empty when the model could not draft one; the staged set above is still real.
  draftFailure?: CommitDraftFailure;
}

export type CommitDraftFailure = "no_model" | "timeout" | "no_answer" | "model_failed";

export interface CommitRequest {
  // The repository below the workspace; absent is the workspace's own.
  repo?: string;
  message: string;
  fingerprint: string;
  acknowledgeSecrets: boolean;
}

// POST /changes/stage. Paths are changes the workspace lists; all means every
// listed change. Unstage moves them out of the index and keeps their content.
export interface StageRequest {
  repo?: string;
  paths?: string[];
  all?: boolean;
  unstage?: boolean;
}

export interface CommitResult {
  hash: string;
  subject: string;
}

// One repository of the workspace. dir is its folder below the workspace, empty
// when the workspace is itself the repository.
export interface GitRepoRow {
  dir: string;
  name: string;
  branch: string;
  detached: boolean;
}

// The workspace's repositories; truncated says a search bound ended the look early.
export interface GitRepos {
  repos: GitRepoRow[];
  truncated: boolean;
}

// POST /changes/discard: one modified or deleted file goes back to the last commit.
export interface DiscardRequest {
  repo?: string;
  path: string;
}

// A path one commit changed. insertions and deletions are absent for a binary file.
export interface GitCommitFile {
  path: string;
  oldPath?: string;
  status: string;
  insertions?: number;
  deletions?: number;
}

export interface GitCommitFiles {
  repo: boolean;
  files: GitCommitFile[];
  truncated: boolean;
}

// What a push would do and where, read without reaching the remote. It is what
// the person confirms: the push is refused if the repository no longer matches.
export interface PushPlan {
  remote: string;
  branch: string;
  upstreamBranch: string;
  scheme: string;
  host: string;
  url: string;
  head: string;
  ahead: number;
  behind: number;
}

export interface PushRequest {
  repo?: string;
  plan: PushPlan;
}

export interface PushResult {
  plan: PushPlan;
  new: boolean;
  trackingStatus?: string;
}
