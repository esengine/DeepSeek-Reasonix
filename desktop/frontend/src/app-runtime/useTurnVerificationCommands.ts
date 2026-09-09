import { useRef, useState } from "react";
import { useCommittedCommand } from "../lib/useCommittedCommand";
import type { WireCompletionSummary } from "../lib/types";
import type { WorkspaceVerificationRevealRequest } from "../components/WorkspacePanel";
import { useVerificationRevealReset } from "./useLocalUiLifecycles";

export type TurnVerificationCommandsInput = {
  activeTabId: string | undefined;
  turnStartAt: number;
  completionSummary: WireCompletionSummary | undefined;
  sessionPath?: string;
  openChangedDock(): void;
};

/**
 * Owns the turn-verification reveal chain: opening the changed-files dock,
 * issuing a monotonically sequenced reveal request bound to the tab and turn
 * that published it, and resetting the request whenever the tab, turn or
 * session changes. Summary updates preserve the selected result. WorkspacePanel consumes the request;
 * only the reveal lifecycle lives here.
 */
export function useTurnVerificationCommands(input: TurnVerificationCommandsInput) {
  const revealSequenceRef = useRef(0);
  const [verificationRevealRequest, setVerificationRevealRequest] = useState<WorkspaceVerificationRevealRequest | null>(null);

  const openTurnResult = useCommittedCommand((summary: WireCompletionSummary, view: "changes" | "checks") => {
    input.openChangedDock();
    revealSequenceRef.current += 1;
    setVerificationRevealRequest({
      id: revealSequenceRef.current,
      summary,
      tabId: input.activeTabId ?? "",
      turnStartAt: input.turnStartAt,
      currentSummary: input.completionSummary,
      sessionPath: input.sessionPath,
      view,
    });
  });

  const openTurnVerification = useCommittedCommand((summary: WireCompletionSummary) => openTurnResult(summary, "checks"));
  const openTurnChanges = useCommittedCommand((summary?: WireCompletionSummary) => {
    if (summary) openTurnResult(summary, "changes");
    else { setVerificationRevealRequest(null); input.openChangedDock(); }
  });
  const closeTurnResult = useCommittedCommand(() => setVerificationRevealRequest(null));

  useVerificationRevealReset({
    activeTabId: input.activeTabId,
    completionSummary: input.completionSummary,
    sessionPath: input.sessionPath,
    turnStartAt: input.turnStartAt,
    reset: setVerificationRevealRequest,
  });

  return { verificationRevealRequest, openTurnVerification, openTurnChanges, closeTurnResult };
}
