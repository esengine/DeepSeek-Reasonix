import { useCallback, type Dispatch } from "react";
import { HttpError, type AgentPort, type ChipCall } from "../port/port";
import { localId } from "../state/session";
import type { reduceTraj } from "../state/trajectory";

type TrajAction = Parameters<typeof reduceTraj>[1];
type Dispatcher = Dispatch<never>;

/** Sending one line. The wire never echoes what was typed, so the row is the
 *  client's to add and its to take back when the line did not leave: reporting
 *  the refusal alone would leave a transcript showing a turn that never
 *  happened, with the composer already emptied. */
export function useSubmit({ port, running, dispatch, trajDispatch, refreshStatus, fail }: {
  port: AgentPort;
  running: boolean;
  dispatch: Dispatcher;
  trajDispatch: Dispatch<TrajAction>;
  refreshStatus: () => void;
  fail: (e: unknown) => void;
}) {
  // The kernel refuses a submit it cannot start with a code, not a sentence:
  // the words are fine, the timing is not. Queueing them is what that code
  // asks for.
  const submitOrQueue = useCallback(
    async (text: string, id: string, chips?: ChipCall) => {
      try {
        const held = await port.submit(text, chips);
        if (held?.paused && held.itemId) dispatch({ kind: "__queued", id, itemId: held.itemId, queued: "followup", paused: true } as never);
        refreshStatus();
      } catch (e) {
        if (!(e instanceof HttpError) || e.reason?.code !== "busy.session_running") throw e;
        const queued = await port.queueFollowup(text, chips);
        if (queued?.itemId) dispatch({ kind: "__queued", id, itemId: queued.itemId, queued: "followup", paused: queued.paused } as never);
      }
    },
    [port, dispatch, refreshStatus],
  );

  return useCallback(
    async (text: string, chips?: ChipCall) => {
      const steering = running;
      const id = localId();
      dispatch({ kind: "__user", text, pending: steering, id } as never);
      trajDispatch({ kind: "__user", text });
      try {
        if (steering) {
          // The row is already on screen; the receipt is what gives it a name
          // to be taken back by while it waits at the tool boundary.
          const queued = chips ? await port.queueFollowup(text, chips) : await port.steer(text);
          if (queued?.itemId) dispatch({ kind: "__queued", id, itemId: queued.itemId, queued: chips || queued.paused ? "followup" : "steer", paused: queued.paused } as never);
        } else {
          await submitOrQueue(text, id, chips);
        }
        return true;
      } catch (e) {
        dispatch({ kind: "__unsent", id } as never);
        fail(e);
        return false;
      }
    },
    [port, running, dispatch, trajDispatch, submitOrQueue, fail],
  );
}
