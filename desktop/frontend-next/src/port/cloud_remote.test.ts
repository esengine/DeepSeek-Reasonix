// @vitest-environment jsdom
import { describe, expect, it, vi } from "vitest";
import { onRemoteConnectionEnded, relayFor, RemoteEventSource, remoteCodec, remoteConnectionEnded } from "./cloud_remote";
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
    // No cursor is an attach, and an attach is answered with whatever a run is
    // stopped on rather than with the stream's history.
    expect(String(fetchMock.mock.calls[0][0])).toBe("/rt/one/events/replay");
    expect(seen).toEqual([{ kind: "stream_watermark", seq: 7 }]);

    await vi.advanceTimersByTimeAsync(1500);
    expect(String(fetchMock.mock.calls[1][0])).toBe("/rt/one/events/replay?lastEventId=7");
    expect(seen.slice(1)).toEqual([{ kind: "message", seq: 8, text: "new reply" }]);

    source.close();
    fetchMock.mockRestore();
    vi.useRealTimers();
  });

  it("hands an attaching page the question a run is waiting on", async () => {
    vi.useFakeTimers();
    const fetchMock = vi.spyOn(globalThis, "fetch")
      .mockResolvedValueOnce(new Response(JSON.stringify({
        frames: [{ kind: "ask_request", ask: { id: "ask-1" } }],
        complete: true,
        watermark: 3,
      }), { status: 200 }))
      .mockResolvedValue(new Response(JSON.stringify({ frames: [], complete: true, watermark: 3 }), { status: 200 }));
    const seen: Array<Record<string, unknown>> = [];
    const source = new RemoteEventSource("/rt/one/events");
    source.onmessage = (event) => seen.push(JSON.parse(event.data) as Record<string, unknown>);

    await vi.advanceTimersByTimeAsync(0);
    // What a run is blocked on is not in the ordinary history a client resumes
    // past, so a page that read only the log would draw the run as stopped with
    // nothing to answer.
    expect(seen).toEqual([{ kind: "ask_request", ask: { id: "ask-1" } }]);

    await vi.advanceTimersByTimeAsync(200);
    expect(String(fetchMock.mock.calls[1][0])).toBe("/rt/one/events/replay?lastEventId=3");

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
    // Every read goes to the replay route: the attach carries no cursor, and
    // every later read resumes from the watermark.
    for (const [input] of fetchMock.mock.calls) expect(String(input)).toMatch(/^\/rt\/one\/events\/replay(\?|$)/);
    fetchMock.mockRestore();
    vi.unstubAllGlobals();
    vi.useRealTimers();
  });

  it("agrees the same secret with the device through the software curve when WebCrypto has no X25519", async () => {
    const device = await crypto.subtle.generateKey({ name: "X25519" }, true, ["deriveBits"]);
    const devicePublic = new Uint8Array(await crypto.subtle.exportKey("raw", device.publicKey));
    const spy = vi.spyOn(crypto.subtle, "generateKey").mockRejectedValueOnce(new DOMException("Algorithm: Unrecognized name", "NotSupportedError"));

    const ours = await remoteCodec.agreement(devicePublic);
    expect(spy).toHaveBeenCalledOnce();
    spy.mockRestore();

    // The device derives from our public key with its own private key, as the desktop does.
    const peer = await crypto.subtle.importKey("raw", ours.publicKey, { name: "X25519" }, false, []);
    const theirs = new Uint8Array(await crypto.subtle.deriveBits({ name: "X25519", public: peer }, device.privateKey, 256));
    expect(ours.secret).toEqual(theirs);
    expect(ours.publicKey).toHaveLength(32);
  });

  it("uses WebCrypto and never loads the software curve when the browser has X25519", async () => {
    const device = await crypto.subtle.generateKey({ name: "X25519" }, true, ["deriveBits"]);
    const devicePublic = new Uint8Array(await crypto.subtle.exportKey("raw", device.publicKey));
    const ours = await remoteCodec.agreement(devicePublic);
    const peer = await crypto.subtle.importKey("raw", ours.publicKey, { name: "X25519" }, false, []);
    const theirs = new Uint8Array(await crypto.subtle.deriveBits({ name: "X25519", public: peer }, device.privateKey, 256));
    expect(ours.secret).toEqual(theirs);
  });

  it("does not hide a failure that is not a missing algorithm", async () => {
    vi.spyOn(crypto.subtle, "generateKey").mockRejectedValueOnce(new Error("quota exceeded"));
    await expect(remoteCodec.agreement(new Uint8Array(32).fill(9))).rejects.toThrow("quota exceeded");
    vi.restoreAllMocks();
  });

  // A relay or a hostile device can send any 32 bytes as its key. A weak point
  // must be refused by the software curve exactly as WebCrypto refuses it.
  const weakPoints: Record<string, Uint8Array> = {
    "all zeros": new Uint8Array(32),
    "order 4 (u = 1)": Uint8Array.from([1, ...new Array(31).fill(0)]),
    "order 8": Uint8Array.from([
      0xe0, 0xeb, 0x7a, 0x7c, 0x3b, 0x41, 0xb8, 0xae, 0x16, 0x56, 0xe3, 0xfa, 0xf1, 0x9f, 0xc4, 0x6a,
      0xda, 0x09, 0x8d, 0xeb, 0x9c, 0x32, 0xb1, 0xfd, 0x86, 0x62, 0x05, 0x16, 0x5f, 0x49, 0xb8, 0x00,
    ]),
    "wrong length": new Uint8Array(31).fill(7),
  };
  for (const [name, point] of Object.entries(weakPoints)) {
    it(`refuses a device key that is ${name} through the software curve`, async () => {
      vi.spyOn(crypto.subtle, "generateKey").mockRejectedValueOnce(new DOMException("Algorithm: Unrecognized name", "NotSupportedError"));
      await expect(remoteCodec.agreement(point as Uint8Array<ArrayBuffer>)).rejects.toThrow();
      vi.restoreAllMocks();
    });
  }
});

describe("the relay a connection grant names", () => {
  const hosted = "wss://remote.reasonix.io";

  it("is the hosted relay when the grant names none", () => {
    expect(relayFor(null, hosted)).toBe(hosted);
    expect(relayFor(undefined, hosted)).toBe(hosted);
  });

  it("speaks wss to the https origin of a self-hosted gateway", () => {
    expect(relayFor("https://relay.example.com", hosted)).toBe("wss://relay.example.com");
    expect(relayFor("https://relay.example.com:8443/", hosted)).toBe("wss://relay.example.com:8443");
  });

  it.each([
    "http://relay.example.com", "wss://relay.example.com", "https://user:pw@relay.example.com", "https://relay.example.com/path",
    "https://relay.example.com/?x=1", "not a url", "", 7, {},
  ])("refuses %j, so a ticket is never offered to a relay it was not issued for", (named) => {
    expect(relayFor(named, hosted)).toBeNull();
  });
});
