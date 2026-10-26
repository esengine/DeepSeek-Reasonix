import type { WireEvent } from "./wire";
import type { Subscription } from "./port";
import { openLiveStream } from "./livestream";
import { mux } from "./muxclient";

/** subscribe delivers the kernel's events, in order, with no holes it did not
 *  announce. Frames that matter are numbered, so a break in the numbers is
 *  the client noticing it missed something rather than a gap it renders as a
 *  quiet turn — and the two transports differ only in how they fetch the
 *  missing frames back, not in what a gap means.
 *
 *  onGap fires when the replay log can no longer close one. The hole is the
 *  transport's fact; which authority each read model returns to — /history,
 *  the run graph — is the caller's.
 *
 *  bootstrap is how a read model with a snapshot of its own joins without a
 *  seam: it reads one and answers with the frame that snapshot is at least as
 *  new as. Frames are held from before the transport is attached until the
 *  replay after that number has landed, so nothing between the two is
 *  skipped. The number is the whole stream's, never one model's — a frame of
 *  any kind carries the cursor past it, or the next number reads as a hole.
 *
 *  lane is the pane id when the hub can carry that pane on its shared stream. */
export function subscribeStream(base: string, onEvent: (ev: WireEvent) => void, onGap?: () => void, bootstrap?: () => Promise<number>, lane?: string): Subscription {
  // Frames arriving while a recovery request is in flight wait for it: the
  // whole point is that the reducer sees one ordered stream, and delivering
  // the new frame first would put a result ahead of the dispatch it answers.
  // null until the stream states a position: 0 cannot tell a subscriber that
  // just attached from one that has seen the stream start.
  let seen: number | null = null;
  // A bootstrapping subscription holds from before the transport is attached:
  // the shell's bus has no connection to open, so it is already delivering,
  // and a frame taken live here would be folded ahead of the replay's.
  let booting = !!bootstrap;
  let recovering = booting;
  let held: WireEvent[] = [];
  let live = true;
  // The shared stream attaches a pane only once it is open, which can be long
  // after this call. Where the stream stood at subscribing is read at once, and
  // an attach that finds it has moved on since reports what went by as a hole.
  let subscribedAt: number | null = null;
  let attachedAt: number | null = null;
  const blindAttach = () => {
    if (subscribedAt !== null && attachedAt !== null && attachedAt > subscribedAt) onGap?.();
  };

  const deliver = (ev: WireEvent) => {
    // The cursor is the record of what has been folded, and the replay and
    // the live stream reach the same frames — a snapshot's own watermark
    // included. Whichever arrives second is the same fact twice.
    if (ev.seq && seen !== null && ev.seq <= seen) return;
    if (ev.seq) seen = Math.max(seen ?? 0, ev.seq);
    onEvent(ev);
  };

  const flush = () => {
    recovering = false;
    const queued = held;
    held = [];
    for (const ev of queued) deliver(ev);
  };

  const recover = async (after: number) => {
    recovering = true;
    try {
      const res = await fetch(`${base}/events/replay?lastEventId=${after}`, { credentials: "same-origin" });
      if (!res.ok) throw new Error(String(res.status));
      const body = (await res.json()) as { frames?: WireEvent[]; complete?: boolean };
      if (!live) return;
      for (const ev of body.frames ?? []) deliver(ev);
      if (!body.complete) onGap?.();
    } catch {
      // The frames are gone and asking again would not bring them back. The
      // transcript is the one source that can still answer.
      if (live) onGap?.();
    } finally {
      flush();
    }
  };

  // What the snapshot and the replay overlap on folds twice onto the same
  // state; what falls between them is gone for good. So the read names where
  // it stands, and the stream resumes from there — one number, one stream.
  const bootstrapFrom = async (read: () => Promise<number>) => {
    let at: number | null = null;
    try {
      at = await read();
    } catch {
      // Nothing to resume onto. Holding the stream any longer would stall the
      // view rather than repair it, so it runs live and says it is behind.
      if (live) onGap?.();
    }
    if (!live) return;
    if (at === null) {
      booting = false;
      flush();
      return;
    }
    seen = at;
    booting = false;
    await recover(at);
  };

  const accept = (ev: WireEvent) => {
    // The stream describing itself: a watermark states the number the client
    // should have reached, which is the only way to notice a frame lost at
    // the end of a turn. Neither reaches the reducer.
    if (ev.kind === "stream_watermark") {
      // The first one states where this subscriber attached: a live-only
      // subscription replays nothing before it, so nothing before it was lost.
      // A bootstrap states that position from its snapshot instead, and taking
      // it from here would baseline the cursor over frames already held.
      if (seen === null) {
        if (!booting) {
          seen = ev.seq ?? 0;
          attachedAt = seen;
          blindAttach();
        }
        return;
      }
      if (!recovering && ev.seq && ev.seq > seen) void recover(seen);
      return;
    }
    if (ev.kind === "stream_gap") {
      // The hole reaches the caller either way — it is the whole stream's,
      // not the bootstrapping model's — but the number it carries is not a
      // position this subscriber reached.
      if (!booting) seen = Math.max(seen ?? 0, ev.seq ?? 0);
      onGap?.();
      return;
    }
    if (recovering) {
      held.push(ev);
      return;
    }
    // Numbering that goes backwards is a different stream, not a gap: the
    // server restarted and is counting from one again. Inferred rather than
    // carried as a stream id — a resumed client is never sent a lower number
    // than it holds, so nothing else produces this. Without it the client
    // compares against a watermark that will never be reached again and
    // silently stops noticing losses.
    if (ev.seq && seen && ev.seq < seen) {
      seen = ev.seq;
      onGap?.();
      onEvent(ev);
      return;
    }
    if (ev.seq && seen && ev.seq > seen + 1) {
      held.push(ev);
      void recover(seen);
      return;
    }
    deliver(ev);
  };

  const feed = (raw: string) => {
    try {
      accept(JSON.parse(raw) as WireEvent);
    } catch {
      // A malformed frame must not tear down the stream.
    }
  };
  // EventSource resumes on its own: it reconnects carrying the last id it
  // saw, and the server replays from there. Recovery here is for the frames
  // shed without the connection ever dropping, and a replacement stream for
  // one that went silent without dropping resumes from the same cursor.
  const own = () => openLiveStream(() => base + "/events" + (seen === null ? "" : `?lastEventId=${seen}`), feed);
  // lane names a pane the hub runs locally: its frames ride the one shared
  // stream instead of costing the window a connection each.
  const detach = lane ? mux.attach(lane, { cursor: () => seen, onData: feed, own }) : own();
  if (bootstrap) void bootstrapFrom(bootstrap);
  else if (lane) {
    // A position that cannot be read cannot vouch for the attach either.
    void positionNow(base).catch(() => -1).then((at) => {
      subscribedAt = at;
      if (live) blindAttach();
    });
  }
  // A subscription that gave its connection up before the stream ever stated a
  // position has nothing to resume from, so what went by is a hole it cannot
  // name: it says so, and the caller re-reads.
  let pausedBlind = false;
  return Object.assign(
    () => {
      live = false;
      detach();
    },
    {
      pause: () => {
        pausedBlind = seen === null;
        detach.pause();
      },
      resume: () => {
        detach.resume();
        if (pausedBlind) {
          pausedBlind = false;
          onGap?.();
        }
      },
      holdsConnection: () => detach.holdsConnection?.() ?? true,
    },
  );
}

// Where the stream stands now, as the attach read of a client with no stream
// open answers it.
async function positionNow(base: string): Promise<number> {
  const res = await fetch(`${base}/events/replay`, { credentials: "same-origin" });
  if (!res.ok) throw new Error(String(res.status));
  const { watermark } = (await res.json()) as { watermark?: unknown };
  if (typeof watermark !== "number") throw new Error("the attach read carried no watermark");
  return watermark;
}
