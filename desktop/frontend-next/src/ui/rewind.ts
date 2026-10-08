import { useCallback } from "react";
import type { AgentPort, RewindScope } from "../port/port";

export function useRewindActions(port: AgentPort, reloadSession: () => void, onRestoreText: (text: string) => void) {
  // Backend state is the authority for an undo offer after a reload.
  const onReadUndo = useCallback(() => port.availableUndo(), [port]);
  const onPrepareRewind = useCallback((turn: number, scope: RewindScope) => port.prepareRewind(turn, scope), [port]);
  const onCommitRewind = useCallback(
    async (planId: string, text?: string) => {
      const result = await port.commitRewind(planId);
      if (result.conversationOk && text !== undefined) onRestoreText(text);
      // Reload history and checkpoints together after a rewind.
      reloadSession();
      return result;
    },
    [port, reloadSession, onRestoreText],
  );
  const onUndoRewind = useCallback((transactionId: string) => port.undoRewind(transactionId).then(reloadSession), [port, reloadSession]);
  const onPrepareFileRevert = useCallback((path: string) => port.prepareFileRevert(path), [port]);
  const onCommitFileRevert = useCallback((planId: string, resolution?: string) => port.commitFileRevert(planId, resolution), [port]);
  return { onReadUndo, onPrepareRewind, onCommitRewind, onUndoRewind, onPrepareFileRevert, onCommitFileRevert };
}
