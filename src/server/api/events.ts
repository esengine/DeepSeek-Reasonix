/** SSE stream of DashboardEvents; 25s ping keeps proxies from dropping idle connections. */

import type { IncomingMessage, ServerResponse } from "node:http";
import type { DashboardContext, DashboardEvent } from "../context.js";

const PING_INTERVAL_MS = 25_000;

export function handleEvents(
  req: IncomingMessage,
  res: ServerResponse,
  ctx: DashboardContext,
): void {
  if (!ctx.subscribeEvents) {
    res.writeHead(503, { "content-type": "application/json" });
    res.end(JSON.stringify({ error: "event stream requires an attached dashboard session." }));
    return;
  }

  res.writeHead(200, {
    "content-type": "text/event-stream",
    "cache-control": "no-cache",
    connection: "keep-alive",
    "x-accel-buffering": "no", // disable Nginx-style buffering if anything proxies us
  });

  const writeEvent = (event: DashboardEvent): void => {
    if (res.writableEnded) return;
    try {
      res.write(`data: ${JSON.stringify(event)}\n\n`);
    } catch {
      /* socket gone — connection close handler will tidy up */
    }
  };

  // Send a snapshot busy-change immediately so the client's button
  // state is correct on first paint (instead of inheriting whatever
  // the prior connection's last delta said).
  if (ctx.isBusy) writeEvent({ kind: "busy-change", busy: ctx.isBusy() });
  // Replay the active modal — without this, a client that connects after
  // the modal-up broadcast fires (e.g., dashboard tab opened mid-turn)
  // misses the gate and the tool appears to hang forever (#1770).
  const activeModal = ctx.getActiveModal?.();
  if (activeModal) writeEvent({ kind: "modal-up", modal: activeModal });

  const unsubscribe = ctx.subscribeEvents(writeEvent);

  const ping = setInterval(() => writeEvent({ kind: "ping" }), PING_INTERVAL_MS);
  // Don't keep the process alive just for the heartbeat.
  ping.unref?.();

  const cleanup = (): void => {
    clearInterval(ping);
    try {
      unsubscribe();
    } catch {
      /* already torn down */
    }
    if (!res.writableEnded) {
      try {
        res.end();
      } catch {
        /* already closed */
      }
    }
  };

  req.on("close", cleanup);
  req.on("error", cleanup);
  res.on("close", cleanup);
}
