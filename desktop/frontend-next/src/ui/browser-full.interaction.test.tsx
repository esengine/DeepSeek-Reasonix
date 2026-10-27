// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { useState } from "react";
import "./testkit";
import { Pane } from "./Pane";
import { ViewSwitch } from "./ViewSwitch";
import type { PaneView } from "../state/paneview";
import { MockPort } from "../port/mock";
import type { RuntimeView } from "../port/hub";
import { useWindowKeys } from "./windowkeys";
import type { WireEvent } from "../port/wire";

afterEach(cleanup);

function open(active = true, setup?: (port: MockPort) => void) {
  const port = new MockPort();
  setup?.(port);
  const width = vi.fn();
  const close = vi.fn();
  function Fixture() {
    const [browser, setBrowser] = useState(true);
    const [req, setReq] = useState<{ to: PaneView; n: number } | undefined>();
    useWindowKeys([], { browser, settings: false, running: false, confirm: false, closeBrowser: () => { close(); setBrowser(false); }, stop: vi.fn(), onArm: vi.fn() });
    return <><ViewSwitch view={browser ? "browser" : "flow"} rows={0} surfaces={0} onView={(to) => setReq((r) => ({ to, n: (r?.n ?? 0) + 1 }))} /><Pane port={port} rt={{ id: "wide", root: "/fixture", name: "Fixture" } as RuntimeView}
      title="Fixture" active={active} visible={active} sideHost={null} side={false}
      onFocus={() => {}} onReport={() => {}} onSessionChanged={() => {}} pulse={0} findPulse={0}
      onSettings={() => {}} needsProject={false} onOpenProject={() => {}} onKeepHere={() => {}}
      theme="light" dockW={560} dockMax={880} onDockW={width}
      manualBrowser={browser} onManualBrowser={setBrowser} viewRequest={req} /></>;
  }
  const view = render(<Fixture />);
  const launcher = view.container.querySelector<HTMLElement>('[data-action="workbench.launch-browser"]');
  if (launcher) fireEvent.click(launcher);
  return { ...view, width, close };
}

