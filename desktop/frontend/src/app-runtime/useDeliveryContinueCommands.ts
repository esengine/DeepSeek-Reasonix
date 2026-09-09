import { useCommittedCommand } from "../lib/useCommittedCommand";
import type { Translator } from "../lib/i18n";
import type { createSessionSurfaceFence } from "./sessionTarget";

const loadDeliveryContinue = () => import("../lib/deliveryContinue");

export type DeliveryContinueCommandsInput = {
  surfaceFence: ReturnType<typeof createSessionSurfaceFence>;
  ready: boolean;
  goal: string | undefined;
  t: Translator;
  ports: {
    resumeGoal(tabId: string): Promise<boolean>;
    recoverDelivery(tabId: string, prompt: string): Promise<void>;
  };
};

/**
 * Owns the delivery "continue checks" chain: the recovery-prompt send and the
 * continue command that captures the committed surface ownership at click
 * time, so a mid-flight tab switch or session replacement can never deliver
 * the continuation into a session that no longer owns the UI. The delivery
 * owner chunk stays lazy behind the command.
 */
export function useDeliveryContinueCommands(input: DeliveryContinueCommandsInput) {
  const { surfaceFence, t, ports } = input;

  const sendDeliveryRecovery = useCommittedCommand((tabId: string) =>
    ports.recoverDelivery(tabId, t("notice.deliveryIncompleteContinuePrompt")));

  const handleDeliveryContinue = useCommittedCommand(() => {
    const ownership = surfaceFence.capture();
    return loadDeliveryContinue().then(({ continueDelivery }) => continueDelivery({
      tabId: ownership?.tabId,
      ready: input.ready,
      goal: input.goal,
      uiOwnership: ownership,
      ownsUI: surfaceFence.ownsUnknown,
      resumeGoal: ports.resumeGoal,
      send: sendDeliveryRecovery,
    }));
  });

  return { handleDeliveryContinue };
}
