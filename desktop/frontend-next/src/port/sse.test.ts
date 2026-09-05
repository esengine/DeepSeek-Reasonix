import { afterEach, describe, expect, it, vi } from "vitest";
import { SsePort } from "./sse";
import type { WireEvent } from "./wire";

// Driven through the shell's bus, the transport with no reconnect to hide
// behind: every frame is handed over by hand, so a gap is only ever closed by
// the logic under test.
type Feed = (raw: string) => void;
type ReplyBody = { frames?: unknown[]; complete?: boolean } | null;

function attach(
  reply: (after: number) => ReplyBody = () => ({ frames: [], complete: true }),
  bootstrap?: () => Promise<number>,
) {
  let feed: Feed = () => {};
  const seen: WireEvent[] = [];
  const asked: string[] = [];
  let gaps = 0;

  vi.stubGlobal("window", {
    runtime: {
      EventsOn: (_name: string, cb: Feed) => {
        feed = cb;
        return () => {};
      },
    },
  });
  vi.stubGlobal("fetch", async (url: string) => {
    asked.push(url);
    if (url.includes("/rx-replay")) return { ok: true, json: async () => ({}) };
    const body = reply(Number(new URL(url, "http://x").searchParams.get("lastEventId") ?? 0));
    if (!body) return { ok: false, status: 500, json: async () => ({}) };
    return { ok: true, json: async () => body };
  });

  const stop = new SsePort("", "r1").subscribe(
    (ev) => seen.push(ev),
    () => {
      gaps++;
    },
    bootstrap,
  );
  return {
    feed: (kind: string, seq?: number) => feed(JSON.stringify({ kind, ...(seq ? { seq } : {}) })),
    seen,
    kinds: () => seen.map((e) => e.kind),
    seqs: () => seen.map((e) => e.seq),
    replays: () => asked.filter((u) => u.includes("/events/replay")),
    gaps: () => gaps,
    stop,
  };
}

const settle = () => new Promise((r) => setTimeout(r, 0));

afterEach(() => vi.unstubAllGlobals());

