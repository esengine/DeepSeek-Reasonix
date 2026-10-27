// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import "./testkit";
import { Pane } from "./Pane";
import { MockPort } from "../port/mock";
import type { ExtensionSurface, WireEvent } from "../port/wire";

afterEach(() => {
  cleanup();
  document.querySelectorAll("[data-test-header]").forEach((el) => el.remove());
});

const header = () => document.querySelector<HTMLElement>("[data-test-header]")!;

async function mount({ active = true, slots = {} as Record<string, string> } = {}) {
  const port = new MockPort();
  vi.spyOn(port, "history").mockResolvedValue([]);
  vi.spyOn(port, "surfaceSlots").mockResolvedValue(slots);
  const assign = vi.spyOn(port, "assignSurface").mockResolvedValue();
  const invoke = vi.spyOn(port, "invokeExtensionAction").mockResolvedValue("Review action completed");
  let emit: (ev: WireEvent) => void = () => {};
  vi.spyOn(port, "subscribe").mockImplementation((onEvent) => {
    emit = onEvent;
    return () => true;
  });
  vi.spyOn(port, "extensionFrameUrl").mockImplementation((plugin, frame, generation) => `/rt/r1/extensions/frames/${plugin}/${frame}?generation=${generation}`);
  const host = document.createElement("span");
  host.dataset.testHeader = "";
  document.body.append(host);
  render(<Pane port={port} rt={{ id: "r1", base: "/rt/r1", root: "/w", name: "w" }}
    title="w" active={active} visible sideHost={null} side={false} headerHost={active ? host : null} onFocus={() => {}} onReport={() => {}}
    onSessionChanged={() => {}} pulse={0} findPulse={0} onSettings={() => {}} needsProject={false}
    onOpenProject={() => {}} onKeepHere={() => {}} theme="light" dockW={320} dockMax={640} onDockW={() => {}} />);
  await act(async () => {});
  const publish = (extension: ExtensionSurface) => act(() => emit({ kind: "extension_surface", extension }));
  return { assign, invoke, publish };
}

const view = (surfaceId: string, slot: string | undefined, value: string): ExtensionSurface => ({
  pluginId: "p", surfaceId, sessionId: "s1", generation: 1, kind: "view",
  view: { slot, body: [{ kind: "kv", key: surfaceId, value }] },
});
const tabs = () => [...document.querySelectorAll("[role=tab]")].map((e) => e.textContent?.trim());

describe("a pane's extension views in every place this window has", () => {
  it.each(["pane.tab", "rail", "session.header", "composer.above"])("preserves action feedback in %s", async (slot) => {
    const { publish, invoke } = await mount();
    publish({ pluginId: "review", surfaceId: "action", kind: "view", view: {
      slot, body: [{ kind: "button", label: "Run review action", actionId: "/review:run" }],
    } });
    const button = await screen.findByRole("button", { name: "Run review action" });
    await act(async () => { fireEvent.click(button); });
    expect(invoke).toHaveBeenCalledWith("/review:run");
    expect(screen.queryByText("Review action completed")).not.toBeNull();
  });

  it("draws each view where it asked to stand", async () => {
    const { publish } = await mount();
    publish(view("hdr", "session.header", "62%"));
    publish(view("cmp", "composer.above", "on"));
    publish(view("tab", "pane.tab", "board"));
    publish(view("rail", undefined, "idle"));
    expect(await within(header(), "hdr: 62%")).toBeTruthy();
    expect(document.querySelector(".slotrail")?.textContent).toContain("cmp");
    expect(tabs()).toEqual(["p", "扩展"]);
  });

  it.each(["pane.tab", "rail", "session.header", "composer.above"])("shows a refused action in %s", async (slot) => {
    const { publish, invoke } = await mount();
    invoke.mockRejectedValueOnce(new Error("Review action refused"));
    publish({ pluginId: "review", surfaceId: "action", kind: "view", view: {
      slot, body: [{ kind: "button", label: "Run review action", actionId: "/review:run" }],
    } });
    const button = await screen.findByRole("button", { name: "Run review action" });
    await act(async () => { fireEvent.click(button); });
    expect(invoke).toHaveBeenCalledWith("/review:run");
    expect(screen.queryByText("Review action refused")).not.toBeNull();
  });

  it("puts nothing in the header of a pane that is not the one in front", async () => {
    const { publish } = await mount({ active: false });
    publish(view("hdr", "session.header", "62%"));
    await act(async () => {});
    expect(header().textContent).toBe("");
  });

  it("follows the user's stored placement over the extension's ask", async () => {
    const { publish } = await mount({ slots: { "p:hdr": "composer-trailing" } });
    publish(view("hdr", "session.header", "62%"));
    await waitFor(() => expect(document.querySelector(".slotrail")?.textContent).toContain("hdr"));
    expect(header().textContent).toBe("");
  });

  it("moves a view at once and records the move", async () => {
    const { publish, assign } = await mount();
    publish(view("hdr", "session.header", "62%"));
    await within(header(), "hdr: 62%");
    fireEvent.click(screen.getByRole("button", { name: "移动 p 的界面" }));
    fireEvent.click(screen.getByRole("menuitem", { name: "输入框上方" }));
    expect(document.querySelector(".slotrail")?.textContent).toContain("hdr");
    expect(header().textContent).toBe("");
    expect(assign).toHaveBeenCalledExactlyOnceWith("p:hdr", "composer-trailing");
  });

  it("updates a republished view in place rather than drawing it again", async () => {
    const { publish } = await mount();
    publish(view("hdr", "session.header", "62%"));
    await within(header(), "hdr: 62%");
    const drawn = header().querySelector(".slotted");
    publish(view("hdr", "session.header", "63%"));
    await within(header(), "hdr: 63%");
    expect(header().querySelector(".slotted")).toBe(drawn);
  });
});

async function within(root: HTMLElement, text: string) {
  await waitFor(() => expect(root.textContent).toContain(text));
  return true;
}

describe("a frame publication in a pane", () => {
  it("opens as its own workbench tab, loads its document, and its actions reach only its plugin", async () => {
    vi.stubGlobal("fetch", vi.fn(async () => new Response("<p>ok</p>", { status: 200 })));
    try {
      const { publish, invoke } = await mount();
      publish({
        pluginId: "board", surfaceId: "kanban", sessionId: "s1", generation: 3, kind: "frame",
        frame: { frame: "board", slot: "pane.tab", title: "Task board", fallback: "3 cards", props: { n: 3 } },
      });
      await waitFor(() => expect(tabs()).toContain("Task board"));
      const iframe = await waitFor(() => {
        const el = document.querySelector<HTMLIFrameElement>(".workbench iframe");
        if (!el) throw new Error("no frame yet");
        return el;
      });
      expect(iframe.getAttribute("src")).toBe("/rt/r1/extensions/frames/board/board?generation=3");
      const posted = vi.spyOn(iframe.contentWindow!, "postMessage");
      fireEvent.load(iframe);
      const port = (posted.mock.calls.at(-1) as unknown as [unknown, string, MessagePort[]])[2][0];
      await act(async () => {
        port.postMessage({ type: "ready" });
        port.postMessage({ type: "action", actionId: "refresh", args: { all: "1" } });
        await new Promise((r) => setTimeout(r, 0));
      });
      expect(invoke).toHaveBeenCalledExactlyOnceWith("/board:refresh", { all: "1" });
    } finally {
      vi.unstubAllGlobals();
    }
  });
});
