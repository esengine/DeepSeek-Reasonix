export interface WireSessionContextSectionDiagnostics {
  digest?: string;
  chars?: number;
}

export interface WireSessionContextDiagnostics {
  version: number;
  digest: string;
  targetRole: "executor" | "planner";
  reasons?: string[];
  environment: WireSessionContextSectionDiagnostics;
  workspace: WireSessionContextSectionDiagnostics;
  backgroundMemory: WireSessionContextSectionDiagnostics;
  skillsCatalog: WireSessionContextSectionDiagnostics;
}
