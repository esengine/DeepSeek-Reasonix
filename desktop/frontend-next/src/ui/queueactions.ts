import { useCallback, useEffect, useState } from "react";
import type { ActionDispatch } from "react";
import type { AgentPort, Queue as QueueSnapshot, QueueItem } from "../port/port";
import type { SessionEvent } from "../state/session";
import { localId } from "../state/session";

interface Inputs {
  port: AgentPort;
  dispatch: ActionDispatch<[SessionEvent]>;
  fail: (e: unknown) => void;
  moved: number;
  sessionPath?: string;
}

/** The outbox as the kernel holds it, and the eight things that can be done to
 *  a line still in it. Nothing here patches the snapshot: every edit is asked
 *  of the kernel and the answer is read back whole, which is also what puts
 *  another window's lines in front of this one. */
export function useQueueActions({ port, dispatch, fail, moved, sessionPath }: Inputs) {
  const [queue, setQueue] = useState<QueueSnapshot | null>(null);
  // The last line taken back, numbered so the same text twice is two requests.
  const [restored, setRestored] = useState({ n: 0, text: "" });
  // The queue as the kernel holds it. The frame says only that it moved, so the
  // answer is read back whole — which is also what puts another window's lines,
  // and the CLI's, in front of this one. The optimistic rows say only what was
  // sent from here, and they do not survive a reload.
  useEffect(() => {
    port.queue().then(setQueue).catch(() => setQueue(null));
  }, [port, moved, sessionPath]);

  const onQueueEdit = useCallback((id: string, text: string) => void port.editQueued(id, text).catch(fail), [port, fail]);
  const onQueueMove = useCallback((id: string, to: number) => void port.moveQueued(id, to).catch(fail), [port, fail]);
  const onQueueRetry = useCallback((id: string) => void port.retryQueued(id).catch(fail), [port, fail]);
  const onQueueRefresh = useCallback((id: string) => void port.refreshQueued(id).catch(fail), [port, fail]);
  const onQueuePause = useCallback((on: boolean) => void port.setQueuePaused(on).catch(fail), [port, fail]);
  const onQueueRead = useCallback((id: string) => port.readQueued(id), [port]);
  // "Send now" means the only thing it can while a turn holds the session: end
  // that turn, and the queue dispatches this line as the next one. Guidance the
  // running turn already accepted has to leave it first — a turn that ends
  // without reading an accepted steer parks it as uncertain and pauses the
  // whole queue, which is the opposite of sending it.
  const onQueueSendNow = useCallback(
    async (item: QueueItem) => {
      try {
        if (item.state === "steer_accepted") {
          const text = await port.readQueued(item.id);
          await port.cancelQueued(item.id);
          dispatch({ kind: "__unsent", id: item.id } as never);
          const id = localId();
          dispatch({ kind: "__user", text, pending: false, id } as never);
          const again = await port.queueFollowup(text);
          if (again?.itemId) dispatch({ kind: "__queued", id, itemId: again.itemId, queued: "followup" } as never);
        }
        await port.cancel();
      } catch (e) {
        fail(e);
      }
    },
    [port, fail],
  );
  // The panel knows the entry, never the row the composer minted for it, so
  // taking one back here has to name it the way the kernel does. The body is
  // read before the entry is given up: once it is cancelled nothing else holds
  // the text, and a line that cannot be read stays queued rather than lost.
  const onQueueCancel = useCallback(
    async (itemId: string) => {
      try {
        const text = await port.readQueued(itemId);
        await port.cancelQueued(itemId);
        dispatch({ kind: "__unsent", id: itemId } as never);
        setRestored((r) => ({ n: r.n + 1, text }));
      } catch (e) {
        fail(e);
      }
    },
    [port, fail, dispatch],
  );
  return { queue, restored, onQueueEdit, onQueueMove, onQueueRetry, onQueueRefresh, onQueuePause, onQueueRead, onQueueSendNow, onQueueCancel };
}
