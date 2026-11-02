import { useCallback, type Dispatch } from "react";
import { HttpError, type AgentPort, type ChipCall } from "../port/port";
import { localId } from "../state/session";
import type { reduceTraj } from "../state/trajectory";
import { useRunningMessageIntent } from "../state/prefs";

/** What came of one line: sent, taken back, or refused because this
 *  connection cannot run a `!` command at all. The third is a different thing to
 *  do next from the second, and is the only one where what was typed is not a
 *  message to edit but a command to replace. */
export type Submitted = boolean | "shell_unavailable";

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
  const runningIntent = useRunningMessageIntent();
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
    async (text: string, chips?: ChipCall): Promise<Submitted> => {
      const queuedWhileRunning = running;
      const id = localId();
      dispatch({ kind: "__user", text, pending: queuedWhileRunning, id } as never);
      trajDispatch({ kind: "__user", text });
      try {
        if (queuedWhileRunning) {
          const followup = !!chips || runningIntent === "followup";
          const queued = followup ? await port.queueFollowup(text, chips) : await port.steer(text);
          if (queued?.itemId) dispatch({ kind: "__queued", id, itemId: queued.itemId, queued: followup || queued.paused || queued.disposition === "queued_followup" ? "followup" : "steer", paused: queued.paused } as never);
        } else {
          await submitOrQueue(text, id, chips);
        }
        return true;
      } catch (e) {
        dispatch({ kind: "__unsent", id } as never);
        fail(e);
        return e instanceof HttpError && e.reason?.code === "shell.unavailable_over_http" ? "shell_unavailable" : false;
      }
    },
    [port, running, runningIntent, dispatch, trajDispatch, submitOrQueue, fail],
  );
}
