import { useCallback } from "react";
import type { AgentPort, RewindScope } from "../port/port";

/** Taking a turn back, and taking one file back. A rewind rewrites the
 *  transcript and the files under it, so the whole session is re-read the way a
 *  session switch is. Reverting one file touches disk but not the transcript,
 *  so it reloads nothing. */
export function useRewindActions(port: AgentPort, reloadSession: () => void) {
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
  return { onPrepareRewind, onCommitRewind, onUndoRewind, onPrepareFileRevert, onCommitFileRevert };
}
