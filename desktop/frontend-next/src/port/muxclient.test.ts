// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { MuxClient } from "./muxclient";
import { subscribeStream } from "./sse_subscribe";
import { StreamBudget } from "./streambudget";
import type { WireEvent } from "./wire";

interface Source { url: string; closed: boolean; open: (conn: string) => void; say: (pane: string, frame: object) => void }

function stubHub(answer: (method: string, url: string, body: string) => { status: number; code?: string } = () => ({ status: 204 })) {
  const sources: Source[] = [];
  const calls: { method: string; url: string; body: string }[] = [];
  vi.stubGlobal(
    "EventSource",
    class {
      onmessage: ((m: { data: string }) => void) | null = null;
      onopen: (() => void) | null = null;
      readonly s: Source;
      constructor(url: string) {
        const emit = (obj: object) => this.onmessage?.({ data: JSON.stringify(obj) });
        this.s = {
          url,
          closed: false,
          open: (conn) => emit({ kind: "mux_open", conn }),
          say: (pane, frame) => emit({ pane, frame }),
        };
        sources.push(this.s);
      }
      close() {
        this.s.closed = true;
      }
    },
  );
  vi.stubGlobal("fetch", async (url: string, init: RequestInit = {}) => {
    const call = { method: init.method ?? "GET", url, body: String(init.body ?? "") };
    calls.push(call);
    const a = answer(call.method, url, call.body);
    return { ok: a.status < 300, status: a.status, json: async () => ({ code: a.code }) } as Response;
  });
  return { sources, calls };
}

const open = (s: Source[]) => s.filter((x) => !x.closed);
const flush = () => vi.advanceTimersByTimeAsync(0);

beforeEach(() => vi.useFakeTimers());
afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

function pane(client: MuxClient, id: string, seen: { at: number | null } = { at: null }) {
  const got: string[] = [];
  const own = vi.fn(() => Object.assign(() => {}, { pause: () => {}, resume: () => {} }));
  const stop = client.attach(id, { cursor: () => seen.at, onData: (raw) => got.push(raw), own });
  return { got, own, stop, seen };
}

describe("panes sharing one stream", () => {
  it("keeps paused SSH fallback streams charged when the connection budget settles again", async () => {
    const hub = stubHub(() => ({ status: 409, code: "mux.pane_not_local" }));
    const client = new MuxClient();
    const a = pane(client, "ssh-a");
    const b = pane(client, "ssh-b");
    hub.sources[0].open("c1");
    await flush();
    const budget = new StreamBudget(() => 1);
    const dropA = budget.attach("ssh-a", a.stop, () => {}, { visible: true, busy: false });
    const dropB = budget.attach("ssh-b", b.stop, () => {}, { visible: false, busy: false });
    expect(budget.liveIds()).toEqual(["ssh-a"]);
    budget.resettle();
    expect(budget.liveIds()).toEqual(["ssh-a"]);
    budget.update("ssh-a", { visible: false, busy: false });
    budget.update("ssh-b", { visible: true, busy: false });
    budget.resettle();
    expect(budget.liveIds()).toEqual(["ssh-b"]);
    dropA(); dropB(); a.stop(); b.stop();
  });
  it("carries nine panes on one connection", async () => {
    const hub = stubHub();
    const client = new MuxClient();
    const panes = Array.from({ length: 9 }, (_, i) => pane(client, `r${i}`));
    expect(open(hub.sources)).toHaveLength(1);
    hub.sources[0].open("c1");
    await flush();
    expect(hub.calls.filter((c) => c.method === "PUT").map((c) => c.url)).toEqual(
      panes.map((_, i) => `/events/mux/c1/panes/r${i}`),
    );
    expect(hub.calls[0].body).toBe('{"after":null}');
  });

  it("hands each frame to its own pane", async () => {
    const hub = stubHub();
    const client = new MuxClient();
    const a = pane(client, "r1");
    const b = pane(client, "r2");
    hub.sources[0].open("c1");
    hub.sources[0].say("r1", { kind: "x", seq: 1 });
    hub.sources[0].say("r2", { kind: "y", seq: 7 });
    hub.sources[0].say("r1", { kind: "mux_pane_closed" });
    expect(a.got).toEqual(['{"kind":"x","seq":1}']);
    expect(b.got).toEqual(['{"kind":"y","seq":7}']);
  });

  it("subscribes again from each pane's own position when the stream comes back", async () => {
    const hub = stubHub();
    const client = new MuxClient();
    const a = pane(client, "r1", { at: 5 });
    pane(client, "r2", { at: null });
    hub.sources[0].open("c1");
    await flush();
    hub.calls.length = 0;
    a.seen.at = 9;
    hub.sources[0].open("c2");
    await flush();
    expect(hub.calls.map((c) => [c.method, c.url, c.body])).toEqual([
      ["PUT", "/events/mux/c2/panes/r1", '{"after":9}'],
      ["PUT", "/events/mux/c2/panes/r2", '{"after":null}'],
    ]);
  });

  it("gives a pane up with a DELETE and takes it again from where it stopped", async () => {
    const hub = stubHub();
    const client = new MuxClient();
    const a = pane(client, "r1", { at: 3 });
    pane(client, "r2");
    hub.sources[0].open("c1");
    await flush();
    hub.calls.length = 0;
    a.stop.pause();
    a.seen.at = 4;
    a.stop.resume();
    await flush();
    expect(hub.calls.map((c) => [c.method, c.url, c.body])).toEqual([
      ["DELETE", "/events/mux/c1/panes/r1", ""],
      ["PUT", "/events/mux/c1/panes/r1", '{"after":4}'],
    ]);
    expect(open(hub.sources)).toHaveLength(1);
  });

  it("closes the shared stream when no pane needs it", async () => {
    const hub = stubHub();
    const client = new MuxClient();
    const a = pane(client, "r1");
    a.stop();
    expect(open(hub.sources)).toHaveLength(0);
  });

  it("opens a stream of its own for a pane the hub does not run", async () => {
    const hub = stubHub((_m, url) => (url.endsWith("/panes/ssh1") ? { status: 409, code: "mux.pane_not_local" } : { status: 204 }));
    const client = new MuxClient();
    const fell = vi.fn();
    client.onFallback(fell);
    const local = pane(client, "r1");
    const remote = pane(client, "ssh1");
    hub.sources[0].open("c1");
    await flush();
    expect(remote.own).toHaveBeenCalledOnce();
    expect(local.own).not.toHaveBeenCalled();
    expect(client.carried()).toEqual(["r1"]);
    expect(fell).toHaveBeenCalledOnce();
    hub.sources[0].say("ssh1", { kind: "z", seq: 1 });
    expect(remote.got).toEqual([]);
  });

  it("reopens the stream when the hub no longer knows the connection", async () => {
    let stale = true;
    const hub = stubHub((m) => {
      if (stale && m === "PUT") {
        stale = false;
        return { status: 404, code: "mux.unknown_connection" };
      }
      return { status: 204 };
    });
    const client = new MuxClient();
    pane(client, "r1");
    hub.sources[0].open("c1");
    await flush();
    expect(hub.sources[0].closed).toBe(true);
    expect(open(hub.sources)).toHaveLength(1);
    hub.sources[1].open("c2");
    await flush();
    expect(hub.calls.at(-1)?.url).toBe("/events/mux/c2/panes/r1");
  });
});

