import { useCallback, type RefObject } from "react";
import { t } from "../i18n";
import type { AgentPort, RewindPlan, RewindResult, RewindScope } from "../port/port";

export interface Rewinding {
  onPrepareRewind: (turn: number, scope: RewindScope) => Promise<RewindPlan>;
  onCommitRewind: (planId: string) => Promise<RewindResult>;
  onUndoRewind: (transactionId: string) => Promise<void>;
  onResend: (turn: number, text: string) => Promise<void>;
  onPrepareFileRevert: (path: string) => Promise<RewindPlan>;
  onCommitFileRevert: (planId: string, resolution?: string) => Promise<RewindResult>;
}

/** Editing a message is a rewind composed with a send, not a capability of its
 *  own: the kernel already owns both halves and holds no state between them. */
export function useRewind(
  port: AgentPort,
  reloadSession: () => void,
  send: RefObject<(text: string) => Promise<unknown>>,
): Rewinding {
  const onPrepareRewind = useCallback((turn: number, scope: RewindScope) => port.prepareRewind(turn, scope), [port]);

  const onCommitRewind = useCallback(
    async (planId: string) => {
      const result = await port.commitRewind(planId);
      reloadSession();
      return result;
    },
    [port, reloadSession],
  );

  const onUndoRewind = useCallback(
    (transactionId: string) => port.undoRewind(transactionId).then(reloadSession),
    [port, reloadSession],
  );

  const onResend = useCallback(
    async (turn: number, text: string) => {
      const plan = await port.prepareRewind(turn, "conversation");
      if (!plan.canConversation) throw new Error(plan.disabledReason || t("这一轮无法回退"));
      const result = await port.commitRewind(plan.planId);
      reloadSession();
      // The commit can touch files and leave the conversation standing, which
      // would send the message a second time rather than replace it.
      if (!result.conversationOk) throw new Error(result.error || t("对话没有被回退，原文仍在记录里"));
      await send.current(text);
    },
    [port, reloadSession, send],
  );

  const onPrepareFileRevert = useCallback((path: string) => port.prepareFileRevert(path), [port]);

  const onCommitFileRevert = useCallback(
    (planId: string, resolution?: string) => port.commitFileRevert(planId, resolution),
    [port],
  );

  return { onPrepareRewind, onCommitRewind, onUndoRewind, onResend, onPrepareFileRevert, onCommitFileRevert };
}
