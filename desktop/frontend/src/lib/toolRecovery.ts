export interface RecoveryCall {
  identity: { session_id?: string; turn_id?: string; call_id?: string; attempt_id?: string; canonical_tool?: string; argument_digest?: string; resource_scope?: string };
  state: string;
  read_only: boolean;
  arguments?: unknown;
  inspection_id?: string;
  inspection_state?: string;
  resolution?: string;
}
export interface ToolRecoverySnapshot {
  silent?: boolean;
  sessionPath: string;
  runtimeEpoch: string;
  revision: string;
  calls: RecoveryCall[];
  retryEnabled: boolean;
}
export interface ToolRecoveryRequest {
  sessionPath: string; runtimeEpoch: string; revision: string;
  attemptId: string; inspectionId: string;
  action: "inspect" | "confirm" | "reject" | "retry";
}
export interface ToolRecoveryBindings {
  GetToolRecoveryForTab?(tabId: string): Promise<ToolRecoverySnapshot>;
  ResolveToolRecoveryForTab?(tabId: string, request: ToolRecoveryRequest): Promise<ToolRecoverySnapshot>;
}
