// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { openLiveStream, STREAM_SILENCE_MS } from "./livestream";
import { SsePort } from "./sse";

type Stream = { url: string; closed: boolean; say: (raw: string) => void };

function stubStreams() {
  const opened: Stream[] = [];
  vi.stubGlobal(
    "EventSource",
    class {
      onmessage: ((m: { data: string }) => void) | null = null;
      onopen: (() => void) | null = null;
      readonly s: Stream;
      constructor(url: string) {
        this.s = { url, closed: false, say: (raw) => this.onmessage?.({ data: raw }) };
        opened.push(this.s);
      }
      close() {
        this.s.closed = true;
      }
    },
  );
  return opened;
}

beforeEach(() => vi.useFakeTimers());
afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

describe("a stream that goes silent", () => {
  it("keeps a stream that goes on hearing the kernel's watermark", () => {
    const opened = stubStreams();
    const stop = openLiveStream(() => "/events", () => {});
    for (let i = 0; i < 12; i++) {
      vi.advanceTimersByTime(15_000);
      opened[0].say('{"kind":"stream_watermark","seq":1}');
    }
    expect(opened).toHaveLength(1);
    stop();
    expect(opened[0].closed).toBe(true);
  });

  it("replaces one that has heard nothing for three watermark intervals", () => {
    const opened = stubStreams();
    const got: string[] = [];
    const stop = openLiveStream(() => `/events?n=${opened.length}`, (raw) => got.push(raw));
    vi.advanceTimersByTime(STREAM_SILENCE_MS + 5_000);
    expect(opened).toHaveLength(2);
    expect(opened[0].closed).toBe(true);
    expect(opened[1].url).toBe("/events?n=1");
    opened[1].say("x");
    expect(got).toEqual(["x"]);
    stop();
  });

  it("checks at once when the page comes back rather than waiting for a tick", () => {
    const opened = stubStreams();
    const stop = openLiveStream(() => "/events", () => {});
    vi.setSystemTime(Date.now() + STREAM_SILENCE_MS + 1);
    document.dispatchEvent(new Event("visibilitychange"));
    expect(opened).toHaveLength(2);
    stop();
  });

  it("resumes the replacement from the last frame the pane folded", () => {
    const opened = stubStreams();
    vi.stubGlobal("fetch", async () => ({ ok: true, json: async () => ({ frames: [], complete: true }) }));
    const seen: number[] = [];
    const stop = new SsePort("", "r1").subscribe((ev) => ev.seq && seen.push(ev.seq));
    opened[0].say('{"kind":"stream_watermark","seq":0}');
    opened[0].say('{"kind":"turn_started","seq":1}');
    opened[0].say('{"kind":"turn_done","seq":2}');
    vi.advanceTimersByTime(STREAM_SILENCE_MS + 5_000);
    expect(opened[1].url).toBe("/events?lastEventId=2");
    opened[1].say('{"kind":"turn_done","seq":2}');
    opened[1].say('{"kind":"turn_started","seq":3}');
    expect(seen).toEqual([1, 2, 3]);
    stop();
  });
});
