import type { HistoryPage, RecoverySessionCandidate } from "./types";

export type RecoveryResumeRequest = {
  tabId: string;
  path: string;
  limit: number;
};

export type RecoveryResumeOutcome =
  | { kind: "loaded"; page: HistoryPage }
  | { kind: "selection"; candidates: RecoverySessionCandidate[] };

export async function requestRecoveryResume(
  request: RecoveryResumeRequest,
  resume: (tabId: string, path: string, limit: number) => Promise<HistoryPage>,
): Promise<RecoveryResumeOutcome> {
  const page = await resume(request.tabId, request.path, request.limit);
  if (page.selectionRequired) {
    return { kind: "selection", candidates: page.recoveryCandidates ?? [] };
  }
  return { kind: "loaded", page };
}
