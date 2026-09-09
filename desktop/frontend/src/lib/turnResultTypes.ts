export interface TurnFileChange {
  path: string;
  kind: "create" | "modify" | "delete";
  added: number;
  removed: number;
  binary?: boolean;
  modeOnly?: boolean;
  uncounted?: boolean;
  unavailable?: string;
  patch?: string;
}

export interface TurnChanges {
  id?: string;
  turn: number;
  coverage: "complete" | "partial" | "unknown";
  files: TurnFileChange[];
  added: number;
  removed: number;
  reasons: string[];
}

export interface WireCompletionReceipt {
  verdict: string;
  diff?: TurnChanges;
  interrupted?: boolean;
  changes?: { path: string; reviewed: boolean }[];
  verifications?: { command: string; passed: boolean; stale?: boolean; interrupted?: boolean; toolCallId?: string; toolResultId?: string; exitCode?: number }[];
  gaps?: { kind: string; detail?: string }[];
  risks?: string[];
}