describe("workbench sizing", () => {
  it("keeps divider resizing available when the retired widening shortcut is pressed", async () => {
    const { container, width } = open();
    await screen.findByLabelText("网页地址");
    const event = new KeyboardEvent("keydown", { key: "B", ctrlKey: true, shiftKey: true, bubbles: true, cancelable: true });
    act(() => { document.body.dispatchEvent(event); });
    expect(event.defaultPrevented).toBe(false);
    expect(container.querySelector(".pane")?.getAttribute("data-browser-size")).toBe("split");
    const divider = screen.getByRole("separator", { name: "调整浏览器宽度" });
    fireEvent.keyDown(divider, { key: "ArrowLeft", shiftKey: true });
    expect(width).toHaveBeenCalledWith(608);
    expect(screen.queryByRole("button", { name: "加宽工作台" })).toBeNull();
  });

  it("ignores full-view shortcuts on an inactive pane and keys already handled by a field", async () => {
    const hidden = open(false);
    fireEvent.keyDown(document.body, { key: "F", ctrlKey: true, shiftKey: true });
    expect(hidden.container.querySelector(".pane")?.getAttribute("data-browser-size")).toBe("split");
    cleanup();
    const { container } = open();
    await screen.findByLabelText("网页地址");
    const input = document.createElement("input");
    input.addEventListener("keydown", e => e.preventDefault());
    document.body.append(input);
    fireEvent.keyDown(input, { key: "F", ctrlKey: true, shiftKey: true });
    expect(container.querySelector(".pane")?.getAttribute("data-browser-size")).toBe("split");
    input.remove();
  });

  it("resets to split after closing the dock without replacing the page", async () => {
    open();
    const frame = await screen.findByLabelText("网页地址");
    fireEvent.click(screen.getByRole("button", { name: "全屏工作台" }));
    fireEvent.click(screen.getByRole("button", { name: "工作台" }));
    fireEvent.click(screen.getByRole("button", { name: "工作台" }));
    expect(screen.getByRole("button", { name: "全屏工作台" }).getAttribute("aria-pressed")).toBe("false");
    expect(screen.getByLabelText("网页地址")).toBe(frame);
  });

  it("gives a transient layer first use of Escape", async () => {
    const { container, close } = open();
    await screen.findByLabelText("网页地址");
    fireEvent.click(screen.getByRole("button", { name: "全屏工作台" }));
    const dismiss = (e: KeyboardEvent) => e.stopPropagation();
    window.addEventListener("keydown", dismiss, true);
    try {
      fireEvent.keyDown(document.body, { key: "Escape" });
      expect(container.querySelector(".pane")?.getAttribute("data-browser-size")).toBe("full");
      expect(close).not.toHaveBeenCalled();
    } finally {
      window.removeEventListener("keydown", dismiss, true);
    }
  });

  it("does not treat IME composition or a plain Ctrl+F as the full-view shortcut", async () => {
    const { container } = open();
    await screen.findByLabelText("网页地址");
    act(() => document.body.dispatchEvent(new KeyboardEvent("keydown", { key: "F", ctrlKey: true, shiftKey: true, isComposing: true, bubbles: true })));
    fireEvent.keyDown(document.body, { key: "f", ctrlKey: true });
    expect(container.querySelector(".pane")?.getAttribute("data-browser-size")).toBe("split");
  });

  it("shows the page alone in full mode, keeps the conversation mounted, and Escape returns to the split", async () => {
    const { container, close, width } = open();
    const frame = await screen.findByLabelText("网页地址");
    const composer = container.querySelector(".compose");
    fireEvent.click(screen.getByRole("button", { name: "全屏工作台" }));
    expect(container.querySelector(".pane")?.getAttribute("data-browser-size")).toBe("full");
    expect(screen.getByRole("button", { name: "退出工作台全屏" }).getAttribute("aria-pressed")).toBe("true");
    expect(screen.getByLabelText("网页地址")).toBe(frame);
    expect(container.querySelector(".compose")).toBe(composer);
    fireEvent.keyDown(document.body, { key: "Escape" });
    expect(container.querySelector(".pane")?.getAttribute("data-browser-size")).toBe("split");
    expect(close).not.toHaveBeenCalled();
    expect(width).not.toHaveBeenCalled();
    expect(screen.getByRole("separator", { name: "调整浏览器宽度" })).toBeTruthy();
    fireEvent.keyDown(document.body, { key: "Escape" });
    expect(close).toHaveBeenCalledOnce();
  });

  it("toggles full mode with Ctrl+Shift+F without reacting to the retired widening shortcut or repeats", async () => {
    const { container } = open();
    await screen.findByLabelText("网页地址");
    fireEvent.keyDown(document.body, { key: "F", ctrlKey: true, shiftKey: true });
    expect(container.querySelector(".pane")?.getAttribute("data-browser-size")).toBe("full");
    fireEvent.keyDown(document.body, { key: "B", ctrlKey: true, shiftKey: true });
    expect(container.querySelector(".pane")?.getAttribute("data-browser-size")).toBe("full");
    fireEvent.keyDown(document.body, { key: "F", ctrlKey: true, shiftKey: true, repeat: true });
    expect(container.querySelector(".pane")?.getAttribute("data-browser-size")).toBe("full");
    fireEvent.keyDown(document.body, { key: "F", ctrlKey: true, shiftKey: true });
    expect(container.querySelector(".pane")?.getAttribute("data-browser-size")).toBe("split");
  });

  it("leaves full mode when the turn starts waiting on the person, so the card is not hidden", async () => {
    let deliver: ((event: WireEvent) => void) | undefined;
    let waiting = false;
    const { container } = open(true, (port) => {
      const read = port.status.bind(port);
      vi.spyOn(port, "status").mockImplementation(async () => ({ ...(await read()), decisions: waiting ? [{ id: "ask-1", kind: "ask" as const }] : [] }));
      vi.spyOn(port, "subscribe").mockImplementation((onEvent) => { deliver = onEvent; return () => true; });
    });
    await screen.findByLabelText("网页地址");
    fireEvent.click(screen.getByRole("button", { name: "全屏工作台" }));
    expect(container.querySelector(".pane")?.getAttribute("data-browser-size")).toBe("full");
    waiting = true;
    await act(async () => { deliver?.({ kind: "ask_request" } as WireEvent); });
    await waitFor(() => expect(container.querySelector(".pane")?.getAttribute("data-browser-size")).toBe("split"));
  });
});
