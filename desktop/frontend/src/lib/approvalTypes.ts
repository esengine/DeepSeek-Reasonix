import type { WireRecoveryApproval } from "./types";

export interface WireWriteAccessApproval {
  directories?: string[];
  display_directories?: string[];
  justification?: string;
  broad_home_access?: boolean;
  ordinary_permission_needed?: boolean;
  persist_allowed?: boolean;
}

export interface WireApproval {
  id: string;
  tool: string;
  subject: string;
  reason?: string;
  fresh?: boolean;
  kind?: "tool" | "plan" | "recovery" | "write_access" | string;
  recovery?: WireRecoveryApproval;
  write_access?: WireWriteAccessApproval;
}
