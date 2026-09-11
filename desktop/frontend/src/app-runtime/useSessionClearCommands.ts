import { useState } from "react";
import type { Translator } from "../lib/i18n";
import { useCommittedCommand } from "../lib/useCommittedCommand";
import type { useSessionOperations } from "./useSessionOperations";

export type SessionClearCommandsInput = {
  activeTabId: string | undefined;
  activeSessionIdentity: string;
  remote: boolean;
  t: Translator;
  notice: (text: string, level?: "info" | "warn") => void;
  operations: ReturnType<typeof useSessionOperations>;
  refreshDock(): void;
  ports: {
    clearSession(): Promise<void>;
    clearRemoteSession(tabId: string): Promise<void>;
    retryRemoteHydration(): Promise<void>;
  };
};

/**
 * Owns the clear-context decision surface: the pending flag, its cancel and
 * the confirm chain — target capture at click time, sessionRuntimeOwner
 * execution under the session operations authority, dock refresh plus notice
 * on commit, and a warning notice on failure. Tab switches and session
 * replacement still reset the flag through the returned setter. The runtime
 * owner chunk stays lazy behind the confirm.
 */
export function useSessionClearCommands(input: SessionClearCommandsInput) {
  const { activeTabId, activeSessionIdentity, t, notice, operations, ports } = input;
  const [clearContextPending, setClearContextPending] = useState(false);

  const cancelClearContext = useCommittedCommand(() => {
    setClearContextPending(false);
  });

  const confirmClearContext = useCommittedCommand(async () => {
    const target = activeTabId ? { tabId: activeTabId, sessionKey: activeSessionIdentity } : null;
    if (!target) return;
    setClearContextPending(false);
    const outcome = await operations(target, "clear-context", { remote: input.remote }, async (operationInput, authority) =>
      (await import("./sessionRuntimeOwner")).executeClearSession(target, operationInput, ports, authority),
    );
    if (outcome.status === "completed") {
      input.refreshDock();
      notice(t("clearContext.done"));
    } else if (outcome.status === "failed") {
      const message = outcome.error instanceof Error ? outcome.error.message : String(outcome.error);
      notice(message || t("clearContext.failed"), "warn");
    }
  });

  return { clearContextPending, setClearContextPending, cancelClearContext, confirmClearContext };
}
