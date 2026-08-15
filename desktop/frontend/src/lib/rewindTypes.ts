export interface RewindPlanView {
  planId?: string;
  turn?: number;
  scope?: string;
  coverage?: string;
  coverageGaps?: string[];
  legacy?: boolean;
  expiredFilePayload?: boolean;
  canFiles?: boolean;
  canConversation?: boolean;
  disabledReason?: string;
  conflicts?: string[];
  files?: string[];
  fileCount?: number;
  activeWriters?: number;
  path?: string;
  conversationAction?: string;
  ok?: boolean;
  error?: string;
}

export interface RewindUndoState {
  turnDiff: number;
  transactionId?: string;
  undoAvailable?: boolean;
  undoTabId?: string;
  filesRestored?: string[];
  filesRemoved?: string[];
}
