// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, fireEvent, render, screen } from "@testing-library/react";
import { useState } from "react";
import "./testkit";
import { Pane } from "./Pane";
import { MockPort } from "../port/mock";
import type { RuntimeView } from "../port/hub";
import { useWindowKeys } from "./windowkeys";

afterEach(cleanup);

function open(active = true) {
  const port = new MockPort();
  const width = vi.fn();
  const close = vi.fn();
  function Fixture() {
    const [browser, setBrowser] = useState(true);
    useWindowKeys([], { browser, settings: false, running: false, closeBrowser: () => { close(); setBrowser(false); }, stop: vi.fn() });
    return <Pane port={port} rt={{ id: "wide", root: "/fixture", name: "Fixture" } as RuntimeView}
      title="Fixture" active={active} visible={active} sideHost={null} side={false}
      onFocus={() => {}} onReport={() => {}} onSessionChanged={() => {}} pulse={0} findPulse={0}
      onSettings={() => {}} needsProject={false} onOpenProject={() => {}} onKeepHere={() => {}}
      theme="light" dockW={560} dockMax={880} onDockW={width}
      manualBrowser={browser} onManualBrowser={setBrowser} />;
  }
  return { ...render(<Fixture />), width, close };
}

describe("browser wide mode", () => {
  it("keeps the page and conversation mounted and leaves the saved split width alone", async () => {
    const { container, width } = open();
    const frame = await screen.findByTitle("Reasonix 内置 Browser");
    const composer = container.querySelector(".compose");
    const button = screen.getByRole("button", { name: "加宽浏览器" });
    expect(button.getAttribute("aria-pressed")).toBe("false");
    fireEvent.click(button);
    expect(container.querySelector(".pane")?.getAttribute("data-browser-size")).toBe("wide");
    expect(screen.getByRole("button", { name: "恢复浏览器分栏" }).getAttribute("aria-pressed")).toBe("true");
    expect(screen.getByTitle("Reasonix 内置 Browser")).toBe(frame);
    expect(container.querySelector(".compose")).toBe(composer);
    expect(composer?.hasAttribute("hidden")).toBe(false);
    expect(container.querySelector(".pbody")?.hasAttribute("data-full")).toBe(false);
    fireEvent.click(screen.getByRole("button", { name: "恢复浏览器分栏" }));
    expect(container.querySelector(".pane")?.getAttribute("data-browser-size")).toBe("split");
    expect(screen.getByTitle("Reasonix 内置 Browser")).toBe(frame);
    expect(width).not.toHaveBeenCalled();
  });

  it("toggles with Ctrl+Shift+B and spends Escape leaving wide mode before the browser closes", async () => {
    const { container, close } = open();
    await screen.findByTitle("Reasonix 内置 Browser");
    fireEvent.keyDown(document.body, { key: "B", ctrlKey: true, shiftKey: true });
    expect(container.querySelector(".pane")?.getAttribute("data-browser-size")).toBe("wide");
    fireEvent.keyDown(document.body, { key: "Escape" });
    expect(container.querySelector(".pane")?.getAttribute("data-browser-size")).toBe("split");
    expect(close).not.toHaveBeenCalled();
    fireEvent.keyDown(document.body, { key: "B", ctrlKey: true, shiftKey: true });
    fireEvent.keyDown(document.body, { key: "B", ctrlKey: true, shiftKey: true, repeat: true });
    expect(container.querySelector(".pane")?.getAttribute("data-browser-size")).toBe("wide");
    fireEvent.keyDown(document.body, { key: "B", ctrlKey: true, shiftKey: true });
    expect(container.querySelector(".pane")?.getAttribute("data-browser-size")).toBe("split");
    fireEvent.keyDown(document.body, { key: "Escape" });
    expect(close).toHaveBeenCalledOnce();
  });

  it("ignores shortcuts on an inactive pane and keys already handled by a field", async () => {
    const hidden = open(false);
    fireEvent.keyDown(document.body, { key: "B", ctrlKey: true, shiftKey: true });
    expect(hidden.container.querySelector(".pane")?.getAttribute("data-browser-size")).toBe("split");
    cleanup();
    const { container } = open();
    await screen.findByTitle("Reasonix 内置 Browser");
    const input = document.createElement("input");
    input.addEventListener("keydown", e => e.preventDefault());
    document.body.append(input);
    fireEvent.keyDown(input, { key: "B", ctrlKey: true, shiftKey: true });
    expect(container.querySelector(".pane")?.getAttribute("data-browser-size")).toBe("split");
    input.remove();
  });

  it("resets to split after closing the dock without replacing the page", async () => {
    open();
    const frame = await screen.findByTitle("Reasonix 内置 Browser");
    fireEvent.click(screen.getByRole("button", { name: "加宽浏览器" }));
    fireEvent.click(screen.getByRole("tab", { name: "对话" }));
    fireEvent.click(screen.getByRole("tab", { name: /^工作台/ }));
    expect(screen.getByRole("button", { name: "加宽浏览器" }).getAttribute("aria-pressed")).toBe("false");
    expect(screen.getByTitle("Reasonix 内置 Browser")).toBe(frame);
  });

  it("gives a transient layer first use of Escape", async () => {
    const { container, close } = open();
    await screen.findByTitle("Reasonix 内置 Browser");
    fireEvent.click(screen.getByRole("button", { name: "加宽浏览器" }));
    const dismiss = (e: KeyboardEvent) => e.stopPropagation();
    window.addEventListener("keydown", dismiss, true);
    try {
      fireEvent.keyDown(document.body, { key: "Escape" });
      expect(container.querySelector(".pane")?.getAttribute("data-browser-size")).toBe("wide");
      expect(close).not.toHaveBeenCalled();
    } finally {
      window.removeEventListener("keydown", dismiss, true);
    }
  });

  it("does not treat IME composition or a plain Ctrl+B as the wide shortcut", async () => {
    const { container } = open();
    await screen.findByTitle("Reasonix 内置 Browser");
    act(() => document.body.dispatchEvent(new KeyboardEvent("keydown", { key: "B", ctrlKey: true, shiftKey: true, isComposing: true, bubbles: true })));
    fireEvent.keyDown(document.body, { key: "b", ctrlKey: true });
    expect(container.querySelector(".pane")?.getAttribute("data-browser-size")).toBe("split");
  });
});
