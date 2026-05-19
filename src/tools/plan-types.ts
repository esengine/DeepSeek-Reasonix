export type PlanStepRisk = "low" | "med" | "high";

export interface PlanStep {
  id: string;
  title: string;
  action: string;
  risk?: PlanStepRisk;
  targets?: string[];
  acceptance?: string;
  verification?: string[];
}

export type StepEvidenceKind = "verification" | "diff" | "checkpoint" | "manual";

export interface StepEvidence {
  kind: StepEvidenceKind;
  summary: string;
  command?: string;
  paths?: string[];
}

export interface StepCompletion {
  kind: "step_completed";
  stepId: string;
  title?: string;
  result: string;
  notes?: string;
  evidence?: StepEvidence[];
}
