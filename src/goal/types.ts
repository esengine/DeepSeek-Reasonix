export const DEFAULT_GOAL_TEST_TIMEOUT_MS = 120_000;

export type TestProjectKind = "node" | "python" | "go" | "rust";

export interface DetectedTestCommand {
  kind: TestProjectKind;
  command: string;
  args: string[];
  display: string;
  reason: string;
}

export interface VerificationCounts {
  passed?: number;
  failed?: number;
}

export interface AutoTestResult {
  status: "passed" | "failed" | "skipped";
  command?: DetectedTestCommand;
  exitCode?: number | null;
  signal?: NodeJS.Signals | null;
  timedOut?: boolean;
  durationMs: number;
  outputTail: string;
  counts: VerificationCounts;
  reason?: string;
}