describe("recovering the event stream", () => {
  it("passes an unbroken stream straight through", async () => {
    const s = attach();
    s.feed("turn_started", 1);
    s.feed("text");
    s.feed("tool_result", 2);
    await settle();
    expect(s.kinds()).toEqual(["turn_started", "text", "tool_result"]);
    expect(s.replays()).toHaveLength(0);
    expect(s.gaps()).toBe(0);
    s.stop();
  });

  it("fetches exactly what a break in the numbers skipped", async () => {
    const s = attach(() => ({
      frames: [
        { kind: "tool_dispatch", seq: 2 },
        { kind: "approval_request", seq: 3 },
      ],
      complete: true,
    }));
    s.feed("turn_started", 1);
    s.feed("turn_done", 4);
    await settle();
    expect(s.kinds()).toEqual(["turn_started", "tool_dispatch", "approval_request", "turn_done"]);
    expect(s.replays()[0]).toContain("lastEventId=1");
    expect(s.gaps()).toBe(0);
    s.stop();
  });

  // Without this a result reaches the reducer ahead of the dispatch it answers.
  it("holds frames that arrive while a replay is in flight", async () => {
    let release!: () => void;
    const gate = new Promise<void>((r) => (release = r));
    const s = attach();
    vi.stubGlobal("fetch", async (url: string) => {
      await gate;
      const after = Number(new URL(url, "http://x").searchParams.get("lastEventId") ?? 0);
      return { ok: true, json: async () => ({ frames: [{ kind: "tool_dispatch", seq: after + 1 }], complete: true }) };
    });
    s.feed("turn_started", 1);
    s.feed("tool_result", 3); // 2 is missing
    s.feed("turn_done", 4); // arrives mid-recovery
    release();
    await settle();
    await settle();
    expect(s.kinds()).toEqual(["turn_started", "tool_dispatch", "tool_result", "turn_done"]);
    s.stop();
  });

  it("reports a gap the log can no longer close", async () => {
    const s = attach(() => ({ frames: [], complete: false }));
    s.feed("turn_started", 1);
    s.feed("turn_done", 9);
    await settle();
    expect(s.gaps()).toBe(1);
    s.stop();
  });

  // Asking again would not bring the frames back; the transcript is the only
  // source left that can answer.
  it("treats a failed replay request as a gap", async () => {
    const s = attach(() => null);
    s.feed("turn_started", 1);
    s.feed("turn_done", 9);
    await settle();
    expect(s.gaps()).toBe(1);
    s.stop();
  });

  it("notices a frame lost at the end of a turn", async () => {
    const s = attach(() => ({ frames: [{ kind: "turn_done", seq: 2 }], complete: true }));
    s.feed("turn_started", 1);
    s.feed("stream_watermark", 2);
    await settle();
    expect(s.kinds()).toContain("turn_done");
    expect(s.kinds()).not.toContain("stream_watermark");
    s.stop();
  });

  it("stays quiet when the watermark matches what it holds", async () => {
    const s = attach();
    s.feed("turn_done", 1);
    s.feed("stream_watermark", 1);
    await settle();
    expect(s.replays()).toHaveLength(0);
    s.stop();
  });

  // A live-only subscription replays nothing before it, so a high watermark on
  // a fresh attach states where the stream is, not a backlog this client lost.
  // Recovering from zero here replayed the whole log into the reducer.
  it("takes the first watermark as its position, not as a loss", async () => {
    const s = attach();
    s.feed("stream_watermark", 500);
    await settle();
    expect(s.replays()).toHaveLength(0);
    s.stop();
  });

  // Baselining must not blind it afterwards: the next watermark is a real test.
  it("still notices a loss once the first watermark set its position", async () => {
    const s = attach(() => ({ frames: [{ kind: "turn_done", seq: 502 }], complete: true }));
    s.feed("stream_watermark", 500);
    s.feed("turn_started", 501);
    s.feed("stream_watermark", 502);
    await settle();
    expect(s.kinds()).toContain("turn_done");
    s.stop();
  });

  it("takes the server's own gap signal without rendering it", async () => {
    const s = attach();
    s.feed("stream_gap", 7);
    await settle();
    expect(s.gaps()).toBe(1);
    expect(s.seen).toHaveLength(0);
    s.stop();
  });

  // A restarted server counts from one again. Comparing against the watermark
  // from the process before would silently disable every later gap check.
  it("reads numbering that goes backwards as a new stream", async () => {
    const s = attach(() => ({ frames: [{ kind: "tool_dispatch", seq: 3 }], complete: true }));
    s.feed("turn_started", 40);
    s.feed("turn_started", 1); // server restarted
    s.feed("tool_result", 2);
    s.feed("turn_done", 9); // a real gap, on the new numbering
    await settle();
    expect(s.gaps()).toBeGreaterThanOrEqual(1);
    expect(s.replays().some((u) => u.includes("lastEventId=2"))).toBe(true);
    s.stop();
  });

  // The log holds everything after the cursor, the frame that exposed the gap
  // included, so the replay answers with a copy of it. Folding both is what put
  // the same tool result on screen twice.
  it("folds a frame the replay and the live stream both carried exactly once", async () => {
    const s = attach(() => ({
      frames: [
        { kind: "tool_dispatch", seq: 2 },
        { kind: "approval_request", seq: 3 },
        { kind: "turn_done", seq: 4 },
      ],
      complete: true,
    }));
    s.feed("turn_started", 1);
    s.feed("turn_done", 4);
    await settle();
    expect(s.seqs()).toEqual([1, 2, 3, 4]);
    s.stop();
  });
});

