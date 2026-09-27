// @vitest-environment jsdom
import { describe, expect, it, vi } from "vitest";
import { onRemoteConnectionEnded, RemoteEventSource, remoteCodec, remoteConnectionEnded } from "./cloud_remote";

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

  it("announces a relay disconnect to the Web Studio shell", () => {
    let reason = "";
    const stop = onRemoteConnectionEnded((value) => { reason = value; });
    remoteCodec.announceClosed("Disconnected by device");
    stop();
    expect(reason).toBe("Disconnected by device");
    expect(remoteConnectionEnded()).toBe(true);
  });

  it("starts polling after the host watermark instead of replaying restored history", async () => {
    vi.useFakeTimers();
    const fetchMock = vi.spyOn(globalThis, "fetch")
      .mockResolvedValueOnce(new Response(JSON.stringify({
        frames: [{ kind: "message", seq: 7, text: "already in history" }],
        complete: true,
        watermark: 7,
      }), { status: 200 }))
      .mockResolvedValueOnce(new Response(JSON.stringify({
        frames: [{ kind: "message", seq: 8, text: "new reply" }],
        complete: true,
        watermark: 8,
      }), { status: 200 }));
    const seen: Array<Record<string, unknown>> = [];
    const source = new RemoteEventSource("/rt/one/events");
    source.onmessage = (event) => seen.push(JSON.parse(event.data) as Record<string, unknown>);

    await vi.advanceTimersByTimeAsync(0);
    expect(seen).toEqual([]);
    expect(String(fetchMock.mock.calls[0][0])).toContain("lastEventId=0");

    await vi.advanceTimersByTimeAsync(200);
    expect(seen).toEqual([{ kind: "message", seq: 8, text: "new reply" }]);
    expect(String(fetchMock.mock.calls[1][0])).toContain("lastEventId=7");

    source.close();
    fetchMock.mockRestore();
    vi.useRealTimers();
  });
});
