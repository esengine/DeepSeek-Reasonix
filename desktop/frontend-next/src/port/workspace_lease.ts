/** One closed account of the workspace write lease. Waits under the notice
 *  grace are counted here and nowhere else, and idleMs is the stretch held
 *  after the last write asked for it. */
export interface WorkspaceLease {
  resourceKind?: string;
  transactionId?: string;
  sessionId?: string;
  executionId?: string;
  callId?: string;
  state?: string;
  reason?: string;
  startedAt?: string;
  holderTransactionId?: string;
  holderExecutionId?: string;
  contended: number;
  reported?: number;
  waitedMs?: number;
  heldMs: number;
  idleMs: number;
  holder?: string;
  holderSessionId?: string;
  paths?: string[];
  requestedPaths?: string[];
  /** The git repository root that folded this session's folder into the shared write domain. */
  repository?: string;
}
