import { useRef, useState } from "react";
import { useCommittedCommand } from "../lib/useCommittedCommand";
import type { WireCompletionSummary } from "../lib/types";
import type { WorkspaceVerificationRevealRequest } from "../components/WorkspacePanel";
import { useVerificationRevealReset } from "./useLocalUiLifecycles";

export type TurnVerificationCommandsInput = {
  activeTabId: string | undefined;
  turnStartAt: number;
  completionSummary: WireCompletionSummary | undefined;
  openChangedDock(): void;
};

/**
 * Owns the turn-verification reveal chain: opening the changed-files dock,
 * issuing a monotonically sequenced reveal request bound to the tab and turn
 * that published it, and resetting the request whenever the tab, turn or
 * current completion summary changes. WorkspacePanel consumes the request;
 * only the reveal lifecycle lives here.
 */
export function useTurnVerificationCommands(input: TurnVerificationCommandsInput) {
  const revealSequenceRef = useRef(0);
  const [verificationRevealRequest, setVerificationRevealRequest] = useState<WorkspaceVerificationRevealRequest | null>(null);

  const openTurnVerification = useCommittedCommand((summary: WireCompletionSummary) => {
    input.openChangedDock();
    revealSequenceRef.current += 1;
    setVerificationRevealRequest({
      id: revealSequenceRef.current,
      summary,
      tabId: input.activeTabId ?? "",
      turnStartAt: input.turnStartAt,
      currentSummary: input.completionSummary,
    });
  });

  useVerificationRevealReset({
    activeTabId: input.activeTabId,
    completionSummary: input.completionSummary,
    turnStartAt: input.turnStartAt,
    reset: setVerificationRevealRequest,
  });

  return { verificationRevealRequest, openTurnVerification };
}