// A snapshot read out of band is the other way onto this stream: a model starts
// from what an authority answered and the numbers carry it from there. The cut
// between the two is where the shell's bus loses frames — it has no reconnect
// to make the handover atomic — so every case here is built out of that cut.
describe("bootstrapping a read model onto the stream", () => {
  const snapshot = () => {
    let land!: (watermark: number) => void;
    const read = new Promise<number>((r) => (land = r));
    return { read: () => read, land };
  };

  const unreadable = () => {
    let refuse!: (why: Error) => void;
    const read = new Promise<number>((_, r) => (refuse = r));
    return { read: () => read, refuse: () => refuse(new Error("unreadable")) };
  };

  it("merges the replay with what arrived while it was in flight", async () => {
    const snap = snapshot();
    const s = attach(
      () => ({
        frames: [
          { kind: "graph_delta", seq: 101 },
          { kind: "tool_result", seq: 102 },
          { kind: "graph_delta", seq: 103 },
        ],
        complete: true,
      }),
      snap.read,
    );
    // The bus never stopped while the snapshot was being read. 101 landed
    // before this subscriber attached, so only the replay can bring it back;
    // 102 and 103 arrive down both paths and may be folded only once.
    s.feed("tool_result", 102);
    s.feed("graph_delta", 103);
    snap.land(100);
    await settle();
    await settle();
    s.feed("turn_done", 104);
    await settle();

    expect(s.seqs()).toEqual([101, 102, 103, 104]);
    expect(s.replays()).toHaveLength(1);
    expect(s.replays()[0]).toContain("lastEventId=100");
    expect(s.gaps()).toBe(0);
    s.stop();
  });

  // 101 is a graph frame, 102 is not, 103 is again. A cursor that only moved on
  // the model's own kind would read 102 as a hole it never had, ask for it
  // back, and fold everything after it a second time.
  it("carries the cursor past frames the model ignores", async () => {
    const snap = snapshot();
    const s = attach(() => ({ frames: [], complete: true }), snap.read);
    snap.land(100);
    await settle();
    await settle();
    s.feed("graph_delta", 101);
    s.feed("tool_result", 102);
    s.feed("stream_watermark", 102);
    await settle();
    expect(s.replays()).toHaveLength(1);

    s.feed("turn_done", 104); // 103 really is missing
    await settle();
    expect(s.replays()[1]).toContain("lastEventId=102");
    s.stop();
  });

  it("does not fold what the snapshot already holds", async () => {
    const snap = snapshot();
    const s = attach(() => ({ frames: [{ kind: "graph_delta", seq: 101 }], complete: true }), snap.read);
    s.feed("graph_delta", 99);
    s.feed("graph_delta", 100);
    snap.land(100);
    await settle();
    await settle();
    expect(s.seqs()).toEqual([101]);
    s.stop();
  });

  // A watermark states where a subscriber attached, and a bootstrap states that
  // from the snapshot instead. Taken from the watermark, the cursor would sit
  // ahead of frames this subscriber is holding but has not folded — and those
  // are then dropped as duplicates of nothing.
  it("does not take a watermark as its position while a snapshot is in flight", async () => {
    const snap = unreadable();
    const s = attach(undefined, snap.read);
    s.feed("stream_watermark", 103);
    s.feed("graph_delta", 101);
    snap.refuse();
    await settle();
    await settle();
    expect(s.seqs()).toEqual([101]);
    s.stop();
  });

  it("announces a hole without taking its number as a position", async () => {
    const snap = unreadable();
    const s = attach(undefined, snap.read);
    s.feed("stream_gap", 103);
    s.feed("graph_delta", 101);
    snap.refuse();
    await settle();
    await settle();
    expect(s.gaps()).toBe(2);
    expect(s.seqs()).toEqual([101]);
    s.stop();
  });

  // The hole is a transport fact; which authority a model re-reads to close it
  // is the model's own business. Saying so is all this layer can do about it.
  it("reports a hole the replay could not close and still delivers what it had", async () => {
    const snap = snapshot();
    const s = attach(() => ({ frames: [{ kind: "graph_delta", seq: 140 }], complete: false }), snap.read);
    snap.land(100);
    await settle();
    await settle();
    expect(s.gaps()).toBe(1);
    expect(s.seqs()).toEqual([140]);
    s.stop();
  });

  // A stream held for a snapshot that never comes is worse than a stale one:
  // the pane stops moving and nothing on it says why.
  it("runs live when the snapshot cannot be read", async () => {
    const snap = unreadable();
    const s = attach(undefined, snap.read);
    s.feed("graph_delta", 101);
    snap.refuse();
    await settle();
    await settle();
    expect(s.gaps()).toBe(1);
    expect(s.seqs()).toEqual([101]);
    s.feed("turn_done", 102);
    await settle();
    expect(s.seqs()).toEqual([101, 102]);
    s.stop();
  });
});
