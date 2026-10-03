import { useCallback, type ActionDispatch } from "react";
import { HttpError, type AgentPort, type ChipCall } from "../port/port";
import { localId, type SessionEvent } from "../state/session";

interface Inputs {
  port: AgentPort;
  running: boolean;
  dispatch: ActionDispatch<[SessionEvent]>;
  trajDispatch: (event: { kind: "__user"; text: string }) => void;
  refreshStatus: () => void;
  fail: (e: unknown) => void;
}

export function useSubmitActions({ port, running, dispatch, trajDispatch, refreshStatus, fail }: Inputs) {
  // A submit refused as busy must enter the queue rather than lose the input.
  const submitOrQueue = useCallback(
    async (text: string, id: string, chips?: ChipCall) => {
      try {
        await port.submit(text, chips);
        refreshStatus();
      } catch (e) {
        if (!(e instanceof HttpError) || e.reason?.code !== "busy.session_running") throw e;
        const queued = await port.queueFollowup(text, chips);
        if (queued?.itemId) dispatch({ kind: "__queued", id, itemId: queued.itemId, queued: "followup" });
      }
    },
    [port, refreshStatus, dispatch],
  );

  // The wire does not echo user input; failed sends must mark the local row unsent.
  const submit = useCallback(
    async (text: string, chips?: ChipCall) => {
      const id = localId();
      dispatch({ kind: "__user", text, pending: running, id });
      trajDispatch({ kind: "__user", text });
      try {
        if (running) {
          // The receipt identifies the pending row for queue actions.
          const queued = chips ? await port.queueFollowup(text, chips) : await port.steer(text);
          if (queued?.itemId) dispatch({ kind: "__queued", id, itemId: queued.itemId, queued: chips ? "followup" : "steer" });
        } else {
          await submitOrQueue(text, id, chips);
        }
        return true;
      } catch (e) {
        dispatch({ kind: "__unsent", id });
        fail(e);
        return false;
      }
    },
    [port, running, dispatch, trajDispatch, submitOrQueue, fail],
  );

  return { submit };
}
