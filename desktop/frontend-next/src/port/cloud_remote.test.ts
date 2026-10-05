// @vitest-environment jsdom
import { describe, expect, it, vi } from "vitest";
import { onRemoteConnectionEnded, RemoteEventSource, remoteCodec, remoteConnectionEnded } from "./cloud_remote";
import { openLiveStream, STREAM_SILENCE_MS } from "./livestream";

describe("remote Studio binary framing", () => {
  it("round-trips binary bodies and joins response chunks in index order", () => {
    const source = new Uint8Array([0, 1, 2, 127, 128, 254, 255]);
    const encoded = remoteCodec.bytesToBase64(source);
    expect(remoteCodec.base64ToBytes(encoded)).toEqual(source);
    const joined = remoteCodec.concatChunks(new Map([
      [1, remoteCodec.bytesToBase64(source.subarray(4))],
      [0, remoteCodec.bytesToBase64(source.subarray(0, 4))],
    ]));
    expect(joined).toEqual(source);
  });

  it("allows a slow mobile handshake and closes a socket that times out", async () => {
    vi.useFakeTimers();
    const socket = new EventTarget() as EventTarget & { close: ReturnType<typeof vi.fn> };
    socket.close = vi.fn();
    const pending = remoteCodec.waitForSocketOpen(socket as unknown as WebSocket, remoteCodec.handshakeTimeoutMS)
      .catch((error: unknown) => error);

    await vi.advanceTimersByTimeAsync(10_000);
    expect(socket.close).not.toHaveBeenCalled();
    await vi.advanceTimersByTimeAsync(20_000);
    expect(await pending).toBeInstanceOf(Error);
    expect(socket.close).toHaveBeenCalledOnce();
    vi.useRealTimers();
  });

  it("stops the handshake timer once the relay opens", async () => {
    vi.useFakeTimers();
    const socket = new EventTarget() as EventTarget & { close: ReturnType<typeof vi.fn> };
    socket.close = vi.fn();
    const pending = remoteCodec.waitForSocketOpen(socket as unknown as WebSocket);
    socket.dispatchEvent(new Event("open"));

    await expect(pending).resolves.toBeUndefined();
    await vi.advanceTimersByTimeAsync(remoteCodec.handshakeTimeoutMS);
    expect(socket.close).not.toHaveBeenCalled();
    vi.useRealTimers();
  });

  it("announces a relay disconnect to the Web Studio shell", () => {
    let reason = "";
    const stop = onRemoteConnectionEnded((end) => { reason = end.reason; });
    remoteCodec.announceClosed({ kind: "ended", reason: "Disconnected by device" });
    stop();
    expect(reason).toBe("Disconnected by device");
    expect(remoteConnectionEnded()).toEqual({ kind: "ended", reason: "Disconnected by device" });
  });

  it("starts polling after the host watermark instead of replaying restored history", async () => {
    vi.useFakeTimers();
    const fetchMock = vi.spyOn(globalThis, "fetch")
      .mockResolvedValueOnce(new Response(JSON.stringify({ frames: null, complete: true, watermark: 7 }), { status: 200 }))
      .mockResolvedValueOnce(new Response(JSON.stringify({
        frames: [{ kind: "message", seq: 8, text: "new reply" }],
        complete: true,
        watermark: 8,
      }), { status: 200 }));
    const seen: Array<Record<string, unknown>> = [];
    const source = new RemoteEventSource("/rt/one/events");
    source.onmessage = (event) => seen.push(JSON.parse(event.data) as Record<string, unknown>);

    await vi.advanceTimersByTimeAsync(0);
    expect(String(fetchMock.mock.calls[0][0])).toBe(`/rt/one/events/replay?lastEventId=${Number.MAX_SAFE_INTEGER}`);
    expect(seen).toEqual([{ kind: "stream_watermark", seq: 7 }]);

    await vi.advanceTimersByTimeAsync(1500);
    expect(String(fetchMock.mock.calls[1][0])).toBe("/rt/one/events/replay?lastEventId=7");
    expect(seen.slice(1)).toEqual([{ kind: "message", seq: 8, text: "new reply" }]);

    source.close();
    fetchMock.mockRestore();
    vi.useRealTimers();
  });

  it("resumes from the cursor its URL carries", async () => {
    vi.useFakeTimers();
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(new Response(JSON.stringify({
      frames: [{ kind: "message", seq: 6, text: "missed while away" }],
      complete: true,
      watermark: 6,
    }), { status: 200 }));
    const seen: Array<Record<string, unknown>> = [];
    const source = new RemoteEventSource("/rt/one/events?lastEventId=5");
    source.onmessage = (event) => seen.push(JSON.parse(event.data) as Record<string, unknown>);

    await vi.advanceTimersByTimeAsync(0);
    source.close();
    expect(String(fetchMock.mock.calls[0][0])).toBe("/rt/one/events/replay?lastEventId=5");
    expect(seen).toEqual([{ kind: "message", seq: 6, text: "missed while away" }]);
    fetchMock.mockRestore();
    vi.useRealTimers();
  });

  it("says so when the log no longer reaches the cursor", async () => {
    vi.useFakeTimers();
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(new Response(JSON.stringify({
      frames: [{ kind: "message", seq: 40, text: "oldest kept" }],
      complete: false,
      watermark: 40,
    }), { status: 200 }));
    const seen: Array<Record<string, unknown>> = [];
    const source = new RemoteEventSource("/rt/one/events?lastEventId=5");
    source.onmessage = (event) => seen.push(JSON.parse(event.data) as Record<string, unknown>);

    await vi.advanceTimersByTimeAsync(0);
    source.close();
    expect(seen).toEqual([
      { kind: "stream_gap", seq: 40 },
      { kind: "message", seq: 40, text: "oldest kept" },
    ]);
    fetchMock.mockRestore();
    vi.useRealTimers();
  });

  it("keeps an idle pane's stream under the silence watchdog", async () => {
    vi.useFakeTimers();
    const fetchMock = vi.spyOn(globalThis, "fetch").mockImplementation(async () =>
      new Response(JSON.stringify({ frames: null, complete: true, watermark: 7 }), { status: 200 }));
    vi.stubGlobal("EventSource", RemoteEventSource);
    let opened = 0;
    const stop = openLiveStream(() => `/rt/one/events${opened++ ? "?lastEventId=7" : ""}`, () => {});

    await vi.advanceTimersByTimeAsync(STREAM_SILENCE_MS * 3);
    stop();
    expect(opened).toBe(1);
    for (const [input] of fetchMock.mock.calls) expect(String(input)).toMatch(/^\/rt\/one\/events\/replay\?/);
    fetchMock.mockRestore();
    vi.unstubAllGlobals();
    vi.useRealTimers();
  });
});
