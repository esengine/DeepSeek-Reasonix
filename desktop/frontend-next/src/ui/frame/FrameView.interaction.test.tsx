// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import "../testkit";
import { FrameView, type FrameHost } from "./FrameView";
import { FrameState } from "./state";
import type { ExtensionSurface } from "../../port/wire";

const frame = (props: Record<string, unknown> = { cards: 3 }, generation = 4): ExtensionSurface => ({
  pluginId: "board", surfaceId: "kanban", sessionId: "s1", generation, kind: "frame",
  frame: { frame: "board", title: "Task board", fallback: "3 cards in progress", props },
});

function host() {
  return {
    state: new FrameState(),
    url: (ext: ExtensionSurface) => `/rt/r1/extensions/frames/${ext.pluginId}/${ext.frame!.frame}?generation=${ext.generation}`,
    act: vi.fn<FrameHost["act"]>(),
    submit: vi.fn<FrameHost["submit"]>(),
  };
}

let fetchMock: ReturnType<typeof vi.fn>;
beforeEach(() => {
  fetchMock = vi.fn(async () => new Response("<p>ok</p>", { status: 200 }));
  vi.stubGlobal("fetch", fetchMock);
});
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
  delete document.documentElement.dataset.theme;
});

/** Loads the iframe and takes the far end of the channel the host hands it. */
async function connect() {
  const iframe = await waitFor(() => {
    const el = document.querySelector<HTMLIFrameElement>("iframe");
    if (!el) throw new Error("no iframe yet");
    return el;
  });
  const posted = vi.spyOn(iframe.contentWindow!, "postMessage");
  fireEvent.load(iframe);
  const [, , transfer] = posted.mock.calls.at(-1) as unknown as [unknown, string, MessagePort[]];
  const port = transfer[0];
  const received: unknown[] = [];
  port.onmessage = (e) => received.push(e.data);
  const say = async (data: unknown) => {
    await act(async () => {
      port.postMessage(data);
      await new Promise((r) => setTimeout(r, 0));
    });
  };
  return { iframe, posted, port, received, say };
}

describe("a frame document in Studio", () => {
  it("is loaded from its route into an opaque sandbox with no permissions, under the plugin's name", async () => {
    render(<FrameView ext={frame()} slot="pane-tab" host={host()} />);
    const { iframe, posted } = await connect();
    expect(iframe.getAttribute("src")).toBe("/rt/r1/extensions/frames/board/board?generation=4");
    expect(iframe.getAttribute("sandbox")).toBe("allow-scripts");
    expect(iframe.getAttribute("allow")).toBe("");
    expect(iframe.getAttribute("referrerpolicy")).toBe("no-referrer");
    expect(posted.mock.calls.at(-1)?.[0]).toEqual({ type: "connect", protocol: "reasonix.frame.v1" });
    expect(screen.getByText("Task board")).toBeTruthy();
    expect(screen.getByText(/扩展内容 · board/)).toBeTruthy();
  });

  it("answers ready with the props, the theme and no write access lost", async () => {
    render(<FrameView ext={frame()} slot="pane-tab" host={host()} />);
    const { say, received } = await connect();
    await say({ type: "ready" });
    const init = received[0] as Record<string, unknown>;
    expect(init).toMatchObject({ type: "init", protocol: "reasonix.frame.v1", props: { cards: 3 }, readOnly: false, reason: "publish" });
    expect(init.theme).toHaveProperty("tokens");
  });

  it("sends a declared action and a submission to its own plugin, and nothing that is not one", async () => {
    const h = host();
    render(<FrameView ext={frame()} slot="pane-tab" host={h} />);
    const { say } = await connect();
    await say({ type: "ready" });
    await say({ type: "action", actionId: "move", args: { card: "a" } });
    await say({ type: "submit", values: { title: "x" } });
    await say({ type: "approve", id: "p1" });
    expect(h.act).toHaveBeenCalledExactlyOnceWith(expect.objectContaining({ pluginId: "board" }), "move", { card: "a" });
    expect(h.submit).toHaveBeenCalledExactlyOnceWith(expect.objectContaining({ surfaceId: "kanban" }), { title: "x" });
  });

  it("closes the channel and shows the fallback after three refused messages", async () => {
    const h = host();
    render(<FrameView ext={frame()} slot="pane-tab" host={h} />);
    const { say } = await connect();
    for (const bad of [{ type: "x" }, { type: "y" }, { type: "z" }]) await say(bad);
    expect(await screen.findByText("3 cards in progress")).toBeTruthy();
    expect(document.querySelector("iframe")).toBeNull();
    await say({ type: "action", actionId: "late" });
    expect(h.act).not.toHaveBeenCalled();
  });

  it("hands new props and a new theme to the same document instead of reloading it", async () => {
    const h = host();
    const view = render(<FrameView ext={frame({ cards: 3 })} slot="pane-tab" host={h} />);
    const { say, received, iframe, posted } = await connect();
    await say({ type: "ready" });
    const connects = posted.mock.calls.length;
    view.rerender(<FrameView ext={frame({ cards: 4 })} slot="pane-tab" host={h} />);
    await act(async () => { await new Promise((r) => setTimeout(r, 0)); });
    expect(received.at(-1)).toEqual({ type: "props", props: { cards: 4 } });
    await act(async () => {
      document.documentElement.dataset.theme = "dark";
      await new Promise((r) => setTimeout(r, 0));
    });
    expect((received.at(-1) as { type: string }).type).toBe("theme");
    expect(document.querySelector("iframe")).toBe(iframe);
    expect(posted.mock.calls.length).toBe(connects);
  });

  it("keeps the frame's height inside its place", async () => {
    render(<FrameView ext={frame()} slot="sidebar" host={host()} />);
    const { say, iframe } = await connect();
    await say({ type: "ready" });
    await say({ type: "size", height: 4000 });
    expect(iframe.style.height).toBe("640px");
  });

  it("says why a document could not be loaded, and loads it again on retry", async () => {
    fetchMock.mockResolvedValueOnce(new Response(JSON.stringify({ code: "ui.frame_stale" }), { status: 409 }));
    render(<FrameView ext={frame()} slot="pane-tab" host={host()} />);
    expect(await screen.findByText("这个扩展界面属于已重载的运行时，请等扩展重新发布")).toBeTruthy();
    expect(screen.getByText("3 cards in progress")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "重试" }));
    await waitFor(() => expect(document.querySelector("iframe")).not.toBeNull());
  });

  it("gives a remounted document its saved state back and says it is a remount", async () => {
    const h = host();
    const first = render(<FrameView ext={frame({}, 9)} slot="pane-tab" host={h} />);
    const one = await connect();
    await one.say({ type: "ready" });
    await one.say({ type: "state", value: { tab: 2 } });
    first.unmount();
    render(<FrameView ext={frame({}, 9)} slot="sidebar" host={h} />);
    const two = await connect();
    await two.say({ type: "ready" });
    expect(two.received[0]).toMatchObject({ type: "init", reason: "remount", state: { tab: 2 } });
  });
});
