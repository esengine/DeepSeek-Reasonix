import { useCallback, useEffect, useRef, useState } from "react";
import type { ActionDispatch } from "react";
import type { AgentPort, Queue as QueueSnapshot, QueueItem } from "../port/port";
import type { SessionEvent } from "../state/session";
import { localId } from "../state/session";

interface Inputs {
  port: AgentPort;
  dispatch: ActionDispatch<[SessionEvent]>;
  fail: (e: unknown) => void;
  moved: number;
  sessionState?: "pending" | "settled";
  sessionPath?: string;
}

/** The outbox as the kernel holds it, and what can be done to a line still in
 *  it: send it now, make it guidance, take it back, retry it. Nothing here patches the snapshot: every edit is asked
 *  of the kernel and the answer is read back whole, which is also what puts
 *  another window's lines in front of this one. */
export function useQueueActions({ port, dispatch, fail, moved, sessionState = "settled", sessionPath }: Inputs) {
  const [queue, setQueue] = useState<QueueSnapshot | null>(null);
  const dispatchRef = useRef(dispatch);
  dispatchRef.current = dispatch;
  // The last line taken back, numbered so the same text twice is two requests.
  const [restored, setRestored] = useState({ n: 0, text: "" });
  const onRestoreText = useCallback((text: string) => setRestored((r) => ({ n: r.n + 1, text })), []);
  // The queue as the kernel holds it. The frame says only that it moved, so the
  // answer is read back whole — which is also what puts another window's lines,
  // and the CLI's, in front of this one. The optimistic rows say only what was
  // sent from here, and they do not survive a reload.
  useEffect(() => {
    if (sessionState === "pending" && !moved) return;
    let current = true;
    port.queue().then((q) => {
      if (!current) return;
      setQueue(q);
      if (!q.paused) dispatchRef.current({ kind: "__queue_resumed" } as never);
    }).catch(() => current && setQueue(null));
    return () => { current = false; };
  }, [port, moved, sessionState, sessionPath]);
  const taking = useRef(new Set<string>());

  const onQueueRetry = useCallback((id: string) => void port.retryQueued(id).catch(fail), [port, fail]);
  const onQueuePause = useCallback((on: boolean) => void port.setQueuePaused(on).catch(fail), [port, fail]);
  // A follow-up becomes guidance by being taken back and said again as a steer;
  // the steer the kernel cannot admit is converted to a follow-up, so the line
  // is never lost. The body is read before the entry is given up.
  const onQueueToSteer = useCallback(
    async (item: QueueItem) => {
      if (taking.current.has(item.id)) return;
      taking.current.add(item.id);
      try {
        const text = await port.readQueued(item.id);
        await port.cancelQueued(item.id);
        dispatch({ kind: "__unsent", id: item.id } as never);
        const id = localId();
        dispatch({ kind: "__user", text, pending: true, id } as never);
        const said = await port.steer(text);
        if (said.itemId) dispatch({ kind: "__queued", id, itemId: said.itemId, queued: said.paused || said.disposition === "queued_followup" ? "followup" : "steer", paused: said.paused } as never);
      } catch (e) {
        fail(e);
      } finally {
        taking.current.delete(item.id);
      }
    },
    [port, fail, dispatch],
  );
  // Cancellation lets the dispatcher take the queue head. Accepted guidance
  // must leave the active turn and become a follow-up before that turn ends.
  const onQueueSendNow = useCallback(
    async (item: QueueItem) => {
      try {
        let itemId = item.id;
        if (item.state === "steer_accepted") {
          const text = await port.readQueued(item.id);
          await port.cancelQueued(item.id);
          dispatch({ kind: "__unsent", id: item.id } as never);
          const id = localId();
          dispatch({ kind: "__user", text, pending: false, id } as never);
          const again = await port.queueFollowup(text);
          itemId = again.itemId;
          dispatch({ kind: "__queued", id, itemId, queued: "followup" } as never);
        }
        await port.moveQueued(itemId, 0);
        await port.cancel();
      } catch (e) {
        fail(e);
      }
    },
    [port, fail, dispatch],
  );
  // The panel knows the entry, never the row the composer minted for it, so
  // taking one back here has to name it the way the kernel does. The body is
  // read before the entry is given up: once it is cancelled nothing else holds
  // the text, and a line that cannot be read stays queued rather than lost.
  const onQueueCancel = useCallback(
    async (itemId: string) => {
      if (taking.current.has(itemId)) return;
      taking.current.add(itemId);
      try {
        const text = await port.readQueued(itemId);
        await port.cancelQueued(itemId);
        dispatch({ kind: "__unsent", id: itemId } as never);
        onRestoreText(text);
      } catch (e) {
        taking.current.delete(itemId);
        fail(e);
      }
    },
    [port, fail, dispatch, onRestoreText],
  );
  return { queue, restored, onRestoreText, onQueueRetry, onQueuePause, onQueueToSteer, onQueueSendNow, onQueueCancel };
}
