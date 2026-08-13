// SessionMeta is one saved session for the history panel.
export interface SessionMeta {
  path: string;
  preview: string;
  title?: string; // user-chosen name; falls back to preview when empty
  turns: number;
  turnsState?: "unknown" | "valid" | "corrupt" | string;
  createdAt: number; // unix milliseconds
  lastActivityAt: number; // unix milliseconds
  modTime: number; // compatibility alias for lastActivityAt
  deletedAt?: number; // unix milliseconds, present for trashed sessions
  current: boolean;
  open: boolean;
  scope?: string; // "project" | "global"; empty for legacy → treated as "global"
  workspaceRoot?: string;
  topicId?: string;
  topicTitle?: string;
  kind?: "session" | "channel" | string;
  channel?: string;
  channelLabel?: string;
  remoteId?: string;
  chatType?: string;
  userId?: string;
  threadId?: string;
  sessionSource?: string;
  recovered?: boolean; // created by conflict recovery, including a continued branch
  recoveryCopy?: boolean; // actual branch content is unchanged and covered by its parent
  recoveryGroupId?: string;
  recoveryRole?: string; // normal|covered_copy|adopted|diverged
  recoveryCanonical?: boolean;
}
