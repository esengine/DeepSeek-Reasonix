// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import "../testkit";
import { FrameView, type FrameHost } from "./FrameView";
import { FrameState } from "./state";
import type { ExtensionSurface } from "../../port/wire";

class ReviewPort {
  onmessage: ((event: { data: unknown }) => void) | null = null;
  sent: unknown[] = [];
  closed = false;
  postMessage(data: unknown) { this.sent.push(structuredClone(data)); }
  close() { this.closed = true; }
  deliver(data: unknown) { if (!this.closed) this.onmessage?.({ data: structuredClone(data) }); }
}

let ports: ReviewPort[];
let fetchMock: ReturnType<typeof vi.fn>;
beforeEach(() => {
  ports = [];
  fetchMock = vi.fn(async () => new Response("<p>fixture</p>"));
  vi.stubGlobal("fetch", fetchMock);
  vi.stubGlobal("MessageChannel", class {
    port1 = new ReviewPort();
    port2 = new ReviewPort();
    constructor() { ports.push(this.port1); }
  });
});
afterEach(() => { cleanup(); vi.restoreAllMocks(); vi.unstubAllGlobals(); });

const surface = (sessionId: string, generation: number, pluginId = "review", props = {}): ExtensionSurface => ({
  pluginId, sessionId, generation, surfaceId: "board", kind: "frame",
  frame: { frame: "board", title: "Review board", fallback: "Fallback", props },
});
const host = (runtime: string): FrameHost => ({
  state: new FrameState(),
  url: (ext) => `/rt/${runtime}/extensions/frames/${ext.pluginId}/board?generation=${ext.generation}`,
  act: vi.fn(), submit: vi.fn(),
});
async function connect() {
  const iframe = await waitFor(() => {
    const node = document.querySelector("iframe");
    if (!node) throw new Error("Waiting for frame preflight");
    return node;
  });
  vi.spyOn(iframe.contentWindow!, "postMessage").mockImplementation(() => {});
  fireEvent.load(iframe);
  return ports.at(-1)!;
}
const send = (port: ReviewPort, data: unknown) => act(() => port.deliver(data));

it("does not restore private frame state across independent runtime hosts", async () => {
  const one = render(<FrameView ext={surface("boot-101", 101)} slot="pane-tab" host={host("runtime-a")} />);
  const a = await connect();
  send(a, { type: "ready" });
  send(a, { type: "state", value: { privateDraft: "runtime-a-only" } });
  one.unmount();
  render(<FrameView ext={surface("boot-101", 101)} slot="pane-tab" host={host("runtime-b")} />);
  const b = await connect();
  send(b, { type: "ready" });
  expect(b.sent.at(-1)).not.toHaveProperty("state");
  expect(b.sent.at(-1)).toMatchObject({ reason: "publish" });
});

it("does not send a replacement plugin's props to the preceding document channel", async () => {
  const h = host("r");
  const view = render(<FrameView ext={surface("s", 102, "plugin-a")} slot="pane-tab" host={h} />);
  const old = await connect();
  send(old, { type: "ready" });
  old.sent.length = 0;
  fetchMock.mockImplementationOnce(() => new Promise(() => {}));
  view.rerender(<FrameView ext={surface("s", 102, "plugin-b", { privateDraft: "plugin-b-only" })} slot="pane-tab" host={h} />);
  expect(old.sent).toEqual([]);
  expect(old.closed).toBe(true);
});

it("does not dispatch a queued old-document action to a replacement plugin", async () => {
  const a = host("r"), b = { ...a, act: vi.fn() };
  const view = render(<FrameView ext={surface("s", 103, "plugin-a")} slot="pane-tab" host={a} />);
  const old = await connect();
  send(old, { type: "ready" });
  const queued = old.onmessage!;
  fetchMock.mockImplementationOnce(() => new Promise(() => {}));
  view.rerender(<FrameView ext={surface("s", 103, "plugin-b")} slot="pane-tab" host={b} />);
  send(old, { type: "action", actionId: "delete" });
  act(() => queued({ data: { type: "action", actionId: "delete" } }));
  expect(b.act).not.toHaveBeenCalled();
});

it("rejects cyclic structured-clone messages without throwing in the host", async () => {
  render(<FrameView ext={surface("s", 104)} slot="pane-tab" host={host("r")} />);
  const port = await connect();
  send(port, { type: "ready" });
  const cycle: Record<string, unknown> = {};
  cycle.self = cycle;
  expect(() => send(port, { type: "state", value: cycle })).not.toThrow();
  send(port, { type: "submit", values: { number: 1n } });
  send(port, { type: "state", value: new Map([["key", "value"]]) });
  expect(port.closed).toBe(true);
});

it("limits duplicate ready messages instead of repeatedly resending initialization", async () => {
  render(<FrameView ext={surface("s", 105)} slot="pane-tab" host={host("r")} />);
  const port = await connect();
  vi.spyOn(Date, "now").mockReturnValue(1000);
  for (let n = 0; n < 100; n++) send(port, { type: "ready" });
  expect(port.sent.filter((message) => (message as { type: string }).type === "init")).toHaveLength(1);
  expect(port.closed).toBe(true);
});

it("refuses actions before ready and still permits a subsequent valid handshake", async () => {
  const h = host("r");
  render(<FrameView ext={surface("s", 106)} slot="pane-tab" host={h} />);
  const port = await connect();
  send(port, { type: "action", actionId: "delete" });
  expect(h.act).not.toHaveBeenCalled();
  send(port, { type: "ready" });
  send(port, { type: "action", actionId: "refresh" });
  expect(h.act).toHaveBeenCalledExactlyOnceWith(expect.objectContaining({ pluginId: "review" }), "refresh", undefined);
});
