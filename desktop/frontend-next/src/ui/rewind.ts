import { useCallback } from "react";
import type { AgentPort, RewindScope } from "../port/port";

export function useRewindActions(port: AgentPort, reloadSession: () => void) {
  const onReadUndo = useCallback(() => port.availableUndo(), [port]);
  const onPrepareRewind = useCallback((turn: number, scope: RewindScope) => port.prepareRewind(turn, scope), [port]);
  const onCommitRewind = useCallback(
    async (planId: string) => {
      const result = await port.commitRewind(planId);
      reloadSession();
      return result;
    },
    [port, reloadSession],
  );
  const onUndoRewind = useCallback((transactionId: string) => port.undoRewind(transactionId).then(reloadSession), [port, reloadSession]);
  const onPrepareFileRevert = useCallback((path: string) => port.prepareFileRevert(path), [port]);
  const onCommitFileRevert = useCallback((planId: string, resolution?: string) => port.commitFileRevert(planId, resolution), [port]);
  return { onReadUndo, onPrepareRewind, onCommitRewind, onUndoRewind, onPrepareFileRevert, onCommitFileRevert };
}
