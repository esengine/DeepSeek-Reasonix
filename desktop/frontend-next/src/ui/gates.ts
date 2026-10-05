import { useCallback } from "react";
import type { ActionDispatch } from "react";
import type { AgentPort, ApprovalVerdict } from "../port/port";
import type { PlanAction } from "../port/session";
import type { SessionEvent } from "../state/session";

interface Inputs {
  port: AgentPort;
  dispatch: ActionDispatch<[SessionEvent]>;
  refreshStatus: () => void;
  fail: (e: unknown) => void;
  onRevise: () => void;
}

/** What a card sitting at a gate can answer with. Each of these settles one
 *  kernel decision and then re-reads the projection rather than patching it,
 *  because the answer that lands is the kernel's, not this one's. */
export function useGateActions({ port, dispatch, refreshStatus, fail, onRevise }: Inputs) {
  // Transcript rows are memoised on their item; a callback rebuilt every render
  // would defeat that on the two cards that take one.
  const onApprove = useCallback(
    async (itemId: string, id: string, v: ApprovalVerdict) => {
      try {
        await port.approve(id, v);
        dispatch({ kind: "__decided", id: itemId, verdict: v } as never);
        refreshStatus();
      } catch (e) {
        fail(e);
      }
    },
    [port, refreshStatus, fail],
  );

  const onFullAccess = useCallback(
    async (itemId: string) => {
      try {
        await port.setApprovalMode("yolo");
        dispatch({ kind: "__decided", id: itemId, verdict: "yolo" } as never);
        refreshStatus();
      } catch (e) {
        fail(e);
      }
    },
    [port, refreshStatus, fail],
  );

  // The plan gate has three outcomes the kernel keeps apart, and only one of
  // them is "allow". The other two both deny at the gate and differ in where
  // they leave you: revise stays in plan mode so the next thing you type is
  // feedback on the plan, exit leaves it. Leaving plan mode is the frontend's
  // job — the kernel does not clear the flag when a plan is approved, which is
  // why the chat TUI clears it here too. Studio never did, so approving a plan
  // executed it and then planned the next turn all over again.
  const onPlan = useCallback(
    async (itemId: string, id: string, action: PlanAction) => {
      // The three outcomes are three kernel transitions, so they go back whole
      // rather than as an allow/deny pair. The kernel moves the lifecycle
      // itself — setting plan mode from here would race its own transition — and
      // a stale decision is ordinary concurrency: say so, then re-read the
      // projection instead of binding this answer to whatever is open now.
      try {
        await port.planDecision(id, action);
        dispatch({ kind: "__decided", id: itemId, verdict: action } as never);
        refreshStatus();
        // Revising is done by talking, so put the cursor where the talking happens.
        if (action === "revise") onRevise();
      } catch (e) {
        fail(e);
      }
    },
    [port, refreshStatus, fail],
  );

  // Taking it back is only cheap while the conversation that caused it is still
  // on screen; the card stays, marked, so the record of what happened survives.
  const onForget = useCallback(
    (itemId: string, name: string) => {
      port.forgetMemory(name).then(() => dispatch({ kind: "__forgot", id: itemId } as never)).catch(fail);
    },
    [port, fail],
  );

  // An extension action reports back in its own words. Surfacing the result as
  // a notice keeps it in the transcript where the card that offered it sits.
  const onExtInvoke = useCallback(
    (name: string) => {
      port
        .invokeExtensionAction(name)
        .then((message) => {
          if (message.trim()) dispatch({ kind: "notice", level: "info", text: message });
        })
        .catch(fail);
    },
    [port, fail],
  );

  const onExtSubmit = useCallback(
    (pluginId: string, surfaceId: string, values: Record<string, unknown>) => {
      port.submitExtensionForm(pluginId, surfaceId, values).catch(fail);
    },
    [port, fail],
  );

  const onAnswer = useCallback(
    async (itemId: string, id: string, answers: { questionId: string; selected: string[] }[]) => {
      try {
        await port.answer(id, answers);
        dispatch({ kind: "__decided", id: itemId, answers: answers.map((a) => a.selected) } as never);
      } catch (e) {
        fail(e);
      }
    },
    [port, fail],
  );
  return { onApprove, onFullAccess, onPlan, onForget, onExtInvoke, onExtSubmit, onAnswer };
}