describe("a shared stream that never opens", () => {
  it("gives each pane a stream of its own once it has waited long enough", async () => {
    const hub = stubHub();
    const client = new MuxClient();
    const a = pane(client, "r1");
    const b = pane(client, "r2");
    await vi.advanceTimersByTimeAsync(9_000);
    expect(a.own).not.toHaveBeenCalled();
    await vi.advanceTimersByTimeAsync(2_000);
    expect(a.own).toHaveBeenCalledOnce();
    expect(b.own).toHaveBeenCalledOnce();
    expect(open(hub.sources)).toHaveLength(0);
    expect(client.carried()).toEqual([]);
  });

  it("keeps the shared stream when it says it is open in time", async () => {
    const hub = stubHub();
    const client = new MuxClient();
    const a = pane(client, "r1");
    await vi.advanceTimersByTimeAsync(5_000);
    hub.sources[0].open("c1");
    await vi.advanceTimersByTimeAsync(20_000);
    expect(a.own).not.toHaveBeenCalled();
    expect(client.carried()).toEqual(["r1"]);
  });

  it("opens a pane attached afterwards on its own stream straight away", async () => {
    stubHub();
    const client = new MuxClient();
    pane(client, "r1");
    await vi.advanceTimersByTimeAsync(11_000);
    const late = pane(client, "r2");
    expect(late.own).toHaveBeenCalledOnce();
  });
});

describe("a pane's subscription over the shared stream", () => {
  it("loses no frame across a reconnect, even when the replay overlaps", async () => {
    const hub = stubHub();
    const got: number[] = [];
    const sub = subscribeStream("/rt/r1", (ev: WireEvent) => got.push(ev.seq ?? 0), undefined, undefined, "r1");
    const s = () => open(hub.sources)[0];
    s().open("c1");
    await flush();
    s().say("r1", { kind: "stream_watermark", seq: 0 });
    for (const seq of [1, 2, 3]) s().say("r1", { kind: "tick", seq });
    hub.sources[0].open("c2");
    await flush();
    expect(hub.calls.at(-1)).toMatchObject({ method: "PUT", url: "/events/mux/c2/panes/r1", body: '{"after":3}' });
    for (const seq of [3, 4, 5]) hub.sources[0].say("r1", { kind: "tick", seq });
    expect(got).toEqual([1, 2, 3, 4, 5]);
    sub();
  });

  it("keeps the budget out of panes that cost no connection", async () => {
    stubHub();
    const sub = subscribeStream("/rt/r1", () => {}, undefined, undefined, "r1");
    expect(sub.holdsConnection?.()).toBe(false);
    sub();
  });
});
